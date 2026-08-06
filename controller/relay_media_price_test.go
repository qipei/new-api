package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
// 折算为 等效每张价(ModelPrice) × n 后预扣与结算一致。
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
	expectedEffective := 0.5 + extra/2
	assert.InDelta(t, expectedEffective, priceData.ModelPrice, 1e-9)
	assert.InDelta(t, 2.0, priceData.OtherRatios()["n"], 1e-9)

	expectedQuota := common.QuotaFromFloat(expectedEffective * 2 * common.QuotaPerUnit * 1.0)
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
