// CUSTOM: 百炼 MiniMax 同步语音合成（fork 扩展）。
// 文档：https://docs.bailian.console.aliyun.com/zh/model-studio/minimax-synchronous-speech-synthesis-api
//
// 模型 MiniMax/speech-2.8-hd、speech-02-hd、speech-2.8-turbo、speech-02-turbo 走
// POST /api/v1/services/aigc/multimodal-generation/generation，参数都在 input 下：
// text、voice_setting（voice_id 必填）、audio_setting、pronunciation_dict 等。
// 非流式响应把音频以 hex 编码放在 output.data.audio（output_format=url 时是 24 小时
// 有效的链接），状态在 output.base_resp，计费字符数在 usage.characters（汉字计 2 个）。
//
// 两个入口与 Qwen-Audio-TTS 相同：
//   - /v1/audio/generations：原生请求体原样透传（只替换 model），原样返回上游 JSON；
//   - /v1/audio/speech：OpenAI 兼容入口，字段换算后由网关解码音频，直接返回音频文件。
package ali

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
)

// maxMiniMaxSpeechTextChars 是 MiniMax 同步合成单次文本的上限（文档：小于 10000 字符）。
// 结算时上游回报的计费字符数（汉字计 2 个）钳制在它的两倍以内。
const maxMiniMaxSpeechTextChars = 10000

// miniMaxSpeechFormats 把 OpenAI response_format 映射到上游 audio_setting.format。
// 上游不支持 opus/aac，直接拒绝，避免返回与声明不符的音频。
var miniMaxSpeechFormats = map[string]string{
	"":     "mp3",
	"mp3":  "mp3",
	"wav":  "wav",
	"pcm":  "pcm",
	"flac": "flac",
}

func isMiniMaxSpeechModel(modelName string) bool {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(modelName)), "minimax/")
	return strings.HasPrefix(name, "speech-")
}

func convertMiniMaxSpeechRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	body := map[string]any{}
	if info.RelayMode == constant.RelayModeAudioGeneration {
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return nil, err
		}
		input, _ := body["input"].(map[string]any)
		text, _ := input["text"].(string)
		if n := utf8.RuneCountInString(text); n >= maxMiniMaxSpeechTextChars {
			return nil, invalidSpeechRequest(fmt.Errorf("input.text must be less than %d characters, got %d", maxMiniMaxSpeechTextChars, n))
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
	if n := utf8.RuneCountInString(request.Input); n >= maxMiniMaxSpeechTextChars {
		return nil, invalidSpeechRequest(fmt.Errorf("input must be less than %d characters, got %d", maxMiniMaxSpeechTextChars, n))
	}
	if request.Voice == "" {
		return nil, invalidSpeechRequest(errors.New("voice is required"))
	}
	if request.Instructions != "" {
		return nil, invalidSpeechRequest(errors.New("instructions is not supported by MiniMax speech; set metadata.voice_setting.emotion instead"))
	}
	format, ok := miniMaxSpeechFormats[strings.ToLower(request.ResponseFormat)]
	if !ok {
		return nil, invalidSpeechRequest(fmt.Errorf("response_format %q is not supported by %s", request.ResponseFormat, info.UpstreamModelName))
	}

	voiceSetting := map[string]any{"voice_id": request.Voice}
	if request.Speed != nil {
		voiceSetting["speed"] = math.Max(0.5, math.Min(2, *request.Speed))
	}
	audioSetting := map[string]any{"format": format}
	input := map[string]any{
		"text":          request.Input,
		"voice_setting": voiceSetting,
		"audio_setting": audioSetting,
		"output_format": "hex",
	}
	if len(request.Metadata) > 0 {
		overrides := map[string]any{}
		if err := common.Unmarshal(request.Metadata, &overrides); err != nil {
			return nil, invalidSpeechRequest(fmt.Errorf("metadata must be a JSON object: %w", err))
		}
		// voice_setting、audio_setting 按字段合并，补充情感、音量等参数时不会丢掉
		// 由 voice、response_format 换算出来的字段；其他参数直接覆盖。
		for key, value := range overrides {
			nested, isObject := value.(map[string]any)
			switch {
			case key == "voice_setting" && isObject:
				for k, v := range nested {
					voiceSetting[k] = v
				}
			case key == "audio_setting" && isObject:
				for k, v := range nested {
					audioSetting[k] = v
				}
			default:
				input[key] = value
			}
		}
	}
	if format, ok := audioSetting["format"].(string); ok {
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

type miniMaxSpeechResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  struct {
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		Data *struct {
			Audio string `json:"audio"`
		} `json:"data"`
		ExtraInfo struct {
			AudioLength int64 `json:"audio_length"`
		} `json:"extra_info"`
	} `json:"output"`
	Usage struct {
		Characters int `json:"characters"`
		Count      int `json:"count"`
	} `json:"usage"`
}

// miniMaxSpeechErrorStatus 把 base_resp.status_code 换成网关返回的 HTTP 状态。上游出错时
// HTTP 状态仍是 200，只能看 base_resp。参数类错误（2013 参数不合法、2054 音色不存在、
// 1042 非法字符超过 10%）是调用方的问题，返回 400 且不重试；限流返回 429；其余按上游故障处理。
func miniMaxSpeechErrorStatus(code int) int {
	switch code {
	case 2013, 2054, 1042:
		return http.StatusBadRequest
	case 1002, 1039:
		return http.StatusTooManyRequests
	default:
		return http.StatusBadGateway
	}
}

func handleMiniMaxSpeechResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream miniMaxSpeechResponse
	if err := common.Unmarshal(body, &upstream); err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to parse minimax speech response: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if upstream.Code != "" {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("%s: %s", upstream.Code, upstream.Message), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if status := upstream.Output.BaseResp.StatusCode; status != 0 {
		httpStatus := miniMaxSpeechErrorStatus(status)
		apiErr := fmt.Errorf("minimax speech error %d: %s", status, upstream.Output.BaseResp.StatusMsg)
		if httpStatus == http.StatusBadRequest {
			return nil, types.NewErrorWithStatusCode(apiErr, types.ErrorCodeInvalidRequest, httpStatus, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewErrorWithStatusCode(apiErr, types.ErrorCodeBadResponse, httpStatus)
	}
	if upstream.Output.Data == nil || upstream.Output.Data.Audio == "" {
		return nil, types.NewErrorWithStatusCode(errors.New("minimax speech returned no audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	// 按字计价的价格矩阵在预扣时放入了 chars_10k 乘数，这里换成上游的计费字符数，
	// 钳制在单次上限内（汉字计 2 个，最多是文本上限的两倍）。
	if info.PriceData.HasOtherRatio(video_billing.CharsRatioKey) && upstream.Usage.Characters > 0 {
		chars := upstream.Usage.Characters
		if limit := 2 * maxMiniMaxSpeechTextChars; chars > limit {
			common.SysError(fmt.Sprintf("minimax speech characters %d exceeds %d, clamped", chars, limit))
			chars = limit
		}
		info.PriceData.AddOtherRatio(video_billing.CharsRatioKey, float64(chars)/10000)
	}
	// 复刻音色的解锁费只在首次使用时由上游收取一次。网关不开放复刻接口，用户请求里
	// 不应出现复刻音色；真出现了只记日志，不向用户加收。
	if upstream.Usage.Count > 0 {
		common.SysError(fmt.Sprintf("minimax speech reported %d cloned voice unlock(s) for model %s", upstream.Usage.Count, info.UpstreamModelName))
	}

	audio := upstream.Output.Data.Audio
	switch {
	case info.RelayMode == constant.RelayModeAudioGeneration:
		c.Data(http.StatusOK, "application/json", body)
	case strings.HasPrefix(audio, "http://") || strings.HasPrefix(audio, "https://"):
		if apiErr := streamSpeechAudio(c, info, audio); apiErr != nil {
			return nil, apiErr
		}
	default:
		data, err := hex.DecodeString(audio)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("decode minimax speech audio failed: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		contentType := speechContentTypes[c.GetString(contextKeySpeechFormat)]
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		c.Data(http.StatusOK, contentType, data)
	}

	// 没有 token 用量，仍要留下可计费的用量，否则结算会把额度清零。
	promptTokens := max(info.GetEstimatePromptTokens(), 1)
	return &dto.Usage{PromptTokens: promptTokens, TotalTokens: promptTokens}, nil
}
