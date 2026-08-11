package doubao

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type      string     `json:"type,omitempty"`
	Text      string     `json:"text,omitempty"`
	ImageURL  *MediaURL  `json:"image_url,omitempty"`
	VideoURL  *MediaURL  `json:"video_url,omitempty"`
	AudioURL  *MediaURL  `json:"audio_url,omitempty"`
	DraftTask *DraftTask `json:"draft_task,omitempty"`
	Role      string     `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type DraftTask struct {
	ID string `json:"id,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	OmniReferenceTaskType string         `json:"omni_reference_task_type,omitempty"`
	OutputFormat          string         `json:"output_format,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	SafetyIdentifier string         `json:"safety_identifier,omitempty"`
	Priority         *dto.IntValue  `json:"priority,omitempty"`
	Resolution       string         `json:"resolution,omitempty"`
	Ratio            string         `json:"ratio,omitempty"`
	Duration         *dto.IntValue  `json:"duration,omitempty"`
	Frames           *dto.IntValue  `json:"frames,omitempty"`
	Seed             *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed      *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark        *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL     string `json:"video_url"`
		LastFrameURL string `json:"last_frame_url"`
		FileURL      string `json:"file_url"`
	} `json:"content"`
	Seed                  int    `json:"seed"`
	Resolution            string `json:"resolution"`
	Duration              int    `json:"duration"`
	Ratio                 string `json:"ratio"`
	FramesPerSecond       int    `json:"framespersecond"`
	ServiceTier           string `json:"service_tier"`
	ExecutionExpiresAfter int    `json:"execution_expires_after"`
	GenerateAudio         bool   `json:"generate_audio"`
	Draft                 bool   `json:"draft"`
	DraftTaskID           string `json:"draft_task_id"`
	Tools                 []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
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

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	if taskErr := relaycommon.ValidateMediaTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	payload, err := a.convertToRequestPayload(&req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	info.OriginModelName = req.Model
	info.UpstreamModelName = req.Model
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}
	payload.Model = info.UpstreamModelName
	if err := validateRequestPayload(payload, info.OriginModelName); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 根据请求 metadata 中的输出分辨率与是否包含视频输入，返回相对基准价的计费 OtherRatio。
// CUSTOM: 配置了 video_billing 价格矩阵的模型基础价已由矩阵接管（按 token 绝对价），
// 无需任何倍率；未配置的模型沿用内置价格表的相对倍率（与上游行为一致）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	payload, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil
	}
	ratios := make(map[string]float64)
	if payload.ServiceTier == "flex" {
		ratios["service_tier"] = 0.5
	}
	if table, ok := video_billing.GetPriceTable(info.OriginModelName); ok {
		if table.Unit == video_billing.UnitPerSecond {
			seconds := 5
			if payload.Duration != nil && int(*payload.Duration) > 0 {
				seconds = min(int(*payload.Duration), relaycommon.MaxTaskDurationSeconds)
			}
			ratios["seconds"] = float64(seconds)
		}
		if len(ratios) == 0 {
			return nil
		}
		return ratios
	}
	hasVideo := req.HasVideo() || contentHasMedia(req.Metadata, "video_url")
	resolution := req.Size
	if metadataResolution, ok := req.Metadata["resolution"].(string); ok && metadataResolution != "" {
		resolution = metadataResolution
	}
	if normalized, _ := normalizeVideoSize(resolution); normalized != "" {
		resolution = normalized
	}
	ratio, ok := legacyVideoInputRatio(info.OriginModelName, resolution, hasVideo)
	if ok && ratio != 1.0 {
		ratios["video_input"] = ratio
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

// AdjustBillingOnComplete 按 token 计费的矩阵模型在任务完成时用实际 usage 结算。
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	quota := taskcommon.VideoMatrixQuotaOnComplete(task, taskResult)
	if quota <= 0 || task == nil || task.PrivateData.BillingContext == nil {
		return quota
	}
	serviceTierRatio, ok := task.PrivateData.BillingContext.OtherRatios["service_tier"]
	if !ok || serviceTierRatio == 1 {
		return quota
	}
	adjustedQuota, clamp := common.QuotaFromFloatChecked(float64(quota) * serviceTierRatio)
	if clamp != nil {
		taskResult.QuotaClamp = clamp
	}
	return adjustedQuota
}

// contentHasMedia 直接检查 metadata 的 content 数组是否包含指定类型的媒体条目，
// 避免构建完整的上游 requestPayload。
func contentHasMedia(metadata map[string]interface{}, mediaType string) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == mediaType {
			return true
		}
		if _, has := itemMap[mediaType]; has {
			return true
		}
	}
	return false
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
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

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	if _, hasNativeContent := req.Metadata["content"]; !hasNativeContent {
		images := append([]string(nil), req.Images...)
		if len(images) == 0 && strings.TrimSpace(req.Image) != "" {
			images = append(images, strings.TrimSpace(req.Image))
		}
		for index, imgURL := range images {
			role := imageRole(req.Mode, index, len(images))
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
				Role: role,
			})
		}
		if strings.TrimSpace(req.Video) != "" {
			r.Content = append(r.Content, ContentItem{
				Type:     "video_url",
				VideoURL: &MediaURL{URL: strings.TrimSpace(req.Video)},
				Role:     "reference_video",
			})
		}
		audios := append([]string(nil), req.Audios...)
		if strings.TrimSpace(req.Audio) != "" {
			audios = append([]string{strings.TrimSpace(req.Audio)}, audios...)
		}
		for _, audioURL := range audios {
			r.Content = append(r.Content, ContentItem{
				Type:     "audio_url",
				AudioURL: &MediaURL{URL: audioURL},
				Role:     "reference_audio",
			})
		}
	}

	metadata := req.Metadata
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if r.OmniReferenceTaskType == "" {
		switch strings.ToLower(strings.TrimSpace(req.Mode)) {
		case "reference", "reference-to-video":
			r.OmniReferenceTaskType = "reference"
		case "video-edit", "edit":
			r.OmniReferenceTaskType = "edit"
		case "video-extend", "extend":
			r.OmniReferenceTaskType = "extend"
		}
	}

	duration := req.Duration
	if sec, err := strconv.Atoi(req.Seconds); err == nil && req.Seconds != "" {
		duration = sec
	}
	if duration != 0 {
		r.Duration = lo.ToPtr(dto.IntValue(duration))
	}

	if r.Resolution == "" || r.Ratio == "" {
		resolution, ratio := normalizeVideoSize(req.Size)
		if r.Resolution == "" {
			r.Resolution = resolution
		}
		if r.Ratio == "" {
			r.Ratio = ratio
		}
	}

	if strings.TrimSpace(req.Prompt) != "" {
		r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
		r.Content = append(r.Content, ContentItem{
			Type: "text",
			Text: req.Prompt,
		})
	}

	return &r, nil
}

func imageRole(mode string, index, count int) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "reference", "reference-to-video":
		return "reference_image"
	case "first-last-frame", "first-tail-frame":
		if index == 0 {
			return "first_frame"
		}
		if index == 1 {
			return "last_frame"
		}
		return ""
	}
	if count == 2 {
		if index == 0 {
			return "first_frame"
		}
		return "last_frame"
	}
	if count > 2 {
		return "reference_image"
	}
	return ""
}

func normalizeVideoSize(size string) (string, string) {
	normalized := strings.ToLower(strings.TrimSpace(size))
	switch normalized {
	case "480p", "720p", "1080p", "4k":
		return normalized, ""
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
		return "", normalized
	}
	normalized = strings.ReplaceAll(normalized, "*", "x")
	var width, height int
	if _, err := fmt.Sscanf(normalized, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		return "", ""
	}
	pixels := int64(width) * int64(height)
	resolution := "4k"
	switch {
	case pixels <= 450_000:
		resolution = "480p"
	case pixels <= 1_000_000:
		resolution = "720p"
	case pixels <= 2_200_000:
		resolution = "1080p"
	}

	aspect := float64(width) / float64(height)
	ratio := "16:9"
	bestDistance := math.MaxFloat64
	for _, candidate := range []struct {
		name  string
		value float64
	}{
		{"16:9", 16.0 / 9.0}, {"4:3", 4.0 / 3.0}, {"1:1", 1},
		{"3:4", 3.0 / 4.0}, {"9:16", 9.0 / 16.0}, {"21:9", 21.0 / 9.0},
	} {
		distance := math.Abs(aspect - candidate.value)
		if distance < bestDistance {
			bestDistance = distance
			ratio = candidate.name
		}
	}
	return resolution, ratio
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		if taskResult.Url == "" {
			taskResult.Url = resTask.Content.FileURL
		}
		taskResult.RemoteUrl = resTask.Content.LastFrameURL
		taskResult.Duration = resTask.Duration
		taskResult.BillingDuration = float64(resTask.Duration)
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed", "cancelled", "expired":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
		if taskResult.Reason == "" {
			taskResult.Reason = "task " + resTask.Status
		}
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	primaryURL := dResp.Content.VideoURL
	if primaryURL == "" {
		primaryURL = dResp.Content.FileURL
	}
	openAIVideo.SetMetadata("url", primaryURL)
	if dResp.Content.LastFrameURL != "" {
		openAIVideo.SetMetadata("last_frame_url", dResp.Content.LastFrameURL)
	}
	if dResp.Content.FileURL != "" {
		openAIVideo.SetMetadata("file_url", dResp.Content.FileURL)
	}
	if dResp.Resolution != "" {
		openAIVideo.SetMetadata("resolution", dResp.Resolution)
	}
	if dResp.Ratio != "" {
		openAIVideo.SetMetadata("ratio", dResp.Ratio)
	}
	if dResp.Duration > 0 {
		openAIVideo.Seconds = strconv.Itoa(dResp.Duration)
	}
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: dResp.Error.Message,
			Code:    dResp.Error.Code,
		}
	}

	return common.Marshal(openAIVideo)
}
