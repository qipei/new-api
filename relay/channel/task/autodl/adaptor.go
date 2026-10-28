// CUSTOM: autodl ComfyUI 工作流（fork 扩展）。
// 文档：https://autodl.art/docs/comfyui_api/
//
// autodl 把 ComfyUI 工作流包装成异步接口：POST /api/v1/comfyui/comfyui_workflow/{workflow_id}
// 提交，GET /api/v1/comfyui/comfyui_workflow/result/{task_id} 查询。每个工作流只接受它自己
// 暴露的参数，多传一个未定义的字段就会被拒（RequestParameterIsWrong）。
//
// 工作流 ID 就是上游模型名：建渠道时用模型映射把对外模型名映射到 workflow_id，适配器
// 不内置任何工作流 ID。一个渠道对应一个工作流，适配器只做协议转换：
//   - 网关的 prompt、duration/seconds、seed 原样传；
//   - 分辨率转成上游的中文档位：档位取 resolution（如 480p），或「宽x高」的短边
//     （864x480 → 480p，1920x1088 → 1088p）；方向取「宽x高」或 ratio（16:9 横、9:16 竖、
//     1:1 方），不传 ratio 按 16:9。都不传时不发，上游按工作流默认档出片（H3 是 768p竖），
//     价格表的默认档应按这个档定价；
//   - 速度模式 mode（fast/quality 或「高速模式」「高质模式」）按工作流定义换成选项名，
//     只有定义了 mode 的工作流（例如 H3 803）接受，不传时上游按默认的高质模式出片；
//   - 首帧/尾帧转成 first_frame/last_frame，参考图转成 ref_image_0..8，参考音频转成
//     ref_audio_0..2。图片和音频都传 URL 或 data URI。
//
// 业务错误一律是 HTTP 200，靠 code 区分。结果链接是 24 小时的签名地址，重新查询会拿到
// 新签名，过期后由查询接口和视频代理实时刷新（见 ResultURLExpired）。
//
// 计费：价格矩阵按秒计费，按请求时长预扣，上游按请求时长收费，完成时不再调整。
//
// 语音工作流（例如 IndexTTS2）走异步语音接口 /v1/audio/speech/tasks：请求体里除 model
// 以外的字段原样作为工作流参数（工作流的参数各不相同，网关不做转换）。上游按生成音频
// 的实际时长收费、单次有最低收费，因此预扣按文本长度估算时长，任务完成后读结果 WAV
// 的文件头得到实际时长结算，两边都按价格表的最低计费时长兜底。
package autodl

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const (
	ChannelName = "autodl-comfyui"

	submitPathPrefix = "/api/v1/comfyui/comfyui_workflow/"
	queryPathPrefix  = "/api/v1/comfyui/comfyui_workflow/result/"
	// workflowDefinitionPathPrefix 公开的工作流定义接口，返回各参数的选项（input_rules）。
	workflowDefinitionPathPrefix = "/api/v1/comfyui/workflows/"
	// 工作流定义很少变，取到后缓存一天；后台可在渠道列表里手动刷新。
	workflowDefinitionTTL = 24 * time.Hour
	// 取定义失败后这么久内不再重取，期间沿用旧定义（没有旧定义时按原样发送）。
	workflowDefinitionRetryAfter = time.Minute

	// 速度模式的英文写法，对应工作流 mode 选项里的「高速」「高质」。
	modeFast    = "fast"
	modeQuality = "quality"

	codeSuccess      = "Success"
	codeInvalidParam = "RequestParameterIsWrong"

	defaultDuration    = 5
	maxDuration        = 15
	maxReferenceImages = 9
	maxReferenceAudios = 3

	// 结果地址在过期前这么久就当作已过期，给调用方留出下载时间。
	resultURLExpiryMargin = 5 * time.Minute

	contextKeySpeechParams = "autodl_speech_params"
	// speechTextField 语音工作流（IndexTTS2）里要合成的文本字段，用于预扣估算。
	speechTextField = "prompt_text"
	// speechCharsPerSecond 预扣时按每秒 3 个字符估算音频时长。中文语速约每秒 4~5 字，
	// 估短了会少扣；完成时按实际时长多退少补。
	speechCharsPerSecond = 3
	// audioHeadBytes 读取结果音频的前这么多字节解析 WAV 头，足够覆盖常见的扩展块。
	audioHeadBytes = 64 << 10
)

type apiResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		TaskID  string  `json:"task_id"`
		Status  string  `json:"status"`
		Cost    float64 `json:"cost"`
		Results []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"results"`
	} `json:"data"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if info.RelayMode == relayconstant.RelayModeAudioTaskSubmit {
		var body map[string]any
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		params, err := speechWorkflowParams(body)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		c.Set(contextKeySpeechParams, params)
		info.Action = constant.TaskActionSpeech
		return nil
	}
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, _, err := buildWorkflowParams(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

// EstimateBilling 价格矩阵按秒计费时按请求时长预扣；上游按请求时长收费，完成时不调整。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	table, ok := video_billing.GetPriceTable(info.OriginModelName)
	if !ok || table.Unit != video_billing.UnitPerSecond {
		return nil
	}
	if params, ok := c.Get(contextKeySpeechParams); ok {
		text, _ := params.(map[string]any)[speechTextField].(string)
		return map[string]float64{"seconds": EstimateSpeechSeconds(text)}
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	_, duration, err := buildWorkflowParams(req)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(duration)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	workflowID := strings.TrimSpace(info.UpstreamModelName)
	if workflowID == "" {
		return "", errors.New("workflow id is empty; map the model to an autodl workflow_id in the channel model mapping")
	}
	return a.baseURL + submitPathPrefix + url.PathEscape(workflowID), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if params, ok := c.Get(contextKeySpeechParams); ok {
		data, err := common.Marshal(params)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	params, _, err := buildWorkflowParams(req)
	if err != nil {
		return nil, err
	}
	if label, ok := params["resolution"].(string); ok {
		option, err := a.workflowResolutionOption(info.UpstreamModelName, label)
		if err != nil {
			return nil, err
		}
		params["resolution"] = option
	}
	if mode, ok := params["mode"].(string); ok {
		option, err := a.workflowSpeedMode(info.UpstreamModelName, mode)
		if err != nil {
			return nil, err
		}
		params["mode"] = option
	}
	data, err := common.Marshal(params)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// workflowResolutionOption 把分辨率选项（例如 768p竖）换成工作流实际的选项名。各工作流的
// 写法不同，有的带像素尺寸（768p竖(768*1376)），按工作流定义里的选项匹配。工作流没有这个
// 档位时在本地拒绝并列出支持的档位；取不到定义时原样发送，由上游校验。
func (a *TaskAdaptor) workflowResolutionOption(workflowID, label string) (string, error) {
	definition, err := CachedWorkflowDefinition(a.baseURL, workflowID)
	if err != nil {
		common.SysError(fmt.Sprintf("autodl load workflow %s definition failed: %v", workflowID, err))
		return label, nil
	}
	options := definition.ResolutionOptions
	if len(options) == 0 {
		return label, nil
	}
	for _, option := range options {
		if option == label || strings.HasPrefix(option, label+"(") {
			return option, nil
		}
	}
	return "", relaycommon.NewInvalidRequest(fmt.Errorf("resolution %s is not supported by this model; supported: %s", label, strings.Join(options, ", ")))
}

// workflowSpeedMode 把请求的速度模式（fast/quality，或工作流的中文选项名）换成工作流的
// mode 选项，例如 H3 803 的「高速模式」「高质模式」；两个模式同价。工作流没有 mode 参数时
// 在本地拒绝，取不到定义时按常见的中文选项名发送，由上游校验。
func (a *TaskAdaptor) workflowSpeedMode(workflowID, mode string) (string, error) {
	keyword := mode
	switch strings.ToLower(mode) {
	case modeFast:
		keyword = "高速"
	case modeQuality:
		keyword = "高质"
	}
	definition, err := CachedWorkflowDefinition(a.baseURL, workflowID)
	if err != nil {
		common.SysError(fmt.Sprintf("autodl load workflow %s definition failed: %v", workflowID, err))
		if keyword == "高速" || keyword == "高质" {
			return keyword + "模式", nil
		}
		return mode, nil
	}
	if !definition.HasMode {
		return "", relaycommon.NewInvalidRequest(fmt.Errorf("mode is not supported by this model"))
	}
	for _, option := range definition.ModeOptions {
		if option == mode || strings.HasPrefix(option, keyword) {
			return option, nil
		}
	}
	return "", relaycommon.NewInvalidRequest(fmt.Errorf("mode %s is not supported by this model; use fast or quality", mode))
}

// WorkflowDefinition 工作流定义里适配器要用的参数选项。
type WorkflowDefinition struct {
	ResolutionOptions []string `json:"resolution_options"`
	HasMode           bool     `json:"has_mode"`
	ModeOptions       []string `json:"mode_options,omitempty"`
}

type workflowDefinitionCacheEntry struct {
	definition WorkflowDefinition
	// loaded 为 false 表示还没成功取到过，err 是最近一次失败的原因。
	loaded    bool
	err       error
	expiresAt time.Time
}

// workflowDefinitionCache 按 baseURL|workflowID 缓存工作流定义（每个节点各自缓存）。
var workflowDefinitionCache sync.Map

// CachedWorkflowDefinition 返回缓存的工作流定义，过期后重新读取。读取失败时沿用旧定义，
// 没有旧定义时返回错误；失败后 workflowDefinitionRetryAfter 内不再重取，免得上游定义接口
// 出问题时每次提交都等它超时。
func CachedWorkflowDefinition(baseURL, workflowID string) (WorkflowDefinition, error) {
	if cached, ok := workflowDefinitionCache.Load(baseURL + "|" + workflowID); ok {
		if entry := cached.(workflowDefinitionCacheEntry); time.Now().Before(entry.expiresAt) {
			return entry.definition, entry.err
		}
	}
	return RefreshWorkflowDefinition(baseURL, workflowID)
}

// RefreshWorkflowDefinition 立即重新读取工作流定义并更新缓存，供后台手动刷新。
func RefreshWorkflowDefinition(baseURL, workflowID string) (WorkflowDefinition, error) {
	key := baseURL + "|" + workflowID
	definition, err := fetchWorkflowDefinition(baseURL, workflowID)
	if err == nil {
		workflowDefinitionCache.Store(key, workflowDefinitionCacheEntry{definition: definition, loaded: true, expiresAt: time.Now().Add(workflowDefinitionTTL)})
		return definition, nil
	}
	retryAt := time.Now().Add(workflowDefinitionRetryAfter)
	if cached, ok := workflowDefinitionCache.Load(key); ok {
		if entry := cached.(workflowDefinitionCacheEntry); entry.loaded {
			common.SysError(fmt.Sprintf("autodl refresh workflow %s definition failed, keep the cached one: %v", workflowID, err))
			workflowDefinitionCache.Store(key, workflowDefinitionCacheEntry{definition: entry.definition, loaded: true, expiresAt: retryAt})
			return entry.definition, nil
		}
	}
	workflowDefinitionCache.Store(key, workflowDefinitionCacheEntry{err: err, expiresAt: retryAt})
	return WorkflowDefinition{}, err
}

// fetchWorkflowDefinition 从工作流定义接口读取分辨率和 mode 的选项名。测试里替换成本地数据。
var fetchWorkflowDefinition = func(baseURL, workflowID string) (WorkflowDefinition, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+workflowDefinitionPathPrefix+url.PathEscape(workflowID), nil)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return WorkflowDefinition{}, err
	}
	var payload struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data *struct {
			InputRules map[string]struct {
				Options []struct {
					Label string `json:"label"`
				} `json:"options"`
			} `json:"input_rules"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &payload); err != nil {
		return WorkflowDefinition{}, err
	}
	if payload.Code != codeSuccess || payload.Data == nil {
		return WorkflowDefinition{}, fmt.Errorf("%s: %s", payload.Code, payload.Msg)
	}
	optionLabels := func(name string) []string {
		var labels []string
		for _, option := range payload.Data.InputRules[name].Options {
			if label := strings.TrimSpace(option.Label); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	}
	_, hasMode := payload.Data.InputRules["mode"]
	return WorkflowDefinition{
		ResolutionOptions: optionLabels("resolution"),
		HasMode:           hasMode,
		ModeOptions:       optionLabels("mode"),
	}, nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var submitResp apiResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %.500s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	switch {
	case submitResp.Code == codeInvalidParam:
		// 参数不合工作流的定义（缺必填项、多传了字段、取值不在选项里）是请求本身的问题，
		// 换渠道也不会成功，不记渠道错误。
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("autodl rejected the parameters: %s", submitResp.Msg), "invalid_request", http.StatusBadRequest)
	case submitResp.Code != codeSuccess:
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("autodl api error %s: %s", submitResp.Code, submitResp.Msg), "upstream_error", http.StatusInternalServerError)
	case submitResp.Data == nil || strings.TrimSpace(submitResp.Data.TaskID) == "":
		// 没有任务号就无法轮询，按提交失败处理，免得留下要等超时才退款的任务。
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty, body: %.500s", responseBody), "invalid_response", http.StatusInternalServerError)
	}

	// 语音任务的提交结果与豆包语音的异步接口保持一致，查询接口也是同一套字段。
	if info.Action == constant.TaskActionSpeech {
		c.JSON(http.StatusOK, gin.H{
			"task_id":    info.PublicTaskID,
			"model":      info.OriginModelName,
			"status":     "queued",
			"created_at": time.Now().Unix(),
		})
		return submitResp.Data.TaskID, responseBody, nil
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return submitResp.Data.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, baseUrl+queryPathPrefix+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var queryResp apiResponse
	if err := common.Unmarshal(respBody, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	if queryResp.Code != codeSuccess || queryResp.Data == nil {
		// 查询本身出错（限流、鉴权、查不到任务），任务状态未知，交给下一轮轮询；
		// 一直查不到的由超时清理退款。
		return nil, fmt.Errorf("autodl query failed %s: %s", queryResp.Code, queryResp.Msg)
	}

	data := queryResp.Data
	result := &relaycommon.TaskInfo{TaskID: data.TaskID}
	switch data.Status {
	case "QUEUED":
		result.Status = model.TaskStatusQueued
		result.Progress = "20%"
	case "RUNNING":
		result.Status = model.TaskStatusInProgress
		result.Progress = "50%"
	case "SUCCESS":
		resultType := ""
		for _, item := range data.Results {
			if (item.Type == "video" || item.Type == "audio") && item.URL != "" {
				result.Url, resultType = item.URL, item.Type
				break
			}
		}
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Progress = "100%"
			result.Reason = "autodl task succeeded without a video or audio result"
			return result, nil
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = "100%"
		if resultType == "audio" {
			// 读不出时长时为 0，完成结算会保持预扣额度不变。
			result.BillingDuration = audioDurationSeconds(result.Url)
		}
	case "FAILED":
		result.Status = model.TaskStatusFailure
		result.Progress = "100%"
		result.Reason = "task failed"
		if msg := strings.TrimSpace(queryResp.Msg); msg != "" {
			result.Reason = msg
		}
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = "30%"
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	openAIVideo := originTask.ToOpenAIVideo()
	if originTask.Status == model.TaskStatusFailure && originTask.FailReason != "" {
		openAIVideo.Error = &dto.OpenAIVideoError{Message: originTask.FailReason}
	}
	return common.Marshal(openAIVideo)
}

// AdjustBillingOnComplete 语音任务按结果音频的实际时长结算；视频任务提交时没打开
// 完成结算，这里返回 0 保持预扣额度。
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return taskcommon.VideoMatrixQuotaOnComplete(task, taskResult)
}

func (a *TaskAdaptor) GetModelList() []string {
	return nil
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// buildWorkflowParams 把网关的任务请求转成工作流参数，返回参数与计费时长。
// 只放调用方给了的字段：工作流不接受未定义的参数。
func buildWorkflowParams(req relaycommon.TaskSubmitReq) (map[string]any, int, error) {
	if _, ok := req.NativeContent(); ok {
		return nil, 0, errors.New("content is not supported; use first_frame_url/last_frame_url/reference_images/audio_urls")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, 0, errors.New("prompt is required")
	}
	params := map[string]any{"prompt": prompt}

	duration := RequestDuration(req)
	if duration == 0 {
		duration = defaultDuration
	}
	if duration < 1 || duration > maxDuration {
		return nil, 0, fmt.Errorf("duration must be between 1 and %d seconds, got %d", maxDuration, duration)
	}
	params["duration"] = duration

	resolution, err := resolutionOption(RequestResolution(req), requestRatio(req))
	if err != nil {
		return nil, 0, err
	}
	if resolution != "" {
		params["resolution"] = resolution
	}

	seed, hasSeed, err := requestSeed(req)
	if err != nil {
		return nil, 0, err
	}
	if hasSeed {
		params["seed"] = seed
	}

	// 顶层 mode 的 reference/reference-to-video 是旧写法的生成方式提示（见 addMediaParams），
	// 其余取值是工作流的速度模式，组装请求时按工作流定义换成选项名。
	if mode := strings.TrimSpace(req.Mode); mode != "" && !isReferenceModeHint(mode) {
		params["mode"] = mode
	}

	if err := addMediaParams(params, req); err != nil {
		return nil, 0, err
	}
	return params, duration, nil
}

// RequestDuration 请求时长：seconds、duration、metadata 里的 duration 依次取，没给返回 0。
// 计费取维度与组装请求共用，两边的时长才对得上。
func RequestDuration(req relaycommon.TaskSubmitReq) int {
	if seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && seconds != 0 {
		return seconds
	}
	if req.Duration != 0 {
		return req.Duration
	}
	if value, ok := metadataValue(req.Metadata, "duration").(float64); ok {
		return int(math.Max(math.Min(value, math.MaxInt32), math.MinInt32))
	}
	return 0
}

// RequestResolution 请求里的分辨率表述：顶层 resolution、size、metadata 里的 resolution 依次取。
func RequestResolution(req relaycommon.TaskSubmitReq) string {
	for _, value := range []string{req.Resolution, req.Size, metadataString(req.Metadata, "resolution")} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func requestRatio(req relaycommon.TaskSubmitReq) string {
	for _, value := range []string{req.Ratio, metadataString(req.Metadata, "ratio"), metadataString(req.Metadata, "aspect_ratio")} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// ResolutionTier 把分辨率表述归到上游的档位名：「宽x高」取短边（864x480 → 480p），
// 档位名（480p、768P）转小写。价格表按同一套档位取价。
func ResolutionTier(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if width, height, ok := parseSize(value); ok {
		return strconv.Itoa(min(width, height)) + "p"
	}
	return value
}

// resolutionOption 组装上游的分辨率选项，例如 480p竖、768p横、480p(1:1)。
func resolutionOption(raw, ratio string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if strings.TrimSpace(ratio) != "" {
			return "", errors.New("resolution is required when ratio is set, e.g. 480p or 768p")
		}
		return "", nil
	}
	tier := ResolutionTier(raw)
	if !strings.HasSuffix(tier, "p") {
		return "", fmt.Errorf("resolution must look like 480p or WIDTHxHEIGHT, got %q", raw)
	}
	if _, err := strconv.Atoi(strings.TrimSuffix(tier, "p")); err != nil {
		return "", fmt.Errorf("resolution must look like 480p or WIDTHxHEIGHT, got %q", raw)
	}

	if width, height, ok := parseSize(strings.ToLower(raw)); ok {
		switch {
		case width > height:
			return tier + "横", nil
		case width < height:
			return tier + "竖", nil
		default:
			return tier + "(1:1)", nil
		}
	}
	switch strings.TrimSpace(ratio) {
	case "", "16:9":
		return tier + "横", nil
	case "9:16":
		return tier + "竖", nil
	case "1:1":
		return tier + "(1:1)", nil
	}
	return "", fmt.Errorf("ratio must be 16:9, 9:16 or 1:1, got %q", ratio)
}

func parseSize(value string) (int, int, bool) {
	normalized := strings.NewReplacer("*", "x", "×", "x").Replace(value)
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// requestSeed 读取 seed：请求体顶层或 metadata 里的整数。
func requestSeed(req relaycommon.TaskSubmitReq) (int64, bool, error) {
	value, ok := req.BodyField("seed")
	if !ok {
		value = metadataValue(req.Metadata, "seed")
	}
	if value == nil {
		return 0, false, nil
	}
	number, ok := value.(float64)
	if !ok || number != math.Trunc(number) || math.Abs(number) > math.MaxInt32 {
		return 0, false, fmt.Errorf("seed must be an integer, got %v", value)
	}
	return int64(number), true, nil
}

// addMediaParams 把素材转成工作流的字段。按用途区分的字段直接对应；旧写法传图时按与
// 官方 H3 相同的规则推断：带参考音频、mode=reference 或图片超过两张时按参考图，否则
// 1 张为首帧、2 张为首尾帧。首尾帧和参考素材不能同时出现，不支持参考视频。
func addMediaParams(params map[string]any, req relaycommon.TaskSubmitReq) error {
	media := req.RoleMedia()
	if len(media.ReferenceVideos) > 0 {
		return errors.New("reference videos are not supported by autodl workflows")
	}
	firstFrame, lastFrame := media.FirstFrame, media.LastFrame
	references := media.ReferenceImages
	if !req.UsesRoleMedia() {
		images := legacyImages(req)
		if len(media.ReferenceAudios) > 0 || len(images) > 2 || isReferenceModeHint(req.Mode) {
			references = images
		} else if len(images) > 0 {
			firstFrame = images[0]
			if len(images) > 1 {
				lastFrame = images[1]
			}
		}
	}

	hasFrames := firstFrame != "" || lastFrame != ""
	hasReferences := len(references) > 0 || len(media.ReferenceAudios) > 0
	switch {
	case hasFrames && hasReferences:
		return errors.New("first/last frames cannot be combined with reference images or audios")
	case len(references) > maxReferenceImages:
		return fmt.Errorf("at most %d reference images are supported, got %d", maxReferenceImages, len(references))
	case len(media.ReferenceAudios) > maxReferenceAudios:
		return fmt.Errorf("at most %d reference audios are supported, got %d", maxReferenceAudios, len(media.ReferenceAudios))
	}

	if firstFrame != "" {
		params["first_frame"] = firstFrame
	}
	if lastFrame != "" {
		params["last_frame"] = lastFrame
	}
	for i, image := range references {
		params["ref_image_"+strconv.Itoa(i)] = image
	}
	for i, audio := range media.ReferenceAudios {
		params["ref_audio_"+strconv.Itoa(i)] = audio
	}
	return nil
}

// isReferenceModeHint 顶层 mode 是否是旧写法里「按参考图生成」的提示。
func isReferenceModeHint(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "reference" || mode == "reference-to-video"
}

// legacyImages 汇总 image/images/input_reference，去空白与重复。
func legacyImages(req relaycommon.TaskSubmitReq) []string {
	var images []string
	seen := map[string]bool{}
	for _, image := range append(append([]string{req.Image}, req.Images...), req.InputReference) {
		image = strings.TrimSpace(image)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		images = append(images, image)
	}
	return images
}

// metadataValue 依次读取 metadata 顶层与 metadata.parameters 里的字段。
func metadataValue(metadata map[string]any, key string) any {
	if value, ok := metadata[key]; ok {
		return value
	}
	if parameters, ok := metadata["parameters"].(map[string]any); ok {
		return parameters[key]
	}
	return nil
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadataValue(metadata, key).(string)
	return strings.TrimSpace(value)
}

// ResultURLExpired 判断结果地址（TOS 预签名地址）是否已过期或即将过期。
// 解析不出签名时间的地址视为未过期，避免对不认识的地址反复查询上游。
func ResultURLExpired(resultURL string, now time.Time) bool {
	expiresAt, ok := resultURLExpiresAt(resultURL)
	return ok && !now.Before(expiresAt.Add(-resultURLExpiryMargin))
}

// resultURLExpiresAt 从 TOS 预签名地址的 X-Tos-Date 与 X-Tos-Expires 算出过期时刻。
func resultURLExpiresAt(resultURL string) (time.Time, bool) {
	parsed, err := url.Parse(resultURL)
	if err != nil {
		return time.Time{}, false
	}
	query := parsed.Query()
	signedAt, err := time.Parse("20060102T150405Z", query.Get("X-Tos-Date"))
	if err != nil {
		return time.Time{}, false
	}
	expires, err := strconv.Atoi(query.Get("X-Tos-Expires"))
	if err != nil || expires <= 0 {
		return time.Time{}, false
	}
	return signedAt.Add(time.Duration(expires) * time.Second), true
}

// speechWorkflowParams 异步语音接口的请求体去掉 model 后原样作为工作流参数。
// 只校验估算计费要用的文本字段，其余参数由工作流自己校验。
func speechWorkflowParams(body map[string]any) (map[string]any, error) {
	if body == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	params := make(map[string]any, len(body))
	for key, value := range body {
		if key != "model" {
			params[key] = value
		}
	}
	text, ok := params[speechTextField].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%s is required", speechTextField)
	}
	return params, nil
}

// EstimateSpeechSeconds 按文本长度估算语音时长，用于预扣；至少 1 秒，不超过单次时长上限。
func EstimateSpeechSeconds(text string) float64 {
	chars := utf8.RuneCountInString(strings.TrimSpace(text))
	seconds := math.Ceil(float64(chars) / speechCharsPerSecond)
	return math.Min(math.Max(seconds, 1), relaycommon.MaxTaskDurationSeconds)
}

// fetchAudioHead 读取结果音频的开头与文件总大小。测试里替换成本地数据。
var fetchAudioHead = func(audioURL string) ([]byte, int64, error) {
	req, err := http.NewRequest(http.MethodGet, audioURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", audioHeadBytes-1))
	resp, err := service.GetSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, audioHeadBytes))
	if err != nil {
		return nil, 0, err
	}
	total := resp.ContentLength
	// 206 的 Content-Range 形如 bytes 0-65535/1234567，斜杠后是文件总大小。
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		if slash := strings.LastIndex(contentRange, "/"); slash >= 0 {
			if size, err := strconv.ParseInt(contentRange[slash+1:], 10, 64); err == nil {
				total = size
			}
		}
	}
	return head, total, nil
}

// audioDurationSeconds 读取结果音频的 WAV 头得到时长（秒）。读不到或不是 WAV 时返回 0。
// 时长来自上游文件，按单次时长上限钳制后才能作为计费乘数。
func audioDurationSeconds(audioURL string) float64 {
	head, total, err := fetchAudioHead(audioURL)
	if err != nil {
		common.SysError(fmt.Sprintf("autodl read audio header failed: %v", err))
		return 0
	}
	seconds, ok := wavDurationSeconds(head, total)
	if !ok {
		common.SysError("autodl result audio is not a readable WAV file")
		return 0
	}
	return relaycommon.ClampUsageSeconds(seconds)
}

// wavDurationSeconds 解析 WAV 头：时长 = data 块字节数 / fmt 块的每秒字节数。data 块
// 长度写成 0 或 0xFFFFFFFF（流式写出的文件），或超出文件实际大小时，按文件总大小推算。
func wavDurationSeconds(head []byte, total int64) (float64, bool) {
	if len(head) < 12 || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate uint32
	for offset := 12; offset+8 <= len(head); {
		chunkID := string(head[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(head[offset+4 : offset+8])
		dataStart := offset + 8
		switch chunkID {
		case "fmt ":
			if dataStart+12 > len(head) {
				return 0, false
			}
			byteRate = binary.LittleEndian.Uint32(head[dataStart+8 : dataStart+12])
		case "data":
			if byteRate == 0 {
				return 0, false
			}
			size := int64(chunkSize)
			if total > 0 && (chunkSize == 0 || chunkSize == math.MaxUint32 || int64(dataStart)+size > total) {
				size = total - int64(dataStart)
			}
			if size <= 0 {
				return 0, false
			}
			return float64(size) / float64(byteRate), true
		}
		// 块按偶数字节对齐。
		offset = dataStart + int(chunkSize) + int(chunkSize&1)
		if chunkSize > uint32(len(head)) {
			return 0, false
		}
	}
	return 0, false
}

// BuildSpeechTaskResponse 异步语音接口的查询结果，字段与豆包语音的查询结果保持一致。
func BuildSpeechTaskResponse(task *model.Task) ([]byte, error) {
	status := "queued"
	switch task.Status {
	case model.TaskStatusSuccess:
		status = "succeeded"
	case model.TaskStatusFailure:
		status = "failed"
	case model.TaskStatusInProgress:
		status = "in_progress"
	}
	resp := map[string]any{
		"task_id":    task.TaskID,
		"model":      task.Properties.OriginModelName,
		"status":     status,
		"progress":   task.Progress,
		"created_at": task.SubmitTime,
	}
	if task.FinishTime > 0 {
		resp["finished_at"] = task.FinishTime
	}
	if task.Status == model.TaskStatusSuccess {
		if audioURL := task.GetResultURL(); audioURL != "" {
			resp["audio_url"] = audioURL
			if expiresAt, ok := resultURLExpiresAt(audioURL); ok {
				resp["url_expire_time"] = expiresAt.Unix()
			}
		}
	}
	if task.Status == model.TaskStatusFailure {
		resp["error"] = map[string]string{"message": task.FailReason}
	}
	return common.Marshal(resp)
}
