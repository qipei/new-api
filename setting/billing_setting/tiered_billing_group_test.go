package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBillingExprForGroupPrefersGroupOverride(t *testing.T) {
	const modelExpr = `tier("base", p * 3 + c * 9)`
	const groupExpr = `hour("Asia/Shanghai") >= 22 ? tier("night", p * 1.5) : tier("day", p * 3)`

	origExpr, origGroup := billingSetting.BillingExpr, billingSetting.GroupBillingExpr
	t.Cleanup(func() {
		billingSetting.BillingExpr, billingSetting.GroupBillingExpr = origExpr, origGroup
	})
	billingSetting.BillingExpr = map[string]string{"m": modelExpr}
	billingSetting.GroupBillingExpr = map[string]map[string]string{
		"m": {"vip": groupExpr, "blank": "   "},
	}

	cases := []struct {
		name     string
		group    string
		wantExpr string
		wantOK   bool
	}{
		{name: "group with an override uses it", group: "vip", wantExpr: groupExpr, wantOK: true},
		{name: "group without an override falls back to the model expression", group: "default", wantExpr: modelExpr, wantOK: true},
		{name: "a blank override is not an override", group: "blank", wantExpr: modelExpr, wantOK: true},
		{name: "an empty group falls back to the model expression", group: "", wantExpr: modelExpr, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, ok := GetBillingExprForGroup("m", tc.group)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantExpr, expr)
		})
	}

	t.Run("a model with no expression at all stays unconfigured", func(t *testing.T) {
		_, ok := GetBillingExprForGroup("unknown", "vip")
		assert.False(t, ok)
	})
}

// 覆盖表是热路径读到的共享 map，拷贝必须是深的，否则调用方改一个分组会串改配置。
func TestGetGroupBillingExprCopyIsDeep(t *testing.T) {
	orig := billingSetting.GroupBillingExpr
	t.Cleanup(func() { billingSetting.GroupBillingExpr = orig })
	billingSetting.GroupBillingExpr = map[string]map[string]string{"m": {"vip": "expr"}}

	copied := GetGroupBillingExprCopy()
	require.Equal(t, "expr", copied["m"]["vip"])
	copied["m"]["vip"] = "tampered"
	delete(copied, "m")

	assert.Equal(t, "expr", billingSetting.GroupBillingExpr["m"]["vip"])
}
