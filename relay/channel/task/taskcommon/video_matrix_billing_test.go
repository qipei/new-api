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

// 参考图是独立于输出秒数的上游成本，只按秒结算会把这部分白送。提交时没记录张数的
// 旧任务按百炼返回的张数计费（已扣过免费额度），不再重复减免。
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

// 输入图按提交时送给上游的张数减去价格表配置的免费张数计费；不配置时每张都计费。
// 提交时没记录张数的旧任务退回上游报告的总张数，同样按配置扣减。
func TestVideoMatrixQuotaOnCompleteAppliesConfiguredFreeInputImages(t *testing.T) {
	sevenImages := 7
	tests := []struct {
		name        string
		free        int
		inputImages *int
		result      relaycommon.TaskInfo
		wantImages  int
	}{
		{"submitted count minus configured free", 5, &sevenImages, relaycommon.TaskInfo{BillingDuration: 4, InputImageCount: 99, BillableImageCount: 50}, 2},
		{"no free configured charges every image", 0, &sevenImages, relaycommon.TaskInfo{BillingDuration: 4}, 7},
		{"free covers all images", 9, &sevenImages, relaycommon.TaskInfo{BillingDuration: 4}, 0},
		{"legacy task uses upstream total", 5, nil, relaycommon.TaskInfo{BillingDuration: 4, InputImageCount: 7}, 2},
		{"legacy bailian task uses billable count", 5, nil, relaycommon.TaskInfo{BillingDuration: 4, BillableImageCount: 3}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTables(t, map[string]video_billing.ModelPriceTable{
				"seedance-matrix": {
					Unit:            video_billing.UnitPerSecond,
					Tiers:           []video_billing.PriceTier{{Price: 0.5}},
					InputImagePrice: 0.2,
					FreeInputImages: tt.free,
				},
			})
			task := matrixTask(0.5, 0.16, true)
			task.PrivateData.BillingContext.SettleOnComplete = true
			task.PrivateData.BillingContext.InputImages = tt.inputImages
			result := tt.result

			quota := VideoMatrixQuotaOnComplete(task, &result)

			expected := common.QuotaFromFloat((4*0.5 + float64(tt.wantImages)*0.2) * common.QuotaPerUnit * 0.16)
			assert.Equal(t, expected, quota)
		})
	}
}

// 计费秒数里输入视频那部分：价格表没配输入视频单价时与输出同价（MiniMax、百炼的规则），
// 配了就按它单独计费，配 0 为免费；负数按未配置处理，不会变成减项。
func TestVideoMatrixQuotaOnCompletePricesInputVideoSeconds(t *testing.T) {
	price := func(v float64) *float64 { return &v }
	tests := []struct {
		name       string
		inputVideo *float64
		inputSecs  float64
		wantUSD    float64
	}{
		{"unset prices input video like output", nil, 8, 13 * 0.5},
		{"configured price", price(0.3), 8, 5*0.5 + 8*0.3},
		{"free input video", price(0), 8, 5 * 0.5},
		{"negative price falls back to output price", price(-1), 8, 13 * 0.5},
		{"input seconds never exceed billed seconds", price(0), 99, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTables(t, map[string]video_billing.ModelPriceTable{
				"seedance-matrix": {
					Unit:            video_billing.UnitPerSecond,
					Tiers:           []video_billing.PriceTier{{Price: 0.5}},
					InputVideoPrice: tt.inputVideo,
				},
			})
			task := matrixTask(0.5, 0.16, true)
			task.PrivateData.BillingContext.SettleOnComplete = true
			result := &relaycommon.TaskInfo{BillingDuration: 13, InputVideoSeconds: tt.inputSecs}

			quota := VideoMatrixQuotaOnComplete(task, result)

			assert.Equal(t, common.QuotaFromFloat(tt.wantUSD*common.QuotaPerUnit*0.16), quota)
		})
	}
}

// 上游单次有最低收费时，不足价格表最低计费时长的按最低时长结算，超过的按实际时长。
func TestVideoMatrixQuotaOnCompleteAppliesMinSeconds(t *testing.T) {
	withTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-matrix": {
			Unit:       video_billing.UnitPerSecond,
			Tiers:      []video_billing.PriceTier{{Price: 0.0002}},
			MinSeconds: 10,
		},
	})
	for _, tt := range []struct {
		name    string
		seconds float64
		billed  float64
	}{
		{"short audio billed at minimum", 2.69, 10},
		{"long audio billed as is", 42.5, 42.5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			task := matrixTask(0.0002, 1, true)
			task.PrivateData.BillingContext.SettleOnComplete = true

			quota := VideoMatrixQuotaOnComplete(task, &relaycommon.TaskInfo{BillingDuration: tt.seconds})

			assert.Equal(t, common.QuotaFromFloat(tt.billed*0.0002*common.QuotaPerUnit), quota)
		})
	}
}
