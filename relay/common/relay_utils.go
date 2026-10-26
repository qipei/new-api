package common

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type HasPrompt interface {
	GetPrompt() string
}

type HasImage interface {
	HasImage() bool
}

func GetFullRequestURL(baseURL string, requestURL string, channelType int) string {
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		switch channelType {
		case constant.ChannelTypeOpenAI:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/v1"))
		case constant.ChannelTypeAzure:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
		}
	}
	return fullRequestURL
}

func SanitizeURLForLog(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	query := parsedURL.Query()
	if len(query) == 0 {
		return rawURL
	}

	changed := false
	for key := range query {
		if isSensitiveURLQueryKey(key) {
			query.Set(key, "***masked***")
			changed = true
		}
	}
	if !changed {
		return rawURL
	}

	parsedURL.RawQuery = query.Encode()
	return parsedURL.String()
}

func isSensitiveURLQueryKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "key",
		"api_key",
		"api-key",
		"apikey",
		"x-api-key",
		"access_token",
		"refresh_token",
		"id_token",
		"token",
		"authorization",
		"auth",
		"client_secret",
		"secret",
		"password",
		"passwd",
		"signature",
		"sig",
		"awsaccesskeyid",
		"x-amz-credential",
		"x-amz-security-token",
		"x-amz-signature":
		return true
	}
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature")
}

func GetAPIVersion(c *gin.Context) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion == "" {
		apiVersion = c.GetString("api_version")
	}
	return apiVersion
}

func createTaskError(err error, code string, statusCode int, localError bool) *dto.TaskError {
	return &dto.TaskError{
		Code:       code,
		Message:    err.Error(),
		StatusCode: statusCode,
		LocalError: localError,
		Error:      err,
	}
}

func storeTaskRequest(c *gin.Context, info *RelayInfo, action string, requestObj TaskSubmitReq) {
	info.Action = action
	c.Set("task_request", requestObj)
}
func GetTaskRequest(c *gin.Context) (TaskSubmitReq, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return TaskSubmitReq{}, fmt.Errorf("request not found in context")
	}
	req, ok := v.(TaskSubmitReq)
	if !ok {
		return TaskSubmitReq{}, fmt.Errorf("invalid task request type")
	}
	return req, nil
}

func validatePrompt(prompt string) *dto.TaskError {
	if strings.TrimSpace(prompt) == "" {
		return createTaskError(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest, true)
	}
	return nil
}

// MaxTaskDurationSeconds caps user-supplied video duration. Duration is used
// as a billing multiplier (OtherRatio "seconds"); an unbounded value could
// overflow quota calculation into a negative charge.
const MaxTaskDurationSeconds = 3600

// ClampUsageSeconds 把上游用量里报告的秒数收进 [0, MaxTaskDurationSeconds]：它是计费
// 乘数，异常值（NaN、负数、极大值）不能放大一次结算。
func ClampUsageSeconds(seconds float64) float64 {
	if math.IsNaN(seconds) || seconds <= 0 {
		return 0
	}
	return math.Min(seconds, float64(MaxTaskDurationSeconds))
}

// CUSTOM: 语音合成与音频生成的计费边界（fork 扩展），取值来自豆包、百炼官方文档与实测。
const (
	// MaxAudioGenerationSeconds 音频生成单次输出的时长上限。按秒计费的音频模型
	// 用它做预扣估算；结算时上游回报的时长也钳制在它以内，异常时长放大不了计费乘数。
	MaxAudioGenerationSeconds = 120
	// MaxAudioGenerationPromptChars 音频生成 text_prompt 的字符上限
	// （豆包 seed-audio 与百炼 qwen-audio-tts-next 相同）。
	MaxAudioGenerationPromptChars = 3000
	// MaxSpeechSynthesisTextChars 百炼 Qwen-Audio-TTS 同步合成单次文本的字符上限
	// （实测超过即返回 400）。
	MaxSpeechSynthesisTextChars = 20000
	// MaxSpeechTaskTextChars 异步长文本语音合成单次输入的字符上限。字符数是按字
	// 计费的乘数，超限请求在提交前以 400 拒绝。
	MaxSpeechTaskTextChars = 100000
)

// smartDurationModels 是把 duration=-1 当作「智能时长」的模型。通用校验默认把
// 负数挡在门外——时长是计费乘数，无上限的值可能把额度算成负数——所以支持这个语义
// 的模型要在这里点名。
//
// 这里读的是请求里的模型名而不是映射后的上游名：模型映射在校验之后才发生，此处
// 拿不到。渠道若把别名映射到这些模型，别名也要一并写进来。
func modelAllowsSmartDuration(model string) bool {
	return strings.HasPrefix(strings.TrimSpace(model), "wan3.0-video")
}

func validateTaskDurationBounds(req TaskSubmitReq, allowAutoDuration bool) *dto.TaskError {
	allowAutoDuration = allowAutoDuration || modelAllowsSmartDuration(req.Model)
	seconds := req.Duration
	if seconds == 0 && req.Seconds != "" {
		seconds, _ = strconv.Atoi(req.Seconds)
	}
	if allowAutoDuration && seconds == -1 {
		return nil
	}
	if seconds < 0 || seconds > MaxTaskDurationSeconds {
		return createTaskError(fmt.Errorf("seconds must be between 1 and %d", MaxTaskDurationSeconds), "invalid_seconds", http.StatusBadRequest, true)
	}
	return nil
}

func validateMultipartTaskRequest(c *gin.Context, info *RelayInfo, action string) (TaskSubmitReq, error) {
	var req TaskSubmitReq
	if _, err := c.MultipartForm(); err != nil {
		return req, err
	}

	formData := c.Request.PostForm
	req = TaskSubmitReq{
		Prompt:   formData.Get("prompt"),
		Model:    formData.Get("model"),
		Mode:     formData.Get("mode"),
		Image:    formData.Get("image"),
		Video:    formData.Get("video"),
		Audio:    formData.Get("audio"),
		Size:     formData.Get("size"),
		Metadata: make(map[string]interface{}),
	}

	durationStr := formData.Get("seconds")
	if durationStr == "" {
		durationStr = formData.Get("duration")
	}
	if durationStr != "" {
		if duration, err := strconv.Atoi(durationStr); err == nil {
			req.Duration = duration
		}
	}

	if images := formData["images"]; len(images) > 0 {
		req.Images = images
	}
	if audios := formData["audios"]; len(audios) > 0 {
		req.Audios = audios
	}

	for key, values := range formData {
		if len(values) > 0 && !isKnownTaskField(key) {
			if intVal, err := strconv.Atoi(values[0]); err == nil {
				req.Metadata[key] = intVal
			} else if floatVal, err := strconv.ParseFloat(values[0], 64); err == nil {
				req.Metadata[key] = floatVal
			} else {
				req.Metadata[key] = values[0]
			}
		}
	}
	return req, nil
}

func ValidateMultipartDirect(c *gin.Context, info *RelayInfo) *dto.TaskError {
	var prompt string
	var model string
	var seconds int
	var size string
	var hasInputReference bool

	var req TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_json", http.StatusBadRequest, true)
	}

	prompt = req.Prompt
	model = req.Model
	size = req.Size
	seconds, _ = strconv.Atoi(req.Seconds)
	if seconds == 0 {
		seconds = req.Duration
	}
	if req.InputReference != "" {
		req.Images = []string{req.InputReference}
	} else if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		// 兼容单图上传
		req.Images = []string{strings.TrimSpace(req.Image)}
	}

	if strings.TrimSpace(req.Model) == "" {
		return createTaskError(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest, true)
	}

	if req.HasImage() || req.HasVideo() {
		hasInputReference = true
	}

	// 原生 content 数组里的 text 项同样是提示词（火山、MiniMax 官方的透传写法）。
	if !nativeContentHasText(req) {
		if taskErr := validatePrompt(prompt); taskErr != nil {
			return taskErr
		}
	}

	if taskErr := validateTaskDurationBounds(req, false); taskErr != nil {
		return taskErr
	}

	if taskErr := validateTaskRoleMedia(req); taskErr != nil {
		return taskErr
	}

	action := constant.TaskActionTextGenerate
	if hasInputReference {
		action = constant.TaskActionGenerate
	}
	if strings.HasPrefix(model, "sora-2") {

		if size == "" {
			size = "720x1280"
		}

		if seconds <= 0 {
			seconds = 4
		}

		if model == "sora-2" && !lo.Contains([]string{"720x1280", "1280x720"}, size) {
			return createTaskError(fmt.Errorf("sora-2 size is invalid"), "invalid_size", http.StatusBadRequest, true)
		}
		if model == "sora-2-pro" && !lo.Contains([]string{"720x1280", "1280x720", "1792x1024", "1024x1792"}, size) {
			return createTaskError(fmt.Errorf("sora-2 size is invalid"), "invalid_size", http.StatusBadRequest, true)
		}
		// OtherRatios 已移到 Sora adaptor 的 EstimateBilling 中设置
	}

	storeTaskRequest(c, info, action, req)

	return nil
}

func isKnownTaskField(field string) bool {
	knownFields := map[string]bool{
		"prompt":          true,
		"model":           true,
		"mode":            true,
		"image":           true,
		"images":          true,
		"video":           true,
		"audio":           true,
		"audios":          true,
		"size":            true,
		"duration":        true,
		"seconds":         true,
		"input_reference": true, // Sora 特有字段
	}
	return knownFields[field]
}

func ValidateBasicTaskRequest(c *gin.Context, info *RelayInfo, action string) *dto.TaskError {
	return validateTaskRequest(c, info, action, false, false)
}

// ValidateMediaTaskRequest accepts media-only requests and the provider's -1
// automatic-duration sentinel. Providers using it must still enforce their
// own model-specific media combinations and duration ranges.
func ValidateMediaTaskRequest(c *gin.Context, info *RelayInfo, action string) *dto.TaskError {
	return validateTaskRequest(c, info, action, true, true)
}

func validateTaskRequest(c *gin.Context, info *RelayInfo, action string, allowMediaOnly, allowAutoDuration bool) *dto.TaskError {
	var err error
	contentType := c.GetHeader("Content-Type")
	var req TaskSubmitReq
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req, err = validateMultipartTaskRequest(c, info, action)
		if err != nil {
			return createTaskError(err, "invalid_multipart_form", http.StatusBadRequest, true)
		}
	}
	// 为了metadata字段的兼容性，统一UnmarshalBodyReusable
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_request", http.StatusBadRequest, true)
	}

	if strings.TrimSpace(req.Prompt) == "" && !nativeContentHasText(req) {
		if !allowMediaOnly || !taskRequestHasMedia(req) {
			if taskErr := validatePrompt(req.Prompt); taskErr != nil {
				return taskErr
			}
		}
	}

	if taskErr := validateTaskDurationBounds(req, allowAutoDuration); taskErr != nil {
		return taskErr
	}

	if taskErr := validateTaskRoleMedia(req); taskErr != nil {
		return taskErr
	}

	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		// 兼容单图上传
		req.Images = []string{req.Image}
	}

	storeTaskRequest(c, info, action, req)
	return nil
}

// 按用途区分的素材字段的通用上限，取各模型里最宽的（Seedance 2.5：30 张图、10 条视频、
// 10 条音频）。这是网关的兜底，各模型更严格的数量和组合规则由对应渠道的适配器校验。
const (
	maxTaskReferenceImages = 30
	maxTaskReferenceVideos = 10
	maxTaskReferenceAudios = 10
)

// validateTaskRoleMedia 校验首帧/尾帧/参考图/参考视频/参考音频字段：不能和
// image/images/video/audio(s) 或原生 metadata.content 混用（两种写法的角色推断不同，
// 混用时无法确定用户意图），数量不能超过通用上限。
func validateTaskRoleMedia(req TaskSubmitReq) *dto.TaskError {
	if req.roleMedia(false).IsEmpty() {
		return nil
	}
	if req.MixesMediaStyles() {
		return createTaskError(fmt.Errorf("use either image/images/input_reference or first_frame_url/last_frame_url/reference_images for images, not both"), "invalid_request", http.StatusBadRequest, true)
	}
	if _, hasNativeContent := req.NativeContent(); hasNativeContent {
		return createTaskError(fmt.Errorf("content already lists the media; do not combine it with first_frame_url/last_frame_url/reference_images/video_urls/audio_urls"), "invalid_request", http.StatusBadRequest, true)
	}
	media := req.RoleMedia()
	switch {
	case len(media.ReferenceImages) > maxTaskReferenceImages:
		return createTaskError(fmt.Errorf("reference_images accepts at most %d items, got %d", maxTaskReferenceImages, len(media.ReferenceImages)), "invalid_request", http.StatusBadRequest, true)
	case len(media.ReferenceVideos) > maxTaskReferenceVideos:
		return createTaskError(fmt.Errorf("video_urls accepts at most %d items, got %d", maxTaskReferenceVideos, len(media.ReferenceVideos)), "invalid_request", http.StatusBadRequest, true)
	case len(media.ReferenceAudios) > maxTaskReferenceAudios:
		return createTaskError(fmt.Errorf("audio_urls accepts at most %d items, got %d", maxTaskReferenceAudios, len(media.ReferenceAudios)), "invalid_request", http.StatusBadRequest, true)
	}
	return nil
}

// nativeContentHasText 原生 content 数组里是否有非空的 text 项。
func nativeContentHasText(req TaskSubmitReq) bool {
	content, _ := req.NativeContent()
	for _, rawItem := range content {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		if typeName, _ := item["type"].(string); typeName != "text" {
			continue
		}
		if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

func taskRequestHasMedia(req TaskSubmitReq) bool {
	if req.HasImage() || strings.TrimSpace(req.Image) != "" || req.HasVideo() || req.HasAudio() {
		return true
	}
	content, ok := req.NativeContent()
	if !ok {
		return false
	}
	for _, rawItem := range content {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		typeName, _ := item["type"].(string)
		switch typeName {
		case "image_url", "video_url", "audio_url", "draft_task":
			return true
		case "text":
			text, _ := item["text"].(string)
			if strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}
