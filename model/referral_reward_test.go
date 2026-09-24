package model

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReferralHistoryMergesRewardsWithoutCheckinOrOtherUsers(t *testing.T) {
	truncateTables(t)
	owner := &User{Username: "reward-owner", Quota: 10000, AffQuota: 60, AffHistoryQuota: 100}
	other := &User{Username: "other-owner", AffCode: "OTHER", AffQuota: 999, AffHistoryQuota: 999}
	require.NoError(t, DB.Create(owner).Error)
	require.NoError(t, DB.Create(other).Error)
	require.NoError(t, DB.Create(&CommissionRecord{TopUpId: 11, InviterId: owner.Id, CommissionQuota: 50, CreatedTime: 20}).Error)
	require.NoError(t, DB.Create(&ReferralRewardRecord{UserID: owner.Id, SourceType: ReferralRewardRegistration, SourceID: "invitee-1", Quota: 20, CreatedAt: 10}).Error)
	require.NoError(t, DB.Create(&ReferralRewardRecord{UserID: other.Id, SourceType: ReferralRewardRegistration, SourceID: "invitee-2", Quota: 999, CreatedAt: 30}).Error)
	// Checkin credits spendable quota only; it must not affect referral history.
	require.NoError(t, DB.Model(owner).UpdateColumn("quota", gorm.Expr("quota + ?", 5000)).Error)
	first, err := GetReferralRewardHistory(owner.Id, 1, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, first.Total)
	assert.EqualValues(t, 60, first.Available)
	assert.EqualValues(t, 100, first.Lifetime)
	assert.EqualValues(t, 30, first.Unitemized)
	require.Len(t, first.Rows, 1)
	assert.Equal(t, "topup", first.Rows[0].SourceType)
	second, err := GetReferralRewardHistory(owner.Id, 2, 1)
	require.NoError(t, err)
	require.Len(t, second.Rows, 1)
	assert.Equal(t, ReferralRewardRegistration, second.Rows[0].SourceType)
	empty, err := GetReferralRewardHistory(owner.Id, 3, 1)
	require.NoError(t, err)
	assert.Empty(t, empty.Rows)
	// The old recharge-only endpoint retains its original contract.
	old, total, err := GetInviterCommissionRecords(owner.Id, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, old, 1)
	assert.Equal(t, 50, old[0].CommissionQuota)
}

func TestReferralHistoryKeepsSnapshotWhileRewardCommits(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "referrals.db")
	reader, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	writer, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	for _, db := range []*gorm.DB{reader, writer} {
		connection, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })
	}
	require.NoError(t, reader.Exec("PRAGMA journal_mode=WAL").Error)
	require.NoError(t, reader.AutoMigrate(&User{}, &CommissionRecord{}, &ReferralRewardRecord{}))
	previousDB := DB
	DB = reader
	t.Cleanup(func() { DB = previousDB })
	owner := &User{Username: "snapshot-owner", AffQuota: 10, AffHistoryQuota: 10}
	require.NoError(t, reader.Create(owner).Error)
	require.NoError(t, reader.Create(&ReferralRewardRecord{UserID: owner.Id, SourceType: ReferralRewardRegistration, SourceID: "first", Quota: 10}).Error)
	committed := false
	require.NoError(t, reader.Callback().Query().After("gorm:query").Register("test:commit_referral_reward", func(tx *gorm.DB) {
		if tx.Statement.Table != "users" || committed {
			return
		}
		committed = true
		tx.AddError(writer.Transaction(func(w *gorm.DB) error {
			if err := w.Model(&User{}).Where("id = ?", owner.Id).Updates(map[string]interface{}{"aff_quota": 35, "aff_history": 35}).Error; err != nil {
				return err
			}
			return w.Create(&ReferralRewardRecord{UserID: owner.Id, SourceType: ReferralRewardRegistration, SourceID: "second", Quota: 25}).Error
		}))
	}))

	history, err := GetReferralRewardHistory(owner.Id, 1, 20)
	require.NoError(t, err)
	require.True(t, committed)
	assert.EqualValues(t, 10, history.Available)
	assert.EqualValues(t, 10, history.Lifetime)
	assert.EqualValues(t, 1, history.Total)
	require.Len(t, history.Rows, 1)
	assert.EqualValues(t, 10, history.Rows[0].Quota)
	assert.Zero(t, history.Unitemized)

	history, err = GetReferralRewardHistory(owner.Id, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 35, history.Available)
	assert.EqualValues(t, 35, history.Lifetime)
	assert.EqualValues(t, 2, history.Total)
	assert.Len(t, history.Rows, 2)
	assert.Zero(t, history.Unitemized)
}

func TestRegistrationRewardLedgerAtomicAndNotRepeatedAfterFinalize(t *testing.T) {
	truncateTables(t)
	oldReward, oldInvitee, oldNew := common.QuotaForInviter, common.QuotaForInvitee, common.QuotaForNewUser
	oldPayment := *operation_setting.GetPaymentSetting()
	t.Cleanup(func() {
		common.QuotaForInviter, common.QuotaForInvitee, common.QuotaForNewUser = oldReward, oldInvitee, oldNew
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	common.QuotaForInviter, common.QuotaForInvitee, common.QuotaForNewUser = 25, 0, 0
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	owner := &User{Username: "atomic-owner"}
	require.NoError(t, DB.Create(owner).Error)
	rollback := errors.New("oauth binding failed")
	err := DB.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, (&User{Username: "rollback-ledger"}).InsertWithTx(tx, owner.Id))
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	history, err := GetReferralRewardHistory(owner.Id, 1, 20)
	require.NoError(t, err)
	assert.Zero(t, history.Lifetime)
	assert.Empty(t, history.Rows)
	user := &User{Username: "committed-ledger"}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return user.InsertWithTx(tx, owner.Id) }))
	user.FinalizeOAuthUserCreation(owner.Id)
	user.FinalizeOAuthUserCreation(owner.Id)
	history, err = GetReferralRewardHistory(owner.Id, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 25, history.Lifetime)
	assert.Zero(t, history.Unitemized)
	require.Len(t, history.Rows, 1)
	// A duplicate source cannot credit the wallet again.
	require.Error(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := creditRegistrationReward(tx, owner.Id, user.Id, 25)
		return err
	}))
	// A full referral wallet must not block a new registration.
	require.NoError(t, DB.Model(owner).UpdateColumn("aff_history", common.MaxWalletQuota).Error)
	require.NoError(t, (&User{Username: "overflow-ledger"}).Insert(owner.Id))
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "overflow-ledger").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, DB.Model(&ReferralRewardRecord{}).Where("user_id = ?", owner.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestReferralHistoryPreservesWalletAndRowsWhenLedgerExceedsLifetime(t *testing.T) {
	truncateTables(t)
	owner := &User{Username: "inconsistent-owner", AffQuota: 10, AffHistoryQuota: 20}
	require.NoError(t, DB.Create(owner).Error)
	require.NoError(t, DB.Create(&ReferralRewardRecord{UserID: owner.Id, SourceType: ReferralRewardRegistration, SourceID: "legacy", Quota: 25}).Error)

	history, err := GetReferralRewardHistory(owner.Id, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 10, history.Available)
	assert.EqualValues(t, 20, history.Lifetime)
	assert.Zero(t, history.Unitemized)
	assert.EqualValues(t, 1, history.Total)
	require.Len(t, history.Rows, 1)
	assert.EqualValues(t, 25, history.Rows[0].Quota)
}

func TestRegistrationSkipsIneligibleInviterReward(t *testing.T) {
	oldReward, oldNew := common.QuotaForInviter, common.QuotaForNewUser
	oldPayment := *operation_setting.GetPaymentSetting()
	t.Cleanup(func() {
		common.QuotaForInviter, common.QuotaForNewUser = oldReward, oldNew
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	common.QuotaForInviter, common.QuotaForNewUser = 25, 0
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	for _, tc := range []struct {
		name             string
		quota, history   int
		status           int
		deleted, missing bool
	}{
		{name: "deleted", deleted: true},
		{name: "missing", missing: true},
		{name: "disabled", status: common.UserStatusDisabled},
		{name: "balance limit", quota: int(common.MaxWalletQuota) - 24},
		{name: "lifetime limit", history: int(common.MaxWalletQuota) - 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			owner := &User{Username: "skip-owner", AffQuota: tc.quota, AffHistoryQuota: tc.history, Status: tc.status}
			require.NoError(t, DB.Create(owner).Error)
			if tc.deleted {
				require.NoError(t, DB.Delete(owner).Error)
			}
			if tc.missing {
				require.NoError(t, DB.Unscoped().Delete(owner).Error)
				owner.Id += 1000 // Do not let SQLite reuse the deleted ID for the invitee.
			}
			invitee := &User{Username: "new-invitee"}
			require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return invitee.InsertWithTx(tx, owner.Id) }))
			var saved User
			require.NoError(t, DB.First(&saved, invitee.Id).Error)
			assert.Equal(t, owner.Id, saved.InviterId)
			var count int64
			require.NoError(t, DB.Model(&ReferralRewardRecord{}).Count(&count).Error)
			assert.Zero(t, count)
			if !tc.missing {
				saved = User{}
				require.NoError(t, DB.Unscoped().First(&saved, owner.Id).Error)
				assert.Equal(t, tc.quota, saved.AffQuota)
				assert.Equal(t, tc.history, saved.AffHistoryQuota)
			}
			invitee.FinalizeOAuthUserCreation(owner.Id)
			require.NoError(t, LOG_DB.Model(&Log{}).Where("user_id = ? AND content LIKE ?", owner.Id, "邀请用户赠送%").Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestRegistrationRewardDatabaseFailureRollsBackAccountAndWallet(t *testing.T) {
	truncateTables(t)
	oldReward := common.QuotaForInviter
	oldPayment := *operation_setting.GetPaymentSetting()
	t.Cleanup(func() {
		common.QuotaForInviter = oldReward
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	common.QuotaForInviter = 25
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	owner := &User{Username: "db-error-owner", AffQuota: 10, AffHistoryQuota: 15}
	require.NoError(t, DB.Create(owner).Error)
	ledgerError := errors.New("referral ledger database failure")
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("test:reject_referral_ledger", func(tx *gorm.DB) {
		if tx.Statement.Table == "referral_reward_records" {
			tx.AddError(ledgerError)
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Create().Remove("test:reject_referral_ledger")) })
	invitee := &User{Username: "db-error-invitee"}
	err := DB.Transaction(func(tx *gorm.DB) error { return invitee.InsertWithTx(tx, owner.Id) })
	require.ErrorIs(t, err, ledgerError)
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", invitee.Username).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.First(owner, owner.Id).Error)
	assert.Equal(t, 10, owner.AffQuota)
	assert.Equal(t, 15, owner.AffHistoryQuota)
	assert.Zero(t, owner.AffCount)
	require.NoError(t, DB.Model(&ReferralRewardRecord{}).Count(&count).Error)
	assert.Zero(t, count)
}
