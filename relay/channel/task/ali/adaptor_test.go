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
	"github.com/samber/lo"
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

func TestConvertToAliRequestHappyHorseT2V(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:    "happyhorse-1.1-t2v",
		Prompt:   "a horse runs through a field",
		Size:     "720p",
		Duration: 15,
		Metadata: map[string]interface{}{
			"parameters": map[string]interface{}{
				"ratio":     "21:9",
				"watermark": false,
				"seed":      0,
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "720P", pointerString(aliReq.Parameters.Resolution))
	assert.Equal(t, "21:9", pointerString(aliReq.Parameters.Ratio))
	assert.Equal(t, 15, pointerInt(aliReq.Parameters.Duration))
	require.NotNil(t, aliReq.Parameters.Watermark)
	assert.False(t, *aliReq.Parameters.Watermark)
	require.NotNil(t, aliReq.Parameters.Seed)
	assert.Zero(t, *aliReq.Parameters.Seed)
	assert.Empty(t, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.ImgURL)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"happyhorse-1.1-t2v",
		"input":{"prompt":"a horse runs through a field"},
		"parameters":{"resolution":"720P","duration":15,"watermark":false,"seed":0,"ratio":"21:9"}
	}`, string(body))
	assert.NotContains(t, string(body), `"size"`)
	assert.NotContains(t, string(body), `"aspect_ratio"`)
}

func TestConvertToAliRequestHappyHorseI2V(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:    "happyhorse-1.1-i2v",
		Image:    "https://example.com/first.webp",
		Size:     "480p",
		Duration: 3,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "480P", pointerString(aliReq.Parameters.Resolution))
	assert.Equal(t, 3, pointerInt(aliReq.Parameters.Duration))
	assert.Nil(t, aliReq.Parameters.Ratio)
	assert.Equal(t, []AliVideoMedia{{Type: "first_frame", URL: "https://example.com/first.webp"}}, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.ImgURL)
	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"happyhorse-1.1-i2v",
		"input":{"media":[{"type":"first_frame","url":"https://example.com/first.webp"}]},
		"parameters":{"resolution":"480P","duration":3}
	}`, string(body))
}

func TestConvertToAliRequestHappyHorseR2V(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "happyhorse-1.1-r2v",
		Prompt: "[Image 1] and [Image 2] enter the same scene",
		Images: []string{
			"https://example.com/one.jpg",
			"https://example.com/two.png",
			"https://example.com/three.webp",
		},
		Size:     "1920x1080",
		Duration: 10,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "1080P", pointerString(aliReq.Parameters.Resolution))
	assert.Equal(t, "16:9", pointerString(aliReq.Parameters.Ratio))
	assert.Equal(t, 10, pointerInt(aliReq.Parameters.Duration))
	require.Len(t, aliReq.Input.Media, 3)
	for _, media := range aliReq.Input.Media {
		assert.Equal(t, "reference_image", media.Type)
	}
	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"happyhorse-1.1-r2v",
		"input":{"prompt":"[Image 1] and [Image 2] enter the same scene","media":[
			{"type":"reference_image","url":"https://example.com/one.jpg"},
			{"type":"reference_image","url":"https://example.com/two.png"},
			{"type":"reference_image","url":"https://example.com/three.webp"}
		]},
		"parameters":{"resolution":"1080P","duration":10,"ratio":"16:9"}
	}`, string(body))
}

func TestConvertToAliRequestHappyHorseVideoEdit(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "happyhorse-1.0-video-edit",
		Prompt: "replace the jacket with the referenced jacket",
		Video:  "https://example.com/source.mp4",
		Images: []string{
			"https://example.com/ref-1.png",
			"https://example.com/ref-2.png",
		},
		Size: "1080p",
		Metadata: map[string]interface{}{
			"parameters": map[string]interface{}{
				"audio_setting": "origin",
				"watermark":     false,
				"seed":          2147483647,
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "1080P", pointerString(aliReq.Parameters.Resolution))
	assert.Nil(t, aliReq.Parameters.Duration)
	assert.Nil(t, aliReq.Parameters.Ratio)
	assert.Equal(t, "origin", pointerString(aliReq.Parameters.AudioSetting))
	assert.Equal(t, []AliVideoMedia{
		{Type: "video", URL: "https://example.com/source.mp4"},
		{Type: "reference_image", URL: "https://example.com/ref-1.png"},
		{Type: "reference_image", URL: "https://example.com/ref-2.png"},
	}, aliReq.Input.Media)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"happyhorse-1.0-video-edit",
		"input":{"prompt":"replace the jacket with the referenced jacket","media":[
			{"type":"video","url":"https://example.com/source.mp4"},
			{"type":"reference_image","url":"https://example.com/ref-1.png"},
			{"type":"reference_image","url":"https://example.com/ref-2.png"}
		]},
		"parameters":{"resolution":"1080P","watermark":false,"seed":2147483647,"audio_setting":"origin"}
	}`, string(body))
	assert.NotContains(t, string(body), `"duration"`)
	assert.NotContains(t, string(body), `"size"`)
}

func TestHappyHorseAcceptsEveryDocumentedRatio(t *testing.T) {
	for _, ratio := range []string{"16:9", "9:16", "1:1", "4:3", "3:4", "4:5", "5:4", "9:21", "21:9"} {
		t.Run(ratio, func(t *testing.T) {
			parameters := &AliVideoParameters{}
			require.NoError(t, applyHappyHorseSize(parameters, "happyhorse-1.1-t2v", ratio))
			assert.Equal(t, ratio, pointerString(parameters.Ratio))
		})
	}
}

func TestConvertToAliRequestHappyHorseKeepsExplicitMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "happyhorse-1.0-video-edit",
		Prompt: "apply a cinematic color grade",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{"type": "video", "url": "https://example.com/input.mov"},
				},
			},
			"parameters": map[string]interface{}{"audio_setting": "auto"},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, []AliVideoMedia{{Type: "video", URL: "https://example.com/input.mov"}}, aliReq.Input.Media)
	assert.Equal(t, "auto", pointerString(aliReq.Parameters.AudioSetting))
}

func TestConvertToAliRequestHappyHorseRejectsInvalidParameters(t *testing.T) {
	adaptor := &TaskAdaptor{}
	tests := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{
			name: "t2v duration below minimum",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-t2v", Prompt: "test", Duration: 2},
			want: "between 3 and 15",
		},
		{
			name: "t2v unsupported ratio",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-t2v", Prompt: "test", Size: "2:1"},
			want: "unsupported HappyHorse ratio",
		},
		{
			name: "i2v requires one image",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-i2v", Prompt: "test"},
			want: "exactly one first_frame",
		},
		{
			name: "i2v rejects multiple simple images",
			req: relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-i2v", Prompt: "test", Images: []string{
				"https://example.com/one.png", "https://example.com/two.png",
			}},
			want: "accepts exactly one input image",
		},
		{
			name: "i2v rejects ratio",
			req: relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-i2v", Prompt: "test", Image: "https://example.com/one.png", Metadata: map[string]interface{}{
				"parameters": map[string]interface{}{"ratio": "16:9"},
			}},
			want: "does not support parameters.ratio",
		},
		{
			name: "r2v rejects more than nine images",
			req: relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-r2v", Prompt: "test", Images: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
			}},
			want: "1 to 9",
		},
		{
			name: "video edit requires video",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.0-video-edit", Prompt: "test"},
			want: "exactly one video",
		},
		{
			name: "video edit rejects duration",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.0-video-edit", Prompt: "test", Video: "https://example.com/in.mp4", Duration: 5},
			want: "does not support parameters.duration",
		},
		{
			name: "video edit rejects 480p",
			req:  relaycommon.TaskSubmitReq{Model: "happyhorse-1.0-video-edit", Prompt: "test", Video: "https://example.com/in.mp4", Size: "480p"},
			want: "unsupported resolution",
		},
		{
			name: "video edit rejects audio setting",
			req: relaycommon.TaskSubmitReq{Model: "happyhorse-1.0-video-edit", Prompt: "test", Video: "https://example.com/in.mp4", Metadata: map[string]interface{}{
				"parameters": map[string]interface{}{"audio_setting": "mute"},
			}},
			want: "must be auto or origin",
		},
		{
			name: "seed above maximum",
			req: relaycommon.TaskSubmitReq{Model: "happyhorse-1.1-t2v", Prompt: "test", Metadata: map[string]interface{}{
				"parameters": map[string]interface{}{"seed": 2147483648},
			}},
			want: "seed must be between",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adaptor.convertToAliRequest(testRelayInfo(), tt.req)
			require.ErrorContains(t, err, tt.want)
		})
	}
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
	require.Equal(t, 9.0, result.BillingDuration)
	require.Equal(t, "https://example.com/video.mp4", result.Url)
	require.Equal(t, "https://example.com/watermarked.mp4", result.RemoteUrl)
}

func TestParseTaskResultCarriesFractionalHappyHorseBillingDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"output": {"task_status": "SUCCEEDED", "task_id": "upstream-task", "video_url": "https://example.com/video.mp4"},
		"usage": {"duration": 13.24, "input_video_duration": 6.62, "output_video_duration": 6.62, "video_count": 1, "SR": 720}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, 14, result.Duration)
	assert.Equal(t, 13.24, result.BillingDuration)
}

func TestParseTaskResultAcceptsStringDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"output": {"task_status": "SUCCEEDED", "task_id": "upstream-task"},
		"usage": {"duration": "9.5"}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, 10, result.Duration)
	assert.Equal(t, 9.5, result.BillingDuration)
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
	require.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), result.BillingDuration)
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

// MiniMax requires typed input.media; without a dedicated branch the generic path
// emits the legacy Wan image fields and every image-to-video request fails upstream.
func TestConvertToAliRequestMiniMaxBuildsFrameMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "MiniMax/MiniMax-H3",
		Prompt: "让图片中的人物动起来",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/last.png",
		},
		Duration: 5,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	require.Equal(t, []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/first.png"},
		{Type: "last_frame", URL: "https://example.com/last.png"},
	}, aliReq.Input.Media)
	require.Empty(t, aliReq.Input.ImgURL)
	require.Empty(t, aliReq.Input.FirstFrameURL)
}

// Reference-to-video and frame-driven media are mutually exclusive upstream.
func TestValidateAliMiniMaxRejectsMixedMediaAndOutOfRangeDuration(t *testing.T) {
	base := func() *AliVideoRequest {
		return &AliVideoRequest{
			Model: "MiniMax/MiniMax-H3",
			Input: AliVideoInput{Prompt: "p"},
			Parameters: &AliVideoParameters{
				Duration: lo.ToPtr(5),
				Ratio:    lo.ToPtr("16:9"),
			},
		}
	}

	mixed := base()
	mixed.Input.Media = []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/a.png"},
		{Type: "feature", URL: "https://example.com/v.mp4"},
	}
	require.ErrorContains(t, validateAliMiniMaxRequest(mixed), "cannot combine")

	tooShort := base()
	tooShort.Parameters.Duration = lo.ToPtr(3)
	require.ErrorContains(t, validateAliMiniMaxRequest(tooShort), "between 4 and 15")

	tooManyRefs := base()
	for i := 0; i < 10; i++ {
		tooManyRefs.Input.Media = append(tooManyRefs.Input.Media, AliVideoMedia{Type: "image_url", URL: "https://example.com/r.png"})
	}
	require.ErrorContains(t, validateAliMiniMaxRequest(tooManyRefs), "at most 9 reference images")

	ok := base()
	ok.Input.Media = []AliVideoMedia{{Type: "first_frame", URL: "https://example.com/a.png"}}
	require.NoError(t, validateAliMiniMaxRequest(ok))
}

// 4k is a documented Kling mode for v3/omni; rejecting it blocked valid requests.
// Turbo is the narrower model: first_frame only, no element_list, no 4k.
func TestValidateAliKlingModeAndTurboRestrictions(t *testing.T) {
	base := func(model string) *AliVideoRequest {
		return &AliVideoRequest{
			Model:      model,
			Input:      AliVideoInput{Prompt: "p"},
			Parameters: &AliVideoParameters{Duration: lo.ToPtr(5)},
		}
	}

	fourK := base("kling/kling-v3-video-generation")
	fourK.Parameters.Mode = lo.ToPtr("4k")
	require.NoError(t, validateAliKlingRequest(fourK))

	turbo4k := base("kling/kling-v3-turbo-video-generation")
	turbo4k.Parameters.Mode = lo.ToPtr("4k")
	require.ErrorContains(t, validateAliKlingRequest(turbo4k), "does not support 4k")

	turboLastFrame := base("kling/kling-v3-turbo-video-generation")
	turboLastFrame.Input.Media = []AliVideoMedia{
		{Type: "first_frame", URL: "https://example.com/a.png"},
		{Type: "last_frame", URL: "https://example.com/b.png"},
	}
	require.ErrorContains(t, validateAliKlingRequest(turboLastFrame), "only supports first_frame")

	turboElements := base("kling/kling-v3-turbo-video-generation")
	turboElements.Input.ElementList = []AliVideoElement{{ElementID: 1}}
	require.ErrorContains(t, validateAliKlingRequest(turboElements), "does not support element_list")

	tooManyElements := base("kling/kling-v3-video-generation")
	tooManyElements.Input.Media = []AliVideoMedia{{Type: "first_frame", URL: "https://example.com/a.png"}}
	tooManyElements.Input.ElementList = []AliVideoElement{{ElementID: 1}, {ElementID: 2}, {ElementID: 3}, {ElementID: 4}}
	require.ErrorContains(t, validateAliKlingRequest(tooManyElements), "at most 3 elements")
}

// A size in WxH form left parameters.resolution empty, so our own validator rejected
// every Vidu request that used the documented size parameter. Sending size alone is
// also wrong upstream: it is ignored and forced to 720P while we price by size.
func TestConvertToAliRequestViduDerivesResolutionFromSize(t *testing.T) {
	adaptor := &TaskAdaptor{}
	cases := []struct {
		size       string
		resolution string
	}{
		{"1920*1080", "1080P"},
		{"1080*1920", "1080P"},
		{"1280*720", "720P"},
		{"1280*1280", "720P"},
		{"1024*576", "540P"},
		{"1024*1024", "540P"},
		{"576*1024", "540P"},
	}
	for _, tc := range cases {
		req := relaycommon.TaskSubmitReq{
			Model:    "vidu/viduq3_reference2video",
			Prompt:   "p",
			Images:   []string{"https://example.com/a.png"},
			Size:     tc.size,
			Duration: 5,
		}

		aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

		require.NoError(t, err, "size %s must be accepted", tc.size)
		assert.Equal(t, tc.resolution, pointerString(aliReq.Parameters.Resolution), "size %s", tc.size)
		assert.Equal(t, tc.size, pointerString(aliReq.Parameters.Size), "size must still be forwarded")
	}
}
