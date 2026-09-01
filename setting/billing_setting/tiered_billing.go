package billing_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	BillingModeField      = "billing_mode"
	BillingExprField      = "billing_expr"
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr
type BillingSetting struct {
	BillingMode map[string]string `json:"billing_mode"`
	BillingExpr map[string]string `json:"billing_expr"`
	// CUSTOM: 模型名 -> 分组名 -> 表达式，覆盖该分组下的 BillingExpr。
	// 计费模式仍然是模型级的：一个分组要"不按分时计价"，给它配一条不含
	// hour()/weekday() 等时间条件的固定价表达式即可，不必切换模式——切换
	// 模式会让 auto 重试跨分组时在两套完全不同的计费结构之间跳，得不偿失。
	GroupBillingExpr map[string]map[string]string `json:"group_billing_expr"`
}

var billingSetting = BillingSetting{
	BillingMode:      make(map[string]string),
	BillingExpr:      make(map[string]string),
	GroupBillingExpr: make(map[string]map[string]string),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

// GetBillingExprForGroup 返回该分组实际生效的表达式：先找分组覆盖，再回落到模型级。
// group 为空时等价于 GetBillingExpr，这样非中转调用方（同步、校验）不必构造分组。
func GetBillingExprForGroup(model string, group string) (string, bool) {
	if group != "" {
		if byGroup, ok := billingSetting.GroupBillingExpr[model]; ok {
			if expr, ok := byGroup[group]; ok && strings.TrimSpace(expr) != "" {
				return expr, true
			}
		}
	}
	return GetBillingExpr(model)
}

// GetGroupBillingExprCopy 返回分组覆盖的深拷贝，供只读展示与保存前的校验使用。
func GetGroupBillingExprCopy() map[string]map[string]string {
	out := make(map[string]map[string]string, len(billingSetting.GroupBillingExpr))
	for model, byGroup := range billingSetting.GroupBillingExpr {
		out[model] = lo.Assign(byGroup)
	}
	return out
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}

// SwapExprConfigForTest 替换表达式配置并返回旧值，供跨包测试构造计费状态。
// 生产代码不要调用：这里直接写共享 map，没有并发保护。
func SwapExprConfigForTest(expr map[string]string, groupExpr map[string]map[string]string) (map[string]string, map[string]map[string]string) {
	prevExpr, prevGroup := billingSetting.BillingExpr, billingSetting.GroupBillingExpr
	billingSetting.BillingExpr, billingSetting.GroupBillingExpr = expr, groupExpr
	return prevExpr, prevGroup
}

// SwapBillingModeForTest 替换计费模式配置并返回旧值，仅供测试使用。
func SwapBillingModeForTest(mode map[string]string) map[string]string {
	prev := billingSetting.BillingMode
	billingSetting.BillingMode = mode
	return prev
}
