package volcengine

import (
	"encoding/base64"
	"encoding/json"
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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedAudioContext(t *testing.T, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func decodeBody(t *testing.T, reader io.Reader) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	body := map[string]any{}
	require.NoError(t, common.Unmarshal(raw, &body))
	return body
}

// 原生入口原样透传全部字段（包括文档未列出的），只把 model 换成上游模型名。
func TestSeedAudioNativeRequestPassesThroughFields(t *testing.T) {
	c, _ := seedAudioContext(t, "/v1/audio/generations", `{
		"model": "my-alias",
		"text_prompt": "hi",
		"references": [{"video_url": "https://v"}],
		"audio_config": {"format": "wav", "sample_rate": 48000},
		"watermark": {"aigc_watermark": true},
		"content_producer": "p"
	}`)
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioGeneration, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seed-audio-1.0"}}
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{})
	require.NoError(t, err)

	body := decodeBody(t, reader)
	assert.Equal(t, "seed-audio-1.0", body["model"])
	assert.Equal(t, []any{map[string]any{"video_url": "https://v"}}, body["references"])
	assert.Equal(t, map[string]any{"format": "wav", "sample_rate": float64(48000)}, body["audio_config"])
	assert.Equal(t, map[string]any{"aigc_watermark": true}, body["watermark"])
	assert.Equal(t, "p", body["content_producer"])
}

func TestSeedAudioSpeechRequestMapsOpenAIFields(t *testing.T) {
	tests := []struct {
		name        string
		request     dto.AudioRequest
		wantConfig  map[string]any
		wantRefs    any
		wantExtra   map[string]any
		wantEncoded string
	}{
		{
			name:        "defaults to mp3 without speed",
			request:     dto.AudioRequest{Input: "hi"},
			wantConfig:  map[string]any{"format": "mp3"},
			wantEncoded: "mp3",
		},
		{
			name:        "voice speed and opus",
			request:     dto.AudioRequest{Input: "hi", Voice: "zh_female_vv_uranus_bigtts", ResponseFormat: "opus", Speed: common.GetPointer(1.5)},
			wantConfig:  map[string]any{"format": "ogg_opus", "speech_rate": float64(50)},
			wantRefs:    []any{map[string]any{"speaker": "zh_female_vv_uranus_bigtts"}},
			wantEncoded: "ogg_opus",
		},
		{
			name:        "speed clamps to upstream range",
			request:     dto.AudioRequest{Input: "hi", Speed: common.GetPointer(4.0)},
			wantConfig:  map[string]any{"format": "mp3", "speech_rate": float64(100)},
			wantEncoded: "mp3",
		},
		{
			name: "metadata merges audio_config and overrides the rest",
			request: dto.AudioRequest{Input: "hi", Voice: "v1", ResponseFormat: "wav",
				Metadata: json.RawMessage(`{"audio_config":{"pitch_rate":3,"format":"pcm"},"references":[{"audio_url":"https://a"}],"watermark":{"aigc_watermark":true}}`)},
			wantConfig:  map[string]any{"format": "pcm", "pitch_rate": float64(3)},
			wantRefs:    []any{map[string]any{"audio_url": "https://a"}},
			wantExtra:   map[string]any{"watermark": map[string]any{"aigc_watermark": true}},
			wantEncoded: "pcm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := seedAudioContext(t, "/v1/audio/speech", `{}`)
			info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioSpeech, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seed-audio-1.0"}}
			reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, tt.request)
			require.NoError(t, err)

			body := decodeBody(t, reader)
			assert.Equal(t, "seed-audio-1.0", body["model"])
			assert.Equal(t, "hi", body["text_prompt"])
			assert.Equal(t, tt.wantConfig, body["audio_config"])
			assert.Equal(t, tt.wantRefs, body["references"])
			for key, value := range tt.wantExtra {
				assert.Equal(t, value, body[key])
			}
			assert.Equal(t, tt.wantEncoded, c.GetString(contextKeyResponseFormat))
		})
	}
}

func TestSeedAudioSpeechRejectsInvalidInputAsBadRequest(t *testing.T) {
	tests := []dto.AudioRequest{
		{Input: ""},
		{Input: strings.Repeat("字", relaycommon.MaxAudioGenerationPromptChars+1)},
		{Input: "hi", ResponseFormat: "flac"},
	}
	for _, request := range tests {
		c, _ := seedAudioContext(t, "/v1/audio/speech", `{}`)
		info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioSpeech, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seed-audio-1.0"}}
		_, err := (&Adaptor{}).ConvertAudioRequest(c, info, request)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	}
}

// 计费按上游的原始输出时长（不受变速影响），并钳制在单次上限内；只有按秒计价的
// 价格矩阵预置了 seconds 乘数，其他计价方式不能被乘上时长。
func TestSeedAudioResponseSettlesBySeconds(t *testing.T) {
	audio := base64.StdEncoding.EncodeToString([]byte("RIFFdata"))
	tests := []struct {
		name        string
		perSecond   bool
		upstream    string
		wantSeconds float64
	}{
		{"original duration", true, `{"audio":"` + audio + `","duration":2.2,"original_duration":4.4}`, 4.4},
		{"clamped to max", true, `{"audio":"` + audio + `","original_duration":99999}`, relaycommon.MaxAudioGenerationSeconds},
		{"per-call pricing untouched", false, `{"audio":"` + audio + `","original_duration":4.4}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := seedAudioContext(t, "/v1/audio/speech", `{}`)
			c.Set(contextKeyResponseFormat, "wav")
			info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioSpeech, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seed-audio-1.0"}}
			if tt.perSecond {
				info.PriceData.AddOtherRatio("seconds", relaycommon.MaxAudioGenerationSeconds)
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.upstream))}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)

			assert.Equal(t, tt.wantSeconds, info.PriceData.OtherRatios()["seconds"])
			assert.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "RIFFdata", recorder.Body.String())
		})
	}
}

func TestSeedAudioNativeResponseReturnsUpstreamJSON(t *testing.T) {
	upstream := `{"audio":"QQ==","duration":1,"original_duration":1,"subtitle":{"text":"hi"},"url":"https://u"}`
	c, recorder := seedAudioContext(t, "/v1/audio/generations", `{}`)
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioGeneration, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "seed-audio-1.0"}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream))}
	_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	assert.JSONEq(t, upstream, recorder.Body.String())
}

func TestSeedAudioRequestTargetsSpeechHost(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioGeneration, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "k"}}
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://openspeech.bytedance.com/api/v3/tts/create", url)

	c, _ := seedAudioContext(t, "/v1/audio/generations", `{}`)
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Equal(t, "k", header.Get("X-Api-Key"))
	assert.NotEmpty(t, header.Get("X-Api-Request-Id"))
	assert.Empty(t, header.Get("Authorization"))
}
