package tencent

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TokenHub key 下只有 IndexTTS 的语音合成换成专用适配器，对话等其余请求仍走 OpenAI 兼容。
func TestDispatchAdaptorRoutesTokenHubSpeech(t *testing.T) {
	newInfo := func(mode int, model string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayMode: mode,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:       constant.ChannelTypeTencent,
				ApiKey:            "sk-xxxxxxxxxxxxxxxx",
				UpstreamModelName: model,
			},
		}
	}

	dispatch := &DispatchAdaptor{}
	dispatch.Init(newInfo(relayconstant.RelayModeAudioSpeech, "indextts-2"))
	assert.IsType(t, &tokenHubSpeechAdaptor{}, dispatch.Adaptor)

	dispatch = &DispatchAdaptor{}
	dispatch.Init(newInfo(relayconstant.RelayModeChatCompletions, "indextts-2"))
	assert.IsType(t, &openai.Adaptor{}, dispatch.Adaptor)

	dispatch = &DispatchAdaptor{}
	dispatch.Init(newInfo(relayconstant.RelayModeAudioSpeech, "hunyuan-tts"))
	assert.IsType(t, &openai.Adaptor{}, dispatch.Adaptor)
}

// 计费字符是 UTF-8 字节数：实测「今天天气不错。」上游 usage.chars 为 21，Hello world. 为 12。
func TestTokenHubSpeechBillableChars(t *testing.T) {
	assert.Equal(t, 21, TokenHubSpeechBillableChars("今天天气不错。"))
	assert.Equal(t, 12, TokenHubSpeechBillableChars("Hello world."))
}

func speechTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	return c
}

func TestTokenHubSpeechConvertAudioRequest(t *testing.T) {
	adaptor := &tokenHubSpeechAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "indextts-2"}}

	body, err := adaptor.ConvertAudioRequest(speechTestContext(), info, dto.AudioRequest{
		Input:          "你好",
		Voice:          "https://example.com/voice.wav",
		ResponseFormat: "wav",
	})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var sent map[string]any
	require.NoError(t, common.Unmarshal(data, &sent))
	assert.Equal(t, map[string]any{
		"model":         "indextts-2",
		"text":          "你好",
		"voice":         "https://example.com/voice.wav",
		"output_format": "hex",
	}, sent)

	// 上游只返回 WAV、voice 只认 http(s) 链接：在本地拒绝，不让调用方等一趟上游再失败。
	for name, request := range map[string]dto.AudioRequest{
		"empty input":        {Input: " ", Voice: "https://example.com/voice.wav"},
		"too long":           {Input: strings.Repeat("字", 3334), Voice: "https://example.com/voice.wav"},
		"data uri voice":     {Input: "你好", Voice: "data:audio/mpeg;base64,AAAA"},
		"missing voice":      {Input: "你好"},
		"unsupported format": {Input: "你好", Voice: "https://example.com/voice.wav", ResponseFormat: "mp3"},
	} {
		_, err := adaptor.ConvertAudioRequest(speechTestContext(), info, request)
		assert.Error(t, err, name)
	}
}

func TestTokenHubSpeechDoResponse(t *testing.T) {
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	upstream := `{"object":"tts.index","status":"completed","output":{"data":"` + hex.EncodeToString(wav) + `"},"usage":{"chars":21,"total_tokens":630}}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(contextKeyTokenHubSpeechBytes, 21)
	info := &relaycommon.RelayInfo{}
	info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, 0.0007)

	usage, apiErr := (&tokenHubSpeechAdaptor{}).DoResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(upstream))}, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
	assert.Equal(t, wav, recorder.Body.Bytes())
	// 预扣按本地估算放入的乘数换成上游回报的计费字符数。
	assert.InDelta(t, 21.0/10000, info.PriceData.OtherRatios()[video_billing.CharsRatioKey], 1e-12)

	// 上游多报的字符数钳制在本次文本的字节数以内。
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Set(contextKeyTokenHubSpeechBytes, 6)
	info = &relaycommon.RelayInfo{}
	info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, 0.0006)
	_, apiErr = (&tokenHubSpeechAdaptor{}).DoResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(upstream))}, info)
	require.Nil(t, apiErr)
	assert.InDelta(t, 6.0/10000, info.PriceData.OtherRatios()[video_billing.CharsRatioKey], 1e-12)

	for name, body := range map[string]string{
		"upstream error": `{"error":{"code":"502001","message":"synthesis failed"}}`,
		"no audio":       `{"object":"tts.index","status":"completed","usage":{"chars":3}}`,
		"bad hex":        `{"output":{"data":"zz"},"usage":{"chars":3}}`,
	} {
		c, _ = gin.CreateTestContext(httptest.NewRecorder())
		_, apiErr := (&tokenHubSpeechAdaptor{}).DoResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, &relaycommon.RelayInfo{})
		assert.NotNil(t, apiErr, name)
	}
}
