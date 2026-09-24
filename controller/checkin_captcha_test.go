package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestCheckinStatusReturnsEffectiveCaptchaProvider(t *testing.T) {
	oldDB, oldRedis := model.DB, common.RedisEnabled
	oldSetting, oldSMS := *operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings()
	oldTurnstile := common.TurnstileCheckEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Checkin{}))
	model.DB, common.RedisEnabled = db, false
	common.TurnstileCheckEnabled = true
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, CaptchaEnabled: true, CaptchaMode: "adaptive", CaptchaTrustDays: 3, CaptchaIPUserLimit: 5}
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{CaptchaAppId: "web-id", MiniAppID: "mini-id", AppSecretKey: "web-secret", MiniAppSecretKey: "mini-secret", SecretId: "id", SecretKey: "key"}
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = oldDB, oldRedis
		common.TurnstileCheckEnabled = oldTurnstile
		*operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings() = oldSetting, oldSMS
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	user := model.User{Id: 1, Username: "checkin-status-user"}
	require.NoError(t, db.Create(&user).Error)
	for _, tc := range []struct {
		name, client, provider, appID string
		verifiedAt                    int64
	}{
		{"first mini checkin", "mini_program", "tencent", "mini-id", 0},
		{"trusted mini checkin", "mini_program", "none", "", time.Now().Unix() - 3600},
		{"trusted web checkin", "web", "none", "", time.Now().Unix() - 3600},
		{"expired web trust", "web", "tencent", "web-id", time.Now().Unix() - 3*86400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("checkin_captcha_verified_at", tc.verifiedAt).Error)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Set("id", 1)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/user/checkin?captcha_client="+tc.client, nil)
			c.Request.RemoteAddr = "192.0.2.171:1234"
			GetCheckinStatus(c)
			var body struct {
				Success bool `json:"success"`
				Data    struct {
					Provider string `json:"captcha_provider"`
					AppID    string `json:"captcha_app_id"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.True(t, body.Success)
			assert.Equal(t, tc.provider, body.Data.Provider)
			assert.Equal(t, tc.appID, body.Data.AppID)
		})
	}
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("checkin_captcha_verified_at", time.Now().Unix()).Error)
	system_setting.GetSMSCaptchaSettings().MiniAppSecretKey = ""
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("id", 1)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/checkin?captcha_client=mini_program", nil)
	GetCheckinStatus(c)
	assert.Contains(t, response.Body.String(), "CAPTCHA_NOT_CONFIGURED")
}

func TestCheckinCaptchaOptionsRejectInvalidPolicies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	oldDB, oldSetting, oldOptions := model.DB, *operation_setting.GetCheckinSetting(), common.OptionMap
	model.DB = db
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		model.DB = oldDB
		*operation_setting.GetCheckinSetting() = oldSetting
		common.OptionMap = oldOptions
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	for _, tc := range []struct{ key, value string }{
		{"captcha_mode", "unknown"}, {"captcha_trust_days", "0"}, {"captcha_trust_days", "31"}, {"captcha_trust_days", "1.5"},
		{"captcha_ip_user_limit", "1"}, {"captcha_ip_user_limit", "101"}, {"captcha_ip_user_limit", "2.5"},
	} {
		t.Run(tc.key+"_"+tc.value, func(t *testing.T) {
			body, err := common.Marshal(OptionUpdateRequest{Key: "checkin_setting." + tc.key, Value: tc.value})
			require.NoError(t, err)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))
			UpdateOption(c)
			assert.Contains(t, response.Body.String(), `"success":false`)
			var count int64
			require.NoError(t, db.Model(&model.Option{}).Count(&count).Error)
			assert.Zero(t, count, "invalid settings must not be persisted")
		})
	}
}
