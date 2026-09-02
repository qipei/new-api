package helper

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shanghaiTime(t *testing.T, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	require.NoError(t, err)
	return parsed
}

// 活动是乘在最终价格上的一个系数，所以按倍率计费和动态计费必须都生效，
// 而且都只乘一次——模型本身是哪种计费方式不该影响活动。
func TestPromotionAppliesToBothBillingModes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{
		"expr-model": billing_setting.BillingModeTieredExpr,
	})
	prevExpr, prevGroup := billing_setting.SwapExprConfigForTest(
		map[string]string{"expr-model": `tier("base", p * 4)`},
		map[string]map[string]string{},
	)
	prevPromotions := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapExprConfigForTest(prevExpr, prevGroup)
		billing_setting.SwapPromotionsForTest(prevPromotions)
	})

	prevQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = prevQPU })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ratio-model":2}`))

	promotions := map[string][]billing_setting.ModelPromotion{
		"expr-model":  {{Name: "开学季", Start: "2026-08-30", End: "2026-09-10", Ratio: 0.5}},
		"ratio-model": {{Name: "开学季", Start: "2026-08-30", End: "2026-09-10", Ratio: 0.5}},
	}

	for _, modelName := range []string{"expr-model", "ratio-model"} {
		t.Run(modelName, func(t *testing.T) {
			billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: modelName, UsingGroup: "vip", UserGroup: "vip",
				StartTime: shanghaiTime(t, "2026-09-01 12:00"),
			}
			regular, err := ModelPriceHelper(c, info, 1_000_000, &types.TokenCountMeta{MaxTokens: 0})
			require.NoError(t, err)

			billing_setting.SwapPromotionsForTest(promotions)
			c2, _ := gin.CreateTestContext(httptest.NewRecorder())
			c2.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info2 := &relaycommon.RelayInfo{
				OriginModelName: modelName, UsingGroup: "vip", UserGroup: "vip",
				StartTime: shanghaiTime(t, "2026-09-01 12:00"),
			}
			promoted, err := ModelPriceHelper(c2, info2, 1_000_000, &types.TokenCountMeta{MaxTokens: 0})
			require.NoError(t, err)

			assert.Equal(t, 0.5, promoted.GroupRatioInfo.PromotionRatio)
			assert.Equal(t, "开学季", promoted.GroupRatioInfo.PromotionName)
			assert.Equal(t, regular.GroupRatioInfo.GroupRatio*0.5, promoted.GroupRatioInfo.GroupRatio,
				"活动倍率必须只乘一次")
		})
	}
}

// 活动判定锚在请求发起时刻，不是评估时刻。跨过活动结束时刻的长请求，结算必须
// 还按活动价走，否则用户看到的报价和实际扣费对不上。
func TestPromotionIsAnchoredToRequestStartTime(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevPromotions := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{
		"m": {{Name: "开学季", Start: "2026-08-30", End: "2026-09-10", TZ: "Asia/Shanghai", Ratio: 0.5}},
	})
	t.Cleanup(func() { billing_setting.SwapPromotionsForTest(prevPromotions) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))

	cases := []struct {
		name      string
		startTime string
		wantRatio float64
	}{
		{name: "活动最后一天发起", startTime: "2026-09-10 23:59", wantRatio: 0.5},
		{name: "活动结束后发起", startTime: "2026-09-11 00:01", wantRatio: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "m", UsingGroup: "vip", UserGroup: "vip",
				StartTime: shanghaiTime(t, tc.startTime),
			}
			got := HandleGroupRatio(c, info)
			assert.Equal(t, tc.wantRatio, got.GroupRatio)
		})
	}
}
