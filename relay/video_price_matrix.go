// CUSTOM: 视频价格矩阵的提交期计费解析（fork 扩展）。
// 配置了 video_billing 价格表的模型完全绕开"模型定价"取价：
//   - 每秒计费：UsePrice=true，Quota = 档价 × QuotaPerUnit × 分组倍率（秒数由 OtherRatios 相乘）
//   - 按 token 计费：UsePrice=false，预扣按约 0.25M token 粗估，
//     档价快照进 PriceData.ModelPrice，完成后由适配器按实际 usage 结算
package relay

import (
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

// resolveTaskPriceData 任务提交期的价格解析：视频价格矩阵优先，
// 未配置矩阵的模型回退到原有的 ModelPriceHelperPerCall 逻辑。
func resolveTaskPriceData(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	if _, ok := video_billing.GetPriceTable(info.OriginModelName); !ok {
		return helper.ModelPriceHelperPerCall(c, info)
	}

	mode, resolution, audio := taskVideoDims(c, info)
	price, unit, ok := video_billing.GetPrice(info.OriginModelName, mode, resolution, audio)
	if !ok {
		return hosttypes.PriceData{}, &videoPriceNotMatchedError{model: info.OriginModelName, mode: mode, resolution: resolution}
	}

	groupRatioInfo := helper.HandleGroupRatio(c, info)

	priceData := hosttypes.PriceData{
		GroupRatioInfo: groupRatioInfo,
		ModelPrice:     price,
	}

	if unit == video_billing.UnitPerSecond {
		priceData.UsePrice = true
		quota, err := common.QuotaFromFloatStrict(price * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.Quota = quota
		return priceData, nil
	}

	// 按 token 计费：ModelRatio 换算为每 token 额度（用于日志展示一致性），
	// 预扣按约 0.25M token 粗估（与上游"倍率一半"启发式同量级），完成后差额结算。
	priceData.UsePrice = false
	priceData.ModelRatio = price * common.QuotaPerUnit / 1_000_000
	quota, err := common.QuotaFromFloatStrict(price * 0.25 * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	priceData.Quota = quota
	return priceData, nil
}

type videoPriceNotMatchedError struct {
	model      string
	mode       string
	resolution string
}

func (e *videoPriceNotMatchedError) Error() string {
	return "video pricing for model " + e.model + " has no tier matching mode=" + e.mode +
		" resolution=" + e.resolution + "; add a default tier or the missing tier in Video Pricing settings"
}

// taskVideoDims 提取本次请求的 (模式, 分辨率, 音轨) 维度。
// remix 请求的模式固定为 v2v，分辨率取自原任务。
func taskVideoDims(c *gin.Context, info *relaycommon.RelayInfo) (string, string, string) {
	if info.Action == constant.TaskActionRemix {
		_, size := originTaskSecondsAndSize(info)
		return video_billing.ModeVideoToVideo, size, ""
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return video_billing.ModeTextToVideo, "", ""
	}

	mode := video_billing.ModeTextToVideo
	if metadataContentHasMedia(req.Metadata, "video_url") {
		mode = video_billing.ModeVideoToVideo
	} else if req.HasImage() || metadataContentHasMedia(req.Metadata, "image_url") {
		mode = video_billing.ModeImageToVideo
	}

	resolution := req.Size
	if metaResolution, ok := req.Metadata["resolution"].(string); ok && metaResolution != "" {
		resolution = metaResolution
	}

	return mode, resolution, requestAudioDimension(c, &req)
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
