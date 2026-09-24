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

func TestRegistrationCountsInvitesIndependentlyOfRewards(t *testing.T) {
	for _, flow := range []string{"password", "phone", "oauth"} {
		for _, scenario := range []struct {
			name       string
			compliant  bool
			reward     int
			wantReward int
		}{
			{"no_reward", true, 0, 0},
			{"reward_enabled", true, 25, 25},
			{"payments_disabled", false, 25, 0},
		} {
			t.Run(flow+"/"+scenario.name, func(t *testing.T) {
				truncateTables(t)
				oldNew, oldInvitee, oldInviter := common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter
				oldPayment := *operation_setting.GetPaymentSetting()
				t.Cleanup(func() {
					common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter = oldNew, oldInvitee, oldInviter
					*operation_setting.GetPaymentSetting() = oldPayment
				})
				common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter = 0, 0, scenario.reward
				operation_setting.GetPaymentSetting().ComplianceConfirmed = scenario.compliant
				operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
				inviter := &User{Username: "referrer", AffCode: "PG3A"}
				require.NoError(t, DB.Create(inviter).Error)
				var invitee *User
				if flow == "phone" {
					var err error
					var created bool
					invitee, created, err = RegisterUserByPhone("13800138888", inviter.Id)
					require.NoError(t, err)
					require.True(t, created)
					// A returning number cannot be reassigned or counted twice.
					existing, created, err := RegisterUserByPhone(invitee.Phone, 0)
					require.NoError(t, err)
					assert.False(t, created)
					assert.Equal(t, invitee.Id, existing.Id)
					assert.Equal(t, inviter.Id, existing.InviterId)
				} else if flow == "oauth" {
					invitee = &User{Username: "oauth-invitee"}
					require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
						return invitee.InsertWithTx(tx, inviter.Id)
					}))
					invitee.FinalizeOAuthUserCreation(inviter.Id)
				} else {
					invitee = &User{Username: "invitee", InviterId: inviter.Id}
					require.NoError(t, invitee.Insert(inviter.Id))
				}
				var stored User
				require.NoError(t, DB.First(&stored, invitee.Id).Error)
				assert.Equal(t, inviter.Id, stored.InviterId)
				require.NoError(t, DB.First(inviter, inviter.Id).Error)
				assert.Equal(t, 1, inviter.AffCount)
				assert.Equal(t, scenario.wantReward, inviter.AffQuota)
				assert.Equal(t, scenario.wantReward, inviter.AffHistoryQuota)
				// Registration rewards must have a structured ledger entry.
				if scenario.wantReward > 0 {
					var ledger struct {
						Quota         int64
						RelatedUserID int
					}
					require.NoError(t, DB.Table("referral_reward_records").Where("user_id = ?", inviter.Id).First(&ledger).Error)
					assert.EqualValues(t, scenario.wantReward, ledger.Quota)
					assert.Equal(t, invitee.Id, ledger.RelatedUserID)
				}

			})
		}
	}
}

func TestRolledBackRegistrationDoesNotCountInvite(t *testing.T) {
	truncateTables(t)
	inviter := &User{Username: "rollback-referrer", AffCode: "ROLL"}
	require.NoError(t, DB.Create(inviter).Error)
	rollback := errors.New("binding failed")
	err := DB.Transaction(func(tx *gorm.DB) error {
		user := &User{Username: "rollback-invitee", InviterId: inviter.Id}
		require.NoError(t, user.InsertWithTx(tx, inviter.Id))
		var counted User
		require.NoError(t, tx.First(&counted, inviter.Id).Error)
		assert.Equal(t, 1, counted.AffCount)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.NoError(t, DB.First(inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffCount)
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "rollback-invitee").Count(&count).Error)
	assert.Zero(t, count)
}
