// CUSTOM: 视频价格矩阵的提交期计费解析（fork 扩展）。
// 配置了 video_billing 价格表的模型完全绕开"模型定价"取价：
//   - 每秒计费：UsePrice=true，Quota = 档价 × QuotaPerUnit × 分组倍率（秒数由 OtherRatios 相乘）
//   - 按 token 计费：UsePrice=false，预扣按约 0.25M token 粗估，
//     档价快照进 PriceData.ModelPrice，完成后由适配器按实际 usage 结算
//   - 按字计费：与每秒计费同构，字符数由适配器以 chars_10k 乘数给出
package relay

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/video_billing"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// videoTokensPerSecond 各分辨率档的预估 token/秒（参考 Seedance 官方刊例：
// 5 秒视频 480p≈50220、720p≈108000、1080p≈243000、4k≈972000）。
// 仅用于预扣估算，结算以上游实际 usage 为准。
var videoTokensPerSecond = map[string]float64{
	"480p":  10044,
	"720p":  21600,
	"1080p": 48600,
	"2k":    97200,
	"4k":    194400,
}

const defaultVideoEstimateSeconds = 5

// resolveTaskPriceData 任务提交期的价格解析：视频价格矩阵优先，
// 未配置矩阵的模型回退到原有的 ModelPriceHelperPerCall 逻辑。
// klingModeResolution 把可灵的画质档位映射到价格矩阵的 resolution 维度。
func klingModeResolution(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "std":
		return "720p"
	case "pro":
		return "1080p"
	case "4k":
		return "4k"
	}
	return ""
}

func resolveTaskPriceData(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	table, ok := video_billing.GetPriceTable(info.OriginModelName)
	if !ok {
		return helper.ModelPriceHelperPerCall(c, info)
	}

	// 语音合成没有视频维度，只取默认档；按视频请求去解析它的请求体没有意义。
	mode, resolution, audio, seconds := "", "", "", 0
	if table.Unit != video_billing.UnitPer10kChars {
		mode, resolution, audio, seconds = taskVideoDims(c, info)
	}
	price, unit, err := video_billing.GetPrice(info.OriginModelName, info.UsingGroup, mode, resolution, audio)
	if err != nil {
		return hosttypes.PriceData{}, &videoPriceNotMatchedError{model: info.OriginModelName, group: info.UsingGroup, mode: mode, resolution: resolution, audio: audio, err: err}
	}

	groupRatioInfo := helper.HandleGroupRatio(c, info)

	priceData := hosttypes.PriceData{
		GroupRatioInfo: groupRatioInfo,
		ModelPrice:     price,
	}

	if unit == video_billing.UnitPerSecond || unit == video_billing.UnitPer10kChars {
		priceData.UsePrice = true
		quota, err := common.QuotaFromFloatStrict(price * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.Quota = quota
		return priceData, nil
	}

	// 按 token 计费：ModelRatio 换算为每 token 额度（用于日志展示一致性），
	// 预扣按 分辨率档 token/秒 × 时长 估算，完成后按实际 usage 差额结算。
	priceData.UsePrice = false
	priceData.ModelRatio = price * common.QuotaPerUnit / 1_000_000
	if seconds <= 0 {
		seconds = defaultVideoEstimateSeconds
	}
	tokensPerSecond, ok := videoTokensPerSecond[video_billing.NormalizeResolution(resolution)]
	if !ok {
		tokensPerSecond = videoTokensPerSecond["720p"]
	}
	estimatedTokens := tokensPerSecond * float64(seconds)
	quota, err := common.QuotaFromFloatStrict(estimatedTokens / 1_000_000 * price * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	priceData.Quota = quota
	return priceData, nil
}

// MatrixTierExcludedForGroup 判断本次请求要用的价格档位是否排除了该分组，供自动分组
// 选路时跳过不支持的分组。只有价格表配置了不适用分组时才解析请求维度；remix 的维度
// 取自原任务，选路时还拿不到，视为不排除，交给取价环节判断。
func MatrixTierExcludedForGroup(c *gin.Context, modelName string, group string) bool {
	table, ok := video_billing.GetPriceTable(modelName)
	if !ok || !table.HasExcludedGroups() {
		return false
	}
	path := c.Request.URL.Path
	mode, resolution, audio := "", "", ""
	switch {
	case strings.Contains(path, "/remix"):
		return false
	case strings.HasPrefix(path, "/v1/audio/") || table.Unit == video_billing.UnitPer10kChars:
		// 音频只有默认档，没有维度。
	case table.Unit == video_billing.UnitPerImage:
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return false
		}
		body, err := storage.Bytes()
		if err != nil {
			return false
		}
		mode = video_billing.ModeTextToImage
		if strings.Contains(path, "/images/edits") || gjson.GetBytes(body, "image").Exists() || gjson.GetBytes(body, "images").Exists() {
			mode = video_billing.ModeImageToImage
		}
		resolution = gjson.GetBytes(body, "size").String()
	default:
		// 选路发生在请求校验之前，任务请求还没放进上下文，这里自己解析一次（只读）。
		var req relaycommon.TaskSubmitReq
		if strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
			return false
		}
		if err := common.UnmarshalBodyReusable(c, &req); err != nil {
			return false
		}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: modelName}}
		mode, resolution, audio, _ = taskRequestVideoDims(c, info, req)
	}
	_, _, err := video_billing.GetPrice(modelName, group, mode, resolution, audio)
	return errors.Is(err, video_billing.ErrTierExcludedForGroup)
}

type videoPriceNotMatchedError struct {
	model      string
	group      string
	mode       string
	resolution string
	audio      string
	err        error
}

func (e *videoPriceNotMatchedError) Error() string {
	if errors.Is(e.err, video_billing.ErrTierExcludedForGroup) {
		return "model " + e.model + " does not support mode=" + e.mode + " resolution=" + e.resolution +
			" audio=" + e.audio + " in group " + e.group + "; choose another resolution or a group that supports it"
	}
	return "video pricing for model " + e.model + " has no tier matching mode=" + e.mode +
		" resolution=" + e.resolution + "; add a default tier or the missing tier in Video Pricing settings"
}

func (e *videoPriceNotMatchedError) Unwrap() error {
	return e.err
}

// taskVideoDims 提取本次请求的 (模式, 分辨率, 音轨, 时长秒数) 维度。
// remix 请求的模式固定为 v2v，分辨率与时长取自原任务。
func taskVideoDims(c *gin.Context, info *relaycommon.RelayInfo) (string, string, string, int) {
	if info.Action == constant.TaskActionRemix {
		seconds, size := originTaskSecondsAndSize(info)
		return video_billing.ModeVideoToVideo, size, "", seconds
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return video_billing.ModeTextToVideo, "", "", 0
	}
	return taskRequestVideoDims(c, info, req)
}

// taskRequestVideoDims 从已解析的任务请求里取 (模式, 分辨率, 音轨, 时长秒数)。
func taskRequestVideoDims(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (string, string, string, int) {
	// media 的 type 名在各家上游之间不通用，这里必须把用到的写全：漏一个就会
	// 把图生/参考生的请求判成文生视频，落到另一个价格档。万相3.0 的参考类型是
	// reference_video / reference_image。
	mode := video_billing.ModeTextToVideo
	if req.HasVideo() || metadataContentHasMedia(req.Metadata, "video_url") ||
		metadataInputHasMedia(req.Metadata, "video", "base", "feature", "reference_video") {
		mode = video_billing.ModeVideoToVideo
	} else if req.HasImage() || strings.TrimSpace(req.Image) != "" || metadataContentHasMedia(req.Metadata, "image_url") ||
		metadataInputHasMedia(req.Metadata, "image", "first_frame", "last_frame", "refer", "reference_image") {
		mode = video_billing.ModeImageToVideo
	}

	resolution := requestResolutionDimension(c)
	if resolution == "" {
		resolution = req.Size
	}
	if resolution == "" {
		if metaResolution, ok := req.Metadata["resolution"].(string); ok && metaResolution != "" {
			resolution = metaResolution
		} else if metaResolution := metadataParameterString(req.Metadata, "resolution"); metaResolution != "" {
			resolution = metaResolution
		} else {
			// Kling 用 mode 表达画质档位，价格矩阵按 resolution 维度取价，
			// 漏掉任一入口都会落到默认档：4k 的档价是默认档的两倍以上。
			// 顶层 mode 与 metadata 里的 parameters.mode 都是有效入口。
			resolution = klingModeResolution(req.Mode)
			if resolution == "" {
				resolution = klingModeResolution(metadataParameterString(req.Metadata, "mode"))
			}
		}
	}
	if resolution == "" && (strings.HasPrefix(info.UpstreamModelName, "happyhorse-") ||
		strings.HasPrefix(info.UpstreamModelName, "wan3.0-video")) {
		// 两者的上游默认档都是 1080P，留空会落到默认档从而与实际出片不符。
		resolution = "1080p"
	}

	seconds, _ := strconv.Atoi(req.Seconds)
	if seconds == 0 {
		seconds = req.Duration
	}
	if seconds == 0 {
		seconds = metadataParameterInt(req.Metadata, "duration")
	}
	if seconds > relaycommon.MaxTaskDurationSeconds {
		seconds = relaycommon.MaxTaskDurationSeconds
	}

	return mode, resolution, requestAudioDimension(c, &req), seconds
}

// requestResolutionDimension reads the top-level resolution accepted by
// pass-through video APIs. TaskSubmitReq intentionally models the common
// Sora fields and therefore does not retain provider-specific resolution.
func requestResolutionDimension(c *gin.Context) string {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if form, err := common.ParseMultipartFormReusable(c); err == nil {
			if values := form.Value["resolution"]; len(values) > 0 {
				return strings.TrimSpace(values[0])
			}
		}
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	body, err := storage.Bytes()
	if err != nil {
		return ""
	}
	result := gjson.GetBytes(body, "resolution")
	if result.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(result.String())
}

func metadataParameters(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	parameters, _ := metadata["parameters"].(map[string]interface{})
	return parameters
}

func metadataParameterString(metadata map[string]interface{}, key string) string {
	value, _ := metadataParameters(metadata)[key].(string)
	return value
}

func metadataParameterInt(metadata map[string]interface{}, key string) int {
	value := metadataParameters(metadata)[key]
	switch typed := value.(type) {
	case int:
		if typed <= 0 {
			return 0
		}
		return min(typed, relaycommon.MaxTaskDurationSeconds)
	case float64:
		if typed <= 0 || math.IsNaN(typed) || math.IsInf(typed, -1) || typed != math.Trunc(typed) {
			return 0
		}
		if math.IsInf(typed, 1) || typed > float64(relaycommon.MaxTaskDurationSeconds) {
			return relaycommon.MaxTaskDurationSeconds
		}
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		if parsed <= 0 {
			return 0
		}
		return min(parsed, relaycommon.MaxTaskDurationSeconds)
	default:
		return 0
	}
}

func metadataInputHasMedia(metadata map[string]interface{}, mediaTypes ...string) bool {
	if metadata == nil {
		return false
	}
	input, ok := metadata["input"].(map[string]interface{})
	if !ok {
		return false
	}
	media, ok := input["media"].([]interface{})
	if !ok {
		return false
	}
	types := make(map[string]struct{}, len(mediaTypes))
	for _, mediaType := range mediaTypes {
		types[mediaType] = struct{}{}
	}
	for _, item := range media {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := itemMap["type"].(string)
		if _, ok := types[mediaType]; ok {
			return true
		}
	}
	return false
}

// originTaskSecondsAndSize 读取 remix 原任务的时长与分辨率（解析失败时返回零值）。
func originTaskSecondsAndSize(info *relaycommon.RelayInfo) (int, string) {
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil || !exist {
		return 0, ""
	}
	var data struct {
		Seconds string `json:"seconds"`
		Size    string `json:"size"`
	}
	if err := common.Unmarshal(originTask.Data, &data); err != nil {
		return 0, ""
	}
	seconds, _ := strconv.Atoi(data.Seconds)
	if seconds > relaycommon.MaxTaskDurationSeconds {
		seconds = relaycommon.MaxTaskDurationSeconds
	}
	return seconds, data.Size
}

// metadataContentHasMedia 检查 metadata 的 content 数组是否包含指定类型的媒体条目。
func metadataContentHasMedia(metadata map[string]interface{}, mediaType string) bool {
	if metadata == nil {
		return false
	}
	contentSlice, ok := metadata["content"].([]interface{})
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

// requestAudioDimension 提取请求的音轨意图（generate_audio 参数），
// 未显式指定时返回空串，匹配音轨无关的价格档。
func requestAudioDimension(c *gin.Context, req *relaycommon.TaskSubmitReq) string {
	toDimension := func(on bool) string {
		if on {
			return video_billing.AudioOn
		}
		return video_billing.AudioOff
	}
	if v, ok := req.Metadata["generate_audio"]; ok {
		switch value := v.(type) {
		case bool:
			return toDimension(value)
		case string:
			if parsed, err := strconv.ParseBool(value); err == nil {
				return toDimension(parsed)
			}
		}
		return ""
	}
	if v, ok := metadataParameters(req.Metadata)["audio"]; ok {
		switch value := v.(type) {
		case bool:
			return toDimension(value)
		case string:
			if parsed, err := strconv.ParseBool(value); err == nil {
				return toDimension(parsed)
			}
		}
	}
	// 兼容 metadata.OutputConfig.AudioGeneration: "Enabled"/"Disabled" 形态
	for _, configKey := range []string{"OutputConfig", "output_config"} {
		outputConfig, ok := req.Metadata[configKey].(map[string]interface{})
		if !ok {
			continue
		}
		for _, audioKey := range []string{"AudioGeneration", "audio_generation"} {
			if value, ok := outputConfig[audioKey].(string); ok {
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "enabled":
					return video_billing.AudioOn
				case "disabled":
					return video_billing.AudioOff
				}
			}
		}
	}
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if form, err := common.ParseMultipartFormReusable(c); err == nil {
			if values := form.Value["generate_audio"]; len(values) > 0 {
				if parsed, err := strconv.ParseBool(values[0]); err == nil {
					return toDimension(parsed)
				}
			}
		}
		return ""
	}
	if storage, err := common.GetBodyStorage(c); err == nil {
		if body, err := storage.Bytes(); err == nil {
			if result := gjson.GetBytes(body, "generate_audio"); result.IsBool() {
				return toDimension(result.Bool())
			}
		}
	}
	return ""
}
