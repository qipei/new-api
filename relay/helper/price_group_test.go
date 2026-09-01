package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预扣费必须按本次选中的分组取表达式：分组覆盖了就用覆盖那条，没覆盖才用模型级。
func TestModelPriceHelperUsesGroupBillingExpr(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const modelExpr = `tier("base", p * 3)`
	const groupExpr = `tier("flat", p * 12)`
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{"m": billing_setting.BillingModeTieredExpr})
	prevExpr, prevGroup := billing_setting.SwapExprConfigForTest(
		map[string]string{"m": modelExpr},
		map[string]map[string]string{"m": {"pricy": groupExpr}},
	)
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapExprConfigForTest(prevExpr, prevGroup)
	})

	prevQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = prevQPU })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"plain":1,"pricy":1}`))

	cases := []struct {
		name      string
		group     string
		wantQuota int
	}{
		{name: "group override wins", group: "pricy", wantQuota: 6_000_000},
		{name: "falls back to the model expression", group: "plain", wantQuota: 1_500_000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{OriginModelName: "m", UsingGroup: tc.group, UserGroup: tc.group}

			priceData, err := ModelPriceHelper(c, info, 1_000_000, &types.TokenCountMeta{MaxTokens: 0})
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, priceData.QuotaToPreConsume)
		})
	}
}
