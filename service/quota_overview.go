package service

import (
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

type QuotaOverviewValue struct {
	Quota  int64  `json:"quota"`
	Amount string `json:"amount"`
}

type QuotaOverviewCurrency struct {
	DisplayType  string  `json:"display_type"`
	Symbol       string  `json:"symbol"`
	ExchangeRate float64 `json:"exchange_rate"`
	QuotaPerUnit float64 `json:"quota_per_unit"`
}

type UserQuotaOverview struct {
	Remaining  QuotaOverviewValue    `json:"remaining"`
	Today      QuotaOverviewValue    `json:"today"`
	Month      QuotaOverviewValue    `json:"month"`
	Currency   QuotaOverviewCurrency `json:"currency"`
	Timezone   string                `json:"timezone"`
	AsOf       int64                 `json:"as_of"`
	TodayStart int64                 `json:"today_start"`
	MonthStart int64                 `json:"month_start"`
}

// GetUserQuotaOverview uses Beijing calendar days regardless of the server's
// local timezone. now is shared by both periods to keep midnight consistent.
func GetUserQuotaOverview(userID int, now time.Time) (*UserQuotaOverview, error) {
	const timezone = "Asia/Shanghai"
	localNow := now.In(time.FixedZone(timezone, 8*60*60))
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	month := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, localNow.Location())
	remaining, err := model.GetUserQuota(userID, true)
	if err != nil {
		return nil, err
	}
	usage, err := model.GetUserPeriodQuota(userID, month.Unix(), today.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	currency := QuotaOverviewCurrency{
		DisplayType:  operation_setting.GetQuotaDisplayType(),
		Symbol:       operation_setting.GetCurrencySymbol(),
		ExchangeRate: operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate),
		QuotaPerUnit: common.QuotaPerUnit,
	}
	if currency.QuotaPerUnit <= 0 || math.IsNaN(currency.QuotaPerUnit) || math.IsInf(currency.QuotaPerUnit, 0) ||
		currency.ExchangeRate <= 0 || math.IsNaN(currency.ExchangeRate) || math.IsInf(currency.ExchangeRate, 0) {
		return nil, fmt.Errorf("invalid quota display conversion settings")
	}
	result := &UserQuotaOverview{
		Remaining:  QuotaOverviewValue{Quota: int64(remaining)},
		Today:      QuotaOverviewValue{Quota: usage.TodayQuota},
		Month:      QuotaOverviewValue{Quota: usage.MonthQuota},
		Currency:   currency,
		Timezone:   timezone,
		AsOf:       now.Unix(),
		TodayStart: today.Unix(),
		MonthStart: month.Unix(),
	}
	for _, value := range []*QuotaOverviewValue{&result.Remaining, &result.Today, &result.Month} {
		amount := decimal.NewFromInt(value.Quota)
		if currency.DisplayType == operation_setting.QuotaDisplayTypeTokens {
			value.Amount = amount.String()
			continue
		}
		value.Amount = amount.Mul(decimal.NewFromFloat(currency.ExchangeRate)).
			Div(decimal.NewFromFloat(currency.QuotaPerUnit)).StringFixed(6)
	}
	return result, nil
}
