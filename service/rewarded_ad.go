package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ValidateRewardedAdCallback requires both a DCloud event signature and the
// configured S2S authentication. SIGN covers all decoded query parameters;
// CONNECTCODE authenticates the sender. Missing S2S fails closed.
func ValidateRewardedAdCallback(request *http.Request, cfg operation_setting.RewardedAdSetting, now time.Time) (model.RewardedAdCallback, error) {
	event := model.RewardedAdCallback{}
	if request.Method != "GET" || len(request.URL.RawQuery) > 8192 {
		return event, errors.New("invalid_request")
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return event, errors.New("invalid_request")
	}
	keys := make([]string, 0, len(query))
	for key, values := range query {
		if len(values) != 1 || len(key) > 128 || len(values[0]) > 1024 || strings.ContainsAny(key, "&=\r\n") || strings.ContainsAny(values[0], "&\r\n") {
			return event, errors.New("invalid_request")
		}
		keys = append(keys, key)
	}
	for _, field := range []string{"adpid", "platform", "provider", "trans_id", "user_id", "extra", "sign"} {
		if query.Get(field) == "" {
			return event, errors.New("invalid_request")
		}
	}
	if query.Get("adpid") != cfg.Adpid || query.Get("platform") != "weixin-mp" || (query.Get("provider") != "china" && query.Get("provider") != "global") || len(query.Get("trans_id")) > 128 || len(query.Get("extra")) > 64 || len(query.Get("user_id")) > 20 {
		return event, errors.New("invalid_request")
	}
	if cfg.Secret == "" || cfg.EffectiveCallbackAuthKey() == "" {
		return event, errors.New("invalid_signature")
	}
	var provided []byte
	switch cfg.EffectiveCallbackAuthMode() {
	case "sign":
		if len(cfg.EffectiveCallbackAuthKey()) < 32 {
			return event, errors.New("invalid_signature")
		}
		if len(request.Header.Values("Unicloud-S2s-Timestamp")) != 1 || len(request.Header.Values("Unicloud-S2s-Signature")) != 1 {
			return event, errors.New("invalid_signature")
		}
		timestamp := request.Header.Get("Unicloud-S2s-Timestamp")
		ms, err := strconv.ParseInt(timestamp, 10, 64)
		// Compare bounded intervals rather than subtracting attacker-controlled ints.
		if err != nil || ms < now.Add(-time.Minute).UnixMilli() || ms > now.Add(time.Minute).UnixMilli() {
			return event, errors.New("invalid_signature")
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+query.Get(key))
		}
		mac := hmac.New(sha256.New, []byte(cfg.EffectiveCallbackAuthKey()))
		mac.Write([]byte(timestamp + "\n" + strings.Join(parts, "&")))
		signatureParts := strings.Fields(request.Header.Get("Unicloud-S2s-Signature"))
		if len(signatureParts) != 2 || signatureParts[0] != "hmac-sha256" {
			return event, errors.New("invalid_signature")
		}
		provided, err = hex.DecodeString(signatureParts[1])
		if err != nil || !hmac.Equal(mac.Sum(nil), provided) {
			return event, errors.New("invalid_signature")
		}
	case "connectCode":
		if len(request.Header.Values("Unicloud-S2s-Authorization")) != 1 || !hmac.Equal([]byte(request.Header.Get("Unicloud-S2s-Authorization")), []byte("CONNECTCODE "+cfg.EffectiveCallbackAuthKey())) {
			return event, errors.New("invalid_signature")
		}
	default:
		return event, errors.New("invalid_signature")
	}
	hash := sha256.Sum256([]byte(cfg.Secret + ":" + query.Get("trans_id")))
	provided, err = hex.DecodeString(query.Get("sign"))
	if err != nil || !hmac.Equal(hash[:], provided) {
		return event, errors.New("invalid_signature")
	}
	return model.RewardedAdCallback{TransID: query.Get("trans_id"), UserID: query.Get("user_id"), Extra: query.Get("extra"), Adpid: query.Get("adpid"), Platform: query.Get("platform"), Provider: query.Get("provider")}, nil
}

func RewardedAdSessionView(session *model.RewardedAdSession, includeCallback bool) (map[string]any, error) {
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	status := session.Status
	if status == "pending" && time.Now().Unix() >= session.ExpiresAt {
		status = "expired"
	}
	data := map[string]any{"session_id": session.ID, "status": status, "created_at": session.CreatedAt, "expires_at": session.ExpiresAt, "currency": currency, "credited_at": nil, "balance": nil, "reward": nil, "reason": nil}
	if session.Reason != "" {
		data["reason"] = session.Reason
	}
	if includeCallback {
		data["adpid"] = session.Adpid
		data["url_callback"] = map[string]string{"userId": strconv.Itoa(session.UserID), "extra": session.ID}
		data["reward"] = quotaDisplayValue(session.RewardQuota, currency)
	}
	if session.Status == "credited" {
		balance, err := model.GetUserQuota(session.UserID, true)
		if err != nil {
			return nil, err
		}
		data["balance"] = quotaDisplayValue(int64(balance), currency)
		data["reward"] = quotaDisplayValue(session.RewardQuota, currency)
		data["credited_at"] = session.CreditedAt
	}
	return data, nil
}
func GetRewardedAdState(userID int, now time.Time) (map[string]any, error) {
	cfg, err := model.GetRewardedAdSettingFromDB()
	if err != nil {
		return nil, err
	}
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	var day model.RewardedAdDay
	err = model.DB.Where("day = ? AND user_id = ?", model.RewardedAdDate(now), userID).First(&day).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var identity model.RewardedAdIdentity
	err = model.DB.Where("user_id = ? AND app_id = ? AND config_fingerprint = ? AND expires_at > ?", userID, cfg.WechatAppID, cfg.IdentityFingerprint(), now.Unix()).First(&identity).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	ready := err == nil
	// Expired reservations are released by the maintenance task; reads only skip them.
	var pending model.RewardedAdSession
	err = model.RewardedAdViewerSessions(model.DB, userID, identity.OpenID).Where("status = ? AND expires_at > ?", "pending", now.Unix()).First(&pending).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hasPending := err == nil
	var viewerToday int64
	if err := model.RewardedAdViewerSessions(model.DB, userID, identity.OpenID).Where("day = ? AND (status = ? OR (status = ? AND expires_at > ?))", model.RewardedAdDate(now), "credited", "pending", now.Unix()).Count(&viewerToday).Error; err != nil {
		return nil, err
	}
	var last model.RewardedAdSession
	err = model.RewardedAdViewerSessions(model.DB, userID, identity.OpenID).Where("status = ?", "credited").Order("credited_at desc").First(&last).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	cooldown := int64(0)
	if err == nil {
		cooldown = last.CreditedAt + int64(cfg.CooldownSeconds) - now.Unix()
		if cooldown < 0 {
			cooldown = 0
		}
	}
	var global model.RewardedAdDay
	err = model.DB.Where("day = ? AND user_id = 0", model.RewardedAdDate(now)).First(&global).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	remaining := cfg.DailyLimit - int(viewerToday)
	if remaining < 0 {
		remaining = 0
	}
	enabled := cfg.Enabled && cfg.Configured()
	reason := ""
	switch {
	case !enabled:
		reason = "disabled"
	case !ready:
		reason = "identity_required"
	case hasPending:
		reason = "session_pending"
	case remaining == 0:
		reason = "daily_limit"
	case cooldown > 0:
		reason = "cooldown"
	case cfg.DailyBudgetQuota-global.CreditedQuota-global.ReservedQuota < cfg.RewardQuota:
		reason = "budget_exhausted"
	}
	data := map[string]any{"enabled": enabled, "identity_ready": ready, "adpid": nil, "reward": quotaDisplayValue(cfg.RewardQuota, currency), "currency": currency, "daily_limit": cfg.DailyLimit, "today_credited": day.Credited, "today_reserved": day.Reserved, "remaining": remaining, "cooldown_seconds": cooldown, "next_available_at": nil, "can_watch": reason == "", "reason": nil, "pending_session": nil}
	if enabled {
		data["adpid"] = cfg.Adpid
	}
	if reason != "" {
		data["reason"] = reason
	}
	if cooldown > 0 {
		data["next_available_at"] = now.Unix() + cooldown
	}
	if hasPending {
		data["pending_session"] = map[string]any{"session_id": pending.ID, "status": pending.Status, "expires_at": pending.ExpiresAt}
	}
	return data, nil
}

// RunRewardedAdMaintenance recovers reservations, cache invalidation and display
// logs after restarts. Database locks/claims permit multiple instances safely.
func RunRewardedAdMaintenance(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		if err := model.ExpireRewardedAdSessions(time.Now()); err != nil {
			common.SysError("ad reservation cleanup failed")
		}
		if err := model.SyncRewardedAdCaches(); err != nil {
			common.SysError("ad quota cache recovery failed")
		}
		if err := model.DeliverRewardLogs(); err != nil {
			common.SysError("reward log recovery failed")
		}
		if time.Since(lastPrune) >= time.Hour {
			if err := model.PruneDeliveredRewardLogs(time.Now().Unix()); err != nil {
				common.SysError("reward log pruning failed: " + err.Error())
			} else {
				lastPrune = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
