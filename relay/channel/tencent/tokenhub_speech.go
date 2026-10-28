// CUSTOM: 腾讯 TokenHub IndexTTS-2 同步语音合成（fork 扩展）。
//
// 接口 POST /v1/wand/tts/sync_tts，请求 {model, text, voice, output_format}，voice 是音色
// 参考音频的 http(s) 链接（data URI 会被拒）。响应 {output: {data}, usage: {chars,
// total_tokens}}，output_format=hex 时 data 是十六进制编码的音频。实测上游不论
// audio_setting.format 填什么都返回 22050Hz 的 WAV，因此只接受 wav。
//
// 计费字符是 UTF-8 字节数（汉字计 3 个、字母计 1 个），价格矩阵按字计价时预扣按同一
// 口径估算，结算换成上游回报的 usage.chars。
//
// 只接 OpenAI 兼容入口 /v1/audio/speech：字段换算后由网关解码音频，直接返回 WAV 文件。
package tencent

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
)

const (
	tokenHubSpeechPath = "/v1/wand/tts/sync_tts"
	// maxTokenHubSpeechTextBytes 单次文本的上限（UTF-8 字节）。上游没有公开 IndexTTS-2
	// 的上限，按同族接口的 1 万字符取；它也是结算时计费字符数的钳制上限。
	maxTokenHubSpeechTextBytes = 10000

	contextKeyTokenHubSpeechBytes = "tencent_tokenhub_speech_bytes"
)

// IsTokenHubSpeechModel 判断上游模型是否走 TokenHub 的 IndexTTS 同步合成接口。
func IsTokenHubSpeechModel(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "indextts")
}

// TokenHubSpeechBillableChars 计费字符数：UTF-8 字节数，与上游 usage.chars 的口径一致。
func TokenHubSpeechBillableChars(text string) int {
	return len(text)
}

// tokenHubSpeechAdaptor 在 OpenAI 兼容适配器上改写语音合成：换接口路径、换请求体、
// 解码音频并按上游字符数修正计费，其余能力沿用 OpenAI 兼容协议。
type tokenHubSpeechAdaptor struct {
	openai.Adaptor
}

type tokenHubSpeechRequest struct {
	Model        string `json:"model"`
	Text         string `json:"text"`
	Voice        string `json:"voice"`
	OutputFormat string `json:"output_format"`
}

type tokenHubSpeechResponse struct {
	Output *struct {
		Data string `json:"data"`
	} `json:"output"`
	Usage struct {
		Chars int `json:"chars"`
	} `json:"usage"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func invalidTokenHubSpeechRequest(err error) error {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func (a *tokenHubSpeechAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return info.ChannelBaseUrl + tokenHubSpeechPath, nil
}

func (a *tokenHubSpeechAdaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	text := request.Input
	if strings.TrimSpace(text) == "" {
		return nil, invalidTokenHubSpeechRequest(errors.New("input is required"))
	}
	if n := TokenHubSpeechBillableChars(text); n > maxTokenHubSpeechTextBytes {
		return nil, invalidTokenHubSpeechRequest(fmt.Errorf("input must be at most %d bytes in UTF-8 (a Chinese character counts as 3), got %d", maxTokenHubSpeechTextBytes, n))
	}
	voice := strings.TrimSpace(request.Voice)
	if !strings.HasPrefix(voice, "http://") && !strings.HasPrefix(voice, "https://") {
		return nil, invalidTokenHubSpeechRequest(errors.New("voice must be an http(s) URL of the reference audio"))
	}
	if format := strings.ToLower(strings.TrimSpace(request.ResponseFormat)); format != "" && format != "wav" {
		return nil, invalidTokenHubSpeechRequest(fmt.Errorf("response_format %q is not supported; this model only returns wav", request.ResponseFormat))
	}
	if request.StreamFormat != "" && request.StreamFormat != "audio" {
		return nil, invalidTokenHubSpeechRequest(fmt.Errorf("stream_format %q is not supported", request.StreamFormat))
	}
	c.Set(contextKeyTokenHubSpeechBytes, TokenHubSpeechBillableChars(text))
	data, err := common.Marshal(tokenHubSpeechRequest{
		Model:        info.UpstreamModelName,
		Text:         text,
		Voice:        voice,
		OutputFormat: "hex",
	})
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *tokenHubSpeechAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *tokenHubSpeechAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream tokenHubSpeechResponse
	if err := common.Unmarshal(body, &upstream); err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to parse tokenhub speech response: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if upstream.Error != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("tokenhub speech error %s: %s", upstream.Error.Code, upstream.Error.Message), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if upstream.Output == nil || upstream.Output.Data == "" {
		return nil, types.NewErrorWithStatusCode(errors.New("tokenhub speech returned no audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	audio, err := hex.DecodeString(upstream.Output.Data)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("decode tokenhub speech audio failed: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	// 按字计价的价格矩阵在预扣时放入了 chars_10k 乘数，这里换成上游的计费字符数，
	// 钳制在本次文本的字节数以内（两者口径相同，上游不该多报）。
	if info.PriceData.HasOtherRatio(video_billing.CharsRatioKey) && upstream.Usage.Chars > 0 {
		chars := upstream.Usage.Chars
		if limit := c.GetInt(contextKeyTokenHubSpeechBytes); limit > 0 && chars > limit {
			common.SysError(fmt.Sprintf("tokenhub speech chars %d exceeds input bytes %d, clamped", chars, limit))
			chars = limit
		}
		info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, float64(chars)/10000)
	}

	c.Data(http.StatusOK, "audio/wav", audio)
	// 没有 token 用量，仍要留下可计费的用量，否则结算会把额度清零。
	promptTokens := max(info.GetEstimatePromptTokens(), 1)
	return &dto.Usage{PromptTokens: promptTokens, TotalTokens: promptTokens}, nil
}
