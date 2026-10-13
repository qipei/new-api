package controller

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRewardedAdSettingsPreserveSecretsAndRejectIncompleteEnable(t *testing.T) {
	cfg := operation_setting.GetRewardedAdSetting()
	t.Cleanup(func() { operation_setting.SetRewardedAdSetting(cfg) })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{})
	require.NoError(t, err)
	before := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = before })
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.RewardedAdSession{}, &model.RewardedAdIdentity{}, &model.RewardedAdDay{}))
	operation_setting.SetRewardedAdSetting(operation_setting.RewardedAdSetting{RewardQuota: 50, DailyLimit: 3, Secret: "hidden-secret", CallbackAuthKey: "hidden-auth", WechatAppSecret: "hidden-wx", BridgeAuthKey: "hidden-bridge"})
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/api/option/rewarded-ad", bytes.NewBufferString(body))
		SaveRewardedAdSettings(c)
		return w
	}
	w := request(`{"enabled":false,"reward_quota":50,"daily_limit":3,"cooldown_seconds":60,"daily_budget_quota":100}`)
	assert.Equal(t, 200, w.Code)
	assert.NotContains(t, w.Body.String(), "hidden-")
	assert.Equal(t, "hidden-secret", operation_setting.GetRewardedAdSetting().Secret)
	w = request(`{"enabled":true,"reward_quota":50,"daily_limit":3,"cooldown_seconds":60,"daily_budget_quota":100}`)
	assert.Equal(t, 400, w.Code)
	assert.False(t, operation_setting.GetRewardedAdSetting().Enabled)
	var option model.Option
	require.NoError(t, db.First(&option, "key = ?", operation_setting.RewardedAdOptionKey).Error)
	var saved operation_setting.RewardedAdSetting
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &saved))
	assert.False(t, saved.Enabled)
}
func TestRewardedAdCallbackWithoutCompleteSignatureDoesNotSucceed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "callback.db")), &gorm.Config{})
	require.NoError(t, err)
	before := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = before })
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/rewarded-ad/dcloud/callback?trans_id=fake", nil)
	RewardedAdCallback(c)
	assert.Equal(t, 400, w.Code)
	assert.JSONEq(t, `{"isValid":false}`, w.Body.String())
}
