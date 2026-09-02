package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func priceRoutingOther(t *testing.T, tokenGroup, usingGroup string, ranked []string) map[string]interface{} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	if ranked != nil {
		common.SetContextKey(ctx, constant.ContextKeyPriceRankedGroups, ranked)
	}
	other := map[string]interface{}{}
	appendPriceRoutingInfo(ctx, &relaycommon.RelayInfo{
		TokenGroup: tokenGroup, UsingGroup: usingGroup,
	}, other)
	return other
}

// 比价路由下"为什么走了这个分组"必须能回答：名次和候选总数给用户看，
// 完整候选顺序只给管理员——它会暴露站点全部分组的相对价格。
func TestPriceRoutingInfoRecordsRankAndCandidates(t *testing.T) {
	ranked := []string{"8.8折", "6.5折", "default"}
	other := priceRoutingOther(t, AutoPriceGroup, "6.5折", ranked)

	assert.Equal(t, AutoPriceGroup, other["price_routing"])
	assert.Equal(t, 2, other["price_rank"], "6.5折 是候选里的第二便宜")
	assert.Equal(t, 3, other["price_candidate_count"])

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok, "完整候选顺序必须落在 admin_info 里")
	assert.Equal(t, ranked, adminInfo["price_ranked_groups"])
}

// 非比价路由的请求不该多出这些字段。
func TestPriceRoutingInfoSkipsOtherRoutingModes(t *testing.T) {
	for _, tokenGroup := range []string{"auto", "default", ""} {
		t.Run(tokenGroup, func(t *testing.T) {
			other := priceRoutingOther(t, tokenGroup, "default", []string{"a", "b"})
			assert.NotContains(t, other, "price_routing")
			assert.NotContains(t, other, "price_rank")
			assert.NotContains(t, other, "admin_info")
		})
	}
}

// 取不到候选列表时仍要标出这是比价路由，但不能凭空编一个名次。
func TestPriceRoutingInfoWithoutCandidates(t *testing.T) {
	other := priceRoutingOther(t, AutoPriceGroup, "default", nil)
	assert.Equal(t, AutoPriceGroup, other["price_routing"])
	assert.NotContains(t, other, "price_rank")
	assert.NotContains(t, other, "price_candidate_count")
}

// 选中的分组不在候选里（例如成本保护把它换掉了）时，不能标一个错的名次。
func TestPriceRoutingInfoOmitsRankWhenGroupIsNotACandidate(t *testing.T) {
	other := priceRoutingOther(t, AutoPriceGroup, "别处来的分组", []string{"a", "b"})
	assert.Equal(t, AutoPriceGroup, other["price_routing"])
	assert.Equal(t, 2, other["price_candidate_count"])
	assert.NotContains(t, other, "price_rank")
}

// 已有的 admin_info 不能被覆盖掉。
func TestPriceRoutingInfoPreservesExistingAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyPriceRankedGroups, []string{"a"})
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{"use_channel": []string{"7"}},
	}
	appendPriceRoutingInfo(ctx, &relaycommon.RelayInfo{
		TokenGroup: AutoPriceGroup, UsingGroup: "a",
	}, other)

	adminInfo := other["admin_info"].(map[string]interface{})
	assert.Equal(t, []string{"7"}, adminInfo["use_channel"])
	assert.Equal(t, []string{"a"}, adminInfo["price_ranked_groups"])
}

// 回归：GenerateTextOtherInfo 里 other["admin_info"] 是整体赋值，比价信息必须写在
// 那之后。第一版写在前面，单元测试孤立调用照样通过，真实请求里字段却被丢掉了。
func TestGenerateTextOtherInfoKeepsPriceRoutingAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ranked := []string{"8.8折", "6.5折", "default"}
	common.SetContextKey(ctx, constant.ContextKeyPriceRankedGroups, ranked)
	ctx.Set("use_channel", []string{"7"})

	now := time.Now()
	info := &relaycommon.RelayInfo{
		TokenGroup: AutoPriceGroup, UsingGroup: "6.5折",
		StartTime: now, FirstResponseTime: now,
		// ChannelMeta 是内嵌指针，GenerateTextOtherInfo 会读它的字段，不给就是空指针
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, 0, -1)

	assert.Equal(t, AutoPriceGroup, other["price_routing"])
	assert.Equal(t, 2, other["price_rank"])

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, ranked, adminInfo["price_ranked_groups"],
		"比价候选顺序不能被 admin_info 的整体赋值冲掉")
	assert.Equal(t, []string{"7"}, adminInfo["use_channel"],
		"原有的 admin_info 内容也要保住")
}
