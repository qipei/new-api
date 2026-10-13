package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type rewardedAdIdentityTransport func(*http.Request) (*http.Response, error)

func (f rewardedAdIdentityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRewardedAdIdentityInitializesAndCachesToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.RewardedAdIdentity{}, &model.Option{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	cfg := operation_setting.RewardedAdSetting{Enabled: true, WechatAppID: "wx-app", WechatAppSecret: "wx-secret", DCloudAppID: "__UNI__app", BridgeURL: "https://bridge.example/uni-open-bridge", BridgeAuthKey: "bridge-secret"}
	encodedConfig, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: operation_setting.RewardedAdOptionKey, Value: string(encodedConfig)}).Error)
	calls := map[string]int{}
	client := &rewardedAdIdentityClient{httpClient: &http.Client{Transport: rewardedAdIdentityTransport(func(req *http.Request) (*http.Response, error) {
		calls[req.URL.Path]++
		response := ""
		switch req.URL.Path {
		case "/sns/jscode2session":
			assert.Equal(t, "api.weixin.qq.com", req.URL.Host)
			assert.Equal(t, "code", req.URL.Query().Get("js_code"))
			response = `{"openid":"wx-user","session_key":"session-secret"}`
		case "/cgi-bin/stable_token":
			assert.Equal(t, http.MethodPost, req.Method)
			var tokenPayload map[string]any
			require.NoError(t, common.DecodeJson(req.Body, &tokenPayload))
			assert.Equal(t, false, tokenPayload["force_refresh"])
			response = `{"access_token":"access-secret","expires_in":7200}`
		case "/uni-open-bridge/setAccessToken", "/uni-open-bridge/setSessionKey":
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "CONNECTCODE bridge-secret", req.Header.Get("Unicloud-S2s-Authorization"))
			var body map[string]any
			require.NoError(t, common.DecodeJson(req.Body, &body))
			assert.Equal(t, "__UNI__app", body["dcloudAppid"])
			assert.Equal(t, "weixin-mp", body["platform"])
			if strings.HasSuffix(req.URL.Path, "setSessionKey") {
				assert.Equal(t, "wx-user", body["openid"])
				assert.Equal(t, float64(-1), body["expiresIn"])
				assert.Equal(t, "session-secret", body["value"].(map[string]any)["session_key"])
			}
			response = ""
		default:
			t.Fatalf("unexpected endpoint %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}}
	for i := 0; i < 2; i++ {
		result, err := client.initialize(context.Background(), cfg, 1, "code")
		require.NoError(t, err)
		assert.Equal(t, &RewardedAdIdentityResult{Ready: true, OpenID: "wx-user", AppID: "wx-app"}, result)
		encoded, err := common.Marshal(result)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "secret")
	}
	assert.Equal(t, 1, calls["/cgi-bin/stable_token"])
	assert.Equal(t, 1, calls["/uni-open-bridge/setAccessToken"])
	assert.Equal(t, 2, calls["/uni-open-bridge/setSessionKey"])
	var identity model.RewardedAdIdentity
	require.NoError(t, db.First(&identity, "user_id = ?", 1).Error)
	assert.Equal(t, "wx-user", identity.OpenID)
	assert.Equal(t, "wx-app", identity.AppID)
	assert.Equal(t, cfg.IdentityFingerprint(), identity.ConfigFingerprint)
	assert.Positive(t, identity.ExpiresAt)
	var count int64
	require.NoError(t, db.Model(&model.RewardedAdIdentity{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestRewardedAdIdentityFailureNeverStoresIdentity(t *testing.T) {
	for _, failure := range []string{"wechat", "access", "session", "missing_confirmation", "config_rotated", "cloud_error", "invalid_response"} {
		t.Run(failure, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.RewardedAdIdentity{}, &model.Option{}))
			oldDB := model.DB
			model.DB = db
			t.Cleanup(func() { model.DB = oldDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
			cfg := operation_setting.RewardedAdSetting{Enabled: true, WechatAppID: "wx-app", WechatAppSecret: "secret", DCloudAppID: "__UNI__app", BridgeURL: "https://bridge.example/uni-open-bridge", BridgeAuthKey: "secret"}
			encodedConfig, err := common.Marshal(cfg)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Option{Key: operation_setting.RewardedAdOptionKey, Value: string(encodedConfig)}).Error)
			client := &rewardedAdIdentityClient{httpClient: &http.Client{Transport: rewardedAdIdentityTransport(func(req *http.Request) (*http.Response, error) {
				response := `{"errCode":0}`
				switch req.URL.Path {
				case "/sns/jscode2session":
					response = `{"openid":"wx-user","session_key":"session-secret"}`
					if failure == "wechat" {
						response = `{"errcode":40029,"errmsg":"secret-body"}`
					}
				case "/cgi-bin/stable_token":
					response = `{"access_token":"access-secret","expires_in":7200}`
				case "/uni-open-bridge/setAccessToken":
					if failure == "access" {
						response = `{"errCode":500,"errMsg":"secret-body"}`
					}
				case "/uni-open-bridge/setSessionKey":
					if failure == "config_rotated" {
						rotated := cfg
						rotated.DCloudAppID = "__UNI__new"
						raw, err := common.Marshal(rotated)
						require.NoError(t, err)
						require.NoError(t, db.Model(&model.Option{}).Where("key = ?", operation_setting.RewardedAdOptionKey).Update("value", string(raw)).Error)
					}
					if failure == "missing_confirmation" {
						response = `{}`
					}
					if failure == "cloud_error" {
						response = `{"success":false,"error":{"code":"FunctionBizError","message":"secret-body"}}`
					}
					if failure == "invalid_response" {
						response = `unexpected response`
					}
					if failure == "session" {
						response = `{"errCode":500,"errMsg":"secret-body"}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
			})}}
			result, err := client.initialize(context.Background(), cfg, 1, "code")
			require.Error(t, err)
			assert.Nil(t, result)
			assert.NotContains(t, err.Error(), "secret")
			var count int64
			require.NoError(t, db.Model(&model.RewardedAdIdentity{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestRewardedAdBridgeSignUsesOfficialHeadersAndCanonicalBody(t *testing.T) {
	cfg := operation_setting.RewardedAdSetting{BridgeAuthMode: "sign", BridgeAuthKey: "0123456789abcdef0123456789abcdef"}
	client := &rewardedAdIdentityClient{httpClient: &http.Client{Transport: rewardedAdIdentityTransport(func(req *http.Request) (*http.Response, error) {
		assert.Empty(t, req.Header.Get("Unicloud-S2s-Authorization"))
		timestamp := req.Header.Get("Unicloud-S2s-Timestamp")
		millis, err := strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		assert.InDelta(t, time.Now().UnixMilli(), millis, 1000)
		mac := hmac.New(sha256.New, []byte(cfg.BridgeAuthKey))
		mac.Write([]byte(timestamp + "\n" + "dcloudAppid=__UNI__app&expiresIn=-1&openid=wx-user&platform=weixin-mp"))
		assert.Equal(t, "hmac-sha256 "+hex.EncodeToString(mac.Sum(nil)), req.Header.Get("Unicloud-S2s-Signature"))
		var body map[string]any
		require.NoError(t, common.DecodeJson(req.Body, &body))
		assert.Equal(t, "session-secret", body["value"].(map[string]any)["session_key"])
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errCode":0}`)), Header: make(http.Header)}, nil
	})}}
	var result map[string]any
	payload := map[string]any{"dcloudAppid": "__UNI__app", "platform": "weixin-mp", "openid": "wx-user", "expiresIn": -1, "value": map[string]string{"session_key": "session-secret"}}
	require.NoError(t, client.request(context.Background(), http.MethodPost, "https://bridge.example/setSessionKey", payload, cfg.BridgeAuthKey, &result, cfg.EffectiveBridgeAuthMode()))
}
