package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Avoiding a loss-making channel can cross a group boundary, and the group is what
// carries the price multiplier — so cost protection silently re-tiers the customer.
// Mirrors production routing for kimi-k3, whose only two channels live in different
// groups (7折 = 0.7 on channel 42, default = 1.0 on channel 45) behind an auto-group
// token. The last step pins the other half of the contract: once every channel is
// avoided, traffic must fall back rather than fail.
func TestRerouteCrossesGroupAndChangesBillingRatio(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	const modelName = "kimi-k3"

	createCostProtectionChannel(t, db, 42, "7折", modelName)
	createCostProtectionChannel(t, db, 45, "default", modelName)
	model.InitChannelCache()

	// Production auto-group order: cheapest first, default last.
	prevAuto := setting.AutoGroups2JsonString()
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["2.5折","5折","7折","8折","default"]`))
	t.Cleanup(func() { _ = setting.UpdateAutoGroupsByJsonString(prevAuto) })

	prevUsable := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"default":"1倍率","2.5折":"0.25倍率","5折":"0.5倍率","7折":"0.7倍率","8折":"0.8倍率","auto":"自动选择分组"}`))
	t.Cleanup(func() { _ = setting.UpdateUserUsableGroupsByJSONString(prevUsable) })

	prevRatio := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"7折":0.7,"5折":0.5,"2.5折":0.25,"8折":0.8,"auto":1}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateGroupRatioByJSONString(prevRatio) })

	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"2.5折", "5折", "7折", "8折", "default"})
		common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
		return ctx
	}
	pick := func() (*model.Channel, string) {
		ch, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
			Ctx: newCtx(), TokenGroup: "auto", ModelName: modelName,
			RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0),
		})
		require.NoError(t, err)
		require.NotNil(t, ch)
		return ch, group
	}

	before, groupBefore := pick()
	t.Logf("标记前: 渠道=%d 分组=%s 倍率=%v", before.Id, groupBefore, ratio_setting.GetGroupRatio(groupBefore))
	assert.Equal(t, 42, before.Id)
	assert.Equal(t, "7折", groupBefore)

	require.NoError(t, markCostProtectedChannel("7折", modelName, 42))

	after, groupAfter := pick()
	t.Logf("标记后: 渠道=%d 分组=%s 倍率=%v", after.Id, groupAfter, ratio_setting.GetGroupRatio(groupAfter))
	assert.Equal(t, 45, after.Id, "应切到 default 分组的渠道")
	assert.Equal(t, "default", groupAfter)

	rBefore := ratio_setting.GetGroupRatio(groupBefore)
	rAfter := ratio_setting.GetGroupRatio(groupAfter)
	t.Logf("用户单价变化: %.2fx -> %.2fx (%.0f%%)", rBefore, rAfter, (rAfter/rBefore-1)*100)
	assert.Greater(t, rAfter, rBefore, "跨分组切换会抬高用户单价")

	// Both channels avoided: traffic must keep flowing rather than hard-fail.
	require.NoError(t, markCostProtectedChannel("default", modelName, 45))
	fallback, fallbackGroup := pick()
	t.Logf("两个渠道都被标记后: 渠道=%d 分组=%s", fallback.Id, fallbackGroup)
	assert.Contains(t, []int{42, 45}, fallback.Id, "全部标记后必须回落，不能断流")
}
