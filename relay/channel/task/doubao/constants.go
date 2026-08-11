package doubao

import (
	"fmt"
	"strings"
)

var ModelList = []string{
	"doubao-seedance-2-5-260628",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
	"doubao-seedance-2-0-mini-260615",
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-pro-250428",
	"doubao-seedance-1-0-pro-fast-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-0-lite-t2v-250219",
	"doubao-seedance-1-0-lite-i2v-250219",
	"doubao-seedance-1-5-pro-251215",
}

var ChannelName = "doubao-video"

// videoPriceKey 价格表的键：输出分辨率档（is1080p/is4k 均为 false 即 480p/720p 基准档）、输入是否含视频。
type videoPriceKey struct {
	is1080p  bool
	is4k     bool
	hasVideo bool
}

// videoPriceTable 各模型在不同 (输出分辨率档, 是否含视频输入) 下的单价（元/百万 token）。
// 其中零值键 {480p/720p, 不含视频} 为基准价，等于管理员应配置的 ModelRatio；
// 计费时取 实际单价/基准价 作为 OtherRatio。
// CUSTOM: 仅作为未配置 video_billing 价格矩阵时的兜底（保持上游行为一致）；
// 配置了矩阵的模型走绝对价计费，不再使用本表。
var videoPriceTable = map[string]map[videoPriceKey]float64{
	"doubao-seedance-2-0-260128": {
		{hasVideo: false}:                46.0,
		{hasVideo: true}:                 28.0,
		{is1080p: true, hasVideo: false}: 51.0,
		{is1080p: true, hasVideo: true}:  31.0,
		{is4k: true, hasVideo: false}:    26.0,
		{is4k: true, hasVideo: true}:     16.0,
	},
	"doubao-seedance-2-0-fast-260128": {
		{hasVideo: false}: 37.0,
		{hasVideo: true}:  22.0,
	},
}

// legacyVideoInputRatio 返回指定模型在给定输出分辨率/是否含视频输入下，相对基准价的计费倍率。
// 第二个返回值表示该模型是否配置了价格表；倍率为 1.0 时调用方可忽略该 OtherRatio。
func legacyVideoInputRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	prices, ok := videoPriceTable[modelName]
	base := prices[videoPriceKey{}] // 零值键 = {480p/720p, 不含视频} 基准价
	if !ok || base <= 0 {
		return 0, false
	}
	res := strings.ToLower(strings.TrimSpace(resolution))
	price, ok := prices[videoPriceKey{is1080p: res == "1080p", is4k: res == "4k", hasVideo: hasVideo}]
	if !ok {
		// 未配置的组合（如 fast 无 1080p/4k，上游会自行报错）按基准价计费即可。
		return 1.0, true
	}
	return price / base, true
}

type seedanceFamily string

const (
	seedanceUnknown seedanceFamily = ""
	seedance10      seedanceFamily = "1.0"
	seedance15      seedanceFamily = "1.5"
	seedance20      seedanceFamily = "2.0"
	seedance25      seedanceFamily = "2.5"
)

func identifySeedanceFamily(names ...string) seedanceFamily {
	for _, name := range names {
		normalized := strings.ToLower(name)
		switch {
		case strings.Contains(normalized, "seedance-2-5"):
			return seedance25
		case strings.Contains(normalized, "seedance-2-0"):
			return seedance20
		case strings.Contains(normalized, "seedance-1-5"):
			return seedance15
		case strings.Contains(normalized, "seedance-1-0"):
			return seedance10
		}
	}
	return seedanceUnknown
}

func validateRequestPayload(req *requestPayload, originModel string) error {
	if req == nil {
		return fmt.Errorf("request payload is required")
	}
	family := identifySeedanceFamily(req.Model, originModel)
	if err := validateCommonParameters(req); err != nil {
		return err
	}

	var textCount, imageCount, videoCount, audioCount, draftTaskCount int
	var firstFrameCount, lastFrameCount, referenceImageCount int
	for _, item := range req.Content {
		switch item.Type {
		case "text":
			if strings.TrimSpace(item.Text) == "" {
				return fmt.Errorf("content text cannot be empty")
			}
			textCount++
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return fmt.Errorf("content image_url.url is required")
			}
			if item.Role != "" && item.Role != "first_frame" && item.Role != "last_frame" && item.Role != "reference_image" {
				return fmt.Errorf("unsupported image role %q", item.Role)
			}
			switch item.Role {
			case "first_frame":
				firstFrameCount++
			case "last_frame":
				lastFrameCount++
			case "reference_image":
				referenceImageCount++
			}
			imageCount++
		case "video_url":
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" {
				return fmt.Errorf("content video_url.url is required")
			}
			if item.Role != "" && item.Role != "reference_video" {
				return fmt.Errorf("unsupported video role %q", item.Role)
			}
			videoCount++
		case "audio_url":
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" {
				return fmt.Errorf("content audio_url.url is required")
			}
			if item.Role != "" && item.Role != "reference_audio" {
				return fmt.Errorf("unsupported audio role %q", item.Role)
			}
			audioCount++
		case "draft_task":
			if item.DraftTask == nil || strings.TrimSpace(item.DraftTask.ID) == "" {
				return fmt.Errorf("content draft_task.id is required")
			}
			draftTaskCount++
		default:
			return fmt.Errorf("unsupported content type %q", item.Type)
		}
	}
	if textCount+imageCount+videoCount+audioCount+draftTaskCount == 0 {
		return fmt.Errorf("content is required")
	}
	if textCount == 0 && imageCount+videoCount+audioCount+draftTaskCount == 0 {
		return fmt.Errorf("prompt or media content is required")
	}
	if draftTaskCount > 1 || draftTaskCount > 0 && textCount+imageCount+videoCount+audioCount > 0 {
		return fmt.Errorf("draft_task must be the only content input")
	}
	if firstFrameCount > 1 || lastFrameCount > 1 || lastFrameCount > firstFrameCount {
		return fmt.Errorf("first/last frame content must contain one first_frame and at most one last_frame")
	}
	if firstFrameCount+lastFrameCount > 0 && referenceImageCount+videoCount+audioCount > 0 {
		return fmt.Errorf("first/last frame inputs cannot be mixed with reference media")
	}

	if family == seedanceUnknown {
		return nil
	}
	if err := validateModelParameters(req, family, imageCount, videoCount, audioCount, draftTaskCount, firstFrameCount, lastFrameCount, referenceImageCount); err != nil {
		return err
	}
	return nil
}

func validateCommonParameters(req *requestPayload) error {
	if req.Resolution != "" {
		switch strings.ToLower(req.Resolution) {
		case "480p", "720p", "1080p", "4k":
		default:
			return fmt.Errorf("unsupported resolution %q", req.Resolution)
		}
	}
	if req.Ratio != "" {
		switch strings.ToLower(req.Ratio) {
		case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
		default:
			return fmt.Errorf("unsupported ratio %q", req.Ratio)
		}
	}
	if req.OutputFormat != "" && req.OutputFormat != "mp4" && req.OutputFormat != "mov" {
		return fmt.Errorf("unsupported output_format %q", req.OutputFormat)
	}
	if req.OmniReferenceTaskType != "" {
		switch req.OmniReferenceTaskType {
		case "auto", "reference", "edit", "extend":
		default:
			return fmt.Errorf("unsupported omni_reference_task_type %q", req.OmniReferenceTaskType)
		}
	}
	if req.Priority != nil && (int(*req.Priority) < 0 || int(*req.Priority) > 9) {
		return fmt.Errorf("priority must be between 0 and 9")
	}
	if req.ExecutionExpiresAfter != nil && (int(*req.ExecutionExpiresAfter) < 3600 || int(*req.ExecutionExpiresAfter) > 259200) {
		return fmt.Errorf("execution_expires_after must be between 3600 and 259200")
	}
	if req.Seed != nil && (int(*req.Seed) < -1 || int64(*req.Seed) > 2147483647) {
		return fmt.Errorf("seed must be between -1 and 2147483647")
	}
	if req.Frames != nil {
		frames := int(*req.Frames)
		if frames < 29 || frames > 289 || (frames-25)%4 != 0 {
			return fmt.Errorf("frames must be between 29 and 289 and match 25 + 4n")
		}
	}
	if req.ServiceTier != "" && req.ServiceTier != "default" && req.ServiceTier != "flex" {
		return fmt.Errorf("unsupported service_tier %q", req.ServiceTier)
	}
	for _, tool := range req.Tools {
		if tool.Type != "web_search" {
			return fmt.Errorf("unsupported tool type %q", tool.Type)
		}
	}
	return nil
}

func validateModelParameters(req *requestPayload, family seedanceFamily, imageCount, videoCount, audioCount, draftTaskCount, firstFrameCount, lastFrameCount, referenceImageCount int) error {
	resolution := strings.ToLower(req.Resolution)
	modelName := strings.ToLower(req.Model)
	if resolution != "" {
		supported := true
		switch family {
		case seedance25:
			supported = resolution == "480p" || resolution == "720p"
		case seedance20:
			if strings.Contains(modelName, "fast") || strings.Contains(modelName, "mini") {
				supported = resolution == "480p" || resolution == "720p"
			}
		case seedance15, seedance10:
			supported = resolution != "4k"
		}
		if !supported {
			return fmt.Errorf("model %s does not support resolution %s", req.Model, req.Resolution)
		}
	}

	if req.Duration != nil {
		duration := int(*req.Duration)
		valid := false
		switch family {
		case seedance25:
			valid = duration == -1 || duration >= 4 && duration <= 30
		case seedance20:
			valid = duration == -1 || duration >= 4 && duration <= 15
		case seedance15:
			valid = duration == -1 || duration >= 4 && duration <= 12
		case seedance10:
			valid = duration >= 2 && duration <= 12
		}
		if !valid {
			return fmt.Errorf("model %s does not support duration %d", req.Model, duration)
		}
	}
	if req.Frames != nil && family != seedance10 {
		return fmt.Errorf("model %s does not support frames", req.Model)
	}
	if req.Seed != nil && family != seedance10 && family != seedance15 {
		return fmt.Errorf("model %s does not support seed", req.Model)
	}
	if req.GenerateAudio != nil && family != seedance15 && family != seedance20 && family != seedance25 {
		return fmt.Errorf("model %s does not support generate_audio", req.Model)
	}
	if req.Priority != nil && family != seedance20 && family != seedance25 {
		return fmt.Errorf("model %s does not support priority", req.Model)
	}
	if req.OmniReferenceTaskType != "" && family != seedance25 {
		return fmt.Errorf("model %s does not support omni_reference_task_type", req.Model)
	}
	if req.OutputFormat != "" && family != seedance25 {
		return fmt.Errorf("model %s does not support output_format", req.Model)
	}
	if req.Draft != nil && bool(*req.Draft) {
		if family != seedance15 {
			return fmt.Errorf("model %s does not support draft mode", req.Model)
		}
		if resolution != "" && resolution != "480p" {
			return fmt.Errorf("draft mode requires resolution 480p")
		}
		if req.ReturnLastFrame != nil && bool(*req.ReturnLastFrame) {
			return fmt.Errorf("draft mode does not support return_last_frame")
		}
		if req.ServiceTier == "flex" {
			return fmt.Errorf("draft mode does not support service_tier flex")
		}
	}
	if draftTaskCount > 0 && family != seedance15 {
		return fmt.Errorf("model %s does not support draft_task", req.Model)
	}
	if (family == seedance20 || family == seedance25) && req.ServiceTier == "flex" {
		return fmt.Errorf("model %s does not support service_tier flex", req.Model)
	}
	if req.CameraFixed != nil && bool(*req.CameraFixed) && referenceImageCount > 0 {
		return fmt.Errorf("camera_fixed is not supported with reference images")
	}
	if family == seedance25 && firstFrameCount+lastFrameCount > 0 && req.Ratio != "" && req.Ratio != "adaptive" {
		return fmt.Errorf("Seedance 2.5 first/last frame mode requires ratio adaptive")
	}
	if strings.Contains(modelName, "lite-t2v") && imageCount+videoCount+audioCount > 0 {
		return fmt.Errorf("model %s only supports text-to-video", req.Model)
	}
	if strings.Contains(modelName, "lite-i2v") {
		if imageCount == 0 {
			return fmt.Errorf("model %s requires image input", req.Model)
		}
		if imageCount > 4 {
			return fmt.Errorf("model %s supports at most 4 images", req.Model)
		}
	}
	if strings.Contains(modelName, "pro-fast") && lastFrameCount > 0 {
		return fmt.Errorf("model %s does not support last_frame", req.Model)
	}

	switch family {
	case seedance25:
		if imageCount > 30 || videoCount > 10 || audioCount > 10 {
			return fmt.Errorf("Seedance 2.5 supports at most 30 images, 10 videos and 10 audios")
		}
	case seedance20:
		if imageCount > 9 || videoCount > 3 || audioCount > 3 {
			return fmt.Errorf("Seedance 2.0 supports at most 9 images, 3 videos and 3 audios")
		}
		if audioCount > 0 && imageCount+videoCount == 0 {
			return fmt.Errorf("Seedance 2.0 audio input requires at least one image or video")
		}
	default:
		if videoCount > 0 || audioCount > 0 {
			return fmt.Errorf("model %s does not support reference video or audio content", req.Model)
		}
	}

	if req.OmniReferenceTaskType == "edit" || req.OmniReferenceTaskType == "extend" {
		if videoCount == 0 {
			return fmt.Errorf("%s mode requires at least one reference_video", req.OmniReferenceTaskType)
		}
		if req.Ratio != "adaptive" {
			return fmt.Errorf("%s mode requires ratio adaptive", req.OmniReferenceTaskType)
		}
		if req.OmniReferenceTaskType == "edit" && (req.Duration == nil || int(*req.Duration) != -1) {
			return fmt.Errorf("edit mode requires duration -1")
		}
	}
	return nil
}
