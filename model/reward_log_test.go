package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func setupRewardLogDBs(t *testing.T) {
	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogDB; common.SetDatabaseTypes(oldMainType, oldLogType) })
	var err error
	DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	LOG_DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, DB.AutoMigrate(&RewardLogOutbox{}))
	require.NoError(t, LOG_DB.AutoMigrate(&RewardLogDelivery{}, &Log{}))
}

func TestRewardLogOutboxRollsBackWithCreditTransaction(t *testing.T) {
	setupRewardLogDBs(t)
	err := DB.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, EnqueueRewardLog(tx, &Log{Type: LogTypeRewardedAd, UserId: 1, Quota: 100}, "ad:1"))
		return errors.New("credit rolled back")
	})
	require.Error(t, err)
	require.NoError(t, DeliverRewardLogs())
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRewardLogRetryAfterLostAcknowledgementDoesNotDuplicate(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, EnqueueRewardLog(DB, &Log{Type: LogTypeRewardedAd, UserId: 1, Quota: 100}, "ad:1"))
	require.NoError(t, DeliverRewardLogs())
	require.NoError(t, DB.Model(&RewardLogOutbox{}).Where("event_key = ?", "ad:1").Update("delivered_at", 0).Error)
	require.NoError(t, DeliverRewardLogs())
	var logs []Log
	require.NoError(t, LOG_DB.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 100, logs[0].Quota)
	assert.Equal(t, "ad:1", logs[0].RequestId)
	var receiptCount int64
	require.NoError(t, LOG_DB.Model(&RewardLogDelivery{}).Where("event_key = ?", "ad:1").Count(&receiptCount).Error)
	assert.EqualValues(t, 1, receiptCount)
	var event RewardLogOutbox
	require.NoError(t, DB.Where("event_key = ?", "ad:1").First(&event).Error)
	assert.Positive(t, event.DeliveredAt)
}

func TestRewardLogFailedDestinationKeepsPendingEvent(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, EnqueueRewardLog(DB, &Log{Type: LogTypeCheckin, UserId: 1, Quota: 100}, "checkin:1"))
	require.NoError(t, LOG_DB.Migrator().DropTable(&Log{}))
	require.Error(t, DeliverRewardLogs())
	var event RewardLogOutbox
	require.NoError(t, DB.First(&event).Error)
	assert.Zero(t, event.DeliveredAt)
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	require.NoError(t, DeliverRewardLogs())
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestCheckinFilterIncludesLegacyRewardsAndRespectsUser(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, LOG_DB.Create(&[]Log{
		{UserId: 1, Type: LogTypeCheckin, Quota: 100},
		{UserId: 1, Type: LogTypeSystem, Content: "用户签到，获得额度 0.1"},
		{UserId: 1, Type: LogTypeSystem, Content: "unrelated system event"},
		{UserId: 2, Type: LogTypeCheckin, Quota: 200},
	}).Error)
	logs, total, err := GetUserLogs(1, LogTypeCheckin, 0, 0, "", "", 0, 20, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	for _, entry := range logs {
		assert.Equal(t, 1, entry.UserId)
	}
}

func TestCheckinCreditAndRewardEventCommitTogether(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Checkin{}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "reward-user", Quota: 200}).Error)
	checkin := &Checkin{UserId: 1, CheckinDate: "2026-10-12", QuotaAwarded: 100, CreatedAt: 1700000000}
	_, err := userCheckinWithTransaction(checkin, 1, 100)
	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 300, user.Quota)
	var event RewardLogOutbox
	require.NoError(t, DB.First(&event).Error)
	var entry Log
	require.NoError(t, common.UnmarshalJsonStr(event.Payload, &entry))
	assert.Equal(t, LogTypeCheckin, entry.Type)
	assert.Equal(t, 100, entry.Quota)
	require.NoError(t, DeliverRewardLogs())
}

func TestCheckinCreditsUserWithNegativeBalance(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Checkin{}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "overdrawn-user", Quota: -50}).Error)
	_, err := userCheckinWithTransaction(&Checkin{UserId: 1, CheckinDate: "2026-10-12", QuotaAwarded: 100}, 1, 100)
	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 50, user.Quota)
}

func TestCheckinOutboxFailureRollsBackBalanceAndRecord(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Checkin{}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "reward-user", Quota: 200}).Error)
	require.NoError(t, DB.Migrator().DropTable(&RewardLogOutbox{}))
	_, err := userCheckinWithTransaction(&Checkin{UserId: 1, CheckinDate: "2026-10-12", QuotaAwarded: 100}, 1, 100)
	require.Error(t, err)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 200, user.Quota)
	var count int64
	require.NoError(t, DB.Model(&Checkin{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRewardLogUserViewKeepsCreditDetailsAndStripsAdAudit(t *testing.T) {
	logs := []*Log{{Type: LogTypeRewardedAd, Quota: 100, Other: common.MapToJsonStr(map[string]interface{}{
		"reward_record_id": 12, "credited_at": 1700000000,
		"admin_info": map[string]interface{}{"trans_id": "private-transaction", "adpid": "private-placement"},
	})}}
	formatUserLogs(logs, 0)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "admin_info")
	assert.EqualValues(t, 12, other["reward_record_id"])
	assert.Equal(t, 100, logs[0].Quota)
}

func TestCheckinRejectsCreditAboveWalletLimitWithoutCreatingReward(t *testing.T) {
	setupRewardLogDBs(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Checkin{}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "reward-user", Quota: int(common.MaxWalletQuota) - 50}).Error)
	_, err := userCheckinWithTransaction(&Checkin{UserId: 1, CheckinDate: "2026-10-12", QuotaAwarded: 100}, 1, 100)
	require.Error(t, err)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, int(common.MaxWalletQuota)-50, user.Quota)
	var count int64
	require.NoError(t, DB.Model(&Checkin{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&RewardLogOutbox{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRewardLogSharedDatabaseStartupDeliversPendingCredit(t *testing.T) {
	setupRewardLogDBs(t)
	t.Setenv("LOG_SQL_DSN", "")
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	// Existing shared databases already have logs, but not the new receipt table.
	require.NoError(t, DB.AutoMigrate(&Log{}))
	require.NoError(t, LOG_DB.Migrator().DropTable(&RewardLogDelivery{}))
	require.NoError(t, EnqueueRewardLog(DB, &Log{Type: LogTypeRewardedAd, UserId: 1, Quota: 100}, "ad:shared-startup"))
	require.NoError(t, InitLogDB())
	require.NoError(t, DeliverRewardLogs())
	logs, total, err := GetUserLogs(1, LogTypeRewardedAd, 0, 0, "", "", 0, 20, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, 100, logs[0].Quota)
	require.NoError(t, DeliverRewardLogs())
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestPruneDeliveredRewardLogsKeepsPendingAndRecentEntries(t *testing.T) {
	setupRewardLogDBs(t)
	now := int64(1800000000)
	old := now - rewardLogRetentionSeconds - 1
	require.NoError(t, DB.Create(&[]RewardLogOutbox{
		{EventKey: "delivered-old", Payload: "{}", DeliveredAt: old},
		{EventKey: "delivered-recent", Payload: "{}", DeliveredAt: now - 60},
		{EventKey: "pending-old", Payload: "{}", CreatedAt: old},
	}).Error)
	require.NoError(t, LOG_DB.Create(&[]RewardLogDelivery{
		{EventKey: "delivered-old", CreatedAt: old},
		{EventKey: "delivered-recent", CreatedAt: now - 60},
	}).Error)

	require.NoError(t, PruneDeliveredRewardLogs(now))

	var outbox []string
	require.NoError(t, DB.Model(&RewardLogOutbox{}).Order("event_key").Pluck("event_key", &outbox).Error)
	assert.Equal(t, []string{"delivered-recent", "pending-old"}, outbox)
	var receipts []string
	require.NoError(t, LOG_DB.Model(&RewardLogDelivery{}).Pluck("event_key", &receipts).Error)
	assert.Equal(t, []string{"delivered-recent"}, receipts)
}
