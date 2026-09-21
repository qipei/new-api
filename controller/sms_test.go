package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupPhoneControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldType := common.MainDatabaseType()
	oldRedis, oldPhone, oldRegister := common.RedisEnabled, common.PhoneLoginEnabled, common.RegisterEnabled
	oldSMS, oldCaptcha := *system_setting.GetSMSSettings(), *system_setting.GetSMSCaptchaSettings()
	oldQuota := common.QuotaForNewUser
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TwoFA{}, &model.AuthFlow{}, &model.UserSession{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.PhoneLoginEnabled = true
	common.RegisterEnabled = true
	common.QuotaForNewUser = 0
	*system_setting.GetSMSSettings() = system_setting.SMSSettings{LocalOnly: true, DebugCode: "123456", ResendIntervalSec: 60}
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{}
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetMainDatabaseType(oldType)
		common.RedisEnabled, common.PhoneLoginEnabled, common.RegisterEnabled = oldRedis, oldPhone, oldRegister
		common.QuotaForNewUser = oldQuota
		*system_setting.GetSMSSettings() = oldSMS
		*system_setting.GetSMSCaptchaSettings() = oldCaptcha
		require.NoError(t, sqlDB.Close())
	})
	return db
}

type phoneTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		RequireCaptcha bool   `json:"require_captcha"`
		Require2FA     bool   `json:"require_2fa"`
		FlowToken      string `json:"flow_token"`
		DebugCode      string `json:"debug_code"`
		AccessToken    string `json:"access_token"`
	} `json:"data"`
}

func phoneTestRequest(t *testing.T, handler gin.HandlerFunc, body string, userID int) phoneTestResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/login/phone", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if userID > 0 {
		c.Set("id", userID)
	}
	handler(c)
	var response phoneTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSMSCaptchaMisconfigurationCannotBypassVerification(t *testing.T) {
	setupPhoneControllerTest(t)
	system_setting.GetSMSCaptchaSettings().Enabled = true
	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138101"}`, 0)
	assert.False(t, response.Success)
	assert.Empty(t, response.Data.DebugCode)
}

func TestPhoneLoginCreatesAccountWithRegistrationDisabled(t *testing.T) {
	db := setupPhoneControllerTest(t)
	common.RegisterEnabled = false
	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138102"}`, 0)
	require.True(t, response.Success)
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138102","code":"000000"}`, 0)
	assert.False(t, response.Success)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.Zero(t, count)
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138102","code":"123456"}`, 0)
	require.True(t, response.Success)
	assert.NotEmpty(t, response.Data.AccessToken)
	user, err := model.GetUserByPhone("13800138102")
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, user.Status)
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestPhoneLoginLegacyBindingWithRegistrationDisabled(t *testing.T) {
	db := setupPhoneControllerTest(t)
	common.RegisterEnabled = false
	var response phoneTestResponse
	legacy := &model.User{Username: "legacy-phone", Password: "legacy-hash", Email: "legacy@example.test", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default"}
	require.NoError(t, db.Create(legacy).Error)
	response = phoneTestRequest(t, SendBindSMSCode, `{"phone":"13800138103"}`, legacy.Id)
	require.True(t, response.Success)
	response = phoneTestRequest(t, BindPhone, `{"phone":"13800138103","code":"123456"}`, legacy.Id)
	require.True(t, response.Success)
	require.NoError(t, db.First(legacy, legacy.Id).Error)
	assert.Equal(t, "13800138103", legacy.Phone)
	assert.Equal(t, "legacy-hash", legacy.Password)
	require.NoError(t, service.StoreSMSCode(service.SMSPurposeLogin, legacy.Phone, "123456", time.Minute))
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138103","code":"123456"}`, 0)
	assert.True(t, response.Success)
	assert.NotEmpty(t, response.Data.AccessToken)
}

func TestPhoneLoginHonorsDisabledAccountsAndTwoFA(t *testing.T) {
	db := setupPhoneControllerTest(t)
	user := &model.User{Username: "phone-2fa", Phone: "13800138104", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, Group: "default"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, service.StoreSMSCode(service.SMSPurposeLogin, user.Phone, "123456", time.Minute))
	response := phoneTestRequest(t, PhoneLogin, `{"phone":"13800138104","code":"123456"}`, 0)
	assert.False(t, response.Success)
	require.NoError(t, db.Model(user).Update("status", common.UserStatusEnabled).Error)
	require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, IsEnabled: true}).Error)
	require.NoError(t, service.StoreSMSCode(service.SMSPurposeLogin, user.Phone, "123456", time.Minute))
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138104","code":"123456"}`, 0)
	assert.True(t, response.Success)
	assert.True(t, response.Data.Require2FA)
	assert.NotEmpty(t, response.Data.FlowToken)
	assert.Empty(t, response.Data.AccessToken)
}

func TestPhoneLoginDisabledPreventsAccountCreation(t *testing.T) {
	db := setupPhoneControllerTest(t)
	common.PhoneLoginEnabled = false
	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138105"}`, 0)
	assert.False(t, response.Success)
	require.NoError(t, service.StoreSMSCode(service.SMSPurposeLogin, "13800138105", "123456", time.Minute))
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138105","code":"123456"}`, 0)
	assert.False(t, response.Success)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestThrottledSMSClientMustVerifyBeforeSending(t *testing.T) {
	setupPhoneControllerTest(t)
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{Enabled: true, CaptchaAppId: "195901070", AppSecretKey: "test", SecretId: "test", SecretKey: "test", WindowSeconds: 600}
	service.RequireSMSCaptchaForIP("192.0.2.1")
	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138182"}`, 0)
	assert.True(t, response.Success)
	assert.True(t, response.Data.RequireCaptcha)
	assert.Empty(t, response.Data.DebugCode)
	assert.False(t, service.VerifySMSCode(service.SMSPurposeLogin, "13800138182", "123456"))
}

func TestSMSCodeForDisabledAccountDoesNotRevealStatus(t *testing.T) {
	db := setupPhoneControllerTest(t)
	user := &model.User{Username: "phone-disabled", Phone: "13800138106", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, Group: "default"}
	require.NoError(t, db.Create(user).Error)

	// 发码接口对已禁用号码的响应与正常号码一致，登录时才会被拒绝。
	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138106"}`, 0)
	assert.True(t, response.Success)
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138106","code":"123456"}`, 0)
	assert.False(t, response.Success)
	assert.Empty(t, response.Data.AccessToken)
}

func TestPhoneLoginAfterSelfDeletionCreatesFreshAccount(t *testing.T) {
	db := setupPhoneControllerTest(t)
	deleted := &model.User{Username: "phone-deleted", Phone: "13800138107", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default"}
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)

	response := phoneTestRequest(t, SendLoginSMSCode, `{"phone":"13800138107"}`, 0)
	require.True(t, response.Success)
	response = phoneTestRequest(t, PhoneLogin, `{"phone":"13800138107","code":"123456"}`, 0)
	require.True(t, response.Success)
	assert.NotEmpty(t, response.Data.AccessToken)

	var fresh model.User
	require.NoError(t, db.Where("phone = ?", "13800138107").First(&fresh).Error)
	assert.NotEqual(t, deleted.Id, fresh.Id)
}
