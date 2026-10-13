package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

type RewardedAdIdentity struct {
	UserID            int    `gorm:"primaryKey"`
	OpenID            string `gorm:"type:varchar(128);uniqueIndex:idx_ad_identity_openid,priority:2"`
	AppID             string `gorm:"type:varchar(128);uniqueIndex:idx_ad_identity_openid,priority:1"`
	ExpiresAt         int64
	ConfigFingerprint string `gorm:"type:varchar(64)"`
}
type RewardedAdSession struct {
	ID          string `gorm:"primaryKey;type:varchar(64)"`
	UserID      int    `gorm:"uniqueIndex:idx_ad_request,priority:1;index:idx_ad_pending,priority:1"`
	RequestID   string `gorm:"type:varchar(64);uniqueIndex:idx_ad_request,priority:2"`
	OpenID      string `gorm:"type:varchar(128);index:idx_ad_openid_day,priority:1"`
	Adpid       string `gorm:"type:varchar(128)"`
	RewardQuota int64
	Day         string `gorm:"type:varchar(10);index;index:idx_ad_openid_day,priority:2"`
	Status      string `gorm:"type:varchar(32);index:idx_ad_pending,priority:2;index:idx_ad_expiry,priority:1"`
	CreatedAt   int64
	ExpiresAt   int64 `gorm:"index:idx_ad_expiry,priority:2"`
	CacheSynced bool
	CreditedAt  int64
	Reason      string `gorm:"type:varchar(64)"`
}
type RewardedAdDay struct {
	Day           string `gorm:"primaryKey;type:varchar(10)"`
	UserID        int    `gorm:"primaryKey;autoIncrement:false"`
	Reserved      int
	Credited      int
	ReservedQuota int64
	CreditedQuota int64
}
type RewardedAdEvent struct {
	TransID   string `gorm:"primaryKey;type:varchar(128)"`
	SessionID string `gorm:"type:varchar(64);index"`
	UserID    int
	Adpid     string `gorm:"type:varchar(128)"`
	Provider  string `gorm:"type:varchar(16)"`
	Platform  string `gorm:"type:varchar(16)"`
	Status    string `gorm:"type:varchar(32)"`
	CreatedAt int64
}
type RewardedAdCallback struct{ TransID, UserID, Extra, Adpid, Platform, Provider string }

// RewardedAdViewerSessions scopes sessions to one viewer: the account plus the
// WeChat identity bound to it, so one WeChat user cannot multiply limits across accounts.
func RewardedAdViewerSessions(tx *gorm.DB, userID int, openID string) *gorm.DB {
	query := tx.Model(&RewardedAdSession{})
	if openID == "" {
		return query.Where("user_id = ?", userID)
	}
	return query.Where("(user_id = ? OR open_id = ?)", userID, openID)
}

func RewardedAdDate(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
}

// RewardedAdDay global row (user_id=0) serializes budget reservation and settlement
// across instances. All paths lock global day before user to keep a fixed order.
func lockRewardedAdDay(tx *gorm.DB, day string) error {
	row := RewardedAdDay{Day: day}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	return lockForUpdate(tx).Where("day = ? AND user_id = 0", day).First(&row).Error
}
func expireRewardedAdDay(tx *gorm.DB, day string, now int64) error {
	var sessions []RewardedAdSession
	if err := tx.Where("day = ? AND status = ? AND expires_at <= ?", day, "pending", now).Find(&sessions).Error; err != nil {
		return err
	}
	for _, s := range sessions {
		res := tx.Model(&RewardedAdSession{}).Where("id = ? AND status = ?", s.ID, "pending").Updates(map[string]any{"status": "expired"})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		for _, id := range []int{0, s.UserID} {
			if err := tx.Model(&RewardedAdDay{}).Where("day = ? AND user_id = ? AND reserved > 0 AND reserved_quota >= ?", day, id, s.RewardQuota).Updates(map[string]any{"reserved": gorm.Expr("reserved - 1"), "reserved_quota": gorm.Expr("reserved_quota - ?", s.RewardQuota)}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
func ExpireRewardedAdSessions(now time.Time) error {
	var days []string
	if err := DB.Model(&RewardedAdSession{}).Where("status = ? AND expires_at <= ?", "pending", now.Unix()).Distinct().Pluck("day", &days).Error; err != nil {
		return err
	}
	for _, day := range days {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			if err := lockRewardedAdDay(tx, day); err != nil {
				return err
			}
			return expireRewardedAdDay(tx, day, now.Unix())
		}); err != nil {
			return err
		}
	}
	return nil
}
func CreateRewardedAdSession(userID int, requestID string, cfg operation_setting.RewardedAdSetting, now time.Time) (*RewardedAdSession, error) {
	if requestID == "" || len(requestID) > 64 {
		return nil, errors.New("invalid_request")
	}
	if err := ExpireRewardedAdSessions(now); err != nil {
		return nil, err
	}
	result := &RewardedAdSession{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		day := RewardedAdDate(now)
		// The global day row already guards the budget reservation. Configuration
		// saves lock it too, so the configuration read below cannot change before
		// this session commits; no separate configuration-row lock is needed.
		if err := lockRewardedAdDay(tx, day); err != nil {
			return err
		}
		var option Option
		if err := tx.Where(&Option{Key: operation_setting.RewardedAdOptionKey}).Limit(1).Find(&option).Error; err != nil {
			return err
		}
		if option.Value != "" {
			cfg.BridgeAuthMode = ""
			cfg.S2SAuthMode = ""
			cfg.S2SAuthKey = ""
			if err := common.UnmarshalJsonStr(option.Value, &cfg); err != nil {
				return err
			}
			cfg.Enabled = cfg.Enabled && cfg.Configured()
		}
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return errors.New("account_unavailable")
		}
		found := tx.Where("user_id = ? AND request_id = ?", userID, requestID).First(result)
		if found.Error == nil {
			return nil
		}
		if !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return found.Error
		}
		if !cfg.Enabled {
			return errors.New("disabled")
		}
		var identity RewardedAdIdentity
		if err := tx.Where("user_id = ? AND app_id = ? AND config_fingerprint = ? AND expires_at > ?", userID, cfg.WechatAppID, cfg.IdentityFingerprint(), now.Unix()).First(&identity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("identity_required")
			}
			return err
		}
		var pending int64
		if err := RewardedAdViewerSessions(tx, userID, identity.OpenID).Where("status = ?", "pending").Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return errors.New("session_pending")
		}
		var previous RewardedAdSession
		err := RewardedAdViewerSessions(tx, userID, identity.OpenID).Where("status = ?", "credited").Order("credited_at desc").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && previous.CreditedAt+int64(cfg.CooldownSeconds) > now.Unix() {
			return errors.New("cooldown")
		}
		counter := RewardedAdDay{Day: day, UserID: userID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&counter).Error; err != nil {
			return err
		}
		if err := tx.Where("day = ? AND user_id = ?", day, userID).First(&counter).Error; err != nil {
			return err
		}
		if counter.Credited+counter.Reserved >= cfg.DailyLimit {
			return errors.New("daily_limit")
		}
		var viewerToday int64
		if err := RewardedAdViewerSessions(tx, userID, identity.OpenID).Where("day = ? AND status IN ?", day, []string{"pending", "credited"}).Count(&viewerToday).Error; err != nil {
			return err
		}
		if viewerToday >= int64(cfg.DailyLimit) {
			return errors.New("daily_limit")
		}
		var global RewardedAdDay
		if err := tx.Where("day = ? AND user_id = 0", day).First(&global).Error; err != nil {
			return err
		}
		if cfg.RewardQuota <= 0 || cfg.RewardQuota > 2147483647 || cfg.DailyBudgetQuota-global.CreditedQuota-global.ReservedQuota < cfg.RewardQuota {
			return errors.New("budget_exhausted")
		}
		bytes := make([]byte, 24)
		if _, err := rand.Read(bytes); err != nil {
			return err
		}
		*result = RewardedAdSession{ID: hex.EncodeToString(bytes), UserID: userID, RequestID: requestID, OpenID: identity.OpenID, Adpid: cfg.Adpid, RewardQuota: cfg.RewardQuota, Day: day, Status: "pending", CreatedAt: now.Unix(), ExpiresAt: now.Unix() + operation_setting.RewardedAdSessionTTL}
		if err := tx.Create(result).Error; err != nil {
			return err
		}
		for _, id := range []int{0, userID} {
			if err := tx.Model(&RewardedAdDay{}).Where("day = ? AND user_id = ?", day, id).Updates(map[string]any{"reserved": gorm.Expr("reserved + 1"), "reserved_quota": gorm.Expr("reserved_quota + ?", cfg.RewardQuota)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}
func SettleRewardedAd(event RewardedAdCallback, now time.Time) error {
	var session RewardedAdSession
	if err := DB.Where("id = ?", event.Extra).First(&session).Error; err != nil {
		return err
	}
	if event.UserID != strconv.Itoa(session.UserID) || event.Adpid != session.Adpid || event.Platform != "weixin-mp" || event.TransID == "" || len(event.TransID) > 128 {
		return errors.New("invalid_request")
	}
	review := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRewardedAdDay(tx, session.Day); err != nil {
			return err
		}
		if err := tx.Where("id = ?", session.ID).First(&session).Error; err != nil {
			return err
		}
		var prior RewardedAdEvent
		err := tx.Where("trans_id = ?", event.TransID).First(&prior).Error
		if err == nil {
			if prior.SessionID != session.ID || prior.UserID != session.UserID || prior.Adpid != event.Adpid || prior.Provider != event.Provider || prior.Platform != event.Platform {
				return errors.New("event_conflict")
			}
			if prior.Status != "credited" {
				review = true
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if session.Status == "credited" || session.Status == "review_required" {
			return errors.New("session_conflict")
		}
		record := RewardedAdEvent{TransID: event.TransID, SessionID: session.ID, UserID: session.UserID, Adpid: event.Adpid, Provider: event.Provider, Platform: event.Platform, Status: "credited", CreatedAt: now.Unix()}
		// A callback after the session expired is still a verified view (for example
		// DCloud redelivering after an outage). Credit it within the late window when
		// the day's limits still allow; otherwise keep it for review without crediting.
		late := session.Status == "expired" || now.Unix() >= session.ExpiresAt
		if late {
			if err := expireRewardedAdDay(tx, session.Day, now.Unix()); err != nil {
				return err
			}
			cfg, err := loadRewardedAdSetting(tx)
			if err != nil {
				return err
			}
			var global RewardedAdDay
			if err := tx.Where("day = ? AND user_id = 0", session.Day).First(&global).Error; err != nil {
				return err
			}
			var viewerDay int64
			if err := RewardedAdViewerSessions(tx, session.UserID, session.OpenID).Where("day = ? AND id <> ? AND (status = ? OR (status = ? AND expires_at > ?))", session.Day, session.ID, "credited", "pending", now.Unix()).Count(&viewerDay).Error; err != nil {
				return err
			}
			reason := ""
			switch {
			case now.Unix()-session.ExpiresAt > operation_setting.RewardedAdLateCreditWindow:
				reason = "session_expired"
			case viewerDay >= int64(cfg.DailyLimit) || cfg.DailyBudgetQuota-global.CreditedQuota-global.ReservedQuota < session.RewardQuota:
				reason = "limit_exceeded"
			}
			if reason != "" {
				record.Status = "review_required"
				review = true
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
				return tx.Model(&session).Updates(map[string]any{"status": "review_required", "reason": reason}).Error
			}
		} else if session.Status != "pending" {
			return errors.New("session_conflict")
		}
		amount := session.RewardQuota
		if amount <= 0 || amount > 2147483647 {
			return errors.New("invalid_reward")
		}
		res := tx.Model(&User{}).Where("id = ? AND status = ? AND quota <= ?", session.UserID, common.UserStatusEnabled, common.MaxWalletQuota-amount).Update("quota", gorm.Expr("quota + ?", amount))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("account_unavailable")
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&session).Updates(map[string]any{"status": "credited", "credited_at": now.Unix()}).Error; err != nil {
			return err
		}
		for _, id := range []int{0, session.UserID} {
			counter := tx.Model(&RewardedAdDay{}).Where("day = ? AND user_id = ?", session.Day, id)
			updates := map[string]any{"credited": gorm.Expr("credited + 1"), "credited_quota": gorm.Expr("credited_quota + ?", amount)}
			// A late session's reservation was already released when it expired.
			if !late {
				counter = counter.Where("reserved > 0 AND reserved_quota >= ?", amount)
				updates["reserved"] = gorm.Expr("reserved - 1")
				updates["reserved_quota"] = gorm.Expr("reserved_quota - ?", amount)
			}
			res := counter.Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errors.New("reservation_conflict")
			}
		}
		var user User
		if err := tx.Select("username").First(&user, session.UserID).Error; err != nil {
			return err
		}
		other := common.MapToJsonStr(map[string]any{"reward_source": "rewarded_ad", "reward_record_id": session.ID, "session_id": session.ID, "credited_at": now.Unix(), "admin_info": map[string]any{"trans_id": event.TransID, "adpid": event.Adpid}})
		return EnqueueRewardLog(tx, &Log{UserId: session.UserID, Username: user.Username, Type: LogTypeRewardedAd, Quota: int(amount), CreatedAt: now.Unix(), Content: fmt.Sprintf("观看激励广告，获得额度 %d", amount), Other: other}, "ad:"+session.ID)
	})
	if err != nil {
		return err
	}
	if review {
		return errors.New("review_required")
	}
	// Never reapply a cache delta on retries. Invalidating forces an authoritative
	// reload and is safe even when a duplicate callback follows a successful credit.
	if err := invalidateUserCache(session.UserID); err != nil {
		return fmt.Errorf("quota cache invalidation: %w", err)
	}
	return DB.Model(&RewardedAdSession{}).Where("id = ? AND status = ?", session.ID, "credited").Update("cache_synced", true).Error
}

func SyncRewardedAdCaches() error {
	var sessions []RewardedAdSession
	if err := DB.Where("status = ? AND cache_synced = ?", "credited", false).Limit(100).Find(&sessions).Error; err != nil {
		return err
	}
	for _, session := range sessions {
		if err := invalidateUserCache(session.UserID); err != nil {
			return err
		}
		if err := DB.Model(&RewardedAdSession{}).Where("id = ?", session.ID).Update("cache_synced", true).Error; err != nil {
			return err
		}
	}
	return nil
}

// SaveRewardedAdSetting serializes sensitive configuration changes with session
// creation across all application nodes, not merely with a process mutex.
var ErrRewardedAdInFlight = errors.New("请先关闭广告并等待在途会话结束，再更改广告或认证配置")

func GetRewardedAdSettingFromDB() (operation_setting.RewardedAdSetting, error) {
	return loadRewardedAdSetting(DB)
}

// loadRewardedAdSetting reads the persisted configuration through db, which may
// be a transaction, so a decision sees the same configuration it commits with.
func loadRewardedAdSetting(db *gorm.DB) (operation_setting.RewardedAdSetting, error) {
	cfg := operation_setting.GetRewardedAdSetting()
	var option Option
	err := db.Where(&Option{Key: operation_setting.RewardedAdOptionKey}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if option.Value != "" {
		cfg.BridgeAuthMode = ""
		cfg.S2SAuthMode = ""
		cfg.S2SAuthKey = ""
		if err := common.UnmarshalJsonStr(option.Value, &cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
func SaveRewardedAdSetting(cfg operation_setting.RewardedAdSetting) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: operation_setting.RewardedAdOptionKey}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where(&Option{Key: operation_setting.RewardedAdOptionKey}).First(&option).Error; err != nil {
			return err
		}
		// Session creation coordinates with saves through today's global day row.
		if err := lockRewardedAdDay(tx, RewardedAdDate(time.Now())); err != nil {
			return err
		}
		old := operation_setting.GetRewardedAdSetting()
		if option.Value != "" {
			old.BridgeAuthMode = ""
			old.S2SAuthMode = ""
			old.S2SAuthKey = ""
			if err := common.UnmarshalJsonStr(option.Value, &old); err != nil {
				return err
			}
		}
		if cfg.BridgeAuthMode == "" {
			cfg.BridgeAuthMode = old.EffectiveBridgeAuthMode()
		}
		if cfg.S2SAuthMode == "" {
			cfg.S2SAuthMode = old.S2SAuthMode
		}
		if cfg.S2SAuthKey == "" {
			cfg.S2SAuthKey = old.S2SAuthKey
		}
		// Resolve empty secrets against the locked DB row, never a stale node cache.
		if cfg.Secret == "" {
			cfg.Secret = old.Secret
		}
		if cfg.CallbackAuthKey == "" {
			cfg.CallbackAuthKey = old.CallbackAuthKey
		}
		if cfg.WechatAppSecret == "" {
			cfg.WechatAppSecret = old.WechatAppSecret
		}
		if cfg.BridgeAuthKey == "" {
			cfg.BridgeAuthKey = old.BridgeAuthKey
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		identityChanged := cfg.IdentityFingerprint() != old.IdentityFingerprint()
		if cfg.Adpid != old.Adpid || cfg.Secret != old.Secret || cfg.EffectiveCallbackAuthKey() != old.EffectiveCallbackAuthKey() || cfg.EffectiveCallbackAuthMode() != old.EffectiveCallbackAuthMode() || identityChanged {
			var pending int64
			if err := tx.Model(&RewardedAdSession{}).Where("status = ?", "pending").Count(&pending).Error; err != nil {
				return err
			}
			if pending > 0 {
				return ErrRewardedAdInFlight
			}
		}
		if identityChanged {
			if err := tx.Model(&RewardedAdIdentity{}).Where("expires_at > ?", 0).Update("expires_at", 0).Error; err != nil {
				return err
			}
		}
		raw, err := common.Marshal(cfg)
		if err != nil {
			return err
		}
		return tx.Model(&option).Update("value", string(raw)).Error
	})
	if err == nil {
		operation_setting.SetRewardedAdSetting(cfg)
	}
	return err
}

// LockRewardedAdSetting reads the current configuration while holding the same
// row lock used by configuration updates, session creation and identity writes.
func LockRewardedAdSetting(tx *gorm.DB) (operation_setting.RewardedAdSetting, error) {
	cfg := operation_setting.GetRewardedAdSetting()
	option := Option{Key: operation_setting.RewardedAdOptionKey}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
		return cfg, err
	}
	if err := lockForUpdate(tx).Where(&Option{Key: operation_setting.RewardedAdOptionKey}).First(&option).Error; err != nil {
		return cfg, err
	}
	if option.Value != "" {
		cfg.BridgeAuthMode = ""
		cfg.S2SAuthMode = ""
		cfg.S2SAuthKey = ""
		if err := common.UnmarshalJsonStr(option.Value, &cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
