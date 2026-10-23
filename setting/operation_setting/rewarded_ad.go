package operation_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"net/url"
	"strings"
	"sync"
)

const RewardedAdOptionKey = "RewardedAdSettings"

// RewardedAdSessionTTL releases abandoned viewing reservations after three minutes.
const RewardedAdSessionTTL = 180

// RewardedAdLateCreditWindow is how long after a session expires a verified ad
// callback may still be credited automatically, subject to that day's limits.
const RewardedAdLateCreditWindow = 86400

// RewardedAdIdentityTTL bounds how long a synced session_key is trusted before the
// mini program must call uni.login again; WeChat publishes no fixed lifetime.
const RewardedAdIdentityTTL = 7200

type RewardedAdSetting struct {
	S2SAuthMode      string `json:"s2s_auth_mode,omitempty"`
	S2SAuthKey       string `json:"s2s_auth_key,omitempty"`
	Enabled          bool   `json:"enabled"`
	RewardQuota      int64  `json:"reward_quota"`
	DailyLimit       int    `json:"daily_limit"`
	CooldownSeconds  int    `json:"cooldown_seconds"`
	DailyBudgetQuota int64  `json:"daily_budget_quota"`
	Adpid            string `json:"adpid"`
	Secret           string `json:"secret"`
	CallbackAuthKey  string `json:"callback_auth_key"`
	WechatAppID      string `json:"wechat_app_id"`
	WechatAppSecret  string `json:"wechat_app_secret"`
	DCloudAppID      string `json:"dcloud_app_id"`
	BridgeURL        string `json:"bridge_url"`
	BridgeAuthKey    string `json:"bridge_auth_key"`
	BridgeAuthMode   string `json:"bridge_auth_mode,omitempty"`
}

var rewardedAdMu sync.RWMutex
var rewardedAdSetting = RewardedAdSetting{RewardQuota: 5000, DailyLimit: 3, CooldownSeconds: 60, BridgeAuthMode: "sign"}

func GetRewardedAdSetting() RewardedAdSetting {
	rewardedAdMu.RLock()
	defer rewardedAdMu.RUnlock()
	return rewardedAdSetting
}
func SetRewardedAdSetting(s RewardedAdSetting) {
	rewardedAdMu.Lock()
	defer rewardedAdMu.Unlock()
	rewardedAdSetting = s
}
func (s RewardedAdSetting) Configured() bool {
	return s.Validate() == nil && s.Adpid != "" && s.Secret != "" && s.WechatAppID != "" && s.WechatAppSecret != "" && s.DCloudAppID != "" && s.BridgeURL != "" && s.EffectiveBridgeAuthKey() != ""
}
func (s RewardedAdSetting) Validate() error {
	if mode := s.EffectiveBridgeAuthMode(); mode != "sign" && mode != "connectCode" {
		return errors.New("S2S 认证方式必须为 sign 或 connectCode")
	}
	if s.RewardQuota <= 0 || s.RewardQuota > 2147483647 || s.DailyLimit < 1 || s.DailyLimit > 100 || s.CooldownSeconds < 0 || s.CooldownSeconds > 86400 || s.DailyBudgetQuota < 0 || s.DailyBudgetQuota > common.MaxWalletQuota {
		return errors.New("奖励额度、次数、间隔或预算超出允许范围")
	}
	for _, v := range []string{s.Adpid, s.WechatAppID, s.DCloudAppID} {
		if len(v) > 128 || strings.ContainsAny(v, "\r\n") {
			return errors.New("广告或应用标识无效")
		}
	}
	for _, v := range []string{s.Secret, s.CallbackAuthKey, s.WechatAppSecret, s.BridgeAuthKey, s.S2SAuthKey} {
		if len(v) > 4096 || strings.ContainsAny(v, "\r\n") {
			return errors.New("密钥格式无效")
		}
	}
	if s.BridgeURL != "" {
		u, e := url.Parse(s.BridgeURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("桥接地址必须是固定 HTTPS 地址")
		}
	}
	if s.Enabled && (s.DailyBudgetQuota <= 0 || s.Adpid == "" || s.Secret == "" || (s.EffectiveBridgeAuthMode() == "sign" && len(s.EffectiveBridgeAuthKey()) < 32) || s.WechatAppID == "" || s.WechatAppSecret == "" || s.DCloudAppID == "" || s.BridgeURL == "" || s.EffectiveBridgeAuthKey() == "") {
		return errors.New("启用前需完整配置奖励预算、广告位、应用凭据及服务器认证（signKey 至少32字节）")
	}
	return nil
}

func (s RewardedAdSetting) IdentityFingerprint() string {
	raw, _ := common.Marshal([]string{s.WechatAppID, s.WechatAppSecret, s.DCloudAppID, s.BridgeURL, s.EffectiveBridgeAuthKey(), s.EffectiveBridgeAuthMode()})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Empty preserves the behavior of configurations saved before auth mode existed.
func (s RewardedAdSetting) EffectiveBridgeAuthMode() string {
	if s.S2SAuthMode != "" {
		return s.S2SAuthMode
	}
	if s.BridgeAuthMode == "" {
		return "connectCode"
	}
	return s.BridgeAuthMode
}

// Legacy settings keep their original credentials until explicitly consolidated.
func (s RewardedAdSetting) EffectiveBridgeAuthKey() string {
	if s.S2SAuthMode != "" {
		return s.S2SAuthKey
	}
	return s.BridgeAuthKey
}
func (s RewardedAdSetting) EffectiveCallbackAuthMode() string {
	if s.S2SAuthMode != "" {
		return s.S2SAuthMode
	}
	return "sign"
}
func (s RewardedAdSetting) EffectiveCallbackAuthKey() string {
	if s.S2SAuthMode != "" {
		return s.S2SAuthKey
	}
	return s.CallbackAuthKey
}
