package ali

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func speechContext(t *testing.T, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func speechInfo(relayMode int, model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayMode,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "k",
			ChannelBaseUrl:    "https://ws-x.cn-beijing.maas.aliyuncs.com",
			UpstreamModelName: model,
		},
	}
}

func decodeSpeechBody(t *testing.T, reader io.Reader) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	body := map[string]any{}
	require.NoError(t, common.Unmarshal(raw, &body))
	return body
}

// 百炼汉字计 2 个字符，其他字符计 1 个（官方示例：「中A文123」为 8 个字符）。
func TestBillableSpeechChars(t *testing.T) {
	assert.Equal(t, 4, BillableSpeechChars("你好"))
	assert.Equal(t, 8, BillableSpeechChars("中A文123"))
	assert.Equal(t, 5, BillableSpeechChars("中文。"))
	assert.Equal(t, 34, BillableSpeechChars("你好，欢迎使用阿里云百炼。Hello 123!"))
}

func TestSpeechRequestTargetsSpeechSynthesizer(t *testing.T) {
	for _, mode := range []int{constant.RelayModeAudioSpeech, constant.RelayModeAudioGeneration} {
		info := speechInfo(mode, "qwen-audio-3.1-tts-flash")
		url, err := (&Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, "https://ws-x.cn-beijing.maas.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer", url)

		c, _ := speechContext(t, "/v1/audio/speech", `{}`)
		info.IsStream = true
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
		assert.Equal(t, "Bearer k", header.Get("Authorization"))
		assert.Empty(t, header.Get("X-DashScope-SSE"), "sync speech must not ask for SSE")
	}
}

func TestSpeechCompatRequestMapsOpenAIFields(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		request    dto.AudioRequest
		wantInput  map[string]any
		wantFormat string
	}{
		{
			name:  "synthesis maps text voice instruction and speed",
			model: "qwen-audio-3.1-tts-plus",
			request: dto.AudioRequest{Input: "你好", Voice: "longanhuan_v3.1", Instructions: "开心地说",
				ResponseFormat: "opus", Speed: common.GetPointer(3.0)},
			wantInput:  map[string]any{"text": "你好", "voice": "longanhuan_v3.1", "instruction": "开心地说", "format": "opus", "rate": float64(2)},
			wantFormat: "opus",
		},
		{
			name:       "generation uses text_prompt and drops the voice",
			model:      "qwen-audio-3.1-tts-next",
			request:    dto.AudioRequest{Input: "用温柔的女声说：你好", Voice: "alloy"},
			wantInput:  map[string]any{"text_prompt": "用温柔的女声说：你好", "format": "mp3"},
			wantFormat: "mp3",
		},
		{
			name:  "metadata overrides converted fields",
			model: "qwen-audio-3.1-tts-flash",
			request: dto.AudioRequest{Input: "hi", Voice: "v", ResponseFormat: "mp3",
				Metadata: json.RawMessage(`{"format":"wav","sample_rate":16000,"language_hints":["en"],"hot_fix":{"replace":[{"hi":"hello"}]}}`)},
			wantInput: map[string]any{"text": "hi", "voice": "v", "format": "wav", "sample_rate": float64(16000),
				"language_hints": []any{"en"}, "hot_fix": map[string]any{"replace": []any{map[string]any{"hi": "hello"}}}},
			wantFormat: "wav",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := speechContext(t, "/v1/audio/speech", `{}`)
			reader, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioSpeech, tt.model), tt.request)
			require.NoError(t, err)
			body := decodeSpeechBody(t, reader)
			assert.Equal(t, tt.model, body["model"])
			assert.Equal(t, tt.wantInput, body["input"])
			assert.Equal(t, tt.wantFormat, c.GetString(contextKeySpeechFormat))
		})
	}
}

func TestSpeechCompatRejectsInvalidRequestsAsBadRequest(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		request dto.AudioRequest
	}{
		{"missing voice for synthesis", "qwen-audio-3.1-tts-flash", dto.AudioRequest{Input: "hi"}},
		{"synthesis text over limit", "qwen-audio-3.1-tts-flash", dto.AudioRequest{Input: strings.Repeat("a", relaycommon.MaxSpeechSynthesisTextChars+1), Voice: "v"}},
		{"generation prompt over limit", "qwen-audio-3.1-tts-next", dto.AudioRequest{Input: strings.Repeat("字", relaycommon.MaxAudioGenerationPromptChars+1)}},
		{"generation cannot output opus", "qwen-audio-3.1-tts-next", dto.AudioRequest{Input: "hi", ResponseFormat: "opus"}},
		{"generation has no instructions", "qwen-audio-3.1-tts-next", dto.AudioRequest{Input: "hi", Instructions: "x"}},
		{"unsupported format", "qwen-audio-3.1-tts-plus", dto.AudioRequest{Input: "hi", Voice: "v", ResponseFormat: "flac"}},
		{"sse is not supported yet", "qwen-audio-3.1-tts-plus", dto.AudioRequest{Input: "hi", Voice: "v", StreamFormat: "sse"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := speechContext(t, "/v1/audio/speech", `{}`)
			_, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioSpeech, tt.model), tt.request)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		})
	}
}

func TestSpeechNativeRequestPassesThroughInput(t *testing.T) {
	c, _ := speechContext(t, "/v1/audio/generations", `{"model":"alias","input":{"text_prompt":"@voice1 说你好","references":[{"audio_url":"https://a"}],"channels":1,"enable_cbr":true}}`)
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioGeneration, "qwen-audio-3.1-tts-next"), dto.AudioRequest{})
	require.NoError(t, err)
	body := decodeSpeechBody(t, reader)
	assert.Equal(t, "qwen-audio-3.1-tts-next", body["model"])
	assert.Equal(t, map[string]any{"text_prompt": "@voice1 说你好", "references": []any{map[string]any{"audio_url": "https://a"}},
		"channels": float64(1), "enable_cbr": true}, body["input"])
}

// 计费契约：token 用量原样上报；按字/按秒计价的矩阵乘数换成上游实际值并钳制，
// 未预置乘数（按 token 计价）的请求不能被乘上字符数或时长。
func TestSpeechResponseSettlesFromUpstreamUsage(t *testing.T) {
	audioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ID3audio"))
	}))
	defer audioServer.Close()
	upstream := func(chars int, duration float64) string {
		return `{"output":{"audio":{"url":"` + audioServer.URL + `/a.mp3","duration":` + fmt.Sprint(duration) +
			`},"finish_reason":"stop"},"usage":{"input_tokens":20,"output_tokens":48,"total_tokens":68,"characters":` + fmt.Sprint(chars) + `}}`
	}
	tests := []struct {
		name       string
		preRatio   string
		upstream   string
		wantRatios map[string]float64
	}{
		{"token pricing keeps no multiplier", "", upstream(34, 0), nil},
		{"chars replaced by upstream count", video_billing.CharsRatioKey, upstream(34, 0), map[string]float64{video_billing.CharsRatioKey: 34.0 / 10000}},
		{"chars clamped", video_billing.CharsRatioKey, upstream(999999, 0), map[string]float64{video_billing.CharsRatioKey: float64(2*relaycommon.MaxSpeechSynthesisTextChars) / 10000}},
		{"seconds replaced and clamped", "seconds", upstream(0, 9999), map[string]float64{"seconds": maxGenerationSeconds}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := speechContext(t, "/v1/audio/speech", `{}`)
			c.Set(contextKeySpeechFormat, "mp3")
			info := speechInfo(constant.RelayModeAudioSpeech, "qwen-audio-3.1-tts-plus")
			if tt.preRatio != "" {
				info.PriceData.AddOtherRatio(tt.preRatio, 1)
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.upstream))}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, apiErr)

			assert.Equal(t, &dto.Usage{PromptTokens: 20, CompletionTokens: 48, TotalTokens: 68}, usage)
			assert.Equal(t, tt.wantRatios, info.PriceData.OtherRatios())
			assert.Equal(t, "audio/mpeg", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "ID3audio", recorder.Body.String())
		})
	}
}

func TestSpeechNativeResponseReturnsUpstreamJSON(t *testing.T) {
	upstream := `{"output":{"audio":{"data":"","url":"https://oss/a.wav","duration":2.24},"finish_reason":"stop"},"usage":{"input_tokens":172,"output_tokens":448,"total_tokens":620,"duration":2}}`
	c, recorder := speechContext(t, "/v1/audio/generations", `{}`)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream))}
	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, speechInfo(constant.RelayModeAudioGeneration, "qwen-audio-3.1-tts-next"))
	require.Nil(t, apiErr)
	assert.JSONEq(t, upstream, recorder.Body.String())
	assert.Equal(t, &dto.Usage{PromptTokens: 172, CompletionTokens: 448, TotalTokens: 620}, usage)
}

func TestSpeechResponseWithoutAudioFails(t *testing.T) {
	c, _ := speechContext(t, "/v1/audio/speech", `{}`)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":"InvalidParameter","message":"bad"}`))}
	_, apiErr := (&Adaptor{}).DoResponse(c, resp, speechInfo(constant.RelayModeAudioSpeech, "qwen-audio-3.1-tts-plus"))
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "bad")
}
