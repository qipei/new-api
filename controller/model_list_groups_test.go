package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 两种自动路由都不是真实分组。落到"把 tokenGroup 当分组名用"的兜底分支就会查出
// 空列表——比价路由的密钥拿不到任何模型，正是这么来的。
func TestGetModelListGroupsHandlesAutoRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalUsable := setting.UserUsableGroups2JSONString()
	originalAuto := setting.AutoGroups2JsonString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAuto))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"default":"默认","vip":"VIP","auto":"自动"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":0.5,"auto":1}`))

	newCtx := func(tokenGroup string) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set("id", 1)
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(ctx, constant.ContextKeyTokenGroup, tokenGroup)
		return ctx
	}

	t.Run("比价路由覆盖用户全部可用分组", func(t *testing.T) {
		got, err := getModelListGroups(newCtx(service.AutoPriceGroup))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"default", "vip"}, got.ownerGroups)
		assert.NotContains(t, got.ownerGroups, service.AutoPriceGroup,
			"不能把 auto_price 本身当成分组名去查模型")
	})

	t.Run("auto 仍按编排顺序", func(t *testing.T) {
		got, err := getModelListGroups(newCtx("auto"))
		require.NoError(t, err)
		assert.Equal(t, []string{"vip", "default"}, got.ownerGroups)
	})

	t.Run("固定分组只查自己", func(t *testing.T) {
		got, err := getModelListGroups(newCtx("vip"))
		require.NoError(t, err)
		assert.Equal(t, []string{"vip"}, got.ownerGroups)
	})
}
