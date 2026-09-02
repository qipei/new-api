// CUSTOM: 比价路由（fork 扩展）。
//
// auto 按管理员编排的固定顺序找分组；auto_price 不需要编排，每次请求按当前实际
// 价格给候选分组排序，从最便宜的开始试。选中之后的重试、跨组顺延复用 auto 那套
// 逻辑，区别只在候选列表怎么来。
//
// 比价的关键前提：全系统按分组区分价格的地方只有三处——分组倍率、用户分组特例
// 倍率、分组表达式覆盖，加上乘在最终价上的限时活动。模型级的那些（model_ratio、
// completion_ratio、model_price、视频/图片价格矩阵）对同一个模型的所有分组都是
// 同一份。所以只要各分组的表达式一致，比价就严格等价于比较「有效分组倍率」，
// 而且这个结论对每 1M token、每秒、每张、每次**所有计价单位**都成立——模型级的
// 那部分是公因子，在分组之间的比较里直接约掉了。
package service

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// AutoPriceGroup 是比价路由的令牌分组名。和 "auto" 一样它不是真实分组，不能被
// 当作普通分组选中，也不该出现在分组倍率表里。
const AutoPriceGroup = "auto_price"

// ExprProbe 在需要对表达式求值时才被调用，返回和预扣费同一口径的 token 估算与
// 请求状态。做成惰性的是因为绝大多数模型走不到求值那一步：各分组表达式一致时
// 排序只看倍率，不该为此付出 token 计数的开销。
//
// 用真实请求数据而不是固定探针，是为了让排序永远跟着表达式走：无论表达式将来
// 用 len、param()、header() 还是以后新增的函数，都不需要回头改这里的代码。
type ExprProbe func() (billingexpr.TokenParams, billingexpr.RequestInput)

// EffectiveGroupRatio 是分组倍率乘上限时活动倍率。userGroup 为空时不查特例倍率。
func EffectiveGroupRatio(modelName string, userGroup string, group string, at time.Time) float64 {
	ratio := ratio_setting.GetGroupRatio(group)
	if userGroup != "" {
		if special, ok := ratio_setting.GetGroupGroupRatio(userGroup, group); ok {
			ratio = special
		}
	}
	if promotion, ok := billing_setting.ActivePromotionAt(modelName, group, at); ok {
		ratio *= promotion.Ratio
	}
	return ratio
}

// resolveGroupExprs 取出每个分组实际生效的表达式，并报告它们是否完全一致。
// 一致时排序不需要求值，这对所有计价单位都精确成立。
func resolveGroupExprs(modelName string, groups []string) (map[string]string, bool) {
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return nil, true
	}
	exprs := make(map[string]string, len(groups))
	uniform := true
	first := ""
	for i, group := range groups {
		expr, _ := billing_setting.GetBillingExprForGroup(modelName, group)
		exprs[group] = expr
		if i == 0 {
			first = expr
			continue
		}
		if expr != first {
			uniform = false
		}
	}
	return exprs, uniform
}

// exprUnitCost 对表达式求值一次，得到这次请求在该表达式下的原始成本。分组之间
// 只比相对高低，所以不需要换算成任何货币单位。
func exprUnitCost(expr string, params billingexpr.TokenParams, input billingexpr.RequestInput) (float64, bool) {
	if expr == "" {
		return 0, false
	}
	cost, _, err := billingexpr.RunExprWithRequest(expr, params, input)
	if err != nil {
		// 跑不通就放弃对这条表达式比价：当成 0 会让一条坏配置永远排第一。
		common.SysError("group price rank: expr eval failed: " + err.Error())
		return 0, false
	}
	return cost, true
}

// RankGroupsByPrice 把候选分组按当前价格从低到高排序。同价时按分组名排序，保证
// 结果稳定——否则 map 遍历顺序会让同价分组每次请求换一个，日志无从对账。
//
// probe 可以为 nil：那样各分组表达式不一致时会退回只比倍率，不会因为拿不到请求
// 数据就整个失效。
func RankGroupsByPrice(modelName string, userGroup string, groups []string, at time.Time, probe ExprProbe) []string {
	if len(groups) <= 1 {
		return groups
	}

	exprs, uniform := resolveGroupExprs(modelName, groups)
	scores := make(map[string]float64, len(groups))

	if uniform || probe == nil {
		for _, group := range groups {
			scores[group] = EffectiveGroupRatio(modelName, userGroup, group, at)
		}
	} else {
		params, input := probe()
		for _, group := range groups {
			ratio := EffectiveGroupRatio(modelName, userGroup, group, at)
			cost, ok := exprUnitCost(exprs[group], params, input)
			if !ok {
				// 这条表达式算不出来，退回只比倍率，别让它凭空排到最前面。
				scores[group] = ratio
				continue
			}
			scores[group] = cost * ratio
		}
	}

	ranked := append([]string(nil), groups...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if scores[ranked[i]] != scores[ranked[j]] {
			return scores[ranked[i]] < scores[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})
	return ranked
}

// GetRequestPriceRankedGroups 是比价路由的候选列表：用户能用的分组，按当前价格从
// 低到高。和 auto 不同，这里不看管理员编排的 auto 列表——不需要编排正是它的卖点。
func GetRequestPriceRankedGroups(userGroup string, modelName string, at time.Time, probe ExprProbe) []string {
	usable := GetUserUsableGroups(userGroup)
	candidates := make([]string, 0, len(usable))
	for group := range usable {
		if group == "" || IsAutoRoutingGroup(group) {
			continue
		}
		if !ratio_setting.ContainsGroupRatio(group) {
			continue
		}
		candidates = append(candidates, group)
	}
	return RankGroupsByPrice(modelName, userGroup, candidates, at, probe)
}

// IsAutoRoutingGroup 报告这个分组名是不是一种自动路由，而不是真实分组。
func IsAutoRoutingGroup(group string) bool {
	return group == "auto" || group == AutoPriceGroup
}

// requestPriceRankedGroups 取本次请求的比价候选列表，并缓存在 context 上。
//
// 缓存不是为了省时间，是为了正确：跨组顺延靠 ContextKeyAutoGroupIndex 记住"走到
// 第几个分组了"，如果重试时重新排一次序，那个下标就会指向一个已经变了的列表——
// 时段刚好跨过档位边界、或者管理员同时改了价，都会让重试跳过或重复某个分组。
func requestPriceRankedGroups(param *RetryParam, userGroup string) []string {
	return RequestAutoRoutingGroups(param.Ctx, AutoPriceGroup, userGroup, param.ModelName)
}

// RequestAutoRoutingGroups 返回某种自动路由本次请求的候选分组顺序。渠道亲和要在
// 候选分组里找回上次用过的渠道，两种路由的候选来法不同，这里统一。
func RequestAutoRoutingGroups(c *gin.Context, tokenGroup string, userGroup string, modelName string) []string {
	if tokenGroup != AutoPriceGroup {
		return GetRequestAutoGroups(c, userGroup)
	}
	if cached, ok := common.GetContextKey(c, constant.ContextKeyPriceRankedGroups); ok {
		if groups, ok := cached.([]string); ok {
			return groups
		}
	}
	groups := GetRequestPriceRankedGroups(userGroup, modelName, time.Now(), nil)
	common.SetContextKey(c, constant.ContextKeyPriceRankedGroups, groups)
	return groups
}
