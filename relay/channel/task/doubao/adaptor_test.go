package doubao

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intValue(value int) *kitdto.IntValue {
	result := kitdto.IntValue(value)
	return &result
}

func boolValue(value bool) *kitdto.BoolValue {
	result := kitdto.BoolValue(value)
	return &result
}

func doubaoRequestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context
}

func withDoubaoPriceTables(t *testing.T, tables map[string]video_billing.ModelPriceTable) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = tables
	t.Cleanup(func() { settings.PriceTables = original })
}

func TestConvertStandardFieldsAndSize(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "city at night",
		Duration: 8,
		Size:     "1920x1080",
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload.Duration)
	assert.Equal(t, 8, int(*payload.Duration))
	assert.Equal(t, "1080p", payload.Resolution)
	assert.Equal(t, "16:9", payload.Ratio)
	require.Len(t, payload.Content, 1)
	assert.Equal(t, "text", payload.Content[0].Type)
	assert.Equal(t, "city at night", payload.Content[0].Text)
}

func TestConvertSecondsOverridesDuration(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "test",
		Duration: 8,
		Seconds:  "10",
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload.Duration)
	assert.Equal(t, 10, int(*payload.Duration))
}

func TestConvertUnifiedMediaAndRoles(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-5-260628",
		Prompt: "animate",
		Mode:   "reference",
		Images: []string{"https://example.com/1.png", "https://example.com/2.png"},
		Video:  "https://example.com/reference.mp4",
		Audio:  "https://example.com/reference.mp3",
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	assert.Equal(t, "reference", payload.OmniReferenceTaskType)
	require.Len(t, payload.Content, 5)
	assert.Equal(t, "reference_image", payload.Content[0].Role)
	assert.Equal(t, "reference_image", payload.Content[1].Role)
	assert.Equal(t, "reference_video", payload.Content[2].Role)
	assert.Equal(t, "reference_audio", payload.Content[3].Role)
	assert.Equal(t, "text", payload.Content[4].Type)
}

func TestConvertFirstAndLastFrameRoles(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-1-5-pro-251215",
		Prompt: "transition",
		Images: []string{"https://example.com/first.png", "https://example.com/last.png"},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.Len(t, payload.Content, 3)
	assert.Equal(t, "first_frame", payload.Content[0].Role)
	assert.Equal(t, "last_frame", payload.Content[1].Role)
}

func TestNativeMetadataTakesPriorityAndPreservesOptionalText(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-5-260628",
		Images: []string{"https://example.com/ignored.png"},
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": "https://example.com/native.mp4"}, "role": "reference_video"},
				map[string]interface{}{"type": "text", "text": "native prompt"},
			},
			"output_format": "mov",
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.Len(t, payload.Content, 2)
	assert.Equal(t, "video_url", payload.Content[0].Type)
	assert.Equal(t, "native prompt", payload.Content[1].Text)
	assert.Equal(t, "mov", payload.OutputFormat)
}

func TestExplicitFalseAndZeroArePreserved(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-5-260628",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"generate_audio": false,
			"watermark":      false,
			"priority":       0,
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload.GenerateAudio)
	require.NotNil(t, payload.Watermark)
	require.NotNil(t, payload.Priority)
	assert.False(t, bool(*payload.GenerateAudio))
	assert.False(t, bool(*payload.Watermark))
	assert.Zero(t, int(*payload.Priority))

	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"generate_audio":false`)
	assert.Contains(t, string(encoded), `"watermark":false`)
	assert.Contains(t, string(encoded), `"priority":0`)
}

func TestNormalizeVideoSize(t *testing.T) {
	tests := []struct {
		input      string
		resolution string
		ratio      string
	}{
		{"854x480", "480p", "16:9"},
		{"1024*768", "720p", "4:3"},
		{"960x960", "720p", "1:1"},
		{"1920x1080", "1080p", "16:9"},
		{"2880x2880", "4k", "1:1"},
		{"720p", "720p", ""},
		{"9:16", "", "9:16"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			resolution, ratio := normalizeVideoSize(test.input)
			assert.Equal(t, test.resolution, resolution)
			assert.Equal(t, test.ratio, ratio)
		})
	}
}

func TestValidateSeedance25Edit(t *testing.T) {
	payload := &requestPayload{
		Model:                 "doubao-seedance-2-5-260628",
		OmniReferenceTaskType: "edit",
		OutputFormat:          "mov",
		Ratio:                 "adaptive",
		Resolution:            "720p",
		Duration:              intValue(-1),
		GenerateAudio:         boolValue(true),
		Priority:              intValue(9),
		ExecutionExpiresAfter: intValue(259200),
		Content: []ContentItem{{
			Type: "video_url", VideoURL: &MediaURL{URL: "https://example.com/input.mov"}, Role: "reference_video",
		}},
	}
	assert.NoError(t, validateRequestPayload(payload, ""))
}

func TestValidateModelSpecificFailures(t *testing.T) {
	tests := []struct {
		name    string
		payload *requestPayload
		want    string
	}{
		{
			name: "Seedance 2.0 rejects audio only",
			payload: &requestPayload{Model: "doubao-seedance-2-0-260128", Content: []ContentItem{
				{Type: "audio_url", AudioURL: &MediaURL{URL: "https://example.com/a.mp3"}, Role: "reference_audio"},
			}},
			want: "requires at least one image or video",
		},
		{
			name: "Seedance 2.5 rejects 1080p",
			payload: &requestPayload{Model: "doubao-seedance-2-5-260628", Resolution: "1080p", Content: []ContentItem{
				{Type: "text", Text: "test"},
			}},
			want: "does not support resolution",
		},
		{
			name: "edit requires video",
			payload: &requestPayload{Model: "doubao-seedance-2-5-260628", OmniReferenceTaskType: "edit", Ratio: "adaptive", Duration: intValue(-1), Content: []ContentItem{
				{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/a.png"}, Role: "reference_image"},
			}},
			want: "requires at least one reference_video",
		},
		{
			name: "draft requires 480p",
			payload: &requestPayload{Model: "doubao-seedance-1-5-pro-251215", Draft: boolValue(true), Resolution: "720p", Content: []ContentItem{
				{Type: "text", Text: "test"},
			}},
			want: "requires resolution 480p",
		},
		{
			name: "2.0 rejects flex",
			payload: &requestPayload{Model: "doubao-seedance-2-0-260128", ServiceTier: "flex", Content: []ContentItem{
				{Type: "text", Text: "test"},
			}},
			want: "does not support service_tier flex",
		},
		{
			name: "first frame cannot mix reference video",
			payload: &requestPayload{Model: "doubao-seedance-2-5-260628", Content: []ContentItem{
				{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/a.png"}, Role: "first_frame"},
				{Type: "video_url", VideoURL: &MediaURL{URL: "https://example.com/a.mp4"}, Role: "reference_video"},
			}},
			want: "cannot be mixed",
		},
		{
			name: "invalid frames",
			payload: &requestPayload{Model: "doubao-seedance-1-0-pro-250428", Frames: intValue(30), Content: []ContentItem{
				{Type: "text", Text: "test"},
			}},
			want: "25 + 4n",
		},
		{
			name: "Seedance 2.0 mini rejects 1080p",
			payload: &requestPayload{Model: "doubao-seedance-2-0-mini-260615", Resolution: "1080p", Content: []ContentItem{
				{Type: "text", Text: "test"},
			}},
			want: "does not support resolution",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRequestPayload(test.payload, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestValidateSeedance25AudioOnlyThroughUnifiedRequest(t *testing.T) {
	context := doubaoRequestContext(t, `{"model":"doubao-seedance-2-5-260628","audio":"https://example.com/voice.mp3","duration":6}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-5-260628",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info)
	require.Nil(t, taskErr)
	req, err := relaycommon.GetTaskRequest(context)
	require.NoError(t, err)
	assert.True(t, req.HasAudio())
	assert.Equal(t, 6, req.Duration)
}

func TestValidateNativeTextWithoutTopLevelPrompt(t *testing.T) {
	context := doubaoRequestContext(t, `{"model":"doubao-seedance-2-5-260628","metadata":{"content":[{"type":"text","text":"native prompt"}]}}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-5-260628",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info)
	require.Nil(t, taskErr)
}

func TestValidateUsesMappedUpstreamModelConstraints(t *testing.T) {
	context := doubaoRequestContext(t, `{"model":"public-seedance","prompt":"test","size":"1080p"}`)
	context.Set("model_mapping", `{"public-seedance":"doubao-seedance-2-0-mini-260615"}`)
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "does not support resolution")
	assert.Equal(t, "public-seedance", info.OriginModelName)
	assert.Equal(t, "doubao-seedance-2-0-mini-260615", info.UpstreamModelName)
}

func TestParseTaskResultCarriesActualBillingData(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"cgt-test","status":"succeeded",
		"content":{"video_url":"https://example.com/out.mp4","last_frame_url":"https://example.com/last.png","file_url":"https://example.com/out.mov"},
		"duration":11,"resolution":"720p","ratio":"16:9",
		"usage":{"completion_tokens":210000,"total_tokens":220000}
	}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Equal(t, "https://example.com/out.mp4", result.Url)
	assert.Equal(t, "https://example.com/last.png", result.RemoteUrl)
	assert.Equal(t, 11, result.Duration)
	assert.Equal(t, 11.0, result.BillingDuration)
	assert.Equal(t, 220000, result.TotalTokens)
}

func TestEstimateBillingUsesUnifiedVideoAndPixelSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model: "doubao-seedance-2-0-260128",
		Video: "https://example.com/reference.mp4",
		Size:  "1920x1080",
	})
	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2-0-260128"}

	ratios := (&TaskAdaptor{}).EstimateBilling(context, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateBillingMatrixPerSecondReservesDurationAndFlexDiscount(t *testing.T) {
	modelName := "doubao-seedance-1-5-pro-251215"
	withDoubaoPriceTables(t, map[string]video_billing.ModelPriceTable{
		modelName: {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.3}}},
	})
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model: modelName, Prompt: "test", Duration: 8,
		Metadata: map[string]interface{}{"service_tier": "flex"},
	})
	info := &relaycommon.RelayInfo{OriginModelName: modelName}

	ratios := (&TaskAdaptor{}).EstimateBilling(context, info)
	assert.Equal(t, map[string]float64{"seconds": 8, "service_tier": 0.5}, ratios)
}

func TestAdjustBillingOnCompleteAppliesFlexDiscount(t *testing.T) {
	modelName := "doubao-seedance-1-5-pro-251215"
	withDoubaoPriceTables(t, map[string]video_billing.ModelPriceTable{
		modelName: {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.3}}},
	})
	baseTask := &model.Task{PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{
		ModelPrice: 0.3, GroupRatio: 1, OriginModelName: modelName, SettleOnComplete: true,
	}}}
	result := &relaycommon.TaskInfo{Duration: 8}
	baseQuota := taskcommon.VideoMatrixQuotaOnComplete(baseTask, result)

	discountedTask := &model.Task{PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{
		ModelPrice: 0.3, GroupRatio: 1, OriginModelName: modelName, SettleOnComplete: true,
		OtherRatios: map[string]float64{"seconds": 8, "service_tier": 0.5},
	}}}
	discountedQuota := (&TaskAdaptor{}).AdjustBillingOnComplete(discountedTask, &relaycommon.TaskInfo{Duration: 8})
	assert.Equal(t, baseQuota/2, discountedQuota)
}

func TestBuildRequestBodyUsesMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Model: "public-name", Prompt: "test"})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		IsModelMapped: true, UpstreamModelName: "doubao-seedance-2-5-260628",
	}}

	body, err := (&TaskAdaptor{}).BuildRequestBody(context, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"model":"doubao-seedance-2-5-260628"`)
}
