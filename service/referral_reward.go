package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/model"
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
