package taskcommon

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTables(t *testing.T, tables map[string]video_billing.ModelPriceTable) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = tables
	t.Cleanup(func() { settings.PriceTables = original })
}

func matrixTask(modelPrice, groupRatio float64, perCall bool) *model.Task {
	task := &model.Task{}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      modelPrice,
		GroupRatio:      groupRatio,
		OriginModelName: "seedance-matrix",
		PerCallBilling:  perCall,
	}
	return task
}

// 结算契约：quota = tokens/1M × 快照档价 × QuotaPerUnit × 快照分组倍率。
func TestVideoMatrixQuotaOnComplete(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:  video_billing.UnitPerMillionToken,
			Tiers: []video_billing.PriceTier{{Price: 6.3}},
		},
	})

	result := &relaycommon.TaskInfo{TotalTokens: 432000}
	quota := VideoMatrixQuotaOnComplete(matrixTask(6.3, 0.85, false), result)
	expected := common.QuotaFromFloat(432000.0 / 1_000_000 * 6.3 * common.QuotaPerUnit * 0.85)
	require.Positive(t, quota)
	assert.Equal(t, expected, quota)
}

func TestVideoMatrixQuotaOnCompletePerSecondUsesActualDuration(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:  video_billing.UnitPerSecond,
			Tiers: []video_billing.PriceTier{{Price: 0.3}},
		},
	})
	task := matrixTask(0.3, 0.85, true)
	task.PrivateData.BillingContext.SettleOnComplete = true
	result := &relaycommon.TaskInfo{Duration: 7}

	quota := VideoMatrixQuotaOnComplete(task, result)

	expected := common.QuotaFromFloat(7 * 0.3 * common.QuotaPerUnit * 0.85)
	require.Positive(t, quota)
	assert.Equal(t, expected, quota)
	assert.Nil(t, result.QuotaClamp)
}

func TestVideoMatrixQuotaOnCompletePerSecondUsesFractionalBillingDuration(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:  video_billing.UnitPerSecond,
			Tiers: []video_billing.PriceTier{{Price: 0.9}},
		},
	})
	task := matrixTask(0.9, 0.85, true)
	task.PrivateData.BillingContext.SettleOnComplete = true
	result := &relaycommon.TaskInfo{Duration: 14, BillingDuration: 13.24}

	quota := VideoMatrixQuotaOnComplete(task, result)

	expected := common.QuotaFromFloat(13.24 * 0.9 * common.QuotaPerUnit * 0.85)
	require.Positive(t, quota)
	assert.Equal(t, expected, quota)
}

func TestVideoMatrixQuotaOnCompleteNotApplicable(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:  video_billing.UnitPerMillionToken,
			Tiers: []video_billing.PriceTier{{Price: 6.3}},
		},
		"per-second-model": {
			Unit:  video_billing.UnitPerSecond,
			Tiers: []video_billing.PriceTier{{Price: 0.3}},
		},
	})
	result := &relaycommon.TaskInfo{TotalTokens: 1000}

	// 按次计费（每秒模型）不走 token 结算
	assert.Zero(t, VideoMatrixQuotaOnComplete(matrixTask(0.3, 1, true), result))

	// 无 BillingContext
	assert.Zero(t, VideoMatrixQuotaOnComplete(&model.Task{}, result))

	// 无 usage
	assert.Zero(t, VideoMatrixQuotaOnComplete(matrixTask(6.3, 1, false), &relaycommon.TaskInfo{}))

	// 模型未配置矩阵（提交后被删除）→ 回退默认结算
	noMatrix := matrixTask(6.3, 1, false)
	noMatrix.PrivateData.BillingContext.OriginModelName = "unknown-model"
	assert.Zero(t, VideoMatrixQuotaOnComplete(noMatrix, result))

	// 每秒单位的矩阵模型即便带 usage 也不按 token 结算
	perSecond := matrixTask(0.3, 1, false)
	perSecond.PrivateData.BillingContext.OriginModelName = "per-second-model"
	assert.Zero(t, VideoMatrixQuotaOnComplete(perSecond, result))
}

// 参考图是独立于输出秒数的上游成本：百炼对超出免费额度的输入图按张计费，
// 只按秒结算会把这部分白送。上游返回的张数已扣除免费额度，不再重复减免。
func TestVideoMatrixQuotaOnCompletePerSecondChargesBillableInputImages(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:            video_billing.UnitPerSecond,
			Tiers:           []video_billing.PriceTier{{Price: 0.2}},
			InputImagePrice: 0.05,
		},
	})

	task := matrixTask(0.2, 1, true)
	task.PrivateData.BillingContext.SettleOnComplete = true
	result := &relaycommon.TaskInfo{BillingDuration: 5, BillableImageCount: 4}
	quota := VideoMatrixQuotaOnComplete(task, result)
	expected := common.QuotaFromFloat((5*0.2 + 4*0.05) * common.QuotaPerUnit * 1)
	assert.Equal(t, expected, quota)

	// 没有可计费图片时结果与旧行为一致。
	plainTask := matrixTask(0.2, 1, true)
	plainTask.PrivateData.BillingContext.SettleOnComplete = true
	noImages := &relaycommon.TaskInfo{BillingDuration: 5}
	assert.Equal(t,
		common.QuotaFromFloat(5*0.2*common.QuotaPerUnit*1),
		VideoMatrixQuotaOnComplete(plainTask, noImages))
}

// 张数是计费乘数，上游返回异常大的值时必须钳制，避免一次结算放大到不可接受的额度。
func TestVideoMatrixQuotaOnCompleteClampsAbsurdImageCount(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:            video_billing.UnitPerSecond,
			Tiers:           []video_billing.PriceTier{{Price: 0.2}},
			InputImagePrice: 0.05,
		},
	})

	task := matrixTask(0.2, 1, true)
	task.PrivateData.BillingContext.SettleOnComplete = true
	result := &relaycommon.TaskInfo{BillingDuration: 5, BillableImageCount: 1 << 30}
	quota := VideoMatrixQuotaOnComplete(task, result)
	expected := common.QuotaFromFloat((5*0.2 + float64(maxBillableImageCount)*0.05) * common.QuotaPerUnit * 1)
	assert.Equal(t, expected, quota)
}

// 负单价会把附加项变成减项，等于给用户返钱。管理端已拦截，但计费侧不能依赖
// 上游校验：配置也可能被直接改库或经其他接口写入。
func TestVideoMatrixQuotaOnCompleteIgnoresNegativeInputImagePrice(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:            video_billing.UnitPerSecond,
			Tiers:           []video_billing.PriceTier{{Price: 0.2}},
			InputImagePrice: -0.05,
		},
	})

	task := matrixTask(0.2, 1, true)
	task.PrivateData.BillingContext.SettleOnComplete = true
	result := &relaycommon.TaskInfo{BillingDuration: 5, BillableImageCount: 4}

	quota := VideoMatrixQuotaOnComplete(task, result)

	// 负单价被忽略，退回纯按秒计费，绝不低于它。
	assert.Equal(t, common.QuotaFromFloat(5*0.2*common.QuotaPerUnit*1), quota)
}
