// CUSTOM: 视频模型分档定价（fork 扩展）。
// 按模型维护 模式(mode) × 分辨率(resolution) × 音轨(audio) 的绝对原价表。
// 配置了价格表的模型不再使用"模型定价"中的价格：
//   - 每秒计费：扣费 = 档位原价(USD/秒) × 秒数 × QuotaPerUnit × 分组倍率
//   - 按 token 计费：结算 = 实际 tokens × 档位原价(USD/百万token)/1M × QuotaPerUnit × 分组倍率
//
// 价格单位与"模型定价"的按次价格一致（美元）。
package video_billing

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// 视频生成模式。
const (
	ModeTextToVideo  = "t2v" // 文生视频
	ModeImageToVideo = "i2v" // 图生视频
	ModeVideoToVideo = "v2v" // 视频生视频（含视频输入）
)

// 图片生成模式（矩阵 v2）。
const (
	ModeTextToImage  = "t2i" // 文生图
	ModeImageToImage = "i2i" // 图生图
)

// 音轨维度取值；空串表示该档价格与音轨无关。
const (
	AudioOn  = "on"
	AudioOff = "off"
)

// 计价单位。
const (
	UnitPerSecond       = "per_second"         // 原价按 USD/秒
	UnitPerMillionToken = "per_million_tokens" // 原价按 USD/百万 token
	UnitPerImage        = "per_image"          // 原价按 USD/张（图片，矩阵 v2）
)

// PriceTier 一档价格。Mode/Resolution/Audio 为空串表示"任意"，
// 查价时取匹配维度最多（最具体）的一档；全空维度的档位是默认档（兜底）。
type PriceTier struct {
	Mode       string  `json:"mode,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
	Audio      string  `json:"audio,omitempty"`
	MinPixels  int64   `json:"min_pixels,omitempty"`
	MaxPixels  int64   `json:"max_pixels,omitempty"`
	Price      float64 `json:"price"`
}

// ResolutionBucket 将平台或模型自定义的具体尺寸归入一个计费档位。
// 例如部分 Vidu 模型会把 1920x1088 定义为 1k，不能用通用短边规则推断。
type ResolutionBucket struct {
	Name  string   `json:"name"`
	Sizes []string `json:"sizes,omitempty"`
}

// ModelPriceTable 单个模型的价格表。
// 两个可选附加单价（加法组件），与输出档价相加构成单次费用：
//   - InputImagePrice 对 per_image 与 per_second 都生效。视频模型的参考图同样
//     产生上游成本（例如百炼对超出免费额度的输入图按张计费），只按输出秒数计价
//     会漏掉这部分，导致平台净亏。
//   - InputTokenPrice 目前只有 per_image 使用。
type ModelPriceTable struct {
	Unit              string             `json:"unit"`
	Tiers             []PriceTier        `json:"tiers,omitempty"`
	ResolutionBuckets []ResolutionBucket `json:"resolution_buckets,omitempty"`
	InputImagePrice   float64            `json:"input_image_price,omitempty"` // USD/张，per_image 与 per_second
	InputTokenPrice   float64            `json:"input_token_price,omitempty"` // USD/百万 token，仅 per_image
}

// VideoBillingSettings 由 config.GlobalConfig 管理。
// DB key: video_billing.price_tables
type VideoBillingSettings struct {
	PriceTables map[string]ModelPriceTable `json:"price_tables"`
}

var videoBillingSettings = VideoBillingSettings{
	PriceTables: map[string]ModelPriceTable{},
}

func init() {
	config.GlobalConfig.Register("video_billing", &videoBillingSettings)
}

// GetPriceTable 返回指定模型的价格表；第二个返回值表示是否配置且单位合法。
func GetPriceTable(modelName string) (ModelPriceTable, bool) {
	table, ok := videoBillingSettings.PriceTables[modelName]
	if !ok {
		return ModelPriceTable{}, false
	}
	if table.Unit != UnitPerSecond && table.Unit != UnitPerMillionToken && table.Unit != UnitPerImage {
		return ModelPriceTable{}, false
	}
	if len(table.Tiers) == 0 {
		return ModelPriceTable{}, false
	}
	return table, true
}

// GetPrice 返回模型在给定 (模式, 分辨率, 音轨) 下命中的原价与计价单位。
// 第二个返回值为计价单位；第三个返回值表示是否命中（模型未配置、或没有任何
// 档位匹配且没有默认档时为 false，调用方应拒绝请求或走原有计费逻辑）。
// 分辨率匹配优先级：精确尺寸串（如 "1440x1440"）> 归档标签（1k/2k/...）> 任意。
func GetPrice(modelName, mode, resolution, audio string) (float64, string, bool) {
	table, ok := GetPriceTable(modelName)
	if !ok {
		return 0, "", false
	}

	rawResolution := strings.ToLower(strings.TrimSpace(resolution))
	normResolution := normalizeForTable(rawResolution, table)
	bestScore := -1
	bestPrice := 0.0
	for _, tier := range table.Tiers {
		score := tierMatchScore(tier, mode, rawResolution, normResolution, audio, table)
		if score < 0 || tier.Price <= 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestPrice = tier.Price
		}
	}
	if bestScore < 0 || bestPrice <= 0 {
		return 0, "", false
	}
	return bestPrice, table.Unit, true
}

// tierMatchScore 返回档位与请求维度的匹配度：-1 不匹配；否则数值越大越具体
// （精确尺寸串命中记 2 分，归档标签命中记 1 分，模式/音轨命中各记 1 分）。
func tierMatchScore(tier PriceTier, mode, rawResolution, normResolution, audio string, table ModelPriceTable) int {
	score := 0
	if tier.Mode != "" {
		if tier.Mode != mode {
			return -1
		}
		score++
	}
	if tier.Resolution != "" {
		tierRaw := strings.ToLower(strings.TrimSpace(tier.Resolution))
		if tierRaw == rawResolution && rawResolution != "" {
			score += 4
		} else if _, _, tierIsExactSize := parseSize(tierRaw); tierIsExactSize {
			return -1
		} else if normalizeForTable(tierRaw, table) == normResolution && normResolution != "" {
			score++
		} else {
			return -1
		}
	}
	if tier.MinPixels != 0 || tier.MaxPixels != 0 {
		if table.Unit != UnitPerImage || tier.MinPixels < 0 || tier.MaxPixels < 0 ||
			(tier.MinPixels > 0 && tier.MaxPixels > 0 && tier.MinPixels > tier.MaxPixels) {
			return -1
		}
		pixelCount, ok := parsePixelCount(rawResolution)
		if !ok || (tier.MinPixels > 0 && pixelCount < tier.MinPixels) ||
			(tier.MaxPixels > 0 && pixelCount > tier.MaxPixels) {
			return -1
		}
		score += 2
	}
	if tier.Audio != "" {
		if tier.Audio != audio {
			return -1
		}
		score++
	}
	return score
}

func parsePixelCount(res string) (int64, bool) {
	parts := strings.Split(normalizeSizeSeparator(res), "x")
	if len(parts) != 2 {
		return 0, false
	}
	w, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	h, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 || w > int64(^uint64(0)>>1)/h {
		return 0, false
	}
	return w * h, true
}

func normalizeSizeSeparator(res string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(res)), "*", "x")
}

func canonicalSize(res string) (string, bool) {
	w, h, ok := parseSize(res)
	if !ok {
		return "", false
	}
	if w > h {
		w, h = h, w
	}
	return strconv.Itoa(w) + "x" + strconv.Itoa(h), true
}

func normalizeForTable(res string, table ModelPriceTable) string {
	res = strings.ToLower(strings.TrimSpace(res))
	// 归档表对所有计价单位都生效：通用的短边规则只产出 480p/720p/1080p/2k/4k，
	// 供应商自定义的档位（例如 Vidu 的 540P，其 1024*576 短边是 576）无法表达，
	// 不配归档表就会命中错误的档价。
	if name, ok := matchResolutionBucket(res, table.ResolutionBuckets); ok {
		return name
	}
	if table.Unit != UnitPerImage {
		return NormalizeResolution(res)
	}
	return NormalizeImageResolution(res)
}

// matchResolutionBucket 先按档位名精确匹配，再按具体尺寸匹配。
func matchResolutionBucket(res string, buckets []ResolutionBucket) (string, bool) {
	for _, bucket := range buckets {
		name := strings.ToLower(strings.TrimSpace(bucket.Name))
		if name != "" && res == name {
			return name, true
		}
	}
	requestSize, requestIsSize := canonicalSize(res)
	if !requestIsSize {
		return "", false
	}
	for _, bucket := range buckets {
		name := strings.ToLower(strings.TrimSpace(bucket.Name))
		if name == "" {
			continue
		}
		for _, size := range bucket.Sizes {
			bucketSize, ok := canonicalSize(size)
			if ok && bucketSize == requestSize {
				return name, true
			}
		}
	}
	return "", false
}

// ResolveImageResolution applies a model's exact resolution buckets first,
// then falls back to the generic short-edge 1K/2K/4K classification.
func ResolveImageResolution(modelName, resolution string) string {
	table, ok := GetPriceTable(modelName)
	if !ok || table.Unit != UnitPerImage {
		return NormalizeImageResolution(resolution)
	}
	return normalizeForTable(resolution, table)
}

// NormalizeImageResolution 将图片分辨率归档：按短边 ≤1024→1k、≤2048→2k、其余→4k；
// 非 宽x高 形式的标签（如 "1k"）原样返回。
func NormalizeImageResolution(res string) string {
	res = strings.ToLower(strings.TrimSpace(res))
	if res == "" {
		return ""
	}
	if w, h, ok := parseSize(res); ok {
		short := w
		if h < w {
			short = h
		}
		switch {
		case short <= 1024:
			return "1k"
		case short <= 2048:
			return "2k"
		default:
			return "4k"
		}
	}
	return res
}

// NormalizeResolution 将分辨率表述归一到档位标签。
// 支持 "480p"/"720P"/"1080p"/"2k"/"4k" 与 "宽x高"（如 "1792x1024"，按短边归档）。
func NormalizeResolution(res string) string {
	res = strings.ToLower(strings.TrimSpace(res))
	if res == "" {
		return ""
	}
	if w, h, ok := parseSize(res); ok {
		short := w
		if h < w {
			short = h
		}
		switch {
		case short <= 480:
			return "480p"
		case short <= 720:
			return "720p"
		case short <= 1080:
			return "1080p"
		case short <= 1440:
			return "2k"
		default:
			return "4k"
		}
	}
	return res
}

func parseSize(res string) (int, int, bool) {
	parts := strings.Split(normalizeSizeSeparator(res), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// GetAllTablesCopy 返回全部价格表的浅拷贝，供定价接口对外展示。
func GetAllTablesCopy() map[string]ModelPriceTable {
	if len(videoBillingSettings.PriceTables) == 0 {
		return nil
	}
	copied := make(map[string]ModelPriceTable, len(videoBillingSettings.PriceTables))
	for name, table := range videoBillingSettings.PriceTables {
		copied[name] = table
	}
	return copied
}
