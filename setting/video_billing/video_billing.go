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

// 音轨维度取值；空串表示该档价格与音轨无关。
const (
	AudioOn  = "on"
	AudioOff = "off"
)

// 计价单位。
const (
	UnitPerSecond       = "per_second"         // 原价按 USD/秒
	UnitPerMillionToken = "per_million_tokens" // 原价按 USD/百万 token
)

// PriceTier 一档价格。Mode/Resolution/Audio 为空串表示"任意"，
// 查价时取匹配维度最多（最具体）的一档；全空维度的档位是默认档（兜底）。
type PriceTier struct {
	Mode       string  `json:"mode,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
	Audio      string  `json:"audio,omitempty"`
	Price      float64 `json:"price"`
}

// ModelPriceTable 单个模型的视频价格表。
type ModelPriceTable struct {
	Unit  string      `json:"unit"`
	Tiers []PriceTier `json:"tiers,omitempty"`
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
	if table.Unit != UnitPerSecond && table.Unit != UnitPerMillionToken {
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
func GetPrice(modelName, mode, resolution, audio string) (float64, string, bool) {
	table, ok := GetPriceTable(modelName)
	if !ok {
		return 0, "", false
	}

	resolution = NormalizeResolution(resolution)
	bestScore := -1
	bestPrice := 0.0
	for _, tier := range table.Tiers {
		score := tierMatchScore(tier, mode, resolution, audio)
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

// tierMatchScore 返回档位与请求维度的匹配度：-1 不匹配，否则为精确匹配的维度数。
func tierMatchScore(tier PriceTier, mode, resolution, audio string) int {
	score := 0
	if tier.Mode != "" {
		if tier.Mode != mode {
			return -1
		}
		score++
	}
	if tier.Resolution != "" {
		if NormalizeResolution(tier.Resolution) != resolution {
			return -1
		}
		score++
	}
	if tier.Audio != "" {
		if tier.Audio != audio {
			return -1
		}
		score++
	}
	return score
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
	parts := strings.Split(res, "x")
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
