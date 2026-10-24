package ali

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const miniMaxModel = "MiniMax/speech-2.8-hd"

func TestMiniMaxSpeechTargetsMultimodalGeneration(t *testing.T) {
	for _, mode := range []int{constant.RelayModeAudioSpeech, constant.RelayModeAudioGeneration} {
		url, err := (&Adaptor{}).GetRequestURL(speechInfo(mode, miniMaxModel))
		require.NoError(t, err)
		assert.Equal(t, "https://ws-x.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", url)
	}
}

func TestMiniMaxSpeechCompatRequestMapsOpenAIFields(t *testing.T) {
	speed := 3.0
	c, _ := speechContext(t, "/v1/audio/speech", `{}`)
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioSpeech, miniMaxModel), dto.AudioRequest{
		Input:          "你好",
		Voice:          "male-qn-qingse",
		Speed:          &speed,
		ResponseFormat: "flac",
		Metadata:       []byte(`{"voice_setting":{"emotion":"happy"},"audio_setting":{"sample_rate":44100},"language_boost":"auto"}`),
	})
	require.NoError(t, err)

	body := decodeSpeechBody(t, reader)
	assert.Equal(t, miniMaxModel, body["model"])
	assert.Equal(t, map[string]any{
		"text":           "你好",
		"voice_setting":  map[string]any{"voice_id": "male-qn-qingse", "speed": float64(2), "emotion": "happy"},
		"audio_setting":  map[string]any{"format": "flac", "sample_rate": float64(44100)},
		"output_format":  "hex",
		"language_boost": "auto",
	}, body["input"])
	assert.Equal(t, "flac", c.GetString(contextKeySpeechFormat))
}

func TestMiniMaxSpeechRejectsInvalidRequestsAsBadRequest(t *testing.T) {
	tests := []struct {
		name    string
		request dto.AudioRequest
	}{
		{"missing voice", dto.AudioRequest{Input: "hi"}},
		{"text at limit", dto.AudioRequest{Input: strings.Repeat("a", maxMiniMaxSpeechTextChars), Voice: "v"}},
		{"opus not supported", dto.AudioRequest{Input: "hi", Voice: "v", ResponseFormat: "opus"}},
		{"instructions not supported", dto.AudioRequest{Input: "hi", Voice: "v", Instructions: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := speechContext(t, "/v1/audio/speech", `{}`)
			_, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioSpeech, miniMaxModel), tt.request)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		})
	}

	c, _ := speechContext(t, "/v1/audio/generations", `{"model":"x","input":{"text":"`+strings.Repeat("a", maxMiniMaxSpeechTextChars)+`","voice_setting":{"voice_id":"v"}}}`)
	_, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioGeneration, miniMaxModel), dto.AudioRequest{})
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestMiniMaxSpeechNativeRequestPassesThroughInput(t *testing.T) {
	c, _ := speechContext(t, "/v1/audio/generations", `{"model":"alias","input":{"text":"你好","voice_setting":{"voice_id":"v","pitch":2},"timbre_weights":[{"voice_id":"a","weight":50}],"subtitle_enable":true}}`)
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, speechInfo(constant.RelayModeAudioGeneration, miniMaxModel), dto.AudioRequest{})
	require.NoError(t, err)
	body := decodeSpeechBody(t, reader)
	assert.Equal(t, miniMaxModel, body["model"])
	assert.Equal(t, map[string]any{
		"text":            "你好",
		"voice_setting":   map[string]any{"voice_id": "v", "pitch": float64(2)},
		"timbre_weights":  []any{map[string]any{"voice_id": "a", "weight": float64(50)}},
		"subtitle_enable": true,
	}, body["input"])
}

func miniMaxUpstream(audio string, characters int) string {
	return `{"output":{"base_resp":{"status_code":0,"status_msg":"success"},"data":{"audio":"` + audio +
		`","status":2},"extra_info":{"audio_length":4536,"usage_characters":` + fmt.Sprint(characters) +
		`}},"usage":{"characters":` + fmt.Sprint(characters) + `},"request_id":"r"}`
}

// 计费契约：按字计价的乘数换成上游计费字符数并钳制；按 token / 按次计价不能被乘上字符数。
// 兼容入口把 hex 音频解码成文件返回。
func TestMiniMaxSpeechResponseDecodesAudioAndSettles(t *testing.T) {
	audio := hex.EncodeToString([]byte("ID3audio"))
	tests := []struct {
		name       string
		preRatio   bool
		characters int
		wantRatios map[string]float64
	}{
		{"no chars multiplier", false, 34, nil},
		{"chars replaced by upstream count", true, 34, map[string]float64{video_billing.CharsRatioKey: 34.0 / 10000}},
		{"chars clamped", true, 999999, map[string]float64{video_billing.CharsRatioKey: float64(2*maxMiniMaxSpeechTextChars) / 10000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := speechContext(t, "/v1/audio/speech", `{}`)
			c.Set(contextKeySpeechFormat, "mp3")
			info := speechInfo(constant.RelayModeAudioSpeech, miniMaxModel)
			if tt.preRatio {
				info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, 1)
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(miniMaxUpstream(audio, tt.characters)))}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, apiErr)

			assert.Equal(t, &dto.Usage{PromptTokens: 1, TotalTokens: 1}, usage)
			assert.Equal(t, tt.wantRatios, info.PriceData.OtherRatios())
			assert.Equal(t, "audio/mpeg", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "ID3audio", recorder.Body.String())
		})
	}
}

func TestMiniMaxSpeechNativeResponseReturnsUpstreamJSON(t *testing.T) {
	upstream := miniMaxUpstream("49443361", 34)
	c, recorder := speechContext(t, "/v1/audio/generations", `{}`)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream))}
	_, apiErr := (&Adaptor{}).DoResponse(c, resp, speechInfo(constant.RelayModeAudioGeneration, miniMaxModel))
	require.Nil(t, apiErr)
	assert.JSONEq(t, upstream, recorder.Body.String())
}

// base_resp 里的业务错误：参数类返回 400 且不重试，限流返回 429，其余按上游故障。
func TestMiniMaxSpeechBusinessErrors(t *testing.T) {
	tests := []struct {
		code       string
		wantStatus int
	}{
		{"2013", http.StatusBadRequest},
		{"2054", http.StatusBadRequest},
		{"1002", http.StatusTooManyRequests},
		{"1000", http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			c, _ := speechContext(t, "/v1/audio/speech", `{}`)
			upstream := `{"output":{"base_resp":{"status_code":` + tt.code + `,"status_msg":"boom"},"data":null}}`
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream))}
			_, apiErr := (&Adaptor{}).DoResponse(c, resp, speechInfo(constant.RelayModeAudioSpeech, miniMaxModel))
			require.NotNil(t, apiErr)
			assert.Equal(t, tt.wantStatus, apiErr.StatusCode)
			assert.Contains(t, apiErr.Error(), "boom")
		})
	}
}
