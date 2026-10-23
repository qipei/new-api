package service

import (
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
	"strings"
	"testing"
	"time"
)

func TestRewardedAdCallbackUsesOfficialEventSignatureWithoutS2S(t *testing.T) {
	cfg := operation_setting.RewardedAdSetting{Adpid: "ad1", Secret: "event-secret", S2SAuthMode: "sign", S2SAuthKey: strings.Repeat("k", 32)}
	hash := sha256.Sum256([]byte("event-secret:transaction"))
	values := url.Values{"adpid": {"ad1"}, "platform": {"weixin-mp"}, "provider": {"china"}, "trans_id": {"transaction"}, "user_id": {"1"}, "extra": {"session"}, "sign": {hex.EncodeToString(hash[:])}}
	request := httptest.NewRequest("GET", "/callback?"+values.Encode(), nil)
	event, err := ValidateRewardedAdCallback(request, cfg, time.Now())
	require.NoError(t, err)
	assert.Equal(t, model.RewardedAdCallback{TransID: "transaction", UserID: "1", Extra: "session", Adpid: "ad1", Platform: "weixin-mp", Provider: "china"}, event)
	// Bridge authentication mode has no bearing on DCloud's ad event signature.
	cfg.S2SAuthMode = "connectCode"
	cfg.S2SAuthKey = ""
	_, err = ValidateRewardedAdCallback(request, cfg, time.Now())
	require.NoError(t, err)
	for _, tc := range []struct{ name, query, method string }{
		{"invalid signature", strings.Replace(values.Encode(), values.Get("sign"), strings.Repeat("0", 64), 1), "GET"},
		{"different transaction", strings.Replace(values.Encode(), "trans_id=transaction", "trans_id=other", 1), "GET"},
		{"duplicate recipient", values.Encode() + "&user_id=2", "GET"},
		{"missing session", strings.Replace(values.Encode(), "extra=session&", "", 1), "GET"},
		{"different ad slot", strings.Replace(values.Encode(), "adpid=ad1", "adpid=other", 1), "GET"},
		{"wrong method", values.Encode(), "POST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateRewardedAdCallback(httptest.NewRequest(tc.method, "/callback?"+tc.query, nil), cfg, time.Now())
			require.Error(t, err)
		})
	}
	cfg.Secret = ""
	_, err = ValidateRewardedAdCallback(request, cfg, time.Now())
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
