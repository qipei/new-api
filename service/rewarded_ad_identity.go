package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RewardedAdIdentityResult contains only the public identity required by the safety network.
type RewardedAdIdentityResult struct {
	Ready  bool   `json:"ready"`
	OpenID string `json:"openid"`
	AppID  string `json:"app_id"`
}

// uni-open-bridge write methods return HTTP 200 with no result on success.
// Older deployments may return an explicit errCode instead.
type rewardedAdBridgeWriteResult struct {
	ErrorCode *int  `json:"errCode"`
	Success   *bool `json:"success"`
}

type rewardedAdIdentityClient struct {
	httpClient     *http.Client
	tokenMu        sync.Mutex
	token          string
	tokenConfig    [32]byte
	tokenExpiresAt time.Time
}

var rewardedAdWechatClient = &rewardedAdIdentityClient{httpClient: &http.Client{
	Timeout: 15 * time.Second,
	// Credentials must never follow redirects to another origin.
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}}

func InitializeRewardedAdIdentity(ctx context.Context, userID int, code string) (*RewardedAdIdentityResult, error) {
	cfg, err := model.GetRewardedAdSettingFromDB()
	if err != nil {
		return nil, errors.New("广告身份配置读取失败")
	}
	return rewardedAdWechatClient.initialize(ctx, cfg, userID, code)
}

func (client *rewardedAdIdentityClient) initialize(ctx context.Context, cfg operation_setting.RewardedAdSetting, userID int, code string) (*RewardedAdIdentityResult, error) {
	if !cfg.Enabled {
		return nil, errors.New("激励广告功能未开启")
	}
	if userID <= 0 || strings.TrimSpace(code) == "" || len(code) > 512 {
		return nil, errors.New("微信登录凭证无效")
	}
	bridge, err := url.Parse(cfg.BridgeURL)
	if err != nil || bridge.Scheme != "https" || bridge.Host == "" || bridge.User != nil || bridge.RawQuery != "" || bridge.Fragment != "" || cfg.EffectiveBridgeAuthKey() == "" || cfg.DCloudAppID == "" || cfg.WechatAppID == "" || cfg.WechatAppSecret == "" {
		return nil, errors.New("微信广告身份配置不完整")
	}
	query := url.Values{"appid": {cfg.WechatAppID}, "secret": {cfg.WechatAppSecret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	var session struct {
		OpenID     string `json:"openid"`
		SessionKey string `json:"session_key"`
		ErrorCode  int    `json:"errcode"`
	}
	if err = client.request(ctx, http.MethodGet, "https://api.weixin.qq.com/sns/jscode2session?"+query.Encode(), nil, "", &session, ""); err != nil {
		return nil, errors.New("微信身份验证暂时不可用")
	}
	if session.ErrorCode != 0 || session.OpenID == "" || session.SessionKey == "" {
		return nil, errors.New("微信登录凭证无效，请重新登录")
	}
	if err := client.syncAccessToken(ctx, cfg); err != nil {
		return nil, err
	}
	payload := map[string]any{"dcloudAppid": cfg.DCloudAppID, "platform": "weixin-mp", "openid": session.OpenID, "value": map[string]string{"session_key": session.SessionKey}, "expiresIn": -1}
	var result rewardedAdBridgeWriteResult
	if err = client.request(ctx, http.MethodPost, strings.TrimRight(cfg.BridgeURL, "/")+"/setSessionKey", payload, cfg.EffectiveBridgeAuthKey(), &result, cfg.EffectiveBridgeAuthMode()); err != nil || (result.ErrorCode == nil || *result.ErrorCode != 0) {
		return nil, errors.New("微信会话同步失败，请重新登录")
	}
	// Session keys remain in uni-open-bridge; the business DB stores no secrets.
	identity := model.RewardedAdIdentity{UserID: userID, OpenID: session.OpenID, AppID: cfg.WechatAppID, ConfigFingerprint: cfg.IdentityFingerprint(), ExpiresAt: time.Now().Add(time.Duration(operation_setting.RewardedAdIdentityTTL) * time.Second).Unix()}
	err = model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := model.LockRewardedAdSetting(tx)
		if err != nil {
			return err
		}
		if !current.Enabled || current.IdentityFingerprint() != cfg.IdentityFingerprint() {
			return errors.New("identity configuration changed")
		}
		// One WeChat identity serves one account at a time; limits also count by
		// openid, so moving it to another account cannot reset the daily quota.
		if err := tx.Where("app_id = ? AND open_id = ? AND user_id <> ?", identity.AppID, identity.OpenID, userID).Delete(&model.RewardedAdIdentity{}).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"open_id", "app_id", "expires_at", "config_fingerprint"})}).Create(&identity).Error
	})
	if err != nil {
		return nil, errors.New("广告身份配置已变更或保存失败，请重新初始化")
	}

	return &RewardedAdIdentityResult{Ready: true, OpenID: session.OpenID, AppID: cfg.WechatAppID}, nil
}

// syncAccessToken keeps uni-open-bridge holding a valid weixin-mp access_token.
// Only the refresh is serialized; per-user session_key sync runs concurrently.
func (client *rewardedAdIdentityClient) syncAccessToken(ctx context.Context, cfg operation_setting.RewardedAdSetting) error {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	cacheKey := sha256.Sum256([]byte(cfg.WechatAppID + "\x00" + cfg.WechatAppSecret + "\x00" + cfg.DCloudAppID + "\x00" + cfg.BridgeURL + "\x00" + cfg.EffectiveBridgeAuthKey() + "\x00" + cfg.EffectiveBridgeAuthMode()))
	if client.token != "" && client.tokenConfig == cacheKey && time.Now().Before(client.tokenExpiresAt) {
		return nil
	}
	tokenPayload := map[string]any{"appid": cfg.WechatAppID, "secret": cfg.WechatAppSecret, "grant_type": "client_credential", "force_refresh": false}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrorCode   int    `json:"errcode"`
	}
	if err := client.request(ctx, http.MethodPost, "https://api.weixin.qq.com/cgi-bin/stable_token", tokenPayload, "", &token, ""); err != nil || token.ErrorCode != 0 || token.AccessToken == "" || token.ExpiresIn <= 60 || token.ExpiresIn > 86400 {
		return errors.New("微信应用凭据获取失败")
	}
	payload := map[string]any{"dcloudAppid": cfg.DCloudAppID, "platform": "weixin-mp", "value": map[string]string{"access_token": token.AccessToken}, "expiresIn": token.ExpiresIn}
	var result rewardedAdBridgeWriteResult
	if err := client.request(ctx, http.MethodPost, strings.TrimRight(cfg.BridgeURL, "/")+"/setAccessToken", payload, cfg.EffectiveBridgeAuthKey(), &result, cfg.EffectiveBridgeAuthMode()); err != nil || (result.ErrorCode == nil || *result.ErrorCode != 0) {
		return errors.New("微信应用凭据同步失败")
	}
	client.token = token.AccessToken
	client.tokenConfig = cacheKey
	client.tokenExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn-60) * time.Second)
	return nil
}

// request bounds upstream responses and deliberately hides bodies and credential-bearing URLs in errors.
func (client *rewardedAdIdentityClient) request(ctx context.Context, method, endpoint string, payload any, authKey string, result any, authMode string) error {
	var body io.Reader
	var encoded []byte
	var err error
	if payload != nil {
		encoded, err = common.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errors.New("invalid upstream request")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authKey != "" {
		switch authMode {
		case "connectCode":
			req.Header.Set("Unicloud-S2s-Authorization", "CONNECTCODE "+authKey)
		case "sign":
			// uni-cloud-s2s signs only top-level primitive JSON fields. Nested value
			// objects remain in the HTTPS body, exactly as required by its protocol.
			var data map[string]any
			if err := common.Unmarshal(encoded, &data); err != nil {
				return errors.New("invalid bridge payload")
			}
			keys := make([]string, 0, len(data))
			values := make(map[string]string, len(data))
			for key, value := range data {
				switch value := value.(type) {
				case string:
					values[key] = value
				case bool:
					values[key] = strconv.FormatBool(value)
				case float64:
					values[key] = strconv.FormatFloat(value, 'f', -1, 64)
				default:
					continue
				}
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, key+"="+values[key])
			}
			timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
			mac := hmac.New(sha256.New, []byte(authKey))
			mac.Write([]byte(timestamp + "\n" + strings.Join(parts, "&")))
			req.Header.Set("Unicloud-S2s-Timestamp", timestamp)
			req.Header.Set("Unicloud-S2s-Signature", "hmac-sha256 "+hex.EncodeToString(mac.Sum(nil)))
		default:
			return errors.New("invalid bridge authentication mode")
		}
	}
	res, err := client.httpClient.Do(req)
	if err != nil {
		return errors.New("upstream request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("invalid upstream response")
	}
	if bridgeResult, ok := result.(*rewardedAdBridgeWriteResult); ok && len(bytes.TrimSpace(data)) == 0 {
		zero := 0
		bridgeResult.ErrorCode = &zero
		return nil
	}
	if err = common.Unmarshal(data, result); err != nil {
		return errors.New("invalid upstream response")
	}
	if bridgeResult, ok := result.(*rewardedAdBridgeWriteResult); ok && bridgeResult.Success != nil && !*bridgeResult.Success {
		return errors.New("bridge rejected request")
	}
	return nil
}
