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

func TestMiniAppProfilePublicContentAndAdminBoundary(t *testing.T) {
	oldOptions, oldLimit := common.OptionMap, common.GlobalApiRateLimitEnable
	common.OptionMap = map[string]string{"About": "<p>About us</p>", "HeaderNavModules": `{"about":false}`}
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() { common.OptionMap, common.GlobalApiRateLimitEnable = oldOptions, oldLimit })
	engine := gin.New()
	SetApiRouter(engine)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/miniapp/about", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "About us")
	old := httptest.NewRecorder()
	engine.ServeHTTP(old, httptest.NewRequest(http.MethodGet, "/api/about", nil))
	assert.NotEqual(t, http.StatusOK, old.Code)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/miniapp/customer-service", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"phone":""`)
	assert.Contains(t, response.Body.String(), `"qrcode_url":""`)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/self/referral/rewards", nil))
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(method, "/api/option/customer-service", nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code)
	}
}
