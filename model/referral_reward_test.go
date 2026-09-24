package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	require.Error(t, DB.Transaction(func(tx *gorm.DB) error { return creditRegistrationReward(tx, owner.Id, user.Id, 25) }))
	// Overflow must roll back both the new account and its ledger.
	require.NoError(t, DB.Model(owner).UpdateColumn("aff_history", common.MaxWalletQuota).Error)
	require.Error(t, (&User{Username: "overflow-ledger"}).Insert(owner.Id))
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "overflow-ledger").Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&ReferralRewardRecord{}).Where("user_id = ?", owner.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
