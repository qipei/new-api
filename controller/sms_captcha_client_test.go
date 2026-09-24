package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type smsCaptchaClientTransport func(*http.Request) (*http.Response, error)

func (f smsCaptchaClientTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSMSMiniProgramChallengeAndTicketUseSharedMiniCredentials(t *testing.T) {
	setupPhoneControllerTest(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldClient, oldTransport := common.RDB, http.DefaultTransport
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RDB = oldClient; http.DefaultTransport = oldTransport; _ = client.Close() })
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{Enabled: true, CaptchaAppId: "123", AppSecretKey: "web-secret", MiniAppID: "456", MiniAppSecretKey: "mini-secret", SecretId: "id", SecretKey: "key"}
	service.RequireSMSCaptchaForIP("198.51.100.244")
	engine := gin.New()
	engine.POST("/code", SendLoginSMSCode)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/code?captcha_client=mini_program", strings.NewReader(`{"phone":"13800138991"}`))
	request.RemoteAddr = "198.51.100.244:1234"
	engine.ServeHTTP(response, request)
	assert.Contains(t, response.Body.String(), `"captcha_app_id":"456"`)
	assert.Contains(t, response.Body.String(), `"require_captcha":true`)
	assert.NotContains(t, response.Body.String(), "mini-secret")
	http.DefaultTransport = smsCaptchaClientTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "DescribeCaptchaMiniResult", r.Header.Get("X-TC-Action"))
		var payload map[string]interface{}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		assert.Equal(t, float64(456), payload["CaptchaAppId"])
		assert.Equal(t, "mini-secret", payload["AppSecretKey"])
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Response":{"CaptchaCode":1}}`))}, nil
	})
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/code?captcha_client=mini_program", strings.NewReader(`{"phone":"13800138991","captcha_ticket":"mini-login-ticket"}`))
	request.RemoteAddr = "198.51.100.244:1234"
	engine.ServeHTTP(response, request)
	assert.Contains(t, response.Body.String(), `"debug_code":"123456"`)
	assert.Contains(t, response.Body.String(), `"require_captcha":false`)
	// A missing mini secret must not fall back to configured Web credentials.
	system_setting.GetSMSCaptchaSettings().MiniAppSecretKey = ""
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/code?captcha_client=mini_program", strings.NewReader(`{"phone":"13800138992"}`)))
	assert.Contains(t, response.Body.String(), `"success":false`)
	assert.NotContains(t, response.Body.String(), "debug_code")
}
