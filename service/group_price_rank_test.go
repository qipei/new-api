package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func atShanghai(t *testing.T, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	require.NoError(t, err)
	return parsed
}

// 按倍率计费的模型：所有分组共用同一个 modelRatio，排序完全由分组倍率决定，
// 这一支是精确的，不受档位探针影响。
func TestRankByPriceForRatioModel(t *testing.T) {
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{})
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"2.5折":0.25,"7折":0.7,"default":1,"8.8折":0.88}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"m":2}`))

	ranked := RankGroupsByPrice("m", "", []string{"default", "8.8折", "2.5折", "7折"}, time.Now(), nil)
	assert.Equal(t, []string{"2.5折", "7折", "8.8折", "default"}, ranked)
}

// 分组各配各的表达式时，排序必须看表达式算出来的实际单价，而不是分组倍率——
// 倍率低但表达式贵的分组，总价可能反而更高。
func TestRankByPriceUsesTheGroupExpression(t *testing.T) {
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{
		"m": billing_setting.BillingModeTieredExpr,
	})
	prevExpr, prevGroup := billing_setting.SwapExprConfigForTest(
		map[string]string{"m": `tier("base", p * 10 + c * 30)`},
		map[string]map[string]string{
			// 便宜分组倍率虽低，但表达式单价高出一大截
			"m": {"cheap-ratio": `tier("pricey", p * 100 + c * 300)`},
		},
	)
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapExprConfigForTest(prevExpr, prevGroup)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"cheap-ratio":0.25,"default":1}`))

	// 分数只表达相对高低，量纲是刻意不做归一的，所以断言比值而不是绝对值。
	// cheap-ratio: (100+300) × 0.25 = 100 份；default: (10+30) × 1 = 40 份
	cheap := groupScoreForTest(t, "m", "cheap-ratio")
	base := groupScoreForTest(t, "m", "default")
	assert.InDelta(t, 100.0/40.0, cheap/base, 1e-9, "倍率低但表达式贵的分组总价应当是 2.5 倍")
	assert.Greater(t, cheap, base)
	// 表达式不同 → 必须求值。cheap-ratio 倍率低但表达式贵，总价反而更高。
	assert.Equal(t, []string{"default", "cheap-ratio"},
		RankGroupsByPrice("m", "", []string{"cheap-ratio", "default"}, time.Now(), testProbe))

	// probe 为 nil 时拿不到请求数据，退回只比倍率——不因为缺数据就整个失效。
	assert.Equal(t, []string{"cheap-ratio", "default"},
		RankGroupsByPrice("m", "", []string{"cheap-ratio", "default"}, time.Now(), nil))
}

// 分时表达式：同一组分组在闲时和忙时的排序可以不同，这正是"当前时段最低价"的意义。
func TestRankByPriceFollowsTheCurrentTimeTier(t *testing.T) {
	const night = `(hour("Asia/Shanghai") >= 22 || hour("Asia/Shanghai") < 8) ? tier("闲时", p * 1 + c * 1) : tier("忙时", p * 100 + c * 100)`
	const flat = `tier("固定", p * 20 + c * 20)`
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{
		"m": billing_setting.BillingModeTieredExpr,
	})
	prevExpr, prevGroup := billing_setting.SwapExprConfigForTest(
		map[string]string{"m": flat},
		map[string]map[string]string{"m": {"night": night}},
	)
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapExprConfigForTest(prevExpr, prevGroup)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"night":1,"default":1}`))

	// 表达式里的 hour() 读的是真实时钟，所以这里只能断言"两者顺序取决于当前时段"。
	ranked := RankGroupsByPrice("m", "", []string{"default", "night"}, time.Now(), testProbe)
	nightScore := groupScoreForTest(t, "m", "night")
	flatScore := groupScoreForTest(t, "m", "default")
	if nightScore < flatScore {
		assert.Equal(t, "night", ranked[0], "闲时应当把 night 排前面")
	} else {
		assert.Equal(t, "default", ranked[0], "忙时应当把 default 排前面")
	}
}

// 限时活动会改变排序：本来更贵的分组打完活动可能变成最便宜的。
func TestRankByPriceAccountsForPromotions(t *testing.T) {
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{})
	t.Cleanup(func() { billing_setting.SwapBillingModeForTest(prevMode) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"7折":0.7,"8.8折":0.88}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"m":2}`))
	at := atShanghai(t, "2026-09-02 12:00")

	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() { billing_setting.SwapPromotionsForTest(prevPromo) })
	assert.Equal(t, []string{"7折", "8.8折"}, RankGroupsByPrice("m", "", []string{"8.8折", "7折"}, at, nil))

	// 8.8折 打五折 = 0.44，比 7折 便宜，排序必须跟着翻转
	billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{
		"m": {{Name: "开学季", Start: "2026-09-01", End: "2026-09-30", TZ: "Asia/Shanghai", Ratio: 0.5, Groups: []string{"8.8折"}}},
	})
	assert.Equal(t, []string{"8.8折", "7折"}, RankGroupsByPrice("m", "", []string{"8.8折", "7折"}, at, nil))
}

// 同价时按名字排序，保证同一次配置下结果稳定，否则日志无从对账。
func TestRankByPriceIsStableForEqualPrices(t *testing.T) {
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{})
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"b":1,"a":1,"c":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"m":2}`))

	for range 5 {
		assert.Equal(t, []string{"a", "b", "c"},
			RankGroupsByPrice("m", "", []string{"c", "a", "b"}, time.Now(), nil))
	}
}

// groupScoreForTest 用一个固定的请求向量给单个分组打分，供断言排序依据用。
func groupScoreForTest(t *testing.T, modelName, group string) float64 {
	t.Helper()
	ranked := RankGroupsByPrice(modelName, "", []string{group}, time.Now(), testProbe)
	_ = ranked
	expr, _ := billing_setting.GetBillingExprForGroup(modelName, group)
	ratio := EffectiveGroupRatio(modelName, "", group, time.Now())
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr || expr == "" {
		return ratio
	}
	params, input := testProbe()
	cost, ok := exprUnitCost(expr, params, input)
	if !ok {
		return ratio
	}
	return cost * ratio
}

func testProbe() (billingexpr.TokenParams, billingexpr.RequestInput) {
	return billingexpr.TokenParams{P: 1_000_000, C: 1_000_000, Len: 1_000_000}, billingexpr.RequestInput{}
}
