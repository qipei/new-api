// CUSTOM: 豆包音频生成 seed-audio（fork 扩展）。
// 文档：https://docs.volcengine.com/docs/DoubaoVoice/audio-generation-http
//
// 两个入口共用同一个上游接口 POST /api/v3/tts/create：
//   - /v1/audio/generations：原生请求体原样透传（只替换 model），原样返回上游 JSON，
//     调用方可拿到时长、字幕与音频地址；
//   - /v1/audio/speech：OpenAI 兼容入口，字段换算后返回二进制音频。
//
// 计费按上游回报的 original_duration（模型原始输出时长，变速不影响），
// 预扣时按单次上限估算，见 controller/relay_media_price.go。
package volcengine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SpeechBaseURL 是豆包语音（openspeech）的默认地址。语音与方舟不是同一套服务和密钥，
// 渠道沿用方舟默认地址时改走这里；填了自定义地址则按自定义地址转发。
const SpeechBaseURL = "https://openspeech.bytedance.com"

// speechSuccessCode 是豆包语音 v3 接口的成功状态码。
const speechSuccessCode = 20000000

func ResolveSpeechBaseURL(baseURL string) string {
	if baseURL == "" || baseURL == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
		return SpeechBaseURL
	}
	return strings.TrimSuffix(baseURL, "/")
}

// SetupSpeechRequestHeader 设置豆包语音 v3 接口的鉴权头。上游要求每次请求都带
// X-Api-Request-Id（文档标为可选，实测缺失会返回 55000000）。
func SetupSpeechRequestHeader(header *http.Header, apiKey string) {
	header.Set("Content-Type", "application/json")
	header.Set("X-Api-Key", apiKey)
	header.Set("X-Api-Request-Id", uuid.NewString())
	header.Del("Authorization")
}

func isSeedAudioModel(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(modelName), "seed-audio")
}

func usesSeedAudio(info *relaycommon.RelayInfo) bool {
	if info.RelayMode == constant.RelayModeAudioGeneration {
		return true
	}
	return info.RelayMode == constant.RelayModeAudioSpeech && isSeedAudioModel(info.UpstreamModelName)
}

// seedAudioFormats 把 OpenAI response_format 映射到上游 audio_config.format；
// aac 与 flac 上游不支持，直接拒绝，避免返回和声明不符的音频。
var seedAudioFormats = map[string]string{
	"":     "mp3",
	"mp3":  "mp3",
	"wav":  "wav",
	"pcm":  "pcm",
	"opus": "ogg_opus",
}

// invalidSeedAudioRequest 标记调用方参数错误：返回 400 且不换渠道重试。
func invalidSeedAudioRequest(err error) error {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// convertSeedAudioRequest 构造上游请求体。原生入口透传全部字段；兼容入口由
// input/voice/response_format/speed 换算，metadata 里的原生字段覆盖换算结果
// （audio_config 按字段合并）。
func convertSeedAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	body := map[string]any{}
	if info.RelayMode == constant.RelayModeAudioGeneration {
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return nil, err
		}
	} else {
		if strings.TrimSpace(request.Input) == "" {
			return nil, invalidSeedAudioRequest(errors.New("input is required"))
		}
		if n := utf8.RuneCountInString(request.Input); n > relaycommon.MaxAudioGenerationPromptChars {
			return nil, invalidSeedAudioRequest(fmt.Errorf("input must be at most %d characters, got %d", relaycommon.MaxAudioGenerationPromptChars, n))
		}
		format, ok := seedAudioFormats[strings.ToLower(request.ResponseFormat)]
		if !ok {
			return nil, invalidSeedAudioRequest(fmt.Errorf("response_format %q is not supported, use mp3, wav, pcm or opus", request.ResponseFormat))
		}
		c.Set(contextKeyResponseFormat, format)
		audioConfig := map[string]any{"format": format}
		if request.Speed != nil {
			// OpenAI 的倍速换算为上游的 speech_rate：-50 为 0.5 倍，100 为 2 倍。
			rate := math.Round((*request.Speed - 1) * 100)
			audioConfig["speech_rate"] = int(math.Max(-50, math.Min(100, rate)))
		}
		body["text_prompt"] = request.Input
		body["audio_config"] = audioConfig
		if request.Voice != "" {
			body["references"] = []map[string]any{{"speaker": request.Voice}}
		}
		if len(request.Metadata) > 0 {
			overrides := map[string]any{}
			if err := common.Unmarshal(request.Metadata, &overrides); err != nil {
				return nil, invalidSeedAudioRequest(fmt.Errorf("metadata must be a JSON object: %w", err))
			}
			for key, value := range overrides {
				override, isObject := value.(map[string]any)
				if key == "audio_config" && isObject {
					for field, fieldValue := range override {
						audioConfig[field] = fieldValue
					}
					continue
				}
				body[key] = value
			}
			if format, ok := audioConfig["format"].(string); ok {
				c.Set(contextKeyResponseFormat, format)
			}
		}
	}
	body["model"] = info.UpstreamModelName

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

type seedAudioResponse struct {
	Code             int             `json:"code"`
	Message          string          `json:"message"`
	Audio            string          `json:"audio"`
	Duration         float64         `json:"duration"`
	OriginalDuration float64         `json:"original_duration"`
	URL              string          `json:"url"`
	Subtitle         json.RawMessage `json:"subtitle,omitempty"`
}

func handleSeedAudioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream seedAudioResponse
	if err := common.Unmarshal(body, &upstream); err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to parse seed-audio response: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	// 成功响应体里没有 code 字段，状态码只在 X-Api-Status-Code 头里。
	if upstream.Audio == "" || (upstream.Code != 0 && upstream.Code != speechSuccessCode) {
		message := upstream.Message
		if message == "" {
			message = "seed-audio returned no audio"
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("seed-audio error %d: %s", upstream.Code, message), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	// 只有按秒计价的价格矩阵会在预扣时放入 seconds 乘数；按次或按倍率计价的模型
	// 不能再乘时长。上游时长同样钳制在单次上限内，防止异常值放大计费。
	if info.PriceData.HasOtherRatio("seconds") {
		seconds := upstream.OriginalDuration
		if seconds <= 0 {
			seconds = upstream.Duration
		}
		if seconds > relaycommon.MaxAudioGenerationSeconds {
			common.SysError(fmt.Sprintf("seed-audio original_duration %v exceeds %d seconds, clamped", seconds, relaycommon.MaxAudioGenerationSeconds))
			seconds = relaycommon.MaxAudioGenerationSeconds
		}
		info.PriceData.AddOtherRatio("seconds", seconds)
	}

	if info.RelayMode == constant.RelayModeAudioGeneration {
		c.Data(http.StatusOK, "application/json", body)
	} else {
		audio, err := base64.StdEncoding.DecodeString(upstream.Audio)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(errors.New("failed to decode seed-audio audio"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		contentType := getContentTypeByEncoding(c.GetString(contextKeyResponseFormat))
		c.Data(http.StatusOK, contentType, audio)
	}

	promptTokens := max(info.GetEstimatePromptTokens(), 1)
	return &dto.Usage{
		PromptTokens: promptTokens,
		TotalTokens:  promptTokens,
	}, nil
}
