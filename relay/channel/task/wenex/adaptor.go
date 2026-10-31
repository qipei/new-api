// CUSTOM: WeNex AI（ai.seekthex.com）的 MiniMax H3 视频生成（fork 扩展）。
// 文档：https://ai.seekthex.com/docs/
//
// 走上游的 OpenAI 视频异步协议：POST /v1/video/generations 提交（成功返回 200 和任务 ID），
// GET /v1/video/generations/{task_id} 查询，完成后直接给出视频直链（url）。上游模型 ID 是
// h3_video，建渠道时用模型映射把对外模型名映射过去。
//
// 这条线路可以和 autodl 的 H3 工作流挂在同一个对外模型下（调用方不能指定渠道），因此
// 请求写法沿用工作流线路的约定：档位名按短边（1080p 也可以写 1088p），什么都不传时按
// 768p 竖屏出片。
//
// 网关请求转换：
//   - 画质只有 768p、1080p 两档：取 resolution / quality / metadata 里的 quality、resolution /
//     size 里的档位名，或「宽x高」的短边（≤768 为 768p，≤1088 为 1080p）；720p 按 768p，
//     1088p 按 1080p。都不传按 768p。
//   - 比例只有 21:9、16:9、4:3、1:1、3:4、9:16：取 ratio / aspect_ratio / metadata 里的
//     ratio、aspect_ratio，或「宽x高」最接近的比例；给了画质没给比例按 16:9，画质和比例
//     都没给按 9:16（与工作流线路的默认出片一致）。官方 H3 的 adaptive（跟随输入图）
//     上游不支持，按没给比例处理，挂在官方 H3 模型下时按官方文档写的请求不会被拒。
//   - 画质和比例总是显式发给上游：上游对不认识的 size 会静默换成别的档位，计费必须和
//     实际出片一致。
//   - 秒数 4~15 的整数，默认 5，上游按这个秒数收费。
//   - 首帧/尾帧发 first_frame/last_frame，参考图、参考音频发 images/audios（带 role）。
//     素材可以是公网 URL 或 data URI。不支持参考视频。
//   - 动态优化 martial_arts、动作速度 motion_speed 放在 metadata 里。
//
// 计费：价格矩阵按秒计费，档位见 PriceTier（768p，1080p 按价格表写成 1080p 或 1088p），
// 按请求秒数预扣，完成时不调整。
// 失败的任务上游不扣费，网关照常退款。
//
// 注意：上游提交接口没有幂等保护，提交超时或 5xx 时任务可能已经受理。结果文件上游只保留
// 3 天。
package wenex

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const (
	ChannelName = "wenex-ai"

	generationsPath = "/v1/video/generations"

	Quality768p  = "768p"
	Quality1080p = "1080p"

	defaultRatio = "16:9"
	// 画质和比例都没给时的比例，与工作流线路的默认出片（768p 竖屏）一致。
	defaultPortraitRatio = "9:16"
	// 1080p 实际出片的短边是 1088（1920x1088），工作流线路的价格表按短边命名这一档。
	priceTier1080p     = "1088p"
	defaultSeconds     = 5
	minSeconds         = 4
	maxSeconds         = 15
	maxPromptChars     = 20000
	maxReferenceImages = 9
	maxReferenceAudios = 3
	maxSeed            = 1<<53 - 1
	maxMotionSpeed     = 8
	// 「宽x高」与支持比例的相对误差在这个范围内才算这个比例（1344x768 → 16:9）。
	ratioTolerance = 0.03
)

// supportedRatios 上游支持的画面比例及其宽高比。
var supportedRatios = []struct {
	name  string
	value float64
}{
	{"21:9", 21.0 / 9}, {"16:9", 16.0 / 9}, {"4:3", 4.0 / 3}, {"1:1", 1}, {"3:4", 3.0 / 4}, {"9:16", 9.0 / 16},
}

// videoResponse 上游提交与查询接口返回的视频任务（失败时只有 error）。
type videoResponse struct {
	ID       string `json:"id"`
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	URL      string `json:"url"`
	VideoURL string `json:"video_url"`
	Error    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
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
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, _, err := buildGenerationBody(req, info.UpstreamModelName); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

// EstimateBilling 价格矩阵按秒计费时按请求秒数预扣；上游按请求秒数收费，完成时不调整。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	table, ok := video_billing.GetPriceTable(info.OriginModelName)
	if !ok || table.Unit != video_billing.UnitPerSecond {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	_, seconds, err := buildGenerationBody(req, info.UpstreamModelName)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(seconds)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + generationsPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	body, _, err := buildGenerationBody(req, info.UpstreamModelName)
	if err != nil {
		return nil, err
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
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

	var submitResp videoResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %.500s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := strings.TrimSpace(submitResp.ID)
	if upstreamID == "" {
		upstreamID = strings.TrimSpace(submitResp.TaskID)
	}
	if upstreamID == "" {
		// 没有任务号就无法轮询，按提交失败处理，免得留下要等超时才退款的任务。
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task id is empty, body: %.500s", responseBody), "invalid_response", http.StatusInternalServerError)
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, baseUrl+generationsPath+"/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var queryResp videoResponse
	if err := common.Unmarshal(respBody, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	result := &relaycommon.TaskInfo{TaskID: queryResp.ID}
	switch queryResp.Status {
	case "queued":
		result.Status = model.TaskStatusQueued
	case "in_progress":
		result.Status = model.TaskStatusInProgress
	case "completed":
		result.Url = strings.TrimSpace(queryResp.URL)
		if result.Url == "" {
			result.Url = strings.TrimSpace(queryResp.VideoURL)
		}
		result.Progress = taskcommon.ProgressComplete
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "task completed without a video url"
			return result, nil
		}
		result.Status = model.TaskStatusSuccess
		return result, nil
	case "failed":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = "task failed"
		if queryResp.Error != nil && strings.TrimSpace(queryResp.Error.Message) != "" {
			result.Reason = fmt.Sprintf("%s: %s", queryResp.Error.Code, queryResp.Error.Message)
		}
		return result, nil
	default:
		// 查询本身出错（鉴权、限流、查不到任务）时只有 error 没有 status，任务状态未知，
		// 交给下一轮轮询；一直查不到的由超时清理退款。
		if queryResp.Error != nil {
			return nil, fmt.Errorf("wenex query failed %s: %s", queryResp.Error.Code, queryResp.Error.Message)
		}
		return nil, fmt.Errorf("wenex query returned unknown status %q", queryResp.Status)
	}
	if queryResp.Progress > 0 && queryResp.Progress < 100 {
		result.Progress = strconv.Itoa(queryResp.Progress) + "%"
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

// SupportsTaskRequest 本线路能否处理这个请求：与提交前的校验同一套规则，校验不过的请求
// （参考视频、2K、时长不在 4~15 秒等）在自动分组选路时换到其他分组。
func (a *TaskAdaptor) SupportsTaskRequest(req relaycommon.TaskSubmitReq) bool {
	_, _, err := buildGenerationBody(req, "")
	return err == nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"h3_video"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// buildGenerationBody 把网关的任务请求转成上游的请求体，返回请求体与计费秒数。
func buildGenerationBody(req relaycommon.TaskSubmitReq, upstreamModel string) (map[string]any, int, error) {
	if _, ok := req.NativeContent(); ok {
		return nil, 0, errors.New("content is not supported; use first_frame_url/last_frame_url/reference_images/audio_urls")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, 0, errors.New("prompt is required")
	}
	if utf8.RuneCountInString(prompt) > maxPromptChars {
		return nil, 0, fmt.Errorf("prompt must be at most %d characters", maxPromptChars)
	}

	seconds, err := requestSeconds(req)
	if err != nil {
		return nil, 0, err
	}
	quality, ratio, err := outputSpec(req)
	if err != nil {
		return nil, 0, err
	}
	body := map[string]any{
		"model":        upstreamModel,
		"prompt":       prompt,
		"seconds":      seconds,
		"resolution":   quality,
		"aspect_ratio": ratio,
	}

	if value := requestValue(req, "seed"); value != nil {
		seed, ok := value.(float64)
		if !ok || seed != math.Trunc(seed) || seed < 0 || seed > maxSeed {
			return nil, 0, fmt.Errorf("seed must be an integer between 0 and %d, got %v", int64(maxSeed), value)
		}
		body["seed"] = int64(seed)
	}

	martialArts := requestValue(req, "martial_arts")
	if martialArts != nil {
		if _, ok := martialArts.(bool); !ok {
			return nil, 0, fmt.Errorf("martial_arts must be a boolean, got %v", martialArts)
		}
	}
	if martialArts == true {
		metadata := map[string]any{"martial_arts": true}
		if value := requestValue(req, "motion_speed"); value != nil {
			speed, ok := value.(float64)
			if !ok || math.Abs(speed) > maxMotionSpeed || speed*2 != math.Trunc(speed*2) {
				return nil, 0, fmt.Errorf("motion_speed must be between -%d and %d in steps of 0.5, got %v", maxMotionSpeed, maxMotionSpeed, value)
			}
			metadata["motion_speed"] = speed
		}
		body["metadata"] = metadata
	} else if requestValue(req, "motion_speed") != nil {
		return nil, 0, errors.New("motion_speed requires martial_arts to be true")
	}

	if err := addMedia(body, req); err != nil {
		return nil, 0, err
	}
	return body, seconds, nil
}

// requestSeconds 请求秒数：seconds、duration、metadata 里的 duration 依次取，默认 5 秒。
func requestSeconds(req relaycommon.TaskSubmitReq) (int, error) {
	if raw := strings.TrimSpace(req.Seconds); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("seconds must be an integer between %d and %d, got %q", minSeconds, maxSeconds, raw)
		}
		return checkSeconds(seconds)
	}
	if req.Duration != 0 {
		return checkSeconds(req.Duration)
	}
	if value := metadataValue(req.Metadata, "duration"); value != nil {
		duration, ok := value.(float64)
		if !ok || duration != math.Trunc(duration) || math.Abs(duration) > math.MaxInt32 {
			return 0, fmt.Errorf("duration must be an integer between %d and %d, got %v", minSeconds, maxSeconds, value)
		}
		return checkSeconds(int(duration))
	}
	return defaultSeconds, nil
}

func checkSeconds(seconds int) (int, error) {
	if seconds < minSeconds || seconds > maxSeconds {
		return 0, fmt.Errorf("seconds must be between %d and %d, got %d", minSeconds, maxSeconds, seconds)
	}
	return seconds, nil
}

// PriceTier 本次请求在价格表里的档位，由发给上游的画质决定，预扣与实际出片的档位才对得上。
// 768p 就是 768p；1080p 这一档按价格表实际的写法取：挂在工作流线路的模型下是 1088p，
// 挂在官方 H3 等模型下通常写 1080p。请求不合法时返回空串（提交前的校验已经拒绝了）。
func PriceTier(req relaycommon.TaskSubmitReq, table video_billing.ModelPriceTable) string {
	quality, _, err := outputSpec(req)
	if err != nil {
		return ""
	}
	if quality != Quality1080p {
		return quality
	}
	for _, tier := range table.Tiers {
		if strings.EqualFold(strings.TrimSpace(tier.Resolution), priceTier1080p) {
			return priceTier1080p
		}
	}
	return Quality1080p
}

// outputSpec 本次请求的输出画质（768p/1080p）和比例。
func outputSpec(req relaycommon.TaskSubmitReq) (string, string, error) {
	sizeWidth, sizeHeight, hasSize := parseSize(req.Size)
	// size 除了「宽x高」，还可以写档位名（与官方 H3 的 768P 写法一致）、比例或 auto。
	sizeLabel := strings.TrimSpace(req.Size)
	if hasSize || strings.Contains(sizeLabel, ":") || strings.EqualFold(sizeLabel, "auto") {
		sizeLabel = ""
	}

	rawQuality := firstNonBlank(req.Resolution, bodyString(req, "quality"),
		metadataString(req.Metadata, "quality"), metadataString(req.Metadata, "resolution"), sizeLabel)
	// resolution 写成「宽x高」时与 size 同义，并优先于 size。
	if width, height, ok := parseSize(rawQuality); ok {
		sizeWidth, sizeHeight, hasSize = width, height, true
		rawQuality = ""
	}
	quality := Quality768p
	switch strings.ToLower(rawQuality) {
	case "":
		if hasSize && min(sizeWidth, sizeHeight) > 1088 {
			return "", "", fmt.Errorf("size %dx%d is larger than 1080p; supported qualities are 768p and 1080p", sizeWidth, sizeHeight)
		}
		if hasSize && min(sizeWidth, sizeHeight) > 768 {
			quality = Quality1080p
		}
	case "720p", "768p":
	case "1080p", priceTier1080p:
		quality = Quality1080p
	default:
		return "", "", fmt.Errorf("resolution must be 768p or 1080p (1088p), got %q", rawQuality)
	}

	ratio := firstNonBlank(req.Ratio, bodyString(req, "aspect_ratio"),
		metadataString(req.Metadata, "ratio"), metadataString(req.Metadata, "aspect_ratio"))
	if strings.EqualFold(ratio, "adaptive") {
		ratio = ""
	}
	// size 也可以直接写比例（例如 16:9），auto 表示不指定。
	if ratio == "" && !hasSize && strings.Contains(req.Size, ":") {
		ratio = strings.TrimSpace(req.Size)
	}
	if ratio != "" {
		for _, supported := range supportedRatios {
			if ratio == supported.name {
				return quality, ratio, nil
			}
		}
		return "", "", fmt.Errorf("ratio must be one of 21:9, 16:9, 4:3, 1:1, 3:4, 9:16, got %q", ratio)
	}
	if !hasSize && rawQuality == "" {
		return quality, defaultPortraitRatio, nil
	}
	if !hasSize {
		return quality, defaultRatio, nil
	}
	actual := float64(sizeWidth) / float64(sizeHeight)
	for _, supported := range supportedRatios {
		if math.Abs(actual-supported.value)/supported.value <= ratioTolerance {
			return quality, supported.name, nil
		}
	}
	return "", "", fmt.Errorf("size %dx%d does not match a supported ratio (21:9, 16:9, 4:3, 1:1, 3:4, 9:16)", sizeWidth, sizeHeight)
}

// addMedia 把素材转成上游字段。按用途区分的字段直接对应；旧写法传图时按与官方 H3 相同的
// 规则推断：带参考音频、mode=reference 或图片超过两张时按参考图，否则 1 张为首帧、2 张为
// 首尾帧。首尾帧和参考素材不能同时出现，参考音频至少要配 1 张参考图，不支持参考视频。
func addMedia(body map[string]any, req relaycommon.TaskSubmitReq) error {
	media := req.RoleMedia()
	if len(media.ReferenceVideos) > 0 {
		return errors.New("reference videos are not supported by this model")
	}
	firstFrame, lastFrame := media.FirstFrame, media.LastFrame
	references := media.ReferenceImages
	if !req.UsesRoleMedia() {
		var images []string
		seen := map[string]bool{}
		for _, image := range append(append([]string{req.Image}, req.Images...), req.InputReference) {
			if image = strings.TrimSpace(image); image != "" && !seen[image] {
				seen[image] = true
				images = append(images, image)
			}
		}
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if len(media.ReferenceAudios) > 0 || len(images) > 2 || mode == "reference" || mode == "reference-to-video" {
			references = images
		} else if len(images) > 0 {
			firstFrame = images[0]
			if len(images) > 1 {
				lastFrame = images[1]
			}
		}
	}

	hasFrames := firstFrame != "" || lastFrame != ""
	switch {
	case hasFrames && (len(references) > 0 || len(media.ReferenceAudios) > 0):
		return errors.New("first/last frames cannot be combined with reference images or audios")
	case len(references) > maxReferenceImages:
		return fmt.Errorf("at most %d reference images are supported, got %d", maxReferenceImages, len(references))
	case len(media.ReferenceAudios) > maxReferenceAudios:
		return fmt.Errorf("at most %d reference audios are supported, got %d", maxReferenceAudios, len(media.ReferenceAudios))
	case len(media.ReferenceAudios) > 0 && len(references) == 0:
		return errors.New("reference audios require at least one reference image")
	}

	if firstFrame != "" {
		body["first_frame"] = firstFrame
	}
	if lastFrame != "" {
		body["last_frame"] = lastFrame
	}
	if len(references) > 0 {
		images := make([]map[string]string, 0, len(references))
		for _, image := range references {
			images = append(images, map[string]string{"url": image, "role": "reference_image"})
		}
		body["images"] = images
	}
	if len(media.ReferenceAudios) > 0 {
		audios := make([]map[string]string, 0, len(media.ReferenceAudios))
		for _, audio := range media.ReferenceAudios {
			audios = append(audios, map[string]string{"url": audio, "role": "reference_audio"})
		}
		body["audios"] = audios
	}
	return nil
}

// requestValue 读取请求体顶层的字段，没有时读 metadata（含 metadata.parameters）。
func requestValue(req relaycommon.TaskSubmitReq, key string) any {
	if value, ok := req.BodyField(key); ok {
		return value
	}
	return metadataValue(req.Metadata, key)
}

func bodyString(req relaycommon.TaskSubmitReq, key string) string {
	value, _ := req.BodyField(key)
	text, _ := value.(string)
	return strings.TrimSpace(text)
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func parseSize(value string) (int, int, bool) {
	normalized := strings.NewReplacer("*", "x", "×", "x").Replace(strings.ToLower(strings.TrimSpace(value)))
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
