package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCheckinCaptchaSecretIsNotReturnedByOptions(t *testing.T) {
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{"sms_captcha.mini_app_id": "123", "sms_captcha.mini_app_secret_key": "private-checkin-secret"}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(c)
	assert.Contains(t, response.Body.String(), "sms_captcha.mini_app_id")
	assert.NotContains(t, response.Body.String(), "private-checkin-secret")
	assert.NotContains(t, response.Body.String(), "mini_app_secret_key")
}

func TestCheckinCaptchaCannotEnableWithoutCredentials(t *testing.T) {
	old := *system_setting.GetSMSCaptchaSettings()
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{}
	t.Cleanup(func() { *system_setting.GetSMSCaptchaSettings() = old })
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(`{"key":"checkin_setting.captcha_enabled","value":"true"}`))
	UpdateOption(c)
	assert.Contains(t, response.Body.String(), `"success":false`)
}
