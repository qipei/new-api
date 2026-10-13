package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRewardedAdCallbackAuthenticationCoversRecipient(t *testing.T) {
	now := time.Unix(1791777600, 0)
	cfg := operation_setting.RewardedAdSetting{Adpid: "ad1", Secret: "event-secret", CallbackAuthKey: "0123456789abcdef0123456789abcdef"}
	hash := sha256.Sum256([]byte(cfg.Secret + ":transaction"))
	values := url.Values{"adpid": {"ad1"}, "platform": {"weixin-mp"}, "provider": {"china"}, "trans_id": {"transaction"}, "user_id": {"1"}, "extra": {"session"}, "sign": {hex.EncodeToString(hash[:])}}
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(cfg.CallbackAuthKey))
	mac.Write([]byte(timestamp + "\n" + "adpid=ad1&extra=session&platform=weixin-mp&provider=china&sign=" + values.Get("sign") + "&trans_id=transaction&user_id=1"))
	request := httptest.NewRequest("GET", "/callback?"+values.Encode(), nil)
	request.Header.Set("Unicloud-S2s-Timestamp", timestamp)
	request.Header.Set("Unicloud-S2s-Signature", "hmac-sha256 "+hex.EncodeToString(mac.Sum(nil)))
	_, err := ValidateRewardedAdCallback(request, cfg, now)
	require.NoError(t, err)
	request.Header.Set("Unicloud-S2s-Signature", "sha256 "+hex.EncodeToString(mac.Sum(nil)))
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
	request.Header.Set("Unicloud-S2s-Signature", "hmac-sha256 "+hex.EncodeToString(mac.Sum(nil)))
	values.Set("user_id", "2")
	request.URL.RawQuery = values.Encode()
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
	values.Set("user_id", "1")
	request.URL.RawQuery = values.Encode() + "&user_id=2"
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
	request.URL.RawQuery = values.Encode()
	request.Header.Del("Unicloud-S2s-Signature")
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
}

func TestRewardedAdSharedS2SCallbackAuthentication(t *testing.T) {
	now := time.Unix(1791777600, 0)
	cfg := operation_setting.RewardedAdSetting{Adpid: "ad1", Secret: "event-secret", S2SAuthMode: "connectCode", S2SAuthKey: "shared-code", CallbackAuthKey: "legacy-key"}
	hash := sha256.Sum256([]byte(cfg.Secret + ":transaction"))
	values := url.Values{"adpid": {"ad1"}, "platform": {"weixin-mp"}, "provider": {"china"}, "trans_id": {"transaction"}, "user_id": {"1"}, "extra": {"session"}, "sign": {hex.EncodeToString(hash[:])}}
	request := httptest.NewRequest("GET", "/callback?"+values.Encode(), nil)
	request.Header.Set("Unicloud-S2s-Authorization", "CONNECTCODE shared-code")
	_, err := ValidateRewardedAdCallback(request, cfg, now)
	require.NoError(t, err)
	request.Header.Set("Unicloud-S2s-Authorization", "CONNECTCODE legacy-key")
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
	request.Header.Set("Unicloud-S2s-Authorization", "CONNECTCODE shared-code")
	values.Set("sign", "wrong")
	request.URL.RawQuery = values.Encode()
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
	values.Set("sign", hex.EncodeToString(hash[:]))
	request.URL.RawQuery = values.Encode()
	cfg.S2SAuthMode = "sign"
	cfg.S2SAuthKey = "0123456789abcdef0123456789abcdef"
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(cfg.S2SAuthKey))
	mac.Write([]byte(timestamp + "\n" + "adpid=ad1&extra=session&platform=weixin-mp&provider=china&sign=" + values.Get("sign") + "&trans_id=transaction&user_id=1"))
	request.Header.Set("Unicloud-S2s-Timestamp", timestamp)
	request.Header.Set("Unicloud-S2s-Signature", "hmac-sha256 "+hex.EncodeToString(mac.Sum(nil)))
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.NoError(t, err)
	values.Set("user_id", "2")
	request.URL.RawQuery = values.Encode()
	_, err = ValidateRewardedAdCallback(request, cfg, now)
	require.Error(t, err)
}

func TestRewardedAdStateDoesNotExposeOtherAccountPendingSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.RewardedAdIdentity{}, &model.RewardedAdSession{}, &model.RewardedAdDay{}))
	now := time.Now()
	cfg := operation_setting.RewardedAdSetting{Enabled: true, RewardQuota: 50, DailyLimit: 3, DailyBudgetQuota: 100, Adpid: "ad", Secret: "secret", CallbackAuthKey: strings.Repeat("k", 32), WechatAppID: "wx-app", WechatAppSecret: "wx-secret", DCloudAppID: "__UNI__app", BridgeURL: "https://bridge.example", BridgeAuthKey: "bridge"}
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: operation_setting.RewardedAdOptionKey, Value: string(raw)}).Error)
	require.NoError(t, db.Create(&model.RewardedAdIdentity{UserID: 2, OpenID: "shared-wechat", AppID: cfg.WechatAppID, ConfigFingerprint: cfg.IdentityFingerprint(), ExpiresAt: now.Unix() + 3600}).Error)
	require.NoError(t, db.Create(&model.RewardedAdSession{ID: "other-account-session", UserID: 1, OpenID: "shared-wechat", RequestID: "original-view", Day: model.RewardedAdDate(now), Status: "pending", ExpiresAt: now.Unix() + 900}).Error)
	state, err := GetRewardedAdState(2, now)
	require.NoError(t, err)
	assert.Equal(t, "session_pending", state["reason"])
	assert.Equal(t, false, state["can_watch"])
	assert.Nil(t, state["pending_session"], "a session owned by another account cannot be resumed by this user")
}
