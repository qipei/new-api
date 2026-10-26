package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withImageTables(t *testing.T, tables map[string]video_billing.ModelPriceTable) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = tables
	t.Cleanup(func() { settings.PriceTables = original })
}

func imageTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	return c
}

// 图片矩阵计费契约：费用 = 输出档价×n + 输入图单价×张数 + token单价×promptTokens/1M，
// 输出档价由 n 放大，一次性输入组件不随实际输出张数重复放大。
func TestResolveRelayPriceDataImageMatrix(t *testing.T) {
	withImageTables(t, map[string]video_billing.ModelPriceTable{
		"qwen-image-3.0": {
			Unit:            video_billing.UnitPerImage,
			InputImagePrice: 0.02,
			InputTokenPrice: 6.82,
			Tiers: []video_billing.PriceTier{
				{Price: 0.18},
				{Mode: video_billing.ModeImageToImage, Resolution: "2k", Price: 0.5},
			},
		},
	})

	c := imageTestContext(t)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.OriginModelName = "qwen-image-3.0"
	info.UsingGroup = "nonexistent-group-ratio-defaults-to-1"

	// 图生图 2K，两张输出，一张输入图，1000 prompt tokens
	request := &dto.ImageRequest{
		Model: "qwen-image-3.0",
		Size:  "2048x2048",
		N:     lo.ToPtr(uint(2)),
		Image: []byte(`"https://example.com/a.png"`),
	}

	priceData, err := resolveRelayPriceData(c, info, 1000, nil, request)
	require.NoError(t, err)
	require.True(t, priceData.UsePrice)

	extra := 1*0.02 + 1000.0/1_000_000*6.82
	assert.InDelta(t, 0.5, priceData.ModelPrice, 1e-9)
	assert.InDelta(t, extra, priceData.FixedPrice, 1e-9)
	assert.InDelta(t, 2.0, priceData.OtherRatios()["n"], 1e-9)

	expectedQuota := common.QuotaFromFloat((0.5*2 + extra) * common.QuotaPerUnit * 1.0)
	assert.Equal(t, expectedQuota, priceData.QuotaToPreConsume)
}

// 文生图（无输入图）命中默认档；未配置组合拒绝。
func TestResolveRelayPriceDataImageModes(t *testing.T) {
	withImageTables(t, map[string]video_billing.ModelPriceTable{
		"qwen-image-3.0": {
			Unit: video_billing.UnitPerImage,
			Tiers: []video_billing.PriceTier{
				{Price: 0.18},
			},
		},
		"no-default-model": {
			Unit: video_billing.UnitPerImage,
			Tiers: []video_billing.PriceTier{
				{Mode: video_billing.ModeImageToImage, Price: 0.5},
			},
		},
	})

	c := imageTestContext(t)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.OriginModelName = "qwen-image-3.0"

	priceData, err := resolveRelayPriceData(c, info, 100, nil, &dto.ImageRequest{Model: "qwen-image-3.0", Size: "1024x1024"})
	require.NoError(t, err)
	assert.InDelta(t, 0.18, priceData.ModelPrice, 1e-9)

	info.OriginModelName = "no-default-model"
	_, err = resolveRelayPriceData(c, info, 100, nil, &dto.ImageRequest{Model: "no-default-model", Size: "1024x1024"})
	assert.Error(t, err, "文生图请求不应命中仅图生图的档位")
}

func TestCountImageRequestInputImages(t *testing.T) {
	assert.Equal(t, 0, countImageRequestInputImages(&dto.ImageRequest{}))
	assert.Equal(t, 1, countImageRequestInputImages(&dto.ImageRequest{Image: []byte(`"https://a.png"`)}))
	assert.Equal(t, 3, countImageRequestInputImages(&dto.ImageRequest{Image: []byte(`["a","b"]`), Images: []byte(`["c"]`)}))
	assert.Equal(t, 0, countImageRequestInputImages(&dto.ImageRequest{Image: []byte(`""`)}))
}

// 音频矩阵计费契约（豆包语音）：按秒计价按单次时长上限预扣，结算由适配器替换实际时长；
// 按字计价的字符数在请求里已确定，乘数为 字符数/1万。
func TestResolveRelayPriceDataAudioMatrix(t *testing.T) {
	withImageTables(t, map[string]video_billing.ModelPriceTable{
		"seed-audio-1.0": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.0023}}},
		"tts-chars":      {Unit: video_billing.UnitPer10kChars, Tiers: []video_billing.PriceTier{{Price: 0.42}}},
	})
	tests := []struct {
		name      string
		model     string
		relayMode int
		input     string
		ratioKey  string
		ratio     float64
		price     float64
	}{
		{"per second", "seed-audio-1.0", relayconstant.RelayModeAudioGeneration, "你好", "seconds", relaycommon.MaxAudioGenerationSeconds, 0.0023},
		{"per 10k chars", "tts-chars", relayconstant.RelayModeAudioSpeech, "提交第一条合成语音，Hello world!", video_billing.CharsRatioKey, 22.0 / 10000, 0.42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			info.RelayMode = tt.relayMode
			info.OriginModelName = tt.model
			info.UsingGroup = "nonexistent-group-ratio-defaults-to-1"

			priceData, err := resolveRelayPriceData(imageTestContext(t), info, 10, nil, &dto.AudioRequest{Model: tt.model, Input: tt.input})
			require.NoError(t, err)
			require.True(t, priceData.UsePrice)
			assert.InDelta(t, tt.price, priceData.ModelPrice, 1e-12)
			assert.Equal(t, map[string]float64{tt.ratioKey: tt.ratio}, priceData.OtherRatios())
			assert.Equal(t, common.QuotaFromFloat(tt.price*tt.ratio*common.QuotaPerUnit), priceData.QuotaToPreConsume)
		})
	}
}

// 百炼的计费字符口径是汉字计 2 个；预扣按渠道口径估算，避免按字计价的预扣明显偏低。
func TestResolveRelayPriceDataAudioCharsFollowChannelRule(t *testing.T) {
	withImageTables(t, map[string]video_billing.ModelPriceTable{
		"qwen-audio-3.1-tts-plus": {Unit: video_billing.UnitPer10kChars, Tiers: []video_billing.PriceTier{{Price: 0.2}}},
	})
	tests := []struct {
		name        string
		channelType int
		wantChars   float64
	}{
		{"bailian counts han twice", constant.ChannelTypeAli, 34},
		{"other channels count characters", constant.ChannelTypeVolcEngine, 23},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := imageTestContext(t)
			common.SetContextKey(c, constant.ContextKeyChannelType, tt.channelType)
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			info.RelayMode = relayconstant.RelayModeAudioSpeech
			info.OriginModelName = "qwen-audio-3.1-tts-plus"
			info.UsingGroup = "nonexistent-group-ratio-defaults-to-1"

			priceData, err := resolveRelayPriceData(c, info, 10, nil, &dto.AudioRequest{Input: "你好，欢迎使用阿里云百炼。Hello 123!"})
			require.NoError(t, err)
			assert.InDelta(t, tt.wantChars/10000, priceData.OtherRatios()[video_billing.CharsRatioKey], 1e-12)
		})
	}
}

// 档位排除了当前分组时直接拒绝并说明原因，不落到默认档按兜底价收费；其他分组照常取价。
func TestResolveRelayPriceDataRejectsTierExcludedForGroup(t *testing.T) {
	withImageTables(t, map[string]video_billing.ModelPriceTable{
		"image-model": {
			Unit: video_billing.UnitPerImage,
			Tiers: []video_billing.PriceTier{
				{Price: 0.1},
				{Resolution: "4k", Price: 0.5, ExcludedGroups: []string{"cheap"}},
			},
		},
	})
	request := &dto.ImageRequest{Model: "image-model", Size: "4096x4096"}

	for _, tt := range []struct {
		group     string
		wantErr   bool
		wantPrice float64
	}{
		{"cheap", true, 0},
		{"vip", false, 0.5},
	} {
		t.Run(tt.group, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			info.OriginModelName = "image-model"
			info.UsingGroup = tt.group
			priceData, err := resolveRelayPriceData(imageTestContext(t), info, 0, nil, request)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "does not support")
				assert.Contains(t, err.Error(), "cheap")
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.wantPrice, priceData.ModelPrice, 1e-9)
		})
	}
}

// 价格表配置的免费张数先从输入图里扣掉，剩下的才按输入图单价计费；不配置时每张都计费。
func TestResolveRelayPriceDataImageMatrixAppliesFreeInputImages(t *testing.T) {
	for _, tt := range []struct {
		name       string
		free       int
		wantImages int
	}{
		{"no free configured", 0, 3},
		{"one free image", 1, 2},
		{"free covers all images", 5, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withImageTables(t, map[string]video_billing.ModelPriceTable{
				"qwen-image-3.0": {
					Unit:            video_billing.UnitPerImage,
					InputImagePrice: 0.02,
					FreeInputImages: tt.free,
					Tiers:           []video_billing.PriceTier{{Price: 0.18}},
				},
			})
			c := imageTestContext(t)
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			info.OriginModelName = "qwen-image-3.0"
			info.UsingGroup = "nonexistent-group-ratio-defaults-to-1"
			request := &dto.ImageRequest{
				Model:  "qwen-image-3.0",
				Images: []byte(`["https://example.com/a.png","https://example.com/b.png","https://example.com/c.png"]`),
			}

			priceData, err := resolveRelayPriceData(c, info, 0, nil, request)
			require.NoError(t, err)
			assert.InDelta(t, float64(tt.wantImages)*0.02, priceData.FixedPrice, 1e-9)
		})
	}
}
