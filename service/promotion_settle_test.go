package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 结算会用实际 token 重算一遍，而不是缩放预扣费的结果。活动倍率必须原样跟到
// 结算：预扣费打了五折、结算按原价扣，用户看到的报价和账单就对不上。
func TestTieredSettleKeepsThePromotionRatio(t *testing.T) {
	const expr = `tier("base", p * 3 + c * 9)`
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{
		"m": billing_setting.BillingModeTieredExpr,
	})
	prevExpr, prevGroup := billing_setting.SwapExprConfigForTest(
		map[string]string{"m": expr},
		map[string]map[string]string{},
	)
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapExprConfigForTest(prevExpr, prevGroup)
	})

	// 0.88 分组 × 0.5 活动，和线上那条"开学季五折"配置一致。
	const groupRatioWithPromotion = 0.88 * 0.5

	newInfo := func(groupRatio float64) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName: "m",
			UsingGroup:      "8.8折",
			StartTime:       time.Now(),
			PriceData: hosttypes.PriceData{
				GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: groupRatio},
			},
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ModelName:                 "m",
				ExprString:                expr,
				QuotaPerUnit:              500_000,
				EstimatedQuotaBeforeGroup: 1_000_000,
				GroupRatio:                1,
			},
		}
	}

	promoted := newInfo(groupRatioWithPromotion)
	snap, err := refreshTieredBillingGroup(promoted)
	require.NoError(t, err)
	require.NotNil(t, snap)

	regular := newInfo(0.88)
	regularSnap, err := refreshTieredBillingGroup(regular)
	require.NoError(t, err)
	require.NotNil(t, regularSnap)

	t.Logf("结算额度：活动期内=%d 活动期外=%d", snap.EstimatedQuotaAfterGroup, regularSnap.EstimatedQuotaAfterGroup)

	assert.Equal(t, groupRatioWithPromotion, snap.GroupRatio, "活动倍率必须跟到结算")
	assert.Equal(t, regularSnap.EstimatedQuotaAfterGroup/2, snap.EstimatedQuotaAfterGroup,
		"结算金额应当正好是不打活动时的一半")
}
