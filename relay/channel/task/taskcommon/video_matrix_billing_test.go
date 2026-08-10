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
