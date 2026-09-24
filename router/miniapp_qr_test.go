package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiniAppQRCodePublicReadAndProtectedSettings(t *testing.T) {
	previous, rateLimit := common.OptionMap, common.GlobalApiRateLimitEnable
	common.OptionMap = map[string]string{}
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() { common.OptionMap, common.GlobalApiRateLimitEnable = previous, rateLimit })
	engine := gin.New()
	SetApiRouter(engine)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/miniapp/qr-code", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"enabled":true`)
	assert.Contains(t, response.Body.String(), `"image_url":"/miniapp-code.jpg"`)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(method, "/api/option/miniapp-qr-code", nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code)
	}
}
