package ali

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
}

func aliTaskContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", req)
	return c
}

func withAliPriceTables(t *testing.T, tables map[string]video_billing.ModelPriceTable) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = tables
	t.Cleanup(func() { settings.PriceTables = original })
}

func TestConvertToAliRequestWan27I2VBuildsMediaFromImage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:    "wan2.7-i2v",
		Prompt:   "animate the first frame",
		Image:    "https://example.com/first.png",
		Size:     "720p",
		Duration: 10,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, "wan2.7-i2v", aliReq.Model)
	require.Equal(t, "720P", pointerString(aliReq.Parameters.Resolution))
	require.Equal(t, 10, pointerInt(aliReq.Parameters.Duration))
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/first.png"},
	}, aliReq.Input.Media)
	require.Empty(t, aliReq.Input.ImgURL)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	require.Contains(t, string(body), `"media"`)
	require.NotContains(t, string(body), `"img_url"`)
}

func TestConvertToAliRequestWan27I2VBuildsFirstAndLastFrameFromImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "interpolate between frames",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/last.png",
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/first.png"},
		{Type: "last_frame", URL: "https://example.com/last.png"},
	}, aliReq.Input.Media)
}

func TestConvertToAliRequestWan27I2VPrefersImageBeforeImagesAndInputReference(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:          "wan2.7-i2v",
		Prompt:         "use the direct image",
		Image:          " https://example.com/direct.png ",
		Images:         []string{"https://example.com/images-first.png", " https://example.com/images-last.png "},
		InputReference: "https://example.com/input-reference.png",
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/direct.png"},
		{Type: "last_frame", URL: "https://example.com/images-last.png"},
	}, aliReq.Input.Media)
}

func TestConvertToAliRequestWan27I2VFallsBackToFirstNonEmptyImage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "skip blank images",
		Image:  " ",
		Images: []string{
			" ",
			" https://example.com/first.png ",
			" https://example.com/last.png ",
		},
		InputReference: "https://example.com/input-reference.png",
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/first.png"},
		{Type: "last_frame", URL: "https://example.com/last.png"},
	}, aliReq.Input.Media)
}

func TestConvertToAliRequestWan27I2VKeepsExplicitMetadataMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:          "wan2.7-i2v",
		Prompt:         "continue the clip",
		Image:          "https://example.com/direct.png",
		Images:         []string{"https://example.com/images-first.png", "https://example.com/images-last.png"},
		InputReference: "https://example.com/input-reference.png",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{
						"type": "first_clip",
						"url":  "https://example.com/input.mp4",
					},
				},
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_clip", URL: "https://example.com/input.mp4"},
	}, aliReq.Input.Media)
	require.Empty(t, aliReq.Input.ImgURL)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	require.Contains(t, string(body), `"media"`)
	require.NotContains(t, string(body), `"img_url"`)
}

func TestConvertToAliRequestWan27I2VRequiresMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "animate without a frame",
	}

	_, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "requires image"))
}

func TestConvertToAliRequestWan25I2VKeepsLegacyImgURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.5-i2v-preview",
		Prompt: "animate the first frame",
		Image:  "https://example.com/first.png",
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/first.png", aliReq.Input.ImgURL)
	require.Empty(t, aliReq.Input.Media)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	require.Contains(t, string(body), `"img_url"`)
	require.NotContains(t, string(body), `"media"`)
}

func TestConvertToAliRequestKlingBuildsFrameMediaAndMapsSize(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "kling/kling-v3-video-generation",
		Prompt: "move from day to night",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/last.png",
		},
		Size:     "1080x1920",
		Duration: 8,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/first.png"},
		{Type: "last_frame", URL: "https://example.com/last.png"},
	}, aliReq.Input.Media)
	require.Equal(t, "pro", pointerString(aliReq.Parameters.Mode))
	require.Equal(t, "9:16", pointerString(aliReq.Parameters.AspectRatio))
	require.Nil(t, aliReq.Parameters.PromptExtend)
	require.Empty(t, aliReq.Input.ImgURL)
}

func TestConvertToAliRequestKlingOmniPreservesAdvancedMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "kling/kling-v3-omni-video-generation",
		Prompt: "unused in custom shot mode",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{"type": "feature", "url": "https://example.com/reference.mp4"},
				},
				"keep_original_sound": "yes",
				"multi_shot":          true,
				"shot_type":           "customize",
				"multi_prompt": []interface{}{
					map[string]interface{}{"index": 1, "prompt": "opening", "duration": 3},
					map[string]interface{}{"index": 2, "prompt": "closing", "duration": 3},
				},
				"element_list": []interface{}{
					map[string]interface{}{"element_id": 42},
				},
			},
			"parameters": map[string]interface{}{
				"mode":         "std",
				"aspect_ratio": "16:9",
				"duration":     6,
				"audio":        false,
				"watermark":    true,
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, "yes", pointerString(aliReq.Input.KeepOriginalSound))
	require.True(t, *aliReq.Input.MultiShot)
	require.Equal(t, "customize", pointerString(aliReq.Input.ShotType))
	require.Len(t, aliReq.Input.MultiPrompt, 2)
	require.Equal(t, 42, aliReq.Input.ElementList[0].ElementID)
	require.Equal(t, "std", pointerString(aliReq.Parameters.Mode))
	require.Equal(t, 6, pointerInt(aliReq.Parameters.Duration))
	require.NotNil(t, aliReq.Parameters.Audio)
	require.False(t, *aliReq.Parameters.Audio)
}

func TestConvertToAliRequestViduDramaUsesOfficialDefaultAndImageMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "vidu/viduq3-drama_reference2video",
		Prompt: "a short dramatic scene",
		Images: []string{"https://example.com/character.png"},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, "1080P", pointerString(aliReq.Parameters.Resolution))
	require.Equal(t, []AliVideoMedia{{Type: "image", URL: "https://example.com/character.png"}}, aliReq.Input.Media)
	require.Nil(t, aliReq.Parameters.PromptExtend)
}

func TestConvertToAliRequestViduQ2ProPreservesAutoDurationAndSeedZero(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "vidu/viduq2-pro_reference2video",
		Prompt: "follow the reference video",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{"type": "image", "url": "https://example.com/subject.png"},
					map[string]interface{}{"type": "video", "url": "https://example.com/motion.mp4"},
				},
			},
			"parameters": map[string]interface{}{
				"resolution": "1080P",
				"size":       "1920*1080",
				"duration":   0,
				"seed":       0,
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, 0, pointerInt(aliReq.Parameters.Duration))
	require.NotNil(t, aliReq.Parameters.Seed)
	require.Zero(t, *aliReq.Parameters.Seed)
	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	require.Contains(t, string(body), `"duration":0`)
	require.Contains(t, string(body), `"seed":0`)
}

func TestConvertToAliRequestViduRejectsUnsupportedAudio(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "vidu/viduq2_reference2video",
		Prompt: "animate",
		Image:  "https://example.com/reference.png",
		Metadata: map[string]interface{}{
			"parameters": map[string]interface{}{"audio": true},
		},
	}

	_, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.ErrorContains(t, err, "does not support parameters.audio")
}

func TestParseTaskResultCarriesActualDurationAndWatermarkURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"output": {
			"task_status": "SUCCEEDED",
			"task_id": "upstream-task",
			"video_url": "https://example.com/video.mp4",
			"watermark_video_url": "https://example.com/watermarked.mp4"
		},
		"usage": {"duration": 9, "video_count": 1, "SR": "1080"}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, 9, result.Duration)
	require.Equal(t, "https://example.com/video.mp4", result.Url)
	require.Equal(t, "https://example.com/watermarked.mp4", result.RemoteUrl)
}

func TestParseTaskResultCapsUntrustedActualDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"output": {"task_status": "SUCCEEDED", "task_id": "upstream-task"},
		"usage": {"duration": 999999}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, relaycommon.MaxTaskDurationSeconds, result.Duration)
}

func TestEstimateBillingAliMatrixPerSecondUsesOnlyDuration(t *testing.T) {
	modelName := "vidu/viduq3_reference2video"
	withAliPriceTables(t, map[string]video_billing.ModelPriceTable{
		modelName: {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.3}}},
	})
	req := relaycommon.TaskSubmitReq{
		Model: modelName, Image: "https://example.com/reference.png", Size: "720p", Seconds: "8",
	}
	info := testRelayInfo()
	info.OriginModelName = modelName

	ratios := (&TaskAdaptor{}).EstimateBilling(aliTaskContext(t, req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, map[string]float64{"seconds": 8}, ratios)
}

func TestEstimateBillingAliMatrixTokenDoesNotDoubleMultiplyDuration(t *testing.T) {
	modelName := "vidu/viduq3_reference2video"
	withAliPriceTables(t, map[string]video_billing.ModelPriceTable{
		modelName: {Unit: video_billing.UnitPerMillionToken, Tiers: []video_billing.PriceTier{{Price: 6.3}}},
	})
	req := relaycommon.TaskSubmitReq{
		Model: modelName, Image: "https://example.com/reference.png", Size: "720p", Seconds: "8",
	}
	info := testRelayInfo()
	info.OriginModelName = modelName

	assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(aliTaskContext(t, req), info))
}
