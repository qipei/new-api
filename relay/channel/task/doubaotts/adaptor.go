// CUSTOM: 豆包语音异步长文本合成（fork 扩展）。
// 文档：https://docs.volcengine.com/docs/DoubaoVoice/Tasksubmission
//
//	https://docs.volcengine.com/docs/DoubaoVoice/Resultquery
//
// 对外接口：POST /v1/audio/speech/tasks 提交，GET /v1/audio/speech/tasks/:task_id 查询。
// 请求体沿用上游原生结构（req_params 原样透传），平台只做三件事：
//   - 模型名作为 X-Api-Resource-Id（seed-tts-2.0 合成 / seed-icl-2.0 复刻音色）；
//   - additions 与 post_process 允许传对象，转成上游要求的 JSON 字符串
//     （post_process 传对象上游会报 55000000，文档没写清楚，实测必须是字符串）；
//   - audio_params.format 缺省补 mp3（文档称默认 mp3，实测不传会报「不支持的音频格式」）；
//   - unique_id 不对外开放，任务 ID 由平台生成，避免重试换渠道时上游判重。
//
// 计费：价格矩阵 per_10k_chars 单位按字符数计价（1 个汉字、字母、标点、空格、
// 回车都算 1 个字符，SSML 标签照常计费）。提交前按本地统计预扣，提交成功后
// 用上游回报的 req_text_length 校正；任务失败由轮询全额退款。
package doubaotts

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const (
	ChannelName = "doubao-tts"
	// 上游要求 user.uid 必填；调用方未指定时使用平台默认值，不外发平台用户 ID。
	defaultUID = "new-api"

	contextKeySubmitRequest = "doubao_tts_submit_request"

	speechSuccessCode  = 20000000
	taskNotExistCode   = 40000001
	taskStatusSuccess  = 2
	taskStatusFailure  = 3
	speechSubmitPath   = "/api/v3/tts/submit"
	speechQueryPath    = "/api/v3/tts/query"
	resourceIDHeader   = "X-Api-Resource-Id"
	defaultResourceID  = "seed-tts-2.0"
	charsPerPriceUnit  = 10000.0
	maxSubmitBodyBytes = 64 << 20
)

var ModelList = []string{"seed-tts-2.0", "seed-icl-2.0"}

// submitRequest 是调用方的提交请求。req_params 用 map 承载以原样透传全部字段
// （text、speaker、model、ssml、audio_params、additions、post_process 等），
// 上游新增字段无需改代码。
type submitRequest struct {
	Model string `json:"model"`
	User  *struct {
		UID string `json:"uid"`
	} `json:"user,omitempty"`
	ReqParams map[string]any `json:"req_params"`
}

type upstreamSubmitBody struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	ReqParams map[string]any `json:"req_params"`
}

type upstreamSubmitResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		TaskID        string `json:"task_id"`
		TaskStatus    int    `json:"task_status"`
		ReqTextLength int    `json:"req_text_length"`
	} `json:"data"`
}

// QueryResponse 是上游查询接口的响应，同时用于对外查询结果的组装。
type QueryResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		TaskID               string `json:"task_id"`
		TaskStatus           int    `json:"task_status"`
		AudioURL             string `json:"audio_url"`
		URLExpireTime        int64  `json:"url_expire_time"`
		ReqTextLength        int    `json:"req_text_length"`
		SynthesizeTextLength int    `json:"synthesize_text_length"`
		Sentences            any    `json:"sentences,omitempty"`
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

// billableChars 统计计费字符数：text 与 ssml 都按 Unicode 字符计。
func billableChars(params map[string]any) int {
	text, _ := params["text"].(string)
	ssml, _ := params["ssml"].(string)
	return utf8.RuneCountInString(text) + utf8.RuneCountInString(ssml)
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	var req submitRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(errors.New("model is required"), "invalid_request", http.StatusBadRequest)
	}
	if req.ReqParams == nil {
		return service.TaskErrorWrapperLocal(errors.New("req_params is required"), "invalid_request", http.StatusBadRequest)
	}
	text, _ := req.ReqParams["text"].(string)
	ssml, _ := req.ReqParams["ssml"].(string)
	if strings.TrimSpace(text) == "" && strings.TrimSpace(ssml) == "" {
		return service.TaskErrorWrapperLocal(errors.New("req_params.text is required"), "invalid_request", http.StatusBadRequest)
	}
	if chars := billableChars(req.ReqParams); chars > relaycommon.MaxSpeechTaskTextChars {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("req_params.text must be at most %d characters, got %d", relaycommon.MaxSpeechTaskTextChars, chars),
			"invalid_request", http.StatusBadRequest)
	}
	if speaker, _ := req.ReqParams["speaker"].(string); strings.TrimSpace(speaker) == "" {
		return service.TaskErrorWrapperLocal(errors.New("req_params.speaker is required"), "invalid_request", http.StatusBadRequest)
	}
	audioParams, ok := req.ReqParams["audio_params"].(map[string]any)
	if !ok {
		if raw, exists := req.ReqParams["audio_params"]; exists && raw != nil {
			return service.TaskErrorWrapperLocal(errors.New("req_params.audio_params must be an object"), "invalid_request", http.StatusBadRequest)
		}
		audioParams = map[string]any{}
		req.ReqParams["audio_params"] = audioParams
	}
	if format, _ := audioParams["format"].(string); format == "" {
		audioParams["format"] = "mp3"
	}
	// 上游的 additions 与 post_process 是 JSON 字符串；为了好写，这里也接受对象。
	for _, field := range []string{"additions", "post_process"} {
		switch value := req.ReqParams[field].(type) {
		case nil, string:
		case map[string]any:
			encoded, err := common.Marshal(value)
			if err != nil {
				return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
			}
			req.ReqParams[field] = string(encoded)
		default:
			return service.TaskErrorWrapperLocal(fmt.Errorf("req_params.%s must be an object or a JSON string", field), "invalid_request", http.StatusBadRequest)
		}
	}

	c.Set(contextKeySubmitRequest, &req)
	info.Action = constant.TaskActionSpeech
	info.OriginModelName = req.Model
	return nil
}

func submitRequestFromContext(c *gin.Context) (*submitRequest, error) {
	value, ok := c.Get(contextKeySubmitRequest)
	if !ok {
		return nil, errors.New("speech task request not found in context")
	}
	req, ok := value.(*submitRequest)
	if !ok {
		return nil, errors.New("invalid speech task request in context")
	}
	return req, nil
}

func usesCharsPricing(modelName string) bool {
	table, ok := video_billing.GetPriceTable(modelName)
	return ok && table.Unit == video_billing.UnitPer10kChars
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !usesCharsPricing(info.OriginModelName) {
		return nil
	}
	req, err := submitRequestFromContext(c)
	if err != nil {
		return nil
	}
	return map[string]float64{video_billing.CharsRatioKey: float64(billableChars(req.ReqParams)) / charsPerPriceUnit}
}

// AdjustBillingOnSubmit 用上游回报的请求字符数校正计费。上游数值同样钳制在单次上限内。
func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	if !usesCharsPricing(info.OriginModelName) {
		return nil
	}
	var resp upstreamSubmitResponse
	if err := common.Unmarshal(taskData, &resp); err != nil || resp.Data.ReqTextLength <= 0 {
		return nil
	}
	chars := min(resp.Data.ReqTextLength, relaycommon.MaxSpeechTaskTextChars)
	return map[string]float64{video_billing.CharsRatioKey: float64(chars) / charsPerPriceUnit}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return volcengine.ResolveSpeechBaseURL(a.baseURL) + speechSubmitPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	volcengine.SetupSpeechRequestHeader(&req.Header, a.apiKey)
	req.Header.Set(resourceIDHeader, info.UpstreamModelName)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, _ *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := submitRequestFromContext(c)
	if err != nil {
		return nil, err
	}
	var body upstreamSubmitBody
	body.User.UID = defaultUID
	if req.User != nil && strings.TrimSpace(req.User.UID) != "" {
		body.User.UID = req.User.UID
	}
	body.ReqParams = req.ReqParams
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubmitBodyBytes))
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream upstreamSubmitResponse
	if err := common.Unmarshal(body, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if upstream.Code/10000000 == 4 {
		// 4xxxxxxx 是请求参数错误（上游以 HTTP 200 返回），换渠道重试也不会成功。
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("%s (code %d)", upstream.Message, upstream.Code), "invalid_request", http.StatusBadRequest)
	}
	if upstream.Code != speechSuccessCode || upstream.Data.TaskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("doubao tts submit failed: %d %s", upstream.Code, upstream.Message), "submit_failed", http.StatusBadGateway)
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id":         info.PublicTaskID,
		"model":           info.OriginModelName,
		"status":          "queued",
		"req_text_length": upstream.Data.ReqTextLength,
		"created_at":      time.Now().Unix(),
	})
	return upstream.Data.TaskID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, errors.New("invalid task_id")
	}
	resourceID, _ := body["model"].(string)
	if resourceID == "" {
		resourceID = defaultResourceID
	}
	payload, err := common.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, volcengine.ResolveSpeechBaseURL(baseURL)+speechQueryPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	volcengine.SetupSpeechRequestHeader(&req.Header, key)
	req.Header.Set(resourceIDHeader, resourceID)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resp QueryResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao tts query result failed")
	}
	info := &relaycommon.TaskInfo{TaskID: resp.Data.TaskID}
	switch {
	case resp.Code == taskNotExistCode:
		info.Status = model.TaskStatusFailure
		info.Reason = resp.Message
	case resp.Code != speechSuccessCode:
		// 限流等临时错误不判失败，保持进行中等下一轮轮询；真的卡住由超时清理兜底。
		info.Status = model.TaskStatusInProgress
	case resp.Data.TaskStatus == taskStatusSuccess:
		info.Status = model.TaskStatusSuccess
		info.Progress = taskcommon.ProgressComplete
		info.Url = resp.Data.AudioURL
	case resp.Data.TaskStatus == taskStatusFailure:
		info.Status = model.TaskStatusFailure
		info.Reason = resp.Message
		if info.Reason == "" || strings.EqualFold(info.Reason, "ok") {
			info.Reason = "speech synthesis failed"
		}
	default:
		info.Status = model.TaskStatusInProgress
	}
	return info, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// publicStatus 把内部任务状态映射为对外的小写状态名。
func publicStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusInProgress:
		return "in_progress"
	default:
		return "queued"
	}
}

// AudioURLExpired 判断轮询保存的音频地址是否已过期（或一分钟内过期）。
// 上游下载地址只有 1 小时有效期，过期后需要重新查询上游拿新地址。
func AudioURLExpired(task *model.Task, now int64) bool {
	var query QueryResponse
	if len(task.Data) == 0 || common.Unmarshal(task.Data, &query) != nil {
		return true
	}
	return query.Data.AudioURL == "" || query.Data.URLExpireTime <= now+60
}

// BuildTaskResponse 组装对外查询结果。upstream 为实时查询到的上游响应，
// 为空时回退到轮询保存的任务数据（其中的音频地址可能已过期）。
func BuildTaskResponse(task *model.Task, upstream []byte) ([]byte, error) {
	if len(upstream) == 0 {
		upstream = task.Data
	}
	var query QueryResponse
	if len(upstream) > 0 {
		_ = common.Unmarshal(upstream, &query)
	}
	resp := map[string]any{
		"task_id":    task.TaskID,
		"model":      task.Properties.OriginModelName,
		"status":     publicStatus(task.Status),
		"progress":   task.Progress,
		"created_at": task.SubmitTime,
	}
	if task.FinishTime > 0 {
		resp["finished_at"] = task.FinishTime
	}
	if query.Code == speechSuccessCode {
		if query.Data.ReqTextLength > 0 {
			resp["req_text_length"] = query.Data.ReqTextLength
		}
		if query.Data.AudioURL != "" {
			resp["audio_url"] = query.Data.AudioURL
			resp["url_expire_time"] = query.Data.URLExpireTime
			resp["synthesize_text_length"] = query.Data.SynthesizeTextLength
		}
		if query.Data.Sentences != nil {
			resp["sentences"] = query.Data.Sentences
		}
	}
	if task.Status == model.TaskStatusFailure {
		resp["error"] = map[string]string{"message": task.FailReason}
	}
	return common.Marshal(resp)
}
