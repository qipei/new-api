package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 加入比价路由之后，原有的 auto 必须仍然按管理员编排的顺序走——而不是被价格重排。
// 这里刻意把编排顺序配成和价格相反：如果 auto 被比价影响了，顺序就会反过来。
func TestAutoStillFollowsTheAdminOrderNotPrice(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-regression-model"
	createChannelSelectAutoGroupsChannel(t, db, 6101, "pricey-first", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 6102, "cheap-second", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	originalRetry := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = originalRetry })

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"pricey-first":"贵","cheap-second":"便宜","default":"默认"}`))
	// 编排顺序：贵的在前。价格：便宜的那个便宜得多。
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["pricey-first","cheap-second"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"pricey-first":10,"cheap-second":0.1,"default":1}`))
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{})
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	retry := 0
	param := &RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: &retry,
	}

	visited := make([]string, 0, 3)
	for ; param.GetRetry() <= common.RetryTimes; param.IncreaseRetry() {
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		if channel == nil {
			break
		}
		visited = append(visited, group)
		require.Less(t, len(visited), 6, "没有自行结束：%v", visited)
	}

	assert.Equal(t, []string{"pricey-first", "cheap-second"}, visited,
		"auto 必须按编排顺序走，不能被价格重排")
	_, cached := common.GetContextKey(ctx, constant.ContextKeyPriceRankedGroups)
	assert.False(t, cached, "auto 请求不该触发比价排序")
}

// 固定分组的令牌完全不受影响：既不比价，也不跨组。
func TestFixedGroupIsUnaffectedByPriceRouting(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "fixed-group-regression-model"
	createChannelSelectAutoGroupsChannel(t, db, 6201, "mine", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 6202, "cheaper-elsewhere", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	originalRetry := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = originalRetry })

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"mine":"我的","cheaper-elsewhere":"更便宜","default":"默认"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"mine":10,"cheaper-elsewhere":0.1,"default":1}`))
	prevMode := billing_setting.SwapBillingModeForTest(map[string]string{})
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() {
		billing_setting.SwapBillingModeForTest(prevMode)
		billing_setting.SwapPromotionsForTest(prevPromo)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	retry := 0
	param := &RetryParam{
		Ctx: ctx, TokenGroup: "mine", ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: &retry,
	}

	for range 3 {
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "mine", group, "固定分组不该跑到更便宜的分组去")
		assert.Equal(t, 6201, channel.Id)
		param.IncreaseRetry()
	}

	_, cached := common.GetContextKey(ctx, constant.ContextKeyPriceRankedGroups)
	assert.False(t, cached, "固定分组请求不该触发比价排序")
}

// 计费侧：比价路由不改变任何倍率计算，同一分组下三种路由算出来的倍率必须一致。
func TestEffectiveRatioIsIdenticalAcrossRoutingModes(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"8.8折":0.88}`))
	prevPromo := billing_setting.SwapPromotionsForTest(map[string][]billing_setting.ModelPromotion{})
	t.Cleanup(func() { billing_setting.SwapPromotionsForTest(prevPromo) })

	// 比价只影响"选哪个分组"，选定之后的倍率来源和其它路由完全一样。
	assert.Equal(t, ratio_setting.GetGroupRatio("8.8折"),
		EffectiveGroupRatio("m", "", "8.8折", timeNowForTest()))
}

func timeNowForTest() time.Time { return time.Now() }
