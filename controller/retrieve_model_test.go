package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 单模型查询要和 /v1/models 用同一套可见性规则，令牌白名单是其中一环：
// 列表里被白名单挡掉的模型，单独查也必须挡。
func TestTokenModelLimitAllows(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newCtx := func(enabled bool, limit any) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, enabled)
		if limit != nil {
			common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, limit)
		}
		return ctx
	}

	t.Run("未启用白名单时放行", func(t *testing.T) {
		assert.True(t, tokenModelLimitAllows(newCtx(false, nil), "任意模型"))
	})

	t.Run("启用了但拿不到白名单内容时拒绝", func(t *testing.T) {
		assert.False(t, tokenModelLimitAllows(newCtx(true, nil), "任意模型"),
			"放行等于白名单形同虚设")
	})

	t.Run("白名单类型不对时拒绝", func(t *testing.T) {
		assert.False(t, tokenModelLimitAllows(newCtx(true, "不是 map"), "任意模型"))
	})

	t.Run("在白名单里才放行", func(t *testing.T) {
		ctx := newCtx(true, map[string]bool{"glm-5.3-flash": true})
		assert.True(t, tokenModelLimitAllows(ctx, "glm-5.3-flash"))
		assert.False(t, tokenModelLimitAllows(ctx, "别的模型"))
	})
}
