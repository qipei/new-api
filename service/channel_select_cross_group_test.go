package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按 controller/relay.go 的重试循环跑一遍，把每次尝试落在哪个分组记下来。
// 上限只是保险，正常应当由循环条件自己结束。
func replayRetryLoop(t *testing.T, ctx *gin.Context, modelName string, cap int) []string {
	t.Helper()
	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}

	attempts := make([]string, 0, cap)
	for ; param.GetRetry() <= common.RetryTimes; param.IncreaseRetry() {
		channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		if channel == nil {
			break
		}
		attempts = append(attempts, fmt.Sprintf("%s/ch%d", selectedGroup, channel.Id))
		require.Less(t, len(attempts), cap, "重试循环没有自行结束，实际序列：%v", attempts)
	}
	return attempts
}

// 跨分组重试开启后，当前分组要先把 RetryTimes 次机会用完才会换组——
// 不是"一失败就换组"。这个次数关系是用户配置 RetryTimes 时的核心预期。
func TestCrossGroupRetryExhaustsCurrentGroupFirst(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "cross-group-retry-model"
	createChannelSelectAutoGroupsChannel(t, db, 3101, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 3102, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	original := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = original })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	attempts := replayRetryLoop(t, ctx, modelName, 20)

	assert.Equal(t, []string{
		"vip/ch3101", "vip/ch3101", "vip/ch3101",
		"default/ch3102", "default/ch3102", "default/ch3102",
	}, attempts)
}

// 关闭跨分组重试时，即使当前分组的渠道一直失败也不会走到下一个分组。
func TestWithoutCrossGroupRetryStaysInTheFirstGroup(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "no-cross-group-retry-model"
	createChannelSelectAutoGroupsChannel(t, db, 3201, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 3202, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	original := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = original })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)

	attempts := replayRetryLoop(t, ctx, modelName, 20)

	assert.Equal(t, []string{"vip/ch3201", "vip/ch3201", "vip/ch3201"}, attempts)
}

// 分组里根本没有这个模型的渠道时，不需要跨分组重试也会顺延——
// 这条和 cross_group_retry 无关，别把两件事混在一起。
func TestGroupWithoutTheModelIsSkippedWithoutCrossGroupRetry(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "skip-empty-group-model"
	createChannelSelectAutoGroupsChannel(t, db, 3302, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	original := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = original })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)

	attempts := replayRetryLoop(t, ctx, modelName, 20)

	require.NotEmpty(t, attempts)
	assert.Equal(t, "default/ch3302", attempts[0], "vip 没有这个模型，应直接落到 default")
}
