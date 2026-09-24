package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"math"
)

type ReferralRewardItem struct {
	ID         string             `json:"id"`
	SourceType string             `json:"source_type"`
	CreatedAt  int64              `json:"created_at"`
	Reward     QuotaOverviewValue `json:"reward"`
}

type ReferralRewards struct {
	Page                 int                   `json:"page"`
	PageSize             int                   `json:"page_size"`
	Total                int64                 `json:"total"`
	Items                []ReferralRewardItem  `json:"items"`
	Available            QuotaOverviewValue    `json:"available"`
	Lifetime             QuotaOverviewValue    `json:"lifetime"`
	HistoricalUnitemized QuotaOverviewValue    `json:"historical_unitemized"`
	Currency             QuotaOverviewCurrency `json:"currency"`
}

func GetReferralRewards(userID, page, size int) (*ReferralRewards, error) {
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	history, err := model.GetReferralRewardHistory(userID, page, size)
	if err != nil {
		return nil, err
	}
	out := &ReferralRewards{Page: page, PageSize: size, Total: history.Total, Items: []ReferralRewardItem{},
		Currency: currency, Available: quotaDisplayValue(history.Available, currency), Lifetime: quotaDisplayValue(history.Lifetime, currency),
		HistoricalUnitemized: quotaDisplayValue(history.Unitemized, currency)}
	for _, row := range history.Rows {
		out.Items = append(out.Items, ReferralRewardItem{ID: fmt.Sprintf("%s:%d", row.SourceType, row.ID), SourceType: row.SourceType,
			CreatedAt: row.CreatedAt, Reward: quotaDisplayValue(row.Quota, currency)})
	}
	return out, nil
}

// ReferralConfig describes the current promoter's reward rules without changing
// any existing endpoint or treating a positive registration quota as a top-up switch.
type ReferralConfig struct {
	RegistrationRewardEnabled bool `json:"registration_reward_enabled"`
	TopupCommissionEnabled    bool `json:"topup_commission_enabled"`
	TransferEnabled           bool `json:"transfer_enabled"`
}

func GetReferralConfig(userID int) ReferralConfig {
	compliance := operation_setting.IsPaymentComplianceConfirmed()
	result := ReferralConfig{
		RegistrationRewardEnabled: compliance && common.QuotaForInviter > 0 && int64(common.QuotaForInviter) <= common.MaxWalletQuota,
		TransferEnabled:           compliance,
	}
	commission := operation_setting.GetCommissionSetting()
	if !commission.Enabled {
		return result
	}
	commission = model.ResolveCommissionSetting(commission, userID)
	result.TopupCommissionEnabled = commission.Value > 0 && !math.IsNaN(commission.Value) && !math.IsInf(commission.Value, 0)
	// Fixed rewards are truncated to whole quota units by settlement.
	if commission.Type == operation_setting.CommissionTypeFixed && commission.Value < 1 {
		result.TopupCommissionEnabled = false
	}
	return result
}
