// CUSTOM: 视频模型分档定价（fork 扩展）。
// 按模型维护 模式(mode) × 分辨率(resolution) × 音轨(audio) 的原价表，
// 计费时换算为相对基准价的倍率（OtherRatio），基准价与"模型定价"处
// 配置的按量倍率（元/百万 token）或按次价格（每秒单价）对应同一档位。
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

// PriceTier 一档价格。Mode/Resolution/Audio 为空串表示"任意"，
// 查价时取匹配维度最多（最具体）的一档；无任何档匹配时按基准价计费。
type PriceTier struct {
	Mode       string  `json:"mode,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
	Audio      string  `json:"audio,omitempty"`
	Price      float64 `json:"price"`
}

// ModelPriceTable 单个模型的视频价格表。
// BasePrice 是基准档原价，必须与"模型定价"处配置的基准价一致，
// 计费倍率 = 档位原价 / BasePrice。
type ModelPriceTable struct {
	BasePrice float64     `json:"base_price"`
	Tiers     []PriceTier `json:"tiers,omitempty"`
}

// VideoBillingSettings 由 config.GlobalConfig 管理。
// DB key: video_billing.price_tables
type VideoBillingSettings struct {
	PriceTables map[string]ModelPriceTable `json:"price_tables"`
}

// 默认表：字节 Seedance 2.0 官方刊例价（元/百万 token），
// 迁移自 relay/channel/task/doubao 原硬编码 videoPriceTable，行为保持一致。
var videoBillingSettings = VideoBillingSettings{
	PriceTables: map[string]ModelPriceTable{
		"doubao-seedance-2-0-260128": {
			BasePrice: 46.0,
			Tiers: []PriceTier{
				{Resolution: "1080p", Price: 51.0},
				{Resolution: "4k", Price: 26.0},
				{Mode: ModeVideoToVideo, Price: 28.0},
				{Mode: ModeVideoToVideo, Resolution: "1080p", Price: 31.0},
				{Mode: ModeVideoToVideo, Resolution: "4k", Price: 16.0},
			},
		},
		"doubao-seedance-2-0-fast-260128": {
			BasePrice: 37.0,
			Tiers: []PriceTier{
				{Mode: ModeVideoToVideo, Price: 22.0},
			},
		},
	},
}

func init() {
	config.GlobalConfig.Register("video_billing", &videoBillingSettings)
}

// GetPriceTable 返回指定模型的价格表；第二个返回值表示是否配置且基准价有效。
func GetPriceTable(modelName string) (ModelPriceTable, bool) {
	table, ok := videoBillingSettings.PriceTables[modelName]
	if !ok || table.BasePrice <= 0 {
		return ModelPriceTable{}, false
	}
	return table, true
}

// GetRatio 返回模型在给定 (模式, 分辨率, 音轨) 下相对基准价的计费倍率。
// 第二个返回值表示该模型是否配置了价格表；未配置时调用方应保持原有计费行为。
// 无匹配档位、或档位价格非法时按基准价（倍率 1.0）处理。
func GetRatio(modelName, mode, resolution, audio string) (float64, bool) {
	table, ok := GetPriceTable(modelName)
	if !ok {
		return 0, false
	}

	resolution = NormalizeResolution(resolution)
	bestScore := -1
	bestPrice := table.BasePrice
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
	return bestPrice / table.BasePrice, true
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
