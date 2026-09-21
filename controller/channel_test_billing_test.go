package controller

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestUsesChannelBillingGroup(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldExport, oldLog := common.RedisEnabled, common.DataExportEnabled, common.LogConsumeEnabled
	oldQPU := common.QuotaPerUnit
	oldRatios := ratio_setting.GroupRatio2JSONString()
	oldSpecial := ratio_setting.GroupGroupRatio2JSONString()
	oldMode := billing_setting.SwapBillingModeForTest(map[string]string{"channel-group-test": billing_setting.BillingModeTieredExpr})
	const baseExpr = `tier("base", p * 2 + c * 8)`
	const discountExpr = `tier("discount", p * 1 + c * 4)`
	oldExpr, oldGroups := billing_setting.SwapExprConfigForTest(map[string]string{"channel-group-test": baseExpr}, map[string]map[string]string{
		"channel-group-test": {"4.5折": discountExpr},
	})
	oldPromotions := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled, common.DataExportEnabled, common.LogConsumeEnabled = oldRedis, oldExport, oldLog
		common.QuotaPerUnit = oldQPU
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldSpecial))
		billing_setting.SwapBillingModeForTest(oldMode)
		billing_setting.SwapExprConfigForTest(oldExpr, oldGroups)
		billing_setting.SwapPromotionsForTest(oldPromotions)
	})
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	common.DataExportEnabled, common.LogConsumeEnabled = false, true
	common.QuotaPerUnit = 500000
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"4.5折":0.45,"vip":0.8}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	user := &model.User{Id: 71001, Username: "channel-billing-admin", Group: "default", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"test","object":"chat.completion","model":"channel-group-test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)
	for _, tc := range []struct {
		name, groups, wantGroup, wantExpr string
		wantRatio                         float64
		wantQuota                         int
	}{
		{"channel discount overrides unrelated admin group", "4.5折", "4.5折", discountExpr, 0.45, 675},
		{"keeps admin group when channel supports it", "4.5折,default", "default", baseExpr, 1, 3000},
		{"uses first configured group when admin group is unavailable", "4.5折,vip", "4.5折", discountExpr, 0.45, 675},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &model.Channel{Id: 138, Type: constant.ChannelTypeOpenAI, Name: "channel under test", Key: "test-key", BaseURL: &upstream.URL, Models: "channel-group-test", Group: tc.groups}
			result := testChannel(context.Background(), channel, user.Id, "channel-group-test", "", false)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError)
			var log model.Log
			require.NoError(t, db.Order("id DESC").First(&log).Error)
			assert.Equal(t, tc.wantGroup, log.Group)
			assert.Equal(t, tc.wantQuota, log.Quota)
			var other struct {
				GroupRatio float64 `json:"group_ratio"`
				ExprB64    string  `json:"expr_b64"`
			}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, tc.wantRatio, other.GroupRatio)
			expr, err := base64.StdEncoding.DecodeString(other.ExprB64)
			require.NoError(t, err)
			assert.Equal(t, tc.wantExpr, string(expr))
		})
	}
}

func TestSettleChannelTestQuotaIncludesGroupRatio(t *testing.T) {
	oldQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQPU })
	for _, tc := range []struct {
		name    string
		ratio   float64
		perCall bool
		want    int
	}{
		{"token discount", 0.45, false, 1350},
		{"per-call discount", 0.45, true, 450000},
		{"token promotion", 0.225, false, 675},
		{"free tokens", 0, false, 0},
		{"free per-call", 0, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota, result := settleTestQuota(nil, types.PriceData{
				ModelRatio: 1, CompletionRatio: 4, ModelPrice: 2, UsePrice: tc.perCall,
				GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tc.ratio},
			}, &dto.Usage{PromptTokens: 1000, CompletionTokens: 500})
			assert.Nil(t, result)
			assert.Equal(t, tc.want, quota)
		})
	}
}
