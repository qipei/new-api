package ali

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoMedia describes Wan2.7 image-to-video media inputs.
type AliVideoMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type AliVideoMultiPrompt struct {
	Index    int    `json:"index"`
	Prompt   string `json:"prompt"`
	Duration int    `json:"duration"`
}

type AliVideoElement struct {
	ElementID int `json:"element_id"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string          `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string          `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string          `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string          `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string          `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	Media          []AliVideoMedia `json:"media,omitempty"`           // 媒体列表（wan2.7-i2v新协议）
	NegativePrompt string          `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string          `json:"template,omitempty"`        // 视频特效模板
	// Kling v3 fields.
	KeepOriginalSound *string               `json:"keep_original_sound,omitempty"`
	MultiShot         *bool                 `json:"multi_shot,omitempty"`
	ShotType          *string               `json:"shot_type,omitempty"`
	MultiPrompt       []AliVideoMultiPrompt `json:"multi_prompt,omitempty"`
	ElementList       []AliVideoElement     `json:"element_list,omitempty"`
}

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
	Mode         *string `json:"mode,omitempty"`          // Kling 生成模式: std/pro
	AspectRatio  *string `json:"aspect_ratio,omitempty"`  // Kling 输出比例
	Ratio        *string `json:"ratio,omitempty"`         // HappyHorse 输出比例
	AudioSetting *string `json:"audio_setting,omitempty"` // HappyHorse 视频编辑声音控制
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

type aliFloatValue float64

func (value *aliFloatValue) UnmarshalJSON(data []byte) error {
	var number float64
	if err := common.Unmarshal(data, &number); err == nil {
		*value = aliFloatValue(number)
		return nil
	}
	var text string
	if err := common.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*value = aliFloatValue(parsed)
	return nil
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID            string `json:"task_id"`
	TaskStatus        string `json:"task_status"`
	SubmitTime        string `json:"submit_time,omitempty"`
	ScheduledTime     string `json:"scheduled_time,omitempty"`
	EndTime           string `json:"end_time,omitempty"`
	OrigPrompt        string `json:"orig_prompt,omitempty"`
	ActualPrompt      string `json:"actual_prompt,omitempty"`
	VideoURL          string `json:"video_url,omitempty"`
	WatermarkVideoURL string `json:"watermark_video_url,omitempty"`
	Code              string `json:"code,omitempty"`
	Message           string `json:"message,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	Duration            aliFloatValue `json:"duration,omitempty"`
	InputVideoDuration  aliFloatValue `json:"input_video_duration,omitempty"`
	OutputVideoDuration aliFloatValue `json:"output_video_duration,omitempty"`
	VideoCount          dto.IntValue  `json:"video_count,omitempty"`
	SR                  dto.IntValue  `json:"SR,omitempty"`
	Ratio               string        `json:"ratio,omitempty"`
	// ImageCount 是超出免费额度、需要计费的输入图张数（5 张以内返回 0，
	// 7 张返回 2）。不解析它就等于把这部分上游成本白送。
	ImageCount dto.IntValue `json:"image_count,omitempty"`
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string          `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string          `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string          `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string          `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	Media          []AliVideoMedia `json:"media,omitempty"`           // 媒体列表（wan2.7-i2v新协议）
	NegativePrompt string          `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string          `json:"template,omitempty"`        // 视频特效模板

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
	Ratio        *string `json:"ratio,omitempty"`         // HappyHorse 输出比例
	AudioSetting *string `json:"audio_setting,omitempty"` // HappyHorse 视频编辑声音控制
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	// ValidateMultipartDirect 负责解析并将原始 TaskSubmitReq 存入 context
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_ali_request_failed")
	}
	logger.LogJson(c, "ali video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	aliRatios := map[string]map[string]float64{
		"wan2.6-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.5-t2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-t2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.5-i2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-i2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.2-kf2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 4.8,
		},
		"wan2.2-i2v-flash": {
			"480P": 1,
			"720P": 2,
		},
		"wan2.2-s2v": {
			"480P": 1,
			"720P": 0.9 / 0.5,
		},
	}
	var resolution string

	// size match
	if aliReq.Parameters == nil {
		return otherRatios, nil
	}
	if size := pointerString(aliReq.Parameters.Size); size != "" {
		toResolution, err := sizeToResolution(size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(pointerString(aliReq.Parameters.Resolution))
		if resolution == "" {
			return otherRatios, nil
		}
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if otherRatio, ok := aliRatios[aliReq.Model]; ok {
		if ratio, ok := otherRatio[resolution]; ok {
			otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
		}
	}
	return otherRatios, nil
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func isWan27I2VModel(model string) bool {
	return strings.HasPrefix(model, "wan2.7-i2v")
}

func isWanVideoModel(model string) bool {
	return strings.HasPrefix(model, "wan")
}

func isAliKlingVideoModel(model string) bool {
	return strings.HasPrefix(model, "kling/kling-v3-")
}

func isAliKlingTurboVideoModel(model string) bool {
	return model == "kling/kling-v3-turbo-video-generation"
}

func isAliMiniMaxVideoModel(model string) bool {
	return strings.HasPrefix(model, "MiniMax/MiniMax-")
}

func isAliViduReferenceVideoModel(model string) bool {
	return strings.HasPrefix(model, "vidu/vidu") && strings.HasSuffix(model, "_reference2video")
}

func isAliViduDramaModel(model string) bool {
	return model == "vidu/viduq3-drama_reference2video"
}

func isAliHappyHorseModel(model string) bool {
	return strings.HasPrefix(model, "happyhorse-")
}

func isAliHappyHorseT2VModel(model string) bool {
	return model == "happyhorse-1.1-t2v" || model == "happyhorse-1.0-t2v"
}

func isAliHappyHorseI2VModel(model string) bool {
	return model == "happyhorse-1.1-i2v" || model == "happyhorse-1.0-i2v"
}

func isAliHappyHorseR2VModel(model string) bool {
	return model == "happyhorse-1.1-r2v" || model == "happyhorse-1.0-r2v"
}

func isAliHappyHorseVideoEditModel(model string) bool {
	return model == "happyhorse-1.0-video-edit"
}

func isHappyHorseRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "1:1", "4:3", "3:4", "4:5", "5:4", "9:21", "21:9":
		return true
	default:
		return false
	}
}

func happyHorseRatioFromDimensions(width, height int) string {
	a, b := width, height
	for b != 0 {
		a, b = b, a%b
	}
	return fmt.Sprintf("%d:%d", width/a, height/a)
}

func applyHappyHorseSize(parameters *AliVideoParameters, model, size string) error {
	normalized := strings.ToLower(strings.TrimSpace(size))
	if normalized == "" {
		return nil
	}
	if strings.Contains(normalized, ":") {
		if isAliHappyHorseI2VModel(model) || isAliHappyHorseVideoEditModel(model) {
			return fmt.Errorf("%s does not support parameters.ratio", model)
		}
		if !isHappyHorseRatio(normalized) {
			return fmt.Errorf("unsupported HappyHorse ratio %q", size)
		}
		parameters.Ratio = lo.ToPtr(normalized)
		return nil
	}
	if normalized == "480p" || normalized == "720p" || normalized == "1080p" {
		parameters.Resolution = lo.ToPtr(strings.ToUpper(normalized))
		return nil
	}

	parts := strings.Split(strings.ReplaceAll(normalized, "x", "*"), "*")
	if len(parts) != 2 {
		return fmt.Errorf("unsupported HappyHorse size %q; use 480p, 720p, 1080p, an allowed ratio, or width x height", size)
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return fmt.Errorf("invalid HappyHorse size %q", size)
	}
	shortEdge := min(width, height)
	resolution := "1080P"
	if shortEdge <= 480 {
		resolution = "480P"
	} else if shortEdge <= 720 {
		resolution = "720P"
	}
	parameters.Resolution = lo.ToPtr(resolution)
	if !isAliHappyHorseI2VModel(model) && !isAliHappyHorseVideoEditModel(model) {
		ratio := happyHorseRatioFromDimensions(width, height)
		if !isHappyHorseRatio(ratio) {
			return fmt.Errorf("HappyHorse does not support the %s aspect ratio derived from size %q", ratio, size)
		}
		parameters.Ratio = lo.ToPtr(ratio)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstTaskImage(req relaycommon.TaskSubmitReq) string {
	if image := strings.TrimSpace(req.Image); image != "" {
		return image
	}
	for _, image := range req.Images {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			return trimmed
		}
	}
	if inputReference := strings.TrimSpace(req.InputReference); inputReference != "" {
		return inputReference
	}
	return ""
}

func secondTaskImage(req relaycommon.TaskSubmitReq) string {
	nonEmptyImages := 0
	for _, image := range req.Images {
		trimmed := strings.TrimSpace(image)
		if trimmed == "" {
			continue
		}
		nonEmptyImages++
		if nonEmptyImages == 2 {
			return trimmed
		}
	}
	return ""
}

func taskImages(req relaycommon.TaskSubmitReq) []string {
	images := make([]string, 0, len(req.Images)+1)
	seen := make(map[string]struct{}, len(req.Images)+1)
	appendImage := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		images = append(images, value)
	}
	appendImage(req.Image)
	for _, image := range req.Images {
		appendImage(image)
	}
	appendImage(req.InputReference)
	return images
}

func normalizeAliThirdPartyMedia(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if !isAliKlingVideoModel(aliReq.Model) && !isAliViduReferenceVideoModel(aliReq.Model) &&
		!isAliMiniMaxVideoModel(aliReq.Model) {
		return nil
	}

	if len(aliReq.Input.Media) == 0 {
		images := taskImages(req)
		if isAliMiniMaxVideoModel(aliReq.Model) {
			// 带参考视频或音频时按多模态参考生视频组装，否则按首尾帧。
			// 两种组合互斥，混用会被上游拒绝。
			video := strings.TrimSpace(req.Video)
			audio := strings.TrimSpace(req.Audio)
			if video != "" || audio != "" || len(req.Audios) > 0 {
				for _, image := range images {
					aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "image_url", URL: image})
				}
				if video != "" {
					aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "feature", URL: video})
				}
				for _, item := range append([]string{audio}, req.Audios...) {
					if item = strings.TrimSpace(item); item != "" {
						aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "driving_audio", URL: item})
					}
				}
			} else if len(images) > 0 {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "first_frame", URL: images[0]})
				if len(images) > 1 {
					aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "last_frame", URL: images[1]})
				}
			}
		} else if isAliViduReferenceVideoModel(aliReq.Model) {
			for _, image := range images {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "image", URL: image})
			}
		} else if len(images) > 0 {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "first_frame", URL: images[0]})
			if len(images) > 1 {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "last_frame", URL: images[1]})
			}
		}
	}

	// Kling v3 and Vidu reference-to-video both use input.media. Legacy Wan
	// image fields are rejected by these third-party protocols.
	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	aliReq.Input.AudioURL = ""

	if isAliViduReferenceVideoModel(aliReq.Model) && len(aliReq.Input.Media) == 0 {
		return fmt.Errorf("%s requires at least one input.media item or image", aliReq.Model)
	}
	return nil
}

func normalizeAliHappyHorseRequest(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if !isAliHappyHorseModel(aliReq.Model) {
		return nil
	}
	if aliReq.Parameters == nil {
		aliReq.Parameters = &AliVideoParameters{}
	}
	parameters := aliReq.Parameters
	if size := pointerString(parameters.Size); size != "" {
		if err := applyHappyHorseSize(parameters, aliReq.Model, size); err != nil {
			return err
		}
		parameters.Size = nil
	}
	if parameters.Resolution == nil {
		parameters.Resolution = lo.ToPtr("1080P")
	} else {
		resolution := strings.ToUpper(strings.TrimSpace(pointerString(parameters.Resolution)))
		if !strings.HasSuffix(resolution, "P") {
			resolution += "P"
		}
		parameters.Resolution = lo.ToPtr(resolution)
	}
	if parameters.Ratio != nil {
		ratio := strings.TrimSpace(pointerString(parameters.Ratio))
		parameters.Ratio = lo.ToPtr(ratio)
	}
	if parameters.AudioSetting != nil {
		audioSetting := strings.ToLower(strings.TrimSpace(pointerString(parameters.AudioSetting)))
		parameters.AudioSetting = lo.ToPtr(audioSetting)
	}
	if parameters.Duration == nil && !isAliHappyHorseVideoEditModel(aliReq.Model) {
		parameters.Duration = lo.ToPtr(5)
	}

	if len(aliReq.Input.Media) == 0 {
		images := taskImages(req)
		switch {
		case isAliHappyHorseI2VModel(aliReq.Model):
			if len(images) > 1 {
				return fmt.Errorf("%s accepts exactly one input image", aliReq.Model)
			}
			if len(images) > 0 {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "first_frame", URL: images[0]})
			}
		case isAliHappyHorseR2VModel(aliReq.Model):
			for _, image := range images {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "reference_image", URL: image})
			}
		case isAliHappyHorseVideoEditModel(aliReq.Model):
			if video := strings.TrimSpace(req.Video); video != "" {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "video", URL: video})
			}
			for _, image := range images {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "reference_image", URL: image})
			}
		}
	}

	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	aliReq.Input.AudioURL = ""
	return validateAliHappyHorseRequest(aliReq, req)
}

func validateAliHappyHorseRequest(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if strings.TrimSpace(aliReq.Input.Prompt) == "" && !isAliHappyHorseI2VModel(aliReq.Model) {
		return fmt.Errorf("%s requires prompt", aliReq.Model)
	}
	parameters := aliReq.Parameters
	resolution := pointerString(parameters.Resolution)
	allowedResolution := resolution == "480P" || resolution == "720P" || resolution == "1080P"
	if isAliHappyHorseVideoEditModel(aliReq.Model) {
		allowedResolution = resolution == "720P" || resolution == "1080P"
	}
	if !allowedResolution {
		return fmt.Errorf("unsupported resolution %q for %s", resolution, aliReq.Model)
	}
	if parameters.Seed != nil && (*parameters.Seed < 0 || *parameters.Seed > 2147483647) {
		return fmt.Errorf("HappyHorse seed must be between 0 and 2147483647")
	}

	ratio := pointerString(parameters.Ratio)
	if ratio != "" {
		if isAliHappyHorseI2VModel(aliReq.Model) || isAliHappyHorseVideoEditModel(aliReq.Model) {
			return fmt.Errorf("%s does not support parameters.ratio", aliReq.Model)
		}
		if !isHappyHorseRatio(ratio) {
			return fmt.Errorf("unsupported HappyHorse ratio %q", ratio)
		}
	}
	audioSetting := pointerString(parameters.AudioSetting)
	if isAliHappyHorseVideoEditModel(aliReq.Model) {
		if parameters.Duration != nil {
			return fmt.Errorf("%s does not support parameters.duration", aliReq.Model)
		}
		if audioSetting != "" && audioSetting != "auto" && audioSetting != "origin" {
			return fmt.Errorf("HappyHorse parameters.audio_setting must be auto or origin")
		}
	} else {
		if audioSetting != "" {
			return fmt.Errorf("%s does not support parameters.audio_setting", aliReq.Model)
		}
		duration := pointerInt(parameters.Duration)
		if duration < 3 || duration > 15 {
			return fmt.Errorf("HappyHorse duration must be between 3 and 15 seconds")
		}
	}

	counts := mediaTypeCounts(aliReq.Input.Media)
	switch {
	case isAliHappyHorseT2VModel(aliReq.Model):
		if len(aliReq.Input.Media) != 0 || len(taskImages(req)) != 0 || req.HasVideo() {
			return fmt.Errorf("%s does not accept image or video media", aliReq.Model)
		}
	case isAliHappyHorseI2VModel(aliReq.Model):
		if len(aliReq.Input.Media) != 1 || counts["first_frame"] != 1 || req.HasVideo() {
			return fmt.Errorf("%s requires exactly one first_frame image", aliReq.Model)
		}
	case isAliHappyHorseR2VModel(aliReq.Model):
		if len(aliReq.Input.Media) < 1 || len(aliReq.Input.Media) > 9 || counts["reference_image"] != len(aliReq.Input.Media) || req.HasVideo() {
			return fmt.Errorf("%s requires 1 to 9 reference_image items", aliReq.Model)
		}
	case isAliHappyHorseVideoEditModel(aliReq.Model):
		if counts["video"] != 1 || counts["reference_image"] > 5 || counts["video"]+counts["reference_image"] != len(aliReq.Input.Media) {
			return fmt.Errorf("%s requires exactly one video and at most 5 reference_image items", aliReq.Model)
		}
	default:
		return fmt.Errorf("unsupported HappyHorse model %s", aliReq.Model)
	}
	return nil
}

func applyKlingSize(parameters *AliVideoParameters, size string) error {
	normalized := strings.ToLower(strings.TrimSpace(size))
	switch normalized {
	case "std", "720p":
		parameters.Mode = lo.ToPtr("std")
		return nil
	case "pro", "1080p":
		parameters.Mode = lo.ToPtr("pro")
		return nil
	case "16:9", "9:16", "1:1":
		parameters.AspectRatio = lo.ToPtr(normalized)
		return nil
	}

	type klingSize struct {
		mode  string
		ratio string
	}
	knownSizes := map[string]klingSize{
		"1280*720":  {mode: "std", ratio: "16:9"},
		"720*1280":  {mode: "std", ratio: "9:16"},
		"1280*1280": {mode: "std", ratio: "1:1"},
		"1920*1080": {mode: "pro", ratio: "16:9"},
		"1080*1920": {mode: "pro", ratio: "9:16"},
		"1920*1920": {mode: "pro", ratio: "1:1"},
	}
	mapped, ok := knownSizes[strings.ReplaceAll(normalized, "x", "*")]
	if !ok {
		return fmt.Errorf("unsupported Kling size %q; use 720p, 1080p, std, pro, 16:9, 9:16, or 1:1", size)
	}
	parameters.Mode = lo.ToPtr(mapped.mode)
	parameters.AspectRatio = lo.ToPtr(mapped.ratio)
	return nil
}

func mediaTypeCounts(media []AliVideoMedia) map[string]int {
	counts := make(map[string]int)
	for _, item := range media {
		counts[item.Type]++
	}
	return counts
}

func validateAliThirdPartyVideoRequest(aliReq *AliVideoRequest) error {
	if isAliKlingVideoModel(aliReq.Model) {
		return validateAliKlingRequest(aliReq)
	}
	if isAliViduReferenceVideoModel(aliReq.Model) {
		return validateAliViduRequest(aliReq)
	}
	if isAliMiniMaxVideoModel(aliReq.Model) {
		return validateAliMiniMaxRequest(aliReq)
	}
	return nil
}

// validateAliMiniMaxRequest 校验 MiniMax 视频生成请求。
// 图生视频（first_frame/last_frame）与多模态参考生视频（image_url/feature/
// driving_audio）互斥，上游不接受混用。
func validateAliMiniMaxRequest(aliReq *AliVideoRequest) error {
	parameters := aliReq.Parameters
	if resolution := strings.ToUpper(pointerString(parameters.Resolution)); resolution != "" &&
		resolution != "768P" && resolution != "2K" {
		return fmt.Errorf("MiniMax parameters.resolution must be 768P or 2K")
	}
	switch ratio := pointerString(parameters.Ratio); ratio {
	case "", "adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9":
	default:
		return fmt.Errorf("MiniMax parameters.ratio %s is unsupported", ratio)
	}
	if duration := pointerInt(parameters.Duration); duration < 4 || duration > 15 {
		return fmt.Errorf("MiniMax duration must be between 4 and 15 seconds")
	}
	if len([]rune(aliReq.Input.Prompt)) > 7000 {
		return fmt.Errorf("MiniMax prompt must not exceed 7000 characters")
	}

	counts := mediaTypeCounts(aliReq.Input.Media)
	for mediaType, count := range counts {
		if count <= 0 {
			continue
		}
		switch mediaType {
		case "first_frame", "last_frame", "image_url", "feature", "driving_audio":
		default:
			return fmt.Errorf("MiniMax does not support media type %s", mediaType)
		}
	}
	frameCount := counts["first_frame"] + counts["last_frame"]
	referenceCount := counts["image_url"] + counts["feature"] + counts["driving_audio"]
	if frameCount > 0 && referenceCount > 0 {
		return fmt.Errorf("MiniMax cannot combine first_frame/last_frame with reference media")
	}
	if counts["first_frame"] > 1 || counts["last_frame"] > 1 {
		return fmt.Errorf("MiniMax accepts at most one first frame and one last frame")
	}
	if counts["image_url"] > 9 {
		return fmt.Errorf("MiniMax accepts at most 9 reference images")
	}
	if counts["feature"] > 3 {
		return fmt.Errorf("MiniMax accepts at most 3 reference videos")
	}
	if counts["driving_audio"] > 3 {
		return fmt.Errorf("MiniMax accepts at most 3 reference audios")
	}
	// 文生视频必须指定具体比例；图生视频以首帧为准，ratio 会被上游忽略。
	if frameCount == 0 && referenceCount == 0 {
		if ratio := pointerString(parameters.Ratio); ratio == "" || ratio == "adaptive" {
			return fmt.Errorf("MiniMax text-to-video requires an explicit parameters.ratio")
		}
	}
	if strings.TrimSpace(aliReq.Input.Prompt) == "" {
		return fmt.Errorf("MiniMax prompt is required")
	}
	return nil
}

func validateAliKlingRequest(aliReq *AliVideoRequest) error {
	parameters := aliReq.Parameters
	isTurbo := isAliKlingTurboVideoModel(aliReq.Model)
	switch mode := pointerString(parameters.Mode); mode {
	case "", "std", "pro":
	case "4k":
		if isTurbo {
			return fmt.Errorf("%s does not support 4k mode", aliReq.Model)
		}
	default:
		return fmt.Errorf("Kling parameters.mode must be std, pro, or 4k")
	}
	if ratio := pointerString(parameters.AspectRatio); ratio != "" && ratio != "16:9" && ratio != "9:16" && ratio != "1:1" {
		return fmt.Errorf("Kling parameters.aspect_ratio must be 16:9, 9:16, or 1:1")
	}
	duration := pointerInt(parameters.Duration)
	if duration < 3 || duration > 15 {
		return fmt.Errorf("Kling duration must be between 3 and 15 seconds")
	}

	counts := mediaTypeCounts(aliReq.Input.Media)
	isOmni := aliReq.Model == "kling/kling-v3-omni-video-generation"
	for mediaType, count := range counts {
		if count <= 0 {
			continue
		}
		if isTurbo && mediaType != "first_frame" {
			return fmt.Errorf("%s only supports first_frame media", aliReq.Model)
		}
		if !isOmni && mediaType != "first_frame" && mediaType != "last_frame" {
			return fmt.Errorf("%s does not support media type %s", aliReq.Model, mediaType)
		}
		if isOmni && mediaType != "first_frame" && mediaType != "last_frame" && mediaType != "refer" && mediaType != "base" && mediaType != "feature" {
			return fmt.Errorf("unsupported Kling Omni media type %s", mediaType)
		}
	}
	if isTurbo && len(aliReq.Input.ElementList) > 0 {
		return fmt.Errorf("%s does not support element_list", aliReq.Model)
	}
	// 帧驱动场景（首帧/首尾帧）的主体上限是 3，与参考生视频的 7 不同。
	if counts["first_frame"] > 0 && counts["feature"] == 0 && counts["base"] == 0 &&
		len(aliReq.Input.ElementList) > 3 {
		return fmt.Errorf("Kling frame-driven generation accepts at most 3 elements")
	}
	if counts["first_frame"] > 1 || counts["last_frame"] > 1 || counts["base"] > 1 || counts["feature"] > 1 {
		return fmt.Errorf("Kling accepts at most one first frame, last frame, base video, and feature video")
	}
	if counts["last_frame"] > 0 && counts["first_frame"] == 0 {
		return fmt.Errorf("Kling last_frame requires first_frame")
	}
	if counts["base"] > 0 && (counts["feature"] > 0 || counts["first_frame"] > 0 || counts["last_frame"] > 0) {
		return fmt.Errorf("Kling base video cannot be combined with feature or frame media")
	}
	if counts["feature"] > 0 && counts["last_frame"] > 0 {
		return fmt.Errorf("Kling feature video cannot be combined with last_frame")
	}
	if counts["refer"]+len(aliReq.Input.ElementList) > 7 {
		return fmt.Errorf("Kling refer media plus element_list cannot exceed 7 items")
	}
	if (counts["feature"] > 0 || counts["base"] > 0) && counts["refer"]+len(aliReq.Input.ElementList) > 4 {
		return fmt.Errorf("Kling video reference plus refer media and element_list cannot exceed 4 items")
	}
	if counts["feature"] > 0 && duration > 10 {
		return fmt.Errorf("Kling feature-video duration must be between 3 and 10 seconds")
	}
	if parameters.Audio != nil && *parameters.Audio && (counts["base"] > 0 || counts["feature"] > 0) {
		return fmt.Errorf("Kling audio must be false when base or feature video is supplied")
	}
	if aliReq.Input.KeepOriginalSound != nil && counts["base"] == 0 && counts["feature"] == 0 {
		return fmt.Errorf("Kling keep_original_sound requires base or feature video")
	}
	if value := pointerString(aliReq.Input.KeepOriginalSound); value != "" && value != "yes" && value != "no" {
		return fmt.Errorf("Kling keep_original_sound must be yes or no")
	}

	multiShot := aliReq.Input.MultiShot != nil && *aliReq.Input.MultiShot
	shotType := pointerString(aliReq.Input.ShotType)
	if multiShot && shotType != "intelligence" && shotType != "customize" {
		return fmt.Errorf("Kling shot_type must be intelligence or customize when multi_shot is true")
	}
	if shotType == "customize" {
		if len(aliReq.Input.MultiPrompt) == 0 || len(aliReq.Input.MultiPrompt) > 6 {
			return fmt.Errorf("Kling customize mode requires 1 to 6 multi_prompt items")
		}
		for index, item := range aliReq.Input.MultiPrompt {
			if item.Index != index+1 || strings.TrimSpace(item.Prompt) == "" || item.Duration < 1 || item.Duration > duration {
				return fmt.Errorf("invalid Kling multi_prompt item at position %d", index+1)
			}
		}
	}
	if shotType != "customize" && strings.TrimSpace(aliReq.Input.Prompt) == "" {
		return fmt.Errorf("Kling prompt is required unless shot_type is customize")
	}
	return nil
}

func validateAliViduRequest(aliReq *AliVideoRequest) error {
	parameters := aliReq.Parameters
	resolution := strings.ToUpper(pointerString(parameters.Resolution))
	allowedResolutions := map[string]bool{"540P": true, "720P": true, "1080P": true}
	if isAliViduDramaModel(aliReq.Model) || aliReq.Model == "vidu/viduq3-ad_reference2video" || aliReq.Model == "vidu/viduq3-mix_reference2video" {
		delete(allowedResolutions, "540P")
	}
	if !allowedResolutions[resolution] {
		return fmt.Errorf("unsupported resolution %q for %s", resolution, aliReq.Model)
	}

	duration := pointerInt(parameters.Duration)
	minDuration, maxDuration := 1, 10
	allowAutoDuration := false
	switch aliReq.Model {
	case "vidu/viduq3-ad_reference2video":
		minDuration, maxDuration = 3, 15
	case "vidu/viduq3-drama_reference2video":
		minDuration, maxDuration = 2, 15
	case "vidu/viduq3-mix_reference2video", "vidu/viduq3_reference2video", "vidu/viduq3-turbo_reference2video":
		minDuration, maxDuration = 1, 16
	case "vidu/viduq2-pro_reference2video":
		allowAutoDuration = true
	}
	if (!allowAutoDuration || duration != 0) && (duration < minDuration || duration > maxDuration) {
		return fmt.Errorf("duration %d is unsupported for %s", duration, aliReq.Model)
	}
	if parameters.Seed != nil && (*parameters.Seed < 0 || *parameters.Seed > 2147483647) {
		return fmt.Errorf("Vidu seed must be between 0 and 2147483647")
	}

	counts := mediaTypeCounts(aliReq.Input.Media)
	if counts["image"] == 0 {
		return fmt.Errorf("%s requires at least one image media item", aliReq.Model)
	}
	if len(counts) > 1 && aliReq.Model != "vidu/viduq2-pro_reference2video" {
		return fmt.Errorf("%s only supports image media", aliReq.Model)
	}
	if counts["video"] > 0 {
		if aliReq.Model != "vidu/viduq2-pro_reference2video" || counts["video"] > 2 || counts["image"] > 4 {
			return fmt.Errorf("invalid Vidu reference image/video combination")
		}
	} else if counts["image"] > 7 {
		return fmt.Errorf("Vidu accepts at most 7 reference images")
	}
	for mediaType := range counts {
		if mediaType != "image" && mediaType != "video" {
			return fmt.Errorf("unsupported Vidu media type %s", mediaType)
		}
	}
	if parameters.Audio != nil {
		supportsAudio := aliReq.Model == "vidu/viduq3-ad_reference2video" || aliReq.Model == "vidu/viduq3-mix_reference2video" || aliReq.Model == "vidu/viduq3_reference2video" || aliReq.Model == "vidu/viduq3-turbo_reference2video"
		if !supportsAudio {
			return fmt.Errorf("%s does not support parameters.audio", aliReq.Model)
		}
	}
	return nil
}

func normalizeWan27I2VInput(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if !isWan27I2VModel(aliReq.Model) {
		return nil
	}

	if len(aliReq.Input.Media) == 0 {
		firstFrameURL := firstNonEmpty(aliReq.Input.FirstFrameURL, aliReq.Input.ImgURL, firstTaskImage(req))
		lastFrameURL := firstNonEmpty(aliReq.Input.LastFrameURL, secondTaskImage(req))
		audioURL := aliReq.Input.AudioURL

		if firstFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "first_frame",
				URL:  firstFrameURL,
			})
		}
		if lastFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "last_frame",
				URL:  lastFrameURL,
			})
		}
		if audioURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "driving_audio",
				URL:  audioURL,
			})
		}
	}

	if len(aliReq.Input.Media) == 0 {
		return fmt.Errorf("wan2.7-i2v requires image, images, input_reference, or input.media")
	}

	// Wan2.7 image-to-video uses the new input.media protocol. Avoid sending
	// legacy fields that belong to wan2.6 and earlier image-to-video APIs.
	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	aliReq.Input.AudioURL = ""
	return nil
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	upstreamModel := req.Model
	if info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}
	parameters := &AliVideoParameters{}
	if isWanVideoModel(upstreamModel) {
		parameters.PromptExtend = lo.ToPtr(true)
		parameters.Watermark = lo.ToPtr(false)
	}
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
			ImgURL: firstTaskImage(req),
		},
		Parameters: parameters,
	}

	// 处理分辨率映射
	if req.Size != "" {
		if isAliHappyHorseModel(upstreamModel) {
			if err := applyHappyHorseSize(parameters, upstreamModel, req.Size); err != nil {
				return nil, err
			}
		} else if isAliKlingVideoModel(upstreamModel) {
			if err := applyKlingSize(parameters, req.Size); err != nil {
				return nil, err
			}
		} else if strings.Contains(upstreamModel, "t2v") && !strings.Contains(req.Size, "*") {
			return nil, fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		} else if strings.Contains(req.Size, "*") {
			parameters.Size = lo.ToPtr(req.Size)
		} else {
			resolution := strings.ToUpper(req.Size)
			if !strings.HasSuffix(resolution, "P") {
				resolution = resolution + "P"
			}
			parameters.Resolution = lo.ToPtr(resolution)
		}
	} else {
		// 根据模型设置默认分辨率
		if isAliHappyHorseModel(upstreamModel) {
			parameters.Resolution = lo.ToPtr("1080P")
		} else if isAliViduReferenceVideoModel(upstreamModel) {
			resolution := "720P"
			if isAliViduDramaModel(upstreamModel) {
				resolution = "1080P"
			}
			parameters.Resolution = lo.ToPtr(resolution)
		} else if strings.Contains(upstreamModel, "t2v") {
			if strings.HasPrefix(upstreamModel, "wan2.5") || strings.HasPrefix(upstreamModel, "wan2.2") {
				parameters.Size = lo.ToPtr("1920*1080")
			} else {
				parameters.Size = lo.ToPtr("1280*720")
			}
		} else if isWanVideoModel(upstreamModel) {
			if strings.HasPrefix(upstreamModel, "wan2.6") || strings.HasPrefix(upstreamModel, "wan2.5") || strings.HasPrefix(upstreamModel, "wan2.2-i2v-plus") {
				parameters.Resolution = lo.ToPtr("1080P")
			} else {
				parameters.Resolution = lo.ToPtr("720P")
			}
		}
	}
	if isAliKlingVideoModel(upstreamModel) {
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode == "std" || mode == "pro" {
			parameters.Mode = lo.ToPtr(mode)
		}
	}

	// 处理时长
	if req.Duration > 0 {
		parameters.Duration = lo.ToPtr(req.Duration)
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		}
		parameters.Duration = lo.ToPtr(seconds)
	}
	if parameters.Duration == nil && !isAliHappyHorseVideoEditModel(upstreamModel) {
		parameters.Duration = lo.ToPtr(5)
	}

	// 从 metadata 中提取额外参数
	if req.Metadata != nil {
		if metadataBytes, err := common.Marshal(req.Metadata); err == nil {
			err = common.Unmarshal(metadataBytes, aliReq)
			if err != nil {
				return nil, errors.Wrap(err, "unmarshal metadata failed")
			}
		} else {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
	}

	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}

	if err := normalizeWan27I2VInput(aliReq, req); err != nil {
		return nil, err
	}
	if err := normalizeAliHappyHorseRequest(aliReq, req); err != nil {
		return nil, err
	}
	if err := normalizeAliThirdPartyMedia(aliReq, req); err != nil {
		return nil, err
	}
	if err := validateAliThirdPartyVideoRequest(aliReq); err != nil {
		return nil, err
	}

	return aliReq, nil
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil
	}
	if table, ok := video_billing.GetPriceTable(info.OriginModelName); ok {
		if table.Unit != video_billing.UnitPerSecond {
			return nil
		}
		seconds := pointerInt(aliReq.Parameters.Duration)
		if seconds <= 0 {
			seconds = 5
		}
		return map[string]float64{"seconds": float64(min(seconds, relaycommon.MaxTaskDurationSeconds))}
	}

	// metadata can override Duration past standard request validation;
	// cap it because it is used as a billing multiplier.
	otherRatios := map[string]float64{
		"seconds": float64(min(pointerInt(aliReq.Parameters.Duration), relaycommon.MaxTaskDurationSeconds)),
	}
	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return otherRatios
	}
	for k, v := range ratios {
		otherRatios[k] = v
	}
	return otherRatios
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return taskcommon.VideoMatrixQuotaOnComplete(task, taskResult)
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 返回 OpenAI 格式
	c.JSON(http.StatusOK, openAIResp)

	return aliResp.Output.TaskID, responseBody, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = aliResp.Output.VideoURL
		taskResult.RemoteUrl = aliResp.Output.WatermarkVideoURL
		if aliResp.Usage != nil {
			if count := int(aliResp.Usage.ImageCount); count > 0 {
				taskResult.BillableImageCount = count
			}
			duration := float64(aliResp.Usage.Duration)
			if duration > 0 {
				if duration > float64(relaycommon.MaxTaskDurationSeconds) {
					taskResult.Duration = relaycommon.MaxTaskDurationSeconds
					taskResult.BillingDuration = float64(relaycommon.MaxTaskDurationSeconds)
				} else {
					taskResult.Duration = int(math.Ceil(duration))
					taskResult.BillingDuration = duration
				}
			}
		}
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal ali response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	// 设置视频URL（核心字段）
	openAIResp.SetMetadata("url", aliResp.Output.VideoURL)
	if aliResp.Output.WatermarkVideoURL != "" {
		openAIResp.SetMetadata("watermark_url", aliResp.Output.WatermarkVideoURL)
	}

	// 错误处理
	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
