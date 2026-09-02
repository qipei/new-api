package service

import (
	"net/http/httptest"
	"testing"

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

// 比价路由从最便宜的分组开始试，重试用完之后按价格顺延到下一贵的分组。
// 这里刻意把分组倍率排得和字母序相反，确保断言的是价格而不是遍历顺序。
func TestAutoPriceRoutingWalksGroupsCheapestFirst(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-price-model"
	createChannelSelectAutoGroupsChannel(t, db, 5101, "aaa-expensive", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 5102, "mmm-cheap", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 5103, "zzz-mid", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	originalRetry := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = originalRetry })

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"aaa-expensive":"贵","mmm-cheap":"便宜","zzz-mid":"中","default":"默认"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"aaa-expensive":1,"mmm-cheap":0.2,"zzz-mid":0.5,"default":1}`))
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
		Ctx: ctx, TokenGroup: AutoPriceGroup, ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: &retry,
	}

	visited := make([]string, 0, 4)
	for ; param.GetRetry() <= common.RetryTimes; param.IncreaseRetry() {
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		if channel == nil {
			break
		}
		visited = append(visited, group)
		require.Less(t, len(visited), 8, "没有自行结束：%v", visited)
	}

	assert.Equal(t, []string{"mmm-cheap", "zzz-mid", "aaa-expensive"}, visited,
		"必须按价格从低到高顺延，而不是分组名顺序")
}

// 一次请求内的多次重试必须看到同一份排序：跨组顺延靠下标记位置，
// 中途重排会让下标指向一个已经变了的列表。
func TestAutoPriceRankingIsFrozenPerRequest(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-price-frozen-model"
	createChannelSelectAutoGroupsChannel(t, db, 5201, "g-cheap", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 5202, "g-pricey", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"g-cheap":"便宜","g-pricey":"贵","default":"默认"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"g-cheap":0.2,"g-pricey":1,"default":1}`))
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
		Ctx: ctx, TokenGroup: AutoPriceGroup, ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: &retry,
	}

	// 用户自己的分组 default 也是可用分组，会一并参与比价；它和 g-pricey 同价
	// （都是 1），按名字定序，这正是"同价时结果稳定"那条规则。
	first := requestPriceRankedGroups(param, "default")
	require.Equal(t, []string{"g-cheap", "default", "g-pricey"}, first)

	// 请求进行到一半时管理员把价格改反了
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"g-cheap":1,"g-pricey":0.2,"default":1}`))

	assert.Equal(t, first, requestPriceRankedGroups(param, "default"),
		"同一次请求内排序必须冻结")

	// 新请求才应当看到新价格
	freshCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(freshCtx, constant.ContextKeyUserGroup, "default")
	freshRetry := 0
	freshParam := &RetryParam{
		Ctx: freshCtx, TokenGroup: AutoPriceGroup, ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: &freshRetry,
	}
	assert.Equal(t, []string{"g-pricey", "default", "g-cheap"},
		requestPriceRankedGroups(freshParam, "default"))
}
