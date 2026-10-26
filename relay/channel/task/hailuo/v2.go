// CUSTOM: MiniMax 官方视频生成 V2 接口，覆盖 H3 系列（fork 扩展）。
// 文档：https://platform.minimax.cn/docs/api-reference/video-generation-v2-create
//
//	https://platform.minimax.cn/docs/api-reference/video-generation-v2-query
//
// V2 用 content 数组传素材，每项以 type（text/image_url/video_url/audio_url）区分、以
// role（first_frame/last_frame/reference_image/reference_video/reference_audio）标注用途；
// 首尾帧与多模态参考互斥。查询结果直接给出视频地址和用量（输出秒数、参考视频秒数、
// 输入图片数），按秒计费时参考视频按同档单价计时长，图片扣掉免费张数后按张计费。
//
// 网关入口的写法都支持：prompt + 首帧/尾帧/参考图/参考视频/参考音频字段，旧的
// image/images/video/audio(s)，以及原样透传的 metadata.content。分辨率、比例、水印
// 与百炼 MiniMax 的写法兼容（metadata.parameters.* 和 metadata 顶层都认）。
package hailuo

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	v2CreateEndpoint = "/v2/video_generation"
	v2QueryEndpoint  = "/v2/query/video_generation/"

	v2MaxDuration        = 15
	v2DefaultDuration    = 5
	v2MaxPromptChars     = 7000
	v2MaxFrameImages     = 1
	v2MaxReferenceImages = 9
	v2MaxReferenceVideos = 3
	v2MaxReferenceAudios = 3
)

// v2ModelConfig 各 H3 型号的取值范围（见官方文档）。
type v2ModelConfig struct {
	resolutions []string
	minDuration int
	// adaptiveReference 多模态参考生视频是否支持 adaptive 比例（Lite 不支持）。
	adaptiveReference bool
	// promptExpansion 是否支持 extra.prompt_expansion_mode（仅 Max）。
	promptExpansion bool
}

var v2Models = map[string]v2ModelConfig{
	"MiniMax-H3":           {resolutions: []string{"768P", "2K"}, minDuration: 4, adaptiveReference: true},
	"MiniMax-H3-Highspeed": {resolutions: []string{"768P"}, minDuration: 4, adaptiveReference: true},
	"MiniMax-H3-Lite":      {resolutions: []string{"480P", "768P"}, minDuration: 4},
	"MiniMax-H3-Max":       {resolutions: []string{"480P", "768P"}, minDuration: 5, adaptiveReference: true, promptExpansion: true},
}

var v2Ratios = []string{"adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}

var v2PromptExpansionModes = []string{"disabled", "balanced", "quality"}

// IsV2Model 判断上游模型是否走 V2 接口（H3 系列）。
func IsV2Model(model string) bool {
	return strings.HasPrefix(model, "MiniMax-H3")
}

// ResolutionTier 把尺寸表述归到 H3 的分辨率档位：档位名原样（480P/768P/2K），
// 「宽x高」按长边归档。计费取价与请求组装用同一套归档，两边才对得上。
func ResolutionTier(size string) string {
	value := strings.ToUpper(strings.TrimSpace(size))
	if value == "" {
		return ""
	}
	if _, err := strconv.Atoi(value); err == nil {
		return value + "P"
	}
	normalized := strings.NewReplacer("*", "X", "×", "X").Replace(value)
	if parts := strings.Split(normalized, "X"); len(parts) == 2 {
		width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
		height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errW == nil && errH == nil && width > 0 && height > 0 {
			// 与百炼 MiniMax 的归档一致：长边超过 1280 为 2K。
			switch longEdge := max(width, height); {
			case longEdge <= 960:
				return "480P"
			case longEdge <= 1280:
				return "768P"
			default:
				return "2K"
			}
		}
	}
	return value
}

type v2MediaURL struct {
	URL string `json:"url"`
}

type v2ContentItem struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *v2MediaURL `json:"image_url,omitempty"`
	VideoURL *v2MediaURL `json:"video_url,omitempty"`
	AudioURL *v2MediaURL `json:"audio_url,omitempty"`
	Role     string      `json:"role,omitempty"`
}

// v2Request 不含 callback_url：回调地址由平台管理，调用方传了也不转发。
type v2Request struct {
	Model         string         `json:"model"`
	Content       []any          `json:"content"`
	Resolution    string         `json:"resolution"`
	Duration      int            `json:"duration"`
	Ratio         string         `json:"ratio,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
	AigcWatermark *bool          `json:"aigc_watermark,omitempty"`
}

func invalidV2Request(format string, args ...any) error {
	return relaycommon.NewInvalidRequest(fmt.Errorf(format, args...))
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

func mediaItem(itemType, role, url string) v2ContentItem {
	item := v2ContentItem{Type: itemType, Role: role}
	switch itemType {
	case "image_url":
		item.ImageURL = &v2MediaURL{URL: url}
	case "video_url":
		item.VideoURL = &v2MediaURL{URL: url}
	case "audio_url":
		item.AudioURL = &v2MediaURL{URL: url}
	}
	return item
}

// v2LegacyImages 汇总 image/images/input_reference，去空白与重复。
func v2LegacyImages(req relaycommon.TaskSubmitReq) []string {
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

// v2MediaItems 把网关的素材字段组装成 content 项。按用途区分的字段直接带 role；
// 旧写法传图时按与百炼 MiniMax 相同的规则推断：带参考视频或音频、mode=reference、或图片
// 超过两张时整体按多模态参考组装，否则 1 张为首帧、2 张为首尾帧。参考视频/音频两种写法合并。
func v2MediaItems(req relaycommon.TaskSubmitReq) []any {
	var items []any
	media := req.RoleMedia()
	if req.UsesRoleMedia() {
		if media.FirstFrame != "" {
			items = append(items, mediaItem("image_url", "first_frame", media.FirstFrame))
		}
		if media.LastFrame != "" {
			items = append(items, mediaItem("image_url", "last_frame", media.LastFrame))
		}
		for _, url := range media.ReferenceImages {
			items = append(items, mediaItem("image_url", "reference_image", url))
		}
		for _, url := range media.ReferenceVideos {
			items = append(items, mediaItem("video_url", "reference_video", url))
		}
		for _, url := range media.ReferenceAudios {
			items = append(items, mediaItem("audio_url", "reference_audio", url))
		}
		return items
	}

	images := v2LegacyImages(req)
	videos, audios := media.ReferenceVideos, media.ReferenceAudios
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	reference := len(videos) > 0 || len(audios) > 0 || len(images) > 2 ||
		mode == "reference" || mode == "reference-to-video"
	for index, image := range images {
		role := "reference_image"
		if !reference {
			role = "first_frame"
			if index == 1 {
				role = "last_frame"
			}
		}
		items = append(items, mediaItem("image_url", role, image))
	}
	for _, video := range videos {
		items = append(items, mediaItem("video_url", "reference_video", video))
	}
	for _, audio := range audios {
		items = append(items, mediaItem("audio_url", "reference_audio", audio))
	}
	return items
}

// v2ContentStats 统计 content 里的文本与各用途素材数量（原生透传的 content 同样统计）。
type v2ContentStats struct {
	hasText bool
	roles   map[string]int
}

func v2Stats(content []any) v2ContentStats {
	stats := v2ContentStats{roles: map[string]int{}}
	for _, raw := range content {
		var itemType, role, text string
		switch item := raw.(type) {
		case v2ContentItem:
			itemType, role, text = item.Type, item.Role, item.Text
		case map[string]any:
			itemType, _ = item["type"].(string)
			role, _ = item["role"].(string)
			text, _ = item["text"].(string)
		}
		if itemType == "text" {
			if strings.TrimSpace(text) != "" {
				stats.hasText = true
			}
			continue
		}
		if role == "" && itemType == "image_url" {
			// 只有一张图且不填 role 时上游按首帧处理。
			role = "first_frame"
		}
		stats.roles[role]++
	}
	return stats
}

func (s v2ContentStats) frames() int {
	return s.roles["first_frame"] + s.roles["last_frame"]
}

func (s v2ContentStats) references() int {
	return s.roles["reference_image"] + s.roles["reference_video"] + s.roles["reference_audio"]
}

// buildV2Request 组装 V2 请求并做与计费、上游规则相关的校验，校验失败返回 400。
func buildV2Request(req relaycommon.TaskSubmitReq, model string) (*v2Request, error) {
	config, ok := v2Models[model]
	if !ok {
		return nil, invalidV2Request("unsupported MiniMax V2 model %s", model)
	}
	metadata := req.Metadata
	prompt := strings.TrimSpace(req.Prompt)
	if n := len([]rune(prompt)); n > v2MaxPromptChars {
		return nil, invalidV2Request("prompt must not exceed %d characters, got %d", v2MaxPromptChars, n)
	}

	var content []any
	if native, ok := req.NativeContent(); ok {
		// 原生 content（metadata.content 或与官方一致的顶层 content）原样透传；
		// 没有文本项时把 prompt 补进去（prompt 必填）。
		content = append(content, native...)
		if !v2Stats(content).hasText && prompt != "" {
			content = append([]any{v2ContentItem{Type: "text", Text: prompt}}, content...)
		}
	} else {
		if prompt != "" {
			content = append(content, v2ContentItem{Type: "text", Text: prompt})
		}
		content = append(content, v2MediaItems(req)...)
	}

	stats := v2Stats(content)
	switch {
	case !stats.hasText:
		return nil, invalidV2Request("%s requires a non-empty prompt", model)
	case stats.frames() > 0 && stats.references() > 0:
		return nil, invalidV2Request("%s cannot combine first_frame/last_frame with reference_image/reference_video/reference_audio", model)
	case stats.roles["first_frame"] > v2MaxFrameImages || stats.roles["last_frame"] > v2MaxFrameImages:
		return nil, invalidV2Request("%s accepts at most one first frame and one last frame", model)
	case stats.roles["reference_image"] > v2MaxReferenceImages:
		return nil, invalidV2Request("%s accepts at most %d reference images", model, v2MaxReferenceImages)
	case stats.roles["reference_video"] > v2MaxReferenceVideos:
		return nil, invalidV2Request("%s accepts at most %d reference videos", model, v2MaxReferenceVideos)
	case stats.roles["reference_audio"] > v2MaxReferenceAudios:
		return nil, invalidV2Request("%s accepts at most %d reference audios", model, v2MaxReferenceAudios)
	}

	resolution := ResolutionTier(firstNonEmpty(metadataString(metadata, "resolution"), req.Resolution, req.Size))
	if resolution == "" {
		resolution = "768P"
	}
	if !contains(config.resolutions, resolution) {
		return nil, invalidV2Request("%s supports resolution %s, got %q", model, strings.Join(config.resolutions, "/"), resolution)
	}

	duration := req.Duration
	if duration == 0 {
		duration, _ = strconv.Atoi(strings.TrimSpace(req.Seconds))
	}
	if duration == 0 {
		if value, ok := metadataValue(metadata, "duration").(float64); ok {
			duration = int(math.Max(math.Min(value, math.MaxInt32), math.MinInt32))
		}
	}
	if duration == 0 {
		duration = v2DefaultDuration
	}
	if duration < config.minDuration || duration > v2MaxDuration {
		return nil, invalidV2Request("%s duration must be between %d and %d seconds, got %d", model, config.minDuration, v2MaxDuration, duration)
	}

	ratio := strings.ToLower(firstNonEmpty(metadataString(metadata, "ratio"), metadataString(metadata, "aspect_ratio"), req.Ratio))
	switch {
	case stats.frames() == 0 && stats.references() == 0:
		// 文生视频比例必填且不能是 adaptive；不填时按 16:9。
		if ratio == "" {
			ratio = "16:9"
		}
		if ratio == "adaptive" {
			return nil, invalidV2Request("%s text-to-video requires an explicit ratio, not adaptive", model)
		}
	case stats.references() > 0 && !config.adaptiveReference && (ratio == "" || ratio == "adaptive"):
		ratio = "16:9"
	}
	if ratio != "" && !contains(v2Ratios, ratio) {
		return nil, invalidV2Request("%s does not support ratio %q; use %s", model, ratio, strings.Join(v2Ratios, ", "))
	}

	request := &v2Request{
		Model:      model,
		Content:    content,
		Resolution: resolution,
		Duration:   duration,
		Ratio:      ratio,
	}

	expansion := metadataString(metadata, "prompt_expansion_mode")
	if extra, ok := metadata["extra"].(map[string]any); ok && expansion == "" {
		expansion, _ = extra["prompt_expansion_mode"].(string)
	}
	if expansion != "" {
		if !config.promptExpansion {
			return nil, invalidV2Request("%s does not support prompt_expansion_mode", model)
		}
		if !contains(v2PromptExpansionModes, expansion) {
			return nil, invalidV2Request("prompt_expansion_mode must be one of %s", strings.Join(v2PromptExpansionModes, ", "))
		}
		request.Extra = map[string]any{"prompt_expansion_mode": expansion}
	}

	for _, key := range []string{"aigc_watermark", "watermark"} {
		if value, ok := metadataValue(metadata, key).(bool); ok {
			request.AigcWatermark = &value
			break
		}
	}
	return request, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// v2QueryResponse 查询接口的响应；出错时是 OpenAI 风格的 error 对象。
type v2QueryResponse struct {
	Task struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Status  string `json:"status"`
		Content struct {
			URL string `json:"url"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			OutputSeconds   float64 `json:"output_seconds"`
			InputSeconds    float64 `json:"input_seconds"`
			InputImageCount int     `json:"input_image_count"`
		} `json:"usage"`
	} `json:"task"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// V2BillingSeconds 计费秒数 = 输出秒数 + 参考视频秒数（参考视频按同档单价计时长）。
// 两者都钳制在单次时长上限内，异常的上游数值放大不了计费乘数。
func V2BillingSeconds(outputSeconds, inputSeconds float64) float64 {
	return relaycommon.ClampUsageSeconds(outputSeconds) + relaycommon.ClampUsageSeconds(inputSeconds)
}
