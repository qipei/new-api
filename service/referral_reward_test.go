package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReferralRewardsCurrencyAndLegacyBalance(t *testing.T) {
	setupQuotaOverviewTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CommissionRecord{}, &model.ReferralRewardRecord{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]interface{}{"aff_quota": 500000, "aff_history": 1000000}).Error)
	require.NoError(t, model.DB.Create(&model.ReferralRewardRecord{UserID: 1, SourceType: model.ReferralRewardRegistration, SourceID: "2", Quota: 500000, CreatedAt: 100}).Error)
	for _, tc := range []struct{ mode, symbol, reward, lifetime string }{
		{"USD", "$", "1.000000", "2.000000"}, {"CNY", "¥", "7.300000", "14.600000"},
		{"CUSTOM", "€", "0.800000", "1.600000"}, {"TOKENS", "", "500000", "1000000"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			setting := operation_setting.GetGeneralSetting()
			setting.QuotaDisplayType = tc.mode
			setting.CustomCurrencySymbol, setting.CustomCurrencyExchangeRate = "€", 0.8
			result, err := GetReferralRewards(1, 1, 20)
			require.NoError(t, err)
			assert.Equal(t, tc.symbol, result.Currency.Symbol)
			assert.Equal(t, tc.lifetime, result.Lifetime.Amount)
			assert.Equal(t, tc.reward, result.Available.Amount)
			assert.Equal(t, tc.reward, result.HistoricalUnitemized.Amount)
			require.Len(t, result.Items, 1)
			assert.Equal(t, tc.reward, result.Items[0].Reward.Amount)
			assert.Equal(t, "registration", result.Items[0].SourceType)
		})
	}
}

func TestReferralRewardsReturnsActualBalancesWhenLedgerExceedsLifetime(t *testing.T) {
	setupQuotaOverviewTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CommissionRecord{}, &model.ReferralRewardRecord{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]interface{}{"aff_quota": 250000, "aff_history": 500000}).Error)
	require.NoError(t, model.DB.Create(&model.ReferralRewardRecord{UserID: 1, SourceType: model.ReferralRewardRegistration, SourceID: "legacy", Quota: 1000000}).Error)
	result, err := GetReferralRewards(1, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, "0.500000", result.Available.Amount)
	assert.Equal(t, "1.000000", result.Lifetime.Amount)
	assert.Equal(t, "0.000000", result.HistoricalUnitemized.Amount)
	assert.EqualValues(t, 1, result.Total)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "2.000000", result.Items[0].Reward.Amount)
}

func TestReferralConfigMatchesIndependentRewardSettings(t *testing.T) {
	setupQuotaOverviewTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserCommissionOverride{}))
	settings := config.GlobalConfig.Get("commission_setting").(*operation_setting.CommissionSetting)
	oldSettings, oldReward, oldPayment := *settings, common.QuotaForInviter, *operation_setting.GetPaymentSetting()
	t.Cleanup(func() {
		*settings = oldSettings
		common.QuotaForInviter = oldReward
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	for _, tc := range []struct {
		name          string
		quota         int
		commission    bool
		value         float64
		compliance    bool
		signup, topup bool
	}{
		{"signup only", 100, false, 10, true, true, false},
		{"topup only", 0, true, 10, true, false, true},
		{"both", 100, true, 10, true, true, true},
		{"neither", 0, false, 10, true, false, false},
		{"zero commission", 100, true, 0, true, true, false},
		{"registration and transfers require compliance", 100, true, 10, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.QuotaForInviter = tc.quota
			*settings = operation_setting.CommissionSetting{Enabled: tc.commission, Type: operation_setting.CommissionTypePercent, Value: tc.value}
			payment := operation_setting.GetPaymentSetting()
			payment.ComplianceConfirmed = tc.compliance
			payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
			got := GetReferralConfig(1)
			assert.Equal(t, tc.signup, got.RegistrationRewardEnabled)
			assert.Equal(t, tc.topup, got.TopupCommissionEnabled)
			assert.Equal(t, tc.compliance, got.TransferEnabled)
		})
	}
	*settings = operation_setting.CommissionSetting{Enabled: true, Type: operation_setting.CommissionTypePercent, Value: 10}
	require.NoError(t, model.DB.Create(&model.UserCommissionOverride{UserId: 1, Type: operation_setting.CommissionTypePercent, Value: 0}).Error)
	assert.False(t, GetReferralConfig(1).TopupCommissionEnabled, "zero personal commission overrides the global rate")
	assert.True(t, GetReferralConfig(2).TopupCommissionEnabled, "another promoter keeps the global rate")
	require.NoError(t, model.DB.Model(&model.UserCommissionOverride{}).Where("user_id = ?", 1).Update("value", 20).Error)
	settings.Enabled = false
	assert.False(t, GetReferralConfig(1).TopupCommissionEnabled, "personal rates cannot bypass the global switch")
}
