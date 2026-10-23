// CUSTOM: 百炼 Qwen-Audio-TTS 语音合成与音频生成（fork 扩展）。
// 文档：https://docs.bailian.console.aliyun.com/zh/model-studio/non-realtime-tts-user-guide
//
//	https://docs.bailian.console.aliyun.com/zh/model-studio/audio-generation-api
//
// 三个模型共用同步 HTTP 接口 POST /api/v1/services/audio/tts/SpeechSynthesizer：
//   - qwen-audio-*-tts-plus / -flash：语音合成，参数为 input.text、input.voice 等；
//   - qwen-audio-*-tts-next：音频生成，参数为 input.text_prompt、input.references 等。
//
// 两个入口：
//   - /v1/audio/generations：原生请求体原样透传（只替换 model），原样返回上游 JSON；
//   - /v1/audio/speech：OpenAI 兼容入口，字段换算后由网关取回音频，直接返回音频文件。
//
// 计费（以实测为准，文档不全）：上游 usage 同时给出 input_tokens/output_tokens 与
// characters（汉字计 2 个字符），音频生成另有 output.audio.duration。按 token 计价的
// 模型直接用 token 数；价格矩阵按字或按秒计价时，由这里把预扣时放入的 chars_10k /
// seconds 乘数替换为上游的实际值。
package ali

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
)

const contextKeySpeechFormat = "ali_speech_format"

// maxGenerationSeconds 是音频生成单次输出的时长上限（播客场景 240 秒，其余 120 秒）。
const maxGenerationSeconds = 240

func isQwenAudioTTSModel(modelName string) bool {
	name := strings.ToLower(modelName)
	return strings.HasPrefix(name, "qwen-audio-") && strings.Contains(name, "-tts")
}

// isQwenAudioGenerationModel 区分音频生成（next，用 text_prompt）与语音合成（用 text）。
func isQwenAudioGenerationModel(modelName string) bool {
	return isQwenAudioTTSModel(modelName) && strings.HasSuffix(strings.ToLower(modelName), "-tts-next")
}

// BillableSpeechChars 按百炼的计费口径统计字符：汉字（含繁体、日韩汉字）计 2 个，
// 其他字符（字母、数字、标点、空格、假名、谚文）计 1 个。仅用于预扣估算，
// 结算以上游回报的 characters 为准。
func BillableSpeechChars(text string) int {
	count := utf8.RuneCountInString(text)
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			count++
		}
	}
	return count
}

// speechFormats 把 OpenAI response_format 映射到上游 format。aac 与 flac 上游不支持，
// next 也不支持 opus 输出，直接拒绝，避免返回与声明不符的音频。
var speechFormats = map[string]string{
	"":     "mp3",
	"mp3":  "mp3",
	"wav":  "wav",
	"pcm":  "pcm",
	"opus": "opus",
}

var speechContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"pcm":  "audio/pcm",
	"opus": "audio/ogg",
}

func invalidSpeechRequest(err error) error {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func convertSpeechRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	body := map[string]any{}
	if info.RelayMode == constant.RelayModeAudioGeneration {
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return nil, err
		}
		body["model"] = info.UpstreamModelName
		data, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}

	if request.StreamFormat != "" && request.StreamFormat != "audio" {
		return nil, invalidSpeechRequest(fmt.Errorf("stream_format %q is not supported", request.StreamFormat))
	}
	if strings.TrimSpace(request.Input) == "" {
		return nil, invalidSpeechRequest(errors.New("input is required"))
	}
	isGeneration := isQwenAudioGenerationModel(info.UpstreamModelName)
	format, ok := speechFormats[strings.ToLower(request.ResponseFormat)]
	if !ok || (isGeneration && format == "opus") {
		return nil, invalidSpeechRequest(fmt.Errorf("response_format %q is not supported by %s", request.ResponseFormat, info.UpstreamModelName))
	}

	input := map[string]any{"format": format}
	if isGeneration {
		if n := utf8.RuneCountInString(request.Input); n > relaycommon.MaxAudioGenerationPromptChars {
			return nil, invalidSpeechRequest(fmt.Errorf("input must be at most %d characters, got %d", relaycommon.MaxAudioGenerationPromptChars, n))
		}
		if request.Instructions != "" {
			return nil, invalidSpeechRequest(errors.New("instructions is not supported by the audio generation model; describe the delivery in input instead"))
		}
		// 音频生成没有预置音色，音色来自 references 里的参考音频；OpenAI SDK 必填的
		// voice 在这里没有意义，上游也会忽略，所以不转发。
		input["text_prompt"] = request.Input
	} else {
		if n := utf8.RuneCountInString(request.Input); n > relaycommon.MaxSpeechSynthesisTextChars {
			return nil, invalidSpeechRequest(fmt.Errorf("input must be at most %d characters, got %d", relaycommon.MaxSpeechSynthesisTextChars, n))
		}
		if request.Voice == "" {
			return nil, invalidSpeechRequest(errors.New("voice is required"))
		}
		input["text"] = request.Input
		input["voice"] = request.Voice
		if request.Instructions != "" {
			input["instruction"] = request.Instructions
		}
	}
	if request.Speed != nil {
		input["rate"] = math.Max(0.5, math.Min(2, *request.Speed))
	}
	if len(request.Metadata) > 0 {
		overrides := map[string]any{}
		if err := common.Unmarshal(request.Metadata, &overrides); err != nil {
			return nil, invalidSpeechRequest(fmt.Errorf("metadata must be a JSON object: %w", err))
		}
		for key, value := range overrides {
			input[key] = value
		}
	}
	if format, ok := input["format"].(string); ok {
		c.Set(contextKeySpeechFormat, format)
	}
	body["model"] = info.UpstreamModelName
	body["input"] = input

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

type speechResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  struct {
		Audio struct {
			URL      string  `json:"url"`
			Duration float64 `json:"duration"`
		} `json:"audio"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		Characters   int `json:"characters"`
	} `json:"usage"`
}

func handleSpeechResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream speechResponse
	if err := common.Unmarshal(body, &upstream); err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to parse qwen audio response: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if upstream.Code != "" || upstream.Output.Audio.URL == "" {
		message := upstream.Message
		if message == "" {
			message = "qwen audio returned no audio"
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("%s: %s", upstream.Code, message), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	// 只有按字计价的价格矩阵会在预扣时放入 chars_10k 乘数；按 token 计价的模型
	// 不能再乘字符数。上游字符数同样钳制在单次上限内（汉字计 2 个）。
	if info.PriceData.HasOtherRatio(video_billing.CharsRatioKey) && upstream.Usage.Characters > 0 {
		chars := upstream.Usage.Characters
		if limit := 2 * relaycommon.MaxSpeechSynthesisTextChars; chars > limit {
			common.SysError(fmt.Sprintf("qwen audio characters %d exceeds %d, clamped", chars, limit))
			chars = limit
		}
		info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, float64(chars)/10000)
	}
	if info.PriceData.HasOtherRatio("seconds") && upstream.Output.Audio.Duration > 0 {
		info.PriceData.AddOtherRatio("seconds", math.Min(upstream.Output.Audio.Duration, maxGenerationSeconds))
	}

	if info.RelayMode == constant.RelayModeAudioGeneration {
		c.Data(http.StatusOK, "application/json", body)
	} else if apiErr := streamSpeechAudio(c, info, upstream.Output.Audio.URL); apiErr != nil {
		return nil, apiErr
	}

	usage := &dto.Usage{
		PromptTokens:     upstream.Usage.InputTokens,
		CompletionTokens: upstream.Usage.OutputTokens,
		TotalTokens:      upstream.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		// 没有 token 用量时（例如按字计价的旧模型）仍要留下可计费的用量，
		// 否则结算会把额度清零。
		usage.PromptTokens = max(info.GetEstimatePromptTokens(), 1)
		usage.TotalTokens = usage.PromptTokens
	}
	return usage, nil
}

// streamSpeechAudio 取回上游生成的音频并直接写给调用方。音频地址来自上游响应，
// 和请求上游一样走渠道的代理设置。
func streamSpeechAudio(c *gin.Context, info *relaycommon.RelayInfo, audioURL string) *types.NewAPIError {
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, audioURL, nil)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	audioResp, err := client.Do(req)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("download qwen audio failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	defer audioResp.Body.Close()
	if audioResp.StatusCode != http.StatusOK {
		return types.NewErrorWithStatusCode(fmt.Errorf("download qwen audio failed: status %d", audioResp.StatusCode), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	}

	contentType := speechContentTypes[c.GetString(contextKeySpeechFormat)]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, audioResp.Body); err != nil {
		common.SysError("write qwen audio to client failed: " + err.Error())
	}
	return nil
}
