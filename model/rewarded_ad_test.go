package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rewardedAdDB(t *testing.T) (operation_setting.RewardedAdSetting, time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ad.db")), &gorm.Config{})
	require.NoError(t, err)
	prev := DB
	DB = db
	t.Cleanup(func() { DB = prev })
	require.NoError(t, db.AutoMigrate(&Option{}, &User{}, &RewardedAdIdentity{}, &RewardedAdSession{}, &RewardedAdDay{}, &RewardedAdEvent{}, &RewardLogOutbox{}))
	require.NoError(t, db.Create(&User{Id: 1, Username: "viewer", Status: common.UserStatusEnabled, Quota: 100, AffCode: "viewer"}).Error)
	now := time.Unix(1791777600, 0)
	cfg := operation_setting.RewardedAdSetting{Enabled: true, RewardQuota: 50, DailyLimit: 3, CooldownSeconds: 60, DailyBudgetQuota: 100, Adpid: "ad-1", WechatAppID: "wx-app"}
	require.NoError(t, db.Create(&RewardedAdIdentity{UserID: 1, OpenID: "wx-viewer", AppID: "wx-app", ExpiresAt: now.Unix() + 3600, ConfigFingerprint: cfg.IdentityFingerprint()}).Error)
	return cfg, now
}

func TestRewardedAdCreditsViewerOnlyOnce(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	s, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	again, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	assert.Equal(t, s.ID, again.ID)
	event := RewardedAdCallback{TransID: "transaction-1", UserID: "1", Extra: s.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.NoError(t, SettleRewardedAd(event, now.Add(time.Minute)))
	require.NoError(t, SettleRewardedAd(event, now.Add(2*time.Minute)))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 150, user.Quota)
	assert.Zero(t, user.AffQuota)
	assert.Zero(t, user.UsedQuota)
	var count int64
	require.NoError(t, DB.Model(&RewardedAdEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, DB.Model(&RewardLogOutbox{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestRewardedAdRejectsRecipientChangeAndWalletOverflow(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	s, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	event := RewardedAdCallback{TransID: "transaction-1", UserID: "2", Extra: s.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.Error(t, SettleRewardedAd(event, now))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("quota", common.MaxWalletQuota).Error)
	event.UserID = "1"
	require.Error(t, SettleRewardedAd(event, now))
	var count int64
	require.NoError(t, DB.Model(&RewardedAdEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRewardedAdReservesBudgetAndReleasesExpiredOnce(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	cfg.DailyBudgetQuota = 50
	s, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	_, err = CreateRewardedAdSession(1, "request-2", cfg, now)
	require.ErrorContains(t, err, "session_pending")
	require.NoError(t, ExpireRewardedAdSessions(now.Add(16*time.Minute)))
	require.NoError(t, ExpireRewardedAdSessions(now.Add(17*time.Minute)))
	_, err = CreateRewardedAdSession(1, "request-2", cfg, now.Add(17*time.Minute))
	require.NoError(t, err)
	event := RewardedAdCallback{TransID: "late", UserID: "1", Extra: s.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.ErrorContains(t, SettleRewardedAd(event, now.Add(18*time.Minute)), "review_required")
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 100, user.Quota)
}

// Run against dedicated disposable databases supplied explicitly by the test runner.
func TestRewardedAdDatabaseContract(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		open      func(string) gorm.Dialector
		kind      common.DatabaseType
	}{
		{"mysql", "TEST_AD_MYSQL_DSN", func(dsn string) gorm.Dialector { return mysql.Open(dsn) }, common.DatabaseTypeMySQL},
		{"postgres", "TEST_AD_POSTGRES_DSN", func(dsn string) gorm.Dialector { return postgres.Open(dsn) }, common.DatabaseTypePostgreSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip("dedicated ad database not configured")
			}
			db, err := gorm.Open(tc.open(dsn), &gorm.Config{})
			require.NoError(t, err)
			prior := DB
			mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
			DB = db
			common.SetDatabaseTypes(tc.kind, logType)
			t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close(); DB = prior; common.SetDatabaseTypes(mainType, logType) })
			require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &RewardedAdIdentity{}, &RewardedAdSession{}, &RewardedAdDay{}, &RewardedAdEvent{}, &RewardLogOutbox{}, &RewardLogDelivery{}, &Log{}))
			for _, table := range []any{&RewardLogDelivery{}, &Log{}, &RewardLogOutbox{}, &RewardedAdEvent{}, &RewardedAdSession{}, &RewardedAdDay{}, &RewardedAdIdentity{}, &Option{}, &User{}} {
				require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
			}
			now := time.Unix(1791777600, 0)
			cfg := operation_setting.RewardedAdSetting{Enabled: true, RewardQuota: 50, DailyLimit: 3, DailyBudgetQuota: 50, Adpid: "ad-1", WechatAppID: "wx-app"}
			for _, id := range []int{1, 2} {
				require.NoError(t, db.Create(&User{Id: id, Username: fmt.Sprintf("viewer-%d", id), Status: common.UserStatusEnabled, Quota: 100, AffCode: fmt.Sprintf("aff-%d", id)}).Error)
				require.NoError(t, db.Create(&RewardedAdIdentity{UserID: id, OpenID: fmt.Sprintf("openid-%d", id), AppID: "wx-app", ExpiresAt: now.Unix() + 3600, ConfigFingerprint: cfg.IdentityFingerprint()}).Error)
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			sessions := make(chan *RewardedAdSession, 2)
			for _, id := range []int{1, 2} {
				go func(userID int) {
					<-gate
					s, err := CreateRewardedAdSession(userID, "request", cfg, now)
					sessions <- s
					results <- err
				}(id)
			}
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else {
					assert.EqualError(t, err, "budget_exhausted")
				}
			}
			assert.Equal(t, 1, success)
			var winner *RewardedAdSession
			for range 2 {
				s := <-sessions
				if s != nil && s.ID != "" {
					winner = s
				}
			}
			require.NotNil(t, winner)
			event := RewardedAdCallback{TransID: "db-transaction", UserID: strconv.Itoa(winner.UserID), Extra: winner.ID, Adpid: "ad-1", Platform: "weixin-mp", Provider: "china"}
			require.NoError(t, SettleRewardedAd(event, now.Add(time.Minute)))
			require.NoError(t, SettleRewardedAd(event, now.Add(2*time.Minute)))
			var user User
			require.NoError(t, db.First(&user, winner.UserID).Error)
			assert.Equal(t, 150, user.Quota)
			// Repeated AutoMigrate must preserve identity, transaction and reward.
			require.NoError(t, db.AutoMigrate(&RewardedAdIdentity{}, &RewardedAdSession{}, &RewardedAdDay{}, &RewardedAdEvent{}, &RewardLogOutbox{}))
			var events int64
			require.NoError(t, db.Model(&RewardedAdEvent{}).Count(&events).Error)
			assert.EqualValues(t, 1, events)
			// Exercise log delivery and retention on the same real database dialect.
			previousLogDB := LOG_DB
			LOG_DB = db
			common.SetLogDatabaseType(tc.kind)
			t.Cleanup(func() { LOG_DB = previousLogDB })
			require.NoError(t, db.AutoMigrate(&Log{}, &RewardLogDelivery{}))
			require.NoError(t, DeliverRewardLogs())
			key := "ad:" + winner.ID
			require.NoError(t, db.Model(&RewardLogOutbox{}).Where("event_key = ?", key).Update("delivered_at", 0).Error)
			currentTime := common.GetTimestamp()
			require.NoError(t, db.Model(&RewardLogDelivery{}).Where("event_key = ?", key).Update("created_at", currentTime-8*24*3600).Error)
			require.NoError(t, PruneDeliveredRewardLogs(currentTime))
			require.NoError(t, DeliverRewardLogs())
			logs, total, err := GetUserLogs(winner.UserID, LogTypeRewardedAd, 0, 0, "", "", 0, 20, "", "", "")
			require.NoError(t, err)
			require.Len(t, logs, 1)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, 50, logs[0].Quota)
		})
	}
}

func TestRewardedAdSettingsKeepDatabaseSecretsAcrossStaleNodes(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	cfg.Enabled = false
	cfg.Secret = "new-secret"
	cfg.CallbackAuthKey = "new-callback-key"
	cfg.WechatAppSecret = "new-wechat-secret"
	cfg.BridgeAuthKey = "new-bridge-key"
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: operation_setting.RewardedAdOptionKey, Value: string(raw)}).Error)
	before := operation_setting.GetRewardedAdSetting()
	t.Cleanup(func() { operation_setting.SetRewardedAdSetting(before) })
	stale := cfg
	stale.Secret = "old-secret"
	stale.CallbackAuthKey = "old-key"
	operation_setting.SetRewardedAdSetting(stale)
	update := cfg
	update.Secret = ""
	update.CallbackAuthKey = ""
	update.WechatAppSecret = ""
	update.BridgeAuthKey = ""
	update.DailyLimit = 4
	require.NoError(t, SaveRewardedAdSetting(update))
	saved, err := GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "new-secret", saved.Secret)
	assert.Equal(t, "new-callback-key", saved.CallbackAuthKey)
	assert.Equal(t, 4, saved.DailyLimit)
	// A changed bridge invalidates existing credentials in the same transaction.
	update = saved
	update.BridgeURL = "https://bridge.example.com"
	require.NoError(t, SaveRewardedAdSetting(update))
	var identity RewardedAdIdentity
	require.NoError(t, DB.First(&identity, 1).Error)
	assert.Less(t, identity.ExpiresAt, now.Unix())
}

func TestRewardedAdCreditRollsBackWhenLedgerCannotPersist(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	session, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	require.NoError(t, DB.Migrator().DropTable(&RewardLogOutbox{}))
	event := RewardedAdCallback{TransID: "transaction-1", UserID: "1", Extra: session.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.Error(t, SettleRewardedAd(event, now))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 100, user.Quota)
	require.NoError(t, DB.First(session, "id = ?", session.ID).Error)
	assert.Equal(t, "pending", session.Status)
	var count int64
	require.NoError(t, DB.Model(&RewardedAdEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRewardedAdBridgeAuthModePreservesLegacyAndExpiresChangedIdentity(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	cfg.Enabled = false
	before := operation_setting.GetRewardedAdSetting()
	t.Cleanup(func() { operation_setting.SetRewardedAdSetting(before) })
	operation_setting.SetRewardedAdSetting(operation_setting.RewardedAdSetting{BridgeAuthMode: "sign"})
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: operation_setting.RewardedAdOptionKey, Value: string(raw)}).Error)
	old, err := GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "connectCode", old.EffectiveBridgeAuthMode())
	cfg.BridgeAuthMode = "sign"
	require.NoError(t, SaveRewardedAdSetting(cfg))
	stored, err := GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "sign", stored.EffectiveBridgeAuthMode())
	var identity RewardedAdIdentity
	require.NoError(t, DB.First(&identity, 1).Error)
	assert.Less(t, identity.ExpiresAt, now.Unix())
	// A client predating the mode field must retain the stored sign selection.
	stored.BridgeAuthMode = ""
	require.NoError(t, SaveRewardedAdSetting(stored))
	stored, err = GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "sign", stored.EffectiveBridgeAuthMode())
	cfg.BridgeAuthMode = "unknown"
	require.Error(t, cfg.Validate())
}

func TestRewardedAdSharedS2SPreservesSavedCredential(t *testing.T) {
	cfg, _ := rewardedAdDB(t)
	cfg.Enabled = false
	cfg.Secret = "event-secret"
	cfg.CallbackAuthKey = "legacy-callback-key"
	cfg.BridgeAuthKey = "legacy-bridge-key"
	require.NoError(t, SaveRewardedAdSetting(cfg))
	cfg.S2SAuthMode = "sign"
	cfg.S2SAuthKey = "0123456789abcdef0123456789abcdef"
	require.NoError(t, SaveRewardedAdSetting(cfg))
	saved, err := GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, cfg.S2SAuthKey, saved.EffectiveBridgeAuthKey())
	assert.Equal(t, cfg.S2SAuthKey, saved.EffectiveCallbackAuthKey())
	cfg.S2SAuthKey = ""
	require.NoError(t, SaveRewardedAdSetting(cfg))
	saved, err = GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", saved.S2SAuthKey)
	cfg.S2SAuthMode = ""
	require.NoError(t, SaveRewardedAdSetting(cfg))
	saved, err = GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	assert.Equal(t, "sign", saved.S2SAuthMode)
}

func TestRewardedAdLimitsFollowWeChatIdentityAcrossAccounts(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	cfg.DailyLimit = 1
	require.NoError(t, DB.Create(&User{Id: 2, Username: "second", Status: common.UserStatusEnabled, Quota: 100, AffCode: "second"}).Error)
	duplicate := RewardedAdIdentity{UserID: 2, OpenID: "wx-viewer", AppID: "wx-app", ExpiresAt: now.Unix() + 3600, ConfigFingerprint: cfg.IdentityFingerprint()}
	require.Error(t, DB.Create(&duplicate).Error, "one WeChat identity must not bind two accounts")

	first, err := CreateRewardedAdSession(1, "request-1", cfg, now)
	require.NoError(t, err)
	// Moving the WeChat identity to another account keeps the pending session and daily count.
	require.NoError(t, DB.Where("user_id = ?", 1).Delete(&RewardedAdIdentity{}).Error)
	require.NoError(t, DB.Create(&duplicate).Error)
	_, err = CreateRewardedAdSession(2, "request-2", cfg, now)
	require.EqualError(t, err, "session_pending")

	event := RewardedAdCallback{TransID: "transaction-1", UserID: "1", Extra: first.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.NoError(t, SettleRewardedAd(event, now.Add(time.Minute)))
	_, err = CreateRewardedAdSession(2, "request-3", cfg, now.Add(2*time.Minute))
	require.EqualError(t, err, "daily_limit")

	var second User
	require.NoError(t, DB.First(&second, 2).Error)
	assert.Equal(t, 100, second.Quota)
}

func TestRewardedAdSessionSnapshotsSavedConfigurationOverStaleNode(t *testing.T) {
	stale, now := rewardedAdDB(t)
	saved := stale
	saved.RewardQuota = 80
	saved.Secret, saved.CallbackAuthKey = "secret", strings.Repeat("k", 32)
	saved.WechatAppSecret, saved.DCloudAppID = "wx-secret", "__UNI__app"
	saved.BridgeURL, saved.BridgeAuthKey = "https://bridge.example/uni-open-bridge", "bridge-key"
	previous := operation_setting.GetRewardedAdSetting()
	t.Cleanup(func() { operation_setting.SetRewardedAdSetting(previous) })
	require.NoError(t, SaveRewardedAdSetting(saved))
	persisted, err := GetRewardedAdSettingFromDB()
	require.NoError(t, err)
	// Saving new WeChat credentials expires identities; simulate re-initialization.
	require.NoError(t, DB.Model(&RewardedAdIdentity{}).Where("user_id = ?", 1).Updates(map[string]any{"config_fingerprint": persisted.IdentityFingerprint(), "expires_at": now.Unix() + 3600}).Error)

	session, err := CreateRewardedAdSession(1, "request-1", stale, now)
	require.NoError(t, err)
	assert.EqualValues(t, 80, session.RewardQuota)
}

func TestRewardedAdLateCallbackCreditsOnlyWithinWindowAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name         string
		settleAfter  time.Duration
		budgetAtLate int64
		wantErr      string
		wantQuota    int
		wantStatus   string
		wantReason   string
	}{
		{name: "credited within window", settleAfter: 20 * time.Minute, budgetAtLate: 100, wantQuota: 150, wantStatus: "credited"},
		{name: "budget no longer allows", settleAfter: 20 * time.Minute, budgetAtLate: 10, wantErr: "review_required", wantQuota: 100, wantStatus: "review_required", wantReason: "limit_exceeded"},
		{name: "beyond late window", settleAfter: 15*time.Minute + 24*time.Hour + time.Second, budgetAtLate: 100, wantErr: "review_required", wantQuota: 100, wantStatus: "review_required", wantReason: "session_expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, now := rewardedAdDB(t)
			previous := operation_setting.GetRewardedAdSetting()
			t.Cleanup(func() { operation_setting.SetRewardedAdSetting(previous) })
			session, err := CreateRewardedAdSession(1, "request-1", cfg, now)
			require.NoError(t, err)
			require.NoError(t, ExpireRewardedAdSessions(now.Add(16*time.Minute)))
			late := cfg
			late.DailyBudgetQuota = tc.budgetAtLate
			operation_setting.SetRewardedAdSetting(late)

			event := RewardedAdCallback{TransID: "late-transaction", UserID: "1", Extra: session.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
			err = SettleRewardedAd(event, now.Add(tc.settleAfter))
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.wantErr)
			}
			// A redelivered callback never credits twice.
			_ = SettleRewardedAd(event, now.Add(tc.settleAfter+time.Minute))

			var user User
			require.NoError(t, DB.First(&user, 1).Error)
			assert.Equal(t, tc.wantQuota, user.Quota)
			var stored RewardedAdSession
			require.NoError(t, DB.First(&stored, "id = ?", session.ID).Error)
			assert.Equal(t, tc.wantStatus, stored.Status)
			assert.Equal(t, tc.wantReason, stored.Reason)
			var global RewardedAdDay
			require.NoError(t, DB.First(&global, "day = ? AND user_id = 0", session.Day).Error)
			assert.Zero(t, global.Reserved)
			if tc.wantStatus == "credited" {
				assert.Equal(t, 1, global.Credited)
				assert.EqualValues(t, 50, global.CreditedQuota)
			} else {
				assert.Zero(t, global.Credited)
			}
		})
	}
}
