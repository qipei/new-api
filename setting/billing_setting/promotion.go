// CUSTOM: 限时活动（fork 扩展）。
//
// 活动是一个乘在最终价格上的倍率，不是一套替代的计价结构。这样常规按倍率计费
// 的模型和动态计费的模型走的是同一条路：活动期内多乘一个系数，活动一过就不再
// 命中，模型完全回到原状态，不需要任何还原动作，也不用改 billing_mode。
//
// 起止日期含当天，按活动自己的时区解释。
package billing_setting

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	ModelPromotionsField = "model_promotions"

	promotionDateLayout  = "2006-01-02"
	promotionDefaultTZ   = "Asia/Shanghai"
	promotionMaxPerModel = 20
)

// ModelPromotion 是一条限时活动。Groups 为空表示该模型的所有分组都参与。
type ModelPromotion struct {
	Name   string   `json:"name"`
	Start  string   `json:"start"`
	End    string   `json:"end"`
	TZ     string   `json:"tz,omitempty"`
	Ratio  float64  `json:"ratio"`
	Groups []string `json:"groups,omitempty"`
}

func (p ModelPromotion) timezone() string {
	if tz := strings.TrimSpace(p.TZ); tz != "" {
		return tz
	}
	return promotionDefaultTZ
}

// coversGroup 空列表表示不限分组。
func (p ModelPromotion) coversGroup(group string) bool {
	if len(p.Groups) == 0 {
		return true
	}
	for _, g := range p.Groups {
		if g == group {
			return true
		}
	}
	return false
}

// IsLiveAt 只判断日期窗口，不看分组。下发和展示要按"这条活动是否进行中"过滤，
// 用带分组的 ActivePromotionAt 传空分组会把限定了分组的活动全判成未命中。
func (p ModelPromotion) IsLiveAt(at time.Time) bool {
	return p.activeAt(at)
}

// activeAt 起止日期都含当天，按活动自己的时区把时刻折算成日期再比较。
// 时区加载失败时退回 UTC，与计费表达式里的 timeInZone 保持一致。
func (p ModelPromotion) activeAt(at time.Time) bool {
	loc, err := time.LoadLocation(p.timezone())
	if err != nil {
		loc = time.UTC
	}
	today := at.In(loc).Format(promotionDateLayout)
	return today >= p.Start && today <= p.End
}

// ---------------------------------------------------------------------------
// Read accessors (hot path)
// ---------------------------------------------------------------------------

// ActivePromotionAt 返回该模型该分组在指定时刻生效的活动。多条同时命中时取倍率
// 最低的那条——对用户最有利，且结果与配置顺序无关。
//
// 调用方必须传 at 而不是让这里取 time.Now()：预扣费和结算是两个时刻，跨过活动
// 边界的请求必须按预扣费当时的事实结算，否则用户看到的报价和实际扣费会不一致。
func ActivePromotionAt(model string, group string, at time.Time) (ModelPromotion, bool) {
	promotions, ok := billingSetting.ModelPromotions[model]
	if !ok || len(promotions) == 0 {
		return ModelPromotion{}, false
	}
	best := ModelPromotion{}
	found := false
	for _, promotion := range promotions {
		if !promotion.coversGroup(group) || !promotion.activeAt(at) {
			continue
		}
		if !found || promotion.Ratio < best.Ratio {
			best = promotion
			found = true
		}
	}
	return best, found
}

// GetModelPromotionsCopy 返回深拷贝，供只读展示与保存前的校验使用。
func GetModelPromotionsCopy() map[string][]ModelPromotion {
	out := make(map[string][]ModelPromotion, len(billingSetting.ModelPromotions))
	for model, promotions := range billingSetting.ModelPromotions {
		out[model] = append([]ModelPromotion(nil), promotions...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// ValidateModelPromotionsJSON 在写库前校验。倍率必须落在 (0, 1]：大于 1 就是借
// 活动之名涨价，配错了没人会发现，直接挡掉。
func ValidateModelPromotionsJSON(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var parsed map[string][]ModelPromotion
	if err := common.UnmarshalJsonStr(trimmed, &parsed); err != nil {
		return fmt.Errorf("限时活动配置不是合法的 JSON: %w", err)
	}
	for model, promotions := range parsed {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("限时活动配置里有空的模型名")
		}
		if len(promotions) > promotionMaxPerModel {
			return fmt.Errorf("模型 %s 的活动条数超过上限 %d", model, promotionMaxPerModel)
		}
		for i, promotion := range promotions {
			if err := validatePromotion(promotion); err != nil {
				return fmt.Errorf("模型 %s 的第 %d 条活动配置有误: %w", model, i+1, err)
			}
		}
	}
	return nil
}

func validatePromotion(promotion ModelPromotion) error {
	if strings.TrimSpace(promotion.Name) == "" {
		return fmt.Errorf("活动名称不能为空")
	}
	start, err := time.Parse(promotionDateLayout, promotion.Start)
	if err != nil {
		return fmt.Errorf("开始日期 %q 不是 YYYY-MM-DD 格式", promotion.Start)
	}
	end, err := time.Parse(promotionDateLayout, promotion.End)
	if err != nil {
		return fmt.Errorf("结束日期 %q 不是 YYYY-MM-DD 格式", promotion.End)
	}
	if end.Before(start) {
		return fmt.Errorf("结束日期 %s 早于开始日期 %s", promotion.End, promotion.Start)
	}
	if _, err := time.LoadLocation(promotion.timezone()); err != nil {
		return fmt.Errorf("时区 %q 无法识别", promotion.timezone())
	}
	if promotion.Ratio <= 0 || promotion.Ratio > 1 {
		return fmt.Errorf("活动倍率必须大于 0 且不超过 1，当前是 %v", promotion.Ratio)
	}
	for _, group := range promotion.Groups {
		if strings.TrimSpace(group) == "" {
			return fmt.Errorf("参与分组里有空值")
		}
	}
	return nil
}

// SwapPromotionsForTest 替换限时活动配置并返回旧值，仅供测试使用。
func SwapPromotionsForTest(promotions map[string][]ModelPromotion) map[string][]ModelPromotion {
	prev := billingSetting.ModelPromotions
	billingSetting.ModelPromotions = promotions
	return prev
}
