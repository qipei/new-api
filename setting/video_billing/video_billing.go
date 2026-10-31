// CUSTOM: 视频模型分档定价（fork 扩展）。
// 按模型维护 模式(mode) × 分辨率(resolution) × 音轨(audio) 的绝对原价表。
// 配置了价格表的模型不再使用"模型定价"中的价格：
//   - 每秒计费：扣费 = 档位原价(USD/秒) × 秒数 × QuotaPerUnit × 分组倍率
//   - 按 token 计费：结算 = 实际 tokens × 档位原价(USD/百万token)/1M × QuotaPerUnit × 分组倍率
//   - 按字计费：扣费 = 档位原价(USD/万字符) × 字符数/1万 × QuotaPerUnit × 分组倍率
//
// 每秒计费同样用于音频生成（按模型原始输出时长），按字计费用于语音合成。
//
// 价格单位与"模型定价"的按次价格一致（美元）。
package video_billing

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// MaxMinSeconds 最低计费时长的上限，与单次任务时长上限（relaycommon.MaxTaskDurationSeconds）一致。
const MaxMinSeconds = 3600

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
	UnitPer10kChars     = "per_10k_chars"      // 原价按 USD/万字符（语音合成）
)

// CharsRatioKey 按字计费时 OtherRatios 里的乘数名，值为 字符数/1万。
// 与上游刊例的计价口径一致，日志里看到的乘数可以直接和账单对上。
const CharsRatioKey = "chars_10k"

// PriceTier 一档价格。Mode/Resolution/Audio 为空串表示"任意"，
// 查价时取匹配维度最多（最具体）的一档；全空维度的档位是默认档（兜底）。
type PriceTier struct {
	Mode       string  `json:"mode,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
	Audio      string  `json:"audio,omitempty"`
	MinPixels  int64   `json:"min_pixels,omitempty"`
	MaxPixels  int64   `json:"max_pixels,omitempty"`
	Price      float64 `json:"price"`
	// CUSTOM: 不适用该档位的分组（fork 扩展），为空表示所有分组都适用。
	// 同一模型挂在不同分组的线路能力不同（例如某条线路只出 768P），这里排除的
	// 分组请求该档位时直接拒绝，而不是落到默认档或交给上游失败。
	ExcludedGroups []string `json:"excluded_groups,omitempty"`
	// CUSTOM: 闲时价（fork 扩展）。请求在渠道配置的闲时时段内（见渠道设置
	// off_peak_pricing）时按它计费；不填表示该档没有闲时价，始终按 Price 计费。
	OffPeakPrice *float64 `json:"off_peak_price,omitempty"`
	// CUSTOM: 分组价（fork 扩展），分组名 -> 单价。同一模型挂在不同分组的线路成本不同，
	// 各档之间的价差也不同，只靠分组倍率没法让每一档都卖到目标价。配了的分组按它代替
	// Price 计费（仍乘分组倍率），并且不使用闲时价。
	GroupPrices map[string]float64 `json:"group_prices,omitempty"`
}

// forGroup 返回该分组实际适用的档位：配了有效分组价时用它代替原价，不再按闲时价计。
// 分组价无效（非正数、NaN/Inf，改库或其他接口写入）时忽略，按原价收。
func (tier PriceTier) forGroup(group string) PriceTier {
	price, ok := tier.GroupPrices[group]
	if !ok {
		return tier
	}
	if price <= 0 || math.IsInf(price, 0) || math.IsNaN(price) {
		common.SysError(fmt.Sprintf("invalid group_prices[%s] %v ignored", group, price))
		return tier
	}
	tier.Price = price
	tier.OffPeakPrice = nil
	return tier
}

// PriceAt 返回档位单价以及是否按闲时价计。只有渠道处于闲时时段且该档配了有效的
// 闲时价才用闲时价；闲时价无效（非正数、NaN/Inf，改库或其他接口写入）按原价收。
func (tier PriceTier) PriceAt(offPeak bool) (float64, bool) {
	if !offPeak || tier.OffPeakPrice == nil {
		return tier.Price, false
	}
	if price := *tier.OffPeakPrice; price > 0 && !math.IsInf(price, 0) && !math.IsNaN(price) {
		return price, true
	}
	common.SysError(fmt.Sprintf("invalid off_peak_price %v ignored", *tier.OffPeakPrice))
	return tier.Price, false
}

// excludes 该分组是否不适用此档位。
func (tier PriceTier) excludes(group string) bool {
	for _, excluded := range tier.ExcludedGroups {
		if excluded == group {
			return true
		}
	}
	return false
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
//
// InputVideoPrice 输入（参考/待编辑）视频每秒单价，仅 per_second：不填时与输出一起
// 按命中档位的单价计费（MiniMax、百炼等上游的规则），填 0 为免费，填正数单独计价。
//
// FreeInputImages 每次请求前 N 张输入图免费，只对 InputImagePrice 生效；不填为 0，
// 每张都计费。免费额度由各上游的价格规则决定（例如 MiniMax-H3 前 5 张免费），
// 由管理员按对接的上游填写，网关结算与模型广场展示都读这一个值。
type ModelPriceTable struct {
	Unit              string             `json:"unit"`
	Tiers             []PriceTier        `json:"tiers,omitempty"`
	ResolutionBuckets []ResolutionBucket `json:"resolution_buckets,omitempty"`
	InputImagePrice   float64            `json:"input_image_price,omitempty"` // USD/张，per_image 与 per_second
	InputTokenPrice   float64            `json:"input_token_price,omitempty"` // USD/百万 token，仅 per_image
	FreeInputImages   int                `json:"free_input_images,omitempty"` // 每次请求免费的输入图张数
	InputVideoPrice   *float64           `json:"input_video_price,omitempty"` // USD/秒，仅 per_second；nil 为同输出档位单价
	// CUSTOM: 最低计费时长（秒），仅 per_second（fork 扩展）。上游有单次最低收费时
	// （例如 autodl IndexTTS2 每次至少 ¥0.01），不足这个时长的请求按它计费，否则短
	// 请求的收入会低于上游成本。预扣与完成结算都按它兜底。
	MinSeconds float64 `json:"min_seconds,omitempty"`
	// CUSTOM: 分组输入价（fork 扩展），分组名 -> 该分组的输入图、输入视频单价。同一模型挂在
	// 不同分组的线路，上游对参考图、参考视频的收费规则不同（例如 WeNex 不按张收费），这里填
	// 了的分组按它代替价格表的 InputImagePrice / InputVideoPrice（仍乘分组倍率）。
	GroupInputPrices map[string]GroupInputPrice `json:"group_input_prices,omitempty"`
}

// GroupInputPrice 一个分组的输入价，不填的项沿用价格表的。
type GroupInputPrice struct {
	// InputImagePrice 每张输入图单价（USD），0 为不收费。免费张数沿用价格表的 FreeInputImages。
	InputImagePrice *float64 `json:"input_image_price,omitempty"`
	// InputVideoPrice 每秒输入视频单价（USD），0 为免费。仅 per_second。
	InputVideoPrice *float64 `json:"input_video_price,omitempty"`
}

// ForGroup 返回该分组实际适用的价格表：配了分组输入价时用它代替价格表的输入图、输入视频
// 单价。单价无效（负数、NaN/Inf，改库或其他接口写入）时忽略，沿用价格表的。
func (table ModelPriceTable) ForGroup(group string) ModelPriceTable {
	prices, ok := table.GroupInputPrices[group]
	if !ok {
		return table
	}
	valid := func(name string, price *float64) bool {
		if price == nil {
			return false
		}
		if *price < 0 || math.IsInf(*price, 0) || math.IsNaN(*price) {
			common.SysError(fmt.Sprintf("invalid group_input_prices[%s].%s %v ignored", group, name, *price))
			return false
		}
		return true
	}
	if valid("input_image_price", prices.InputImagePrice) {
		table.InputImagePrice = *prices.InputImagePrice
	}
	if valid("input_video_price", prices.InputVideoPrice) {
		price := *prices.InputVideoPrice
		table.InputVideoPrice = &price
	}
	return table
}

// BillableSeconds 按最低计费时长兜底后的计费秒数。最低时长无效（负数、NaN/Inf、超过
// 单次时长上限，改库或其他接口写入）时忽略，不会把计费乘数放大到不可控。
func (table ModelPriceTable) BillableSeconds(seconds float64) float64 {
	minSeconds := table.MinSeconds
	if minSeconds == 0 {
		return seconds
	}
	if minSeconds < 0 || math.IsNaN(minSeconds) || math.IsInf(minSeconds, 0) || minSeconds > MaxMinSeconds {
		common.SysError(fmt.Sprintf("invalid min_seconds %v ignored", minSeconds))
		return seconds
	}
	return max(seconds, minSeconds)
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

// AdditiveInputPrices 返回两个附加单价的安全取值。
// 负单价会让附加项变成减项，把一次计费算成部分退款；管理端已拦截负值，
// 但配置也可能被直接改库或经其他接口写入，计费侧不依赖上游校验。
func (table ModelPriceTable) AdditiveInputPrices() (imagePrice, tokenPrice float64) {
	imagePrice, tokenPrice = table.InputImagePrice, table.InputTokenPrice
	if imagePrice < 0 {
		common.SysError(fmt.Sprintf("negative input_image_price %v ignored", imagePrice))
		imagePrice = 0
	}
	if tokenPrice < 0 {
		common.SysError(fmt.Sprintf("negative input_token_price %v ignored", tokenPrice))
		tokenPrice = 0
	}
	return imagePrice, tokenPrice
}

// BillableInputImages 扣掉免费张数后需要按输入图单价计费的张数。
// 免费张数为负（改库或其他接口写入）按 0 处理，不会把计费张数放大。
func (table ModelPriceTable) BillableInputImages(total int) int {
	return max(total-max(table.FreeInputImages, 0), 0)
}

// InputVideoSecondPrice 输入视频每秒的单价：没配置时等于输出档位单价 tierPrice；
// 配置为负（改库或其他接口写入）同样按未配置处理，不会变成减项。
func (table ModelPriceTable) InputVideoSecondPrice(tierPrice float64) float64 {
	if table.InputVideoPrice == nil {
		return tierPrice
	}
	if price := *table.InputVideoPrice; price >= 0 && !math.IsInf(price, 0) && !math.IsNaN(price) {
		return price
	}
	common.SysError(fmt.Sprintf("invalid input_video_price %v ignored", *table.InputVideoPrice))
	return tierPrice
}

// GetPriceTable 返回指定模型的价格表；第二个返回值表示是否配置且单位合法。
func GetPriceTable(modelName string) (ModelPriceTable, bool) {
	table, ok := videoBillingSettings.PriceTables[modelName]
	if !ok {
		return ModelPriceTable{}, false
	}
	if table.Unit != UnitPerSecond && table.Unit != UnitPerMillionToken && table.Unit != UnitPerImage && table.Unit != UnitPer10kChars {
		return ModelPriceTable{}, false
	}
	if len(table.Tiers) == 0 {
		return ModelPriceTable{}, false
	}
	return table, true
}

// ErrNoMatchingTier 模型未配置价格表，或没有档位匹配且没有默认档。
var ErrNoMatchingTier = errors.New("no matching price tier")

// ErrTierExcludedForGroup 请求的档位对当前分组不适用。
var ErrTierExcludedForGroup = errors.New("price tier is not available for this group")

// GetPrice 返回模型在给定分组与 (模式, 分辨率, 音轨) 下命中的原价与计价单位。
// 分辨率匹配优先级：精确尺寸串（如 "1440x1440"）> 归档标签（1k/2k/...）> 任意。
//
// 分组排除：最具体的匹配档位排除了该分组时返回 ErrTierExcludedForGroup，不退到
// 更宽泛的档位（尤其是默认档）——否则「这条线路不出 1080P」会变成按兜底价收费、
// 再交给上游失败。只有更宽泛的档位本来就是最佳匹配时（被排除的档位不匹配该请求）
// 才按它取价。
func GetPrice(modelName, group, mode, resolution, audio string) (float64, string, error) {
	tier, unit, err := GetTier(modelName, group, mode, resolution, audio)
	if err != nil {
		return 0, "", err
	}
	return tier.Price, unit, nil
}

// GetTier 与 GetPrice 的匹配规则相同，返回命中的整个档位，供需要闲时价的计费路径
// 用 PriceAt 取价。
func GetTier(modelName, group, mode, resolution, audio string) (PriceTier, string, error) {
	table, ok := GetPriceTable(modelName)
	if !ok {
		return PriceTier{}, "", ErrNoMatchingTier
	}

	rawResolution := strings.ToLower(strings.TrimSpace(resolution))
	normResolution := normalizeForTable(rawResolution, table)
	bestScore, excludedScore := -1, -1
	var best PriceTier
	for _, tier := range table.Tiers {
		tier = tier.forGroup(group)
		score := tierMatchScore(tier, mode, rawResolution, normResolution, audio, table)
		if score < 0 || tier.Price <= 0 {
			continue
		}
		if tier.excludes(group) {
			excludedScore = max(excludedScore, score)
			continue
		}
		if score > bestScore {
			bestScore = score
			best = tier
		}
	}
	if excludedScore > bestScore {
		return PriceTier{}, "", ErrTierExcludedForGroup
	}
	if bestScore < 0 || best.Price <= 0 {
		return PriceTier{}, "", ErrNoMatchingTier
	}
	return best, table.Unit, nil
}

// HasGroupPrices 价格表是否有档位配了分组价。没有时各分组的价差只有分组倍率。
func (table ModelPriceTable) HasGroupPrices() bool {
	for _, tier := range table.Tiers {
		if len(tier.GroupPrices) > 0 {
			return true
		}
	}
	return false
}

// HasExcludedGroups 价格表是否有档位排除了分组。没有时调用方可以跳过按请求解析维度。
func (table ModelPriceTable) HasExcludedGroups() bool {
	for _, tier := range table.Tiers {
		if len(tier.ExcludedGroups) > 0 {
			return true
		}
	}
	return false
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

// PublicTables 返回供定价接口对外展示的价格表。档位的不适用分组、分组价和分组输入价只
// 保留查看者可见的分组：不对外的分组名不能出现在公开接口里，而且它们本来就与查看者无关。
func PublicTables(visibleGroups map[string]string) map[string]ModelPriceTable {
	if len(videoBillingSettings.PriceTables) == 0 {
		return nil
	}
	copied := make(map[string]ModelPriceTable, len(videoBillingSettings.PriceTables))
	for name, table := range videoBillingSettings.PriceTables {
		if len(table.GroupInputPrices) > 0 {
			var groupInputPrices map[string]GroupInputPrice
			for group, prices := range table.GroupInputPrices {
				if _, ok := visibleGroups[group]; ok {
					if groupInputPrices == nil {
						groupInputPrices = make(map[string]GroupInputPrice)
					}
					groupInputPrices[group] = prices
				}
			}
			table.GroupInputPrices = groupInputPrices
		}
		if !table.HasExcludedGroups() && !table.HasGroupPrices() {
			copied[name] = table
			continue
		}
		tiers := make([]PriceTier, len(table.Tiers))
		for i, tier := range table.Tiers {
			var visible []string
			for _, group := range tier.ExcludedGroups {
				if _, ok := visibleGroups[group]; ok {
					visible = append(visible, group)
				}
			}
			tier.ExcludedGroups = visible
			var groupPrices map[string]float64
			for group, price := range tier.GroupPrices {
				if _, ok := visibleGroups[group]; ok {
					if groupPrices == nil {
						groupPrices = make(map[string]float64)
					}
					groupPrices[group] = price
				}
			}
			tier.GroupPrices = groupPrices
			tiers[i] = tier
		}
		table.Tiers = tiers
		copied[name] = table
	}
	return copied
}
