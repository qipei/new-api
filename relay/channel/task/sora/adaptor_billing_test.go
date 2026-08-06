package sora

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTaskContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", req)
	return c
}

func withPriceTables(t *testing.T, tables map[string]video_billing.ModelPriceTable) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = tables
	t.Cleanup(func() { settings.PriceTables = original })
}

// 每秒计费的矩阵模型：基础价由矩阵接管，只返回 seconds 倍率。
func TestEstimateBillingMatrixPerSecond(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{
		"sora-2-pro": {
			Unit:  video_billing.UnitPerSecond,
			Tiers: []video_billing.PriceTier{{Price: 0.3}},
		},
	})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "8", Size: "1792x1024"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "sora-2-pro"

	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 8.0, ratios["seconds"], 1e-9)
	_, hasSize := ratios["size"]
	assert.False(t, hasSize, "矩阵模型不应再有 size 倍率")
}

// 按 token 计费的矩阵模型：时长体现在 usage 中，不返回任何倍率。
func TestEstimateBillingMatrixTokenBilled(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{
		"seedance-via-sora": {
			Unit:  video_billing.UnitPerMillionToken,
			Tiers: []video_billing.PriceTier{{Price: 6.3}},
		},
	})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "10", Size: "1080p"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "seedance-via-sora"

	assert.Nil(t, adaptor.EstimateBilling(c, info))
}

// 未配置矩阵的模型保持原有硬编码行为。
func TestEstimateBillingLegacyFallback(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "4", Size: "1792x1024"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "sora-2-pro"

	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 4.0, ratios["seconds"], 1e-9)
	assert.InDelta(t, 1.666667, ratios["size"], 1e-9)
}

// remix 在未配置矩阵时返回 nil（保持原有行为）。
func TestEstimateBillingRemixUnconfigured(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Prompt: "remix it"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "sora-2"
	info.Action = constant.TaskActionRemix

	assert.Nil(t, adaptor.EstimateBilling(c, info))
}

// 上游任务态响应带 usage 时，完成态应透出 token 数以触发差额结算。
func TestParseTaskResultUsageTokens(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"video_123","status":"completed","progress":100,` +
		`"usage":{"completion_tokens":432000,"total_tokens":432123}}`)

	result, err := adaptor.ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, 432000, result.CompletionTokens)
	assert.Equal(t, 432123, result.TotalTokens)
}

// 不带 usage 的响应（原生 Sora）不应产生 token 数。
func TestParseTaskResultWithoutUsage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"video_123","status":"completed","progress":100}`)

	result, err := adaptor.ParseTaskResult(body)
	require.NoError(t, err)
	assert.Zero(t, result.TotalTokens)
	assert.Zero(t, result.CompletionTokens)
}
