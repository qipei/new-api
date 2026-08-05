package sora

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"
	hosttypes "github.com/QuantumNous/new-api/types"

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

// 按次（每秒）计费模型：矩阵命中时返回 seconds + video_price。
func TestEstimateBillingMatrixPerCall(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{
		"sora-2-pro": {
			BasePrice: 0.3,
			Tiers: []video_billing.PriceTier{
				{Resolution: "1080p", Price: 0.5},
				{Audio: video_billing.AudioOn, Resolution: "1080p", Price: 0.6},
			},
		},
	})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "8", Size: "1792x1024"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "sora-2-pro"
	info.PriceData = hosttypes.PriceData{UsePrice: true}

	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 8.0, ratios["seconds"], 1e-9)
	assert.InDelta(t, 0.5/0.3, ratios["video_price"], 1e-9)
	assert.InDelta(t, 1.0, ratios["size"], 1e-9)
}

// 按量（token）计费模型：只返回 video_price，时长由 token 用量体现。
func TestEstimateBillingMatrixTokenBilled(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{
		"doubao-seedance-2-0-260128-sora": {
			BasePrice: 46,
			Tiers: []video_billing.PriceTier{
				{Resolution: "1080p", Price: 51},
			},
		},
	})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "10", Size: "1080p"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "doubao-seedance-2-0-260128-sora"
	info.PriceData = hosttypes.PriceData{UsePrice: false}

	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_price"], 1e-9)
	_, hasSeconds := ratios["seconds"]
	assert.False(t, hasSeconds, "token 计费不应包含 seconds 倍率")
}

// 未配置矩阵的模型保持原有硬编码行为。
func TestEstimateBillingLegacyFallback(t *testing.T) {
	withPriceTables(t, map[string]video_billing.ModelPriceTable{})

	adaptor := &TaskAdaptor{}
	c := newTaskContext(t, relaycommon.TaskSubmitReq{Seconds: "4", Size: "1792x1024"})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.OriginModelName = "sora-2-pro"
	info.PriceData = hosttypes.PriceData{UsePrice: true}

	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 4.0, ratios["seconds"], 1e-9)
	assert.InDelta(t, 1.666667, ratios["size"], 1e-9)
	_, hasPrice := ratios["video_price"]
	assert.False(t, hasPrice)
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

// generate_audio 参数决定音轨维度：JSON metadata、显式 false、缺省三种情形。
func TestRequestAudioDimension(t *testing.T) {
	c := newTaskContext(t, relaycommon.TaskSubmitReq{})

	req := relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"generate_audio": true}}
	assert.Equal(t, video_billing.AudioOn, requestAudioDimension(c, &req))

	req = relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"generate_audio": false}}
	assert.Equal(t, video_billing.AudioOff, requestAudioDimension(c, &req))

	req = relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"generate_audio": "true"}}
	assert.Equal(t, video_billing.AudioOn, requestAudioDimension(c, &req))

	req = relaycommon.TaskSubmitReq{}
	assert.Equal(t, "", requestAudioDimension(c, &req))
}
