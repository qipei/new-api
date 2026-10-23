package doubaotts

import (
	"io"
	"net/http"
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

func speechTaskContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech/tasks", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context
}

func withCharsPricing(t *testing.T, modelName string) {
	t.Helper()
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		modelName: {Unit: video_billing.UnitPer10kChars, Tiers: []video_billing.PriceTier{{Price: 0.42}}},
	}
	t.Cleanup(func() { settings.PriceTables = original })
}

// 上游要求 additions 与 post_process 是 JSON 字符串、audio_params.format 必填；
// 网关接受对象并补默认格式，构造出的上游请求必须满足这些约束。
func TestBuildRequestBodyNormalizesUpstreamOnlyShapes(t *testing.T) {
	c := speechTaskContext(t, `{
		"model": "seed-tts-2.0",
		"req_params": {
			"text": "我在北京",
			"speaker": "zh_female_vv_uranus_bigtts",
			"audio_params": {"sample_rate": 24000},
			"additions": {"silence_duration": 300},
			"post_process": {"pitch": 2}
		}
	}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, "seed-tts-2.0", info.OriginModelName)

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(raw, &body))

	assert.Equal(t, map[string]any{"uid": defaultUID}, body["user"])
	params := body["req_params"].(map[string]any)
	assert.Equal(t, `{"silence_duration":300}`, params["additions"])
	assert.Equal(t, `{"pitch":2}`, params["post_process"])
	assert.Equal(t, map[string]any{"sample_rate": float64(24000), "format": "mp3"}, params["audio_params"])
	assert.NotContains(t, body, "unique_id")
}

func TestValidateRejectsInvalidSpeechTasks(t *testing.T) {
	tooLong := strings.Repeat("字", relaycommon.MaxSpeechTaskTextChars+1)
	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing text", `{"model":"seed-tts-2.0","req_params":{"speaker":"s"}}`, "req_params.text is required"},
		{"missing speaker", `{"model":"seed-tts-2.0","req_params":{"text":"hi"}}`, "req_params.speaker is required"},
		{"text over limit", `{"model":"seed-tts-2.0","req_params":{"speaker":"s","text":"` + tooLong + `"}}`, "at most 100000 characters"},
		{"text and ssml counted together", `{"model":"seed-tts-2.0","req_params":{"speaker":"s","text":"` + tooLong[:len(tooLong)-6] + `","ssml":"<speak>x</speak>"}}`, "at most 100000 characters"},
		{"additions wrong type", `{"model":"seed-tts-2.0","req_params":{"speaker":"s","text":"hi","additions":5}}`, "req_params.additions must be an object or a JSON string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(speechTaskContext(t, tt.body), &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.True(t, taskErr.LocalError)
			assert.Contains(t, taskErr.Message, tt.want)
		})
	}
}

// 按字计费：预扣按本地字符数（text 与 ssml 合计），提交后按上游 req_text_length 校正，
// 上游回报值同样钳制在单次上限内。
func TestCharsBillingRatios(t *testing.T) {
	withCharsPricing(t, "seed-tts-2.0")
	c := speechTaskContext(t, `{"model":"seed-tts-2.0","req_params":{"speaker":"s","text":"提交第一条合成语音，Hello world!"}}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	assert.Equal(t, map[string]float64{video_billing.CharsRatioKey: 22.0 / 10000}, adaptor.EstimateBilling(c, info))
	assert.Equal(t, map[string]float64{video_billing.CharsRatioKey: 30.0 / 10000},
		adaptor.AdjustBillingOnSubmit(info, []byte(`{"code":20000000,"data":{"task_id":"t","req_text_length":30}}`)))
	assert.Equal(t, map[string]float64{video_billing.CharsRatioKey: float64(relaycommon.MaxSpeechTaskTextChars) / 10000},
		adaptor.AdjustBillingOnSubmit(info, []byte(`{"code":20000000,"data":{"task_id":"t","req_text_length":999999999}}`)))
}

func TestCharsBillingRatiosSkippedWithoutCharsPricing(t *testing.T) {
	withCharsPricing(t, "other-model")
	c := speechTaskContext(t, `{"model":"seed-tts-2.0","req_params":{"speaker":"s","text":"hi"}}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	assert.Nil(t, adaptor.EstimateBilling(c, info))
	assert.Nil(t, adaptor.AdjustBillingOnSubmit(info, []byte(`{"code":20000000,"data":{"req_text_length":30}}`)))
}

func TestParseTaskResultMapsUpstreamStates(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus string
		wantURL    string
		wantReason string
	}{
		{"running", `{"code":20000000,"data":{"task_status":1}}`, model.TaskStatusInProgress, "", ""},
		{"success", `{"code":20000000,"data":{"task_status":2,"audio_url":"https://a/b.mp3"}}`, model.TaskStatusSuccess, "https://a/b.mp3", ""},
		{"failure", `{"code":20000000,"message":"ok","data":{"task_status":3}}`, model.TaskStatusFailure, "", "speech synthesis failed"},
		{"expired task", `{"code":40000001,"message":"任务不存在或已过期"}`, model.TaskStatusFailure, "", "任务不存在或已过期"},
		{"transient error keeps polling", `{"code":55000000,"message":"busy"}`, model.TaskStatusInProgress, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, info.Status)
			assert.Equal(t, tt.wantURL, info.Url)
			assert.Equal(t, tt.wantReason, info.Reason)
		})
	}
}

func TestSubmitParameterErrorIsLocalBadRequest(t *testing.T) {
	c := speechTaskContext(t, `{}`)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`{"code":40000000,"message":"请求参数错误: 不支持的音频格式"}`))}
	_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
}

func TestBuildTaskResponseAndURLExpiry(t *testing.T) {
	stored := []byte(`{"code":20000000,"data":{"task_status":2,"audio_url":"https://old","url_expire_time":1000,"req_text_length":9,"synthesize_text_length":9,"sentences":[{"text":"hi"}]}}`)
	task := &model.Task{
		TaskID:     "task_x",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		SubmitTime: 10,
		FinishTime: 20,
		Data:       stored,
		Properties: model.Properties{OriginModelName: "seed-tts-2.0"},
	}
	assert.True(t, AudioURLExpired(task, 950), "expires within a minute")
	assert.False(t, AudioURLExpired(task, 900))

	raw, err := BuildTaskResponse(task, []byte(`{"code":20000000,"data":{"task_status":2,"audio_url":"https://fresh","url_expire_time":5000,"req_text_length":9,"synthesize_text_length":9}}`))
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, common.Unmarshal(raw, &resp))
	assert.Equal(t, "task_x", resp["task_id"])
	assert.Equal(t, "succeeded", resp["status"])
	assert.Equal(t, "https://fresh", resp["audio_url"])
	assert.Equal(t, float64(5000), resp["url_expire_time"])
	assert.Equal(t, float64(20), resp["finished_at"])

	task.Status = model.TaskStatusFailure
	task.FailReason = "boom"
	task.Data = nil
	raw, err = BuildTaskResponse(task, nil)
	require.NoError(t, err)
	resp = map[string]any{}
	require.NoError(t, common.Unmarshal(raw, &resp))
	assert.Equal(t, "failed", resp["status"])
	assert.Equal(t, map[string]any{"message": "boom"}, resp["error"])
}
