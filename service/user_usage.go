package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

type UserUsageRange struct {
	Period      string `json:"period"`
	Start       int64  `json:"start_timestamp"`
	End         int64  `json:"end_timestamp"`
	AsOf        int64  `json:"as_of"`
	Timezone    string `json:"timezone"`
	Granularity string `json:"granularity"`
	step        int64
}

func ResolveUserUsageRange(period string, now time.Time) (UserUsageRange, error) {
	if period == "" {
		period = "7d"
	}
	local := now.In(time.FixedZone("Asia/Shanghai", 28800))
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	r := UserUsageRange{Period: period, Start: today.Unix(), End: now.Unix() + 1, AsOf: now.Unix(), Timezone: "Asia/Shanghai", Granularity: "day", step: 86400}
	switch period {
	case "today":
		r.Granularity = "hour"
		r.step = 3600
	case "7d":
		r.Start = today.AddDate(0, 0, -6).Unix()
	case "30d":
		r.Start = today.AddDate(0, 0, -29).Unix()
	default:
		return r, errors.New("period must be today, 7d or 30d")
	}
	return r, nil
}

func getQuotaDisplayCurrency() (QuotaOverviewCurrency, error) {
	c := QuotaOverviewCurrency{DisplayType: operation_setting.GetQuotaDisplayType(), Symbol: operation_setting.GetCurrencySymbol(), ExchangeRate: operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate), QuotaPerUnit: common.QuotaPerUnit}
	if c.QuotaPerUnit <= 0 || math.IsNaN(c.QuotaPerUnit) || math.IsInf(c.QuotaPerUnit, 0) || c.ExchangeRate <= 0 || math.IsNaN(c.ExchangeRate) || math.IsInf(c.ExchangeRate, 0) {
		return c, errors.New("invalid quota display conversion settings")
	}
	return c, nil
}

func quotaDisplayValue(quota int64, c QuotaOverviewCurrency) QuotaOverviewValue {
	amount := decimal.NewFromInt(quota)
	if c.DisplayType == operation_setting.QuotaDisplayTypeTokens {
		return QuotaOverviewValue{Quota: quota, Amount: amount.String()}
	}
	return QuotaOverviewValue{Quota: quota, Amount: amount.Mul(decimal.NewFromFloat(c.ExchangeRate)).Div(decimal.NewFromFloat(c.QuotaPerUnit)).StringFixed(6)}
}

type UserUsagePoint struct {
	Timestamp   int64              `json:"timestamp"`
	Label       string             `json:"label"`
	Consumption QuotaOverviewValue `json:"consumption"`
	Requests    int64              `json:"requests"`
}

type UserUsageModel struct {
	ModelName   string             `json:"model_name"`
	Consumption QuotaOverviewValue `json:"consumption"`
	Requests    int64              `json:"requests"`
	Share       float64            `json:"share"`
}

type UserUsageCoverage struct {
	TrackingStartedAt int64 `json:"tracking_started_at"`
	Complete          bool  `json:"complete"`
	UnknownRequests   int64 `json:"unknown_requests"`
}

type UserUsageOverview struct {
	UserUsageRange
	Currency           QuotaOverviewCurrency `json:"currency"`
	Requests           int64                 `json:"requests"`
	SuccessfulRequests int64                 `json:"successful_requests"`
	FailedRequests     int64                 `json:"failed_requests"`
	SuccessRate        *float64              `json:"success_rate"`
	Consumption        QuotaOverviewValue    `json:"consumption"`
	Coverage           UserUsageCoverage     `json:"coverage"`
	Series             []UserUsagePoint      `json:"series"`
	Peak               *UserUsagePoint       `json:"peak"`
	Models             []UserUsageModel      `json:"models"`
	OtherModels        struct {
		Count       int                `json:"count"`
		Consumption QuotaOverviewValue `json:"consumption"`
		Requests    int64              `json:"requests"`
	} `json:"other_models"`
}

func GetUserUsageOverview(userID int, r UserUsageRange) (*UserUsageOverview, error) {
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	epoch, err := model.GetUserUsageTrackingStart()
	if err != nil {
		return nil, err
	}
	rows, err := model.GetUserUsageAggregates(userID, r.Start, r.End, epoch, r.step)
	if err != nil {
		return nil, err
	}
	complete, err := model.UserUsageRangeComplete(r.Start, r.End)
	if err != nil {
		return nil, err
	}
	out := &UserUsageOverview{UserUsageRange: r, Currency: currency, Coverage: UserUsageCoverage{TrackingStartedAt: epoch, Complete: complete}, Series: []UserUsagePoint{}, Models: []UserUsageModel{}}
	points := map[int64]*UserUsagePoint{}
	models := map[string]*UserUsageModel{}
	var total int64
	for _, row := range rows {
		out.Requests += row.Requests
		total += row.Quota
		switch row.Outcome {
		case "success":
			out.SuccessfulRequests += row.Requests
		case "failed":
			out.FailedRequests += row.Requests
		case "unknown":
			out.Coverage.UnknownRequests += row.Requests
		}
		if points[row.Bucket] == nil {
			points[row.Bucket] = &UserUsagePoint{}
		}
		points[row.Bucket].Consumption.Quota += row.Quota
		points[row.Bucket].Requests += row.Requests
		if models[row.ModelName] == nil {
			models[row.ModelName] = &UserUsageModel{ModelName: row.ModelName}
		}
		models[row.ModelName].Consumption.Quota += row.Quota
		models[row.ModelName].Requests += row.Requests
	}
	out.Consumption = quotaDisplayValue(total, currency)
	known := out.SuccessfulRequests + out.FailedRequests
	if out.Coverage.Complete && out.Coverage.UnknownRequests == 0 && known > 0 {
		rate := float64(out.SuccessfulRequests) * 100 / float64(known)
		out.SuccessRate = &rate
	}
	for ts := r.Start; ts < r.End; ts += r.step {
		point := UserUsagePoint{Timestamp: ts, Label: time.Unix(ts, 0).In(time.FixedZone("Asia/Shanghai", 28800)).Format("2006-01-02")}
		if r.Granularity == "hour" {
			point.Label = time.Unix(ts, 0).In(time.FixedZone("Asia/Shanghai", 28800)).Format("15:04")
		}
		if p := points[ts]; p != nil {
			point.Requests = p.Requests
			point.Consumption.Quota = p.Consumption.Quota
		}
		point.Consumption = quotaDisplayValue(point.Consumption.Quota, currency)
		out.Series = append(out.Series, point)
		if point.Consumption.Quota > 0 && (out.Peak == nil || point.Consumption.Quota > out.Peak.Consumption.Quota) {
			copy := point
			out.Peak = &copy
		}
	}
	for _, m := range models {
		m.Consumption = quotaDisplayValue(m.Consumption.Quota, currency)
		if total > 0 {
			m.Share = float64(m.Consumption.Quota) / float64(total)
		}
		out.Models = append(out.Models, *m)
	}
	sort.Slice(out.Models, func(i, j int) bool {
		if out.Models[i].Consumption.Quota == out.Models[j].Consumption.Quota {
			return out.Models[i].ModelName < out.Models[j].ModelName
		}
		return out.Models[i].Consumption.Quota > out.Models[j].Consumption.Quota
	})
	var otherQuota int64
	if len(out.Models) > 5 {
		for _, m := range out.Models[5:] {
			otherQuota += m.Consumption.Quota
			out.OtherModels.Requests += m.Requests
		}
		out.OtherModels.Count = len(out.Models) - 5
		out.Models = out.Models[:5]
	}
	out.OtherModels.Consumption = quotaDisplayValue(otherQuota, currency)
	return out, nil
}

type UserUsageRecord struct {
	ID                  string             `json:"id"`
	RequestID           string             `json:"request_id"`
	CreatedAt           int64              `json:"created_at"`
	ModelName           string             `json:"model_name"`
	Outcome             string             `json:"outcome"`
	Consumption         QuotaOverviewValue `json:"consumption"`
	PromptTokens        int64              `json:"prompt_tokens"`
	CompletionTokens    int64              `json:"completion_tokens"`
	TotalTokens         int64              `json:"total_tokens"`
	StatusCode          *int               `json:"status_code"`
	RetryCount          *int               `json:"retry_count"`
	LastErrorStatusCode *int               `json:"last_error_status_code"`
	IsStream            *bool              `json:"is_stream"`
	DurationMS          *int64             `json:"duration_ms"`
}

type UserUsageRecords struct {
	UserUsageRange
	Currency QuotaOverviewCurrency `json:"currency"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Total    int64                 `json:"total"`
	Items    []UserUsageRecord     `json:"items"`
}

func GetUserUsageRecords(userID int, r UserUsageRange, page, size int, modelName, outcome string) (*UserUsageRecords, error) {
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	epoch, err := model.GetUserUsageTrackingStart()
	if err != nil {
		return nil, err
	}
	rows, total, err := model.GetUserUsageRecords(model.UserUsageRecordQuery{UserID: userID, Start: r.Start, End: r.End, TrackingStart: epoch, Offset: (page - 1) * size, Limit: size, Model: modelName, Outcome: outcome})
	if err != nil {
		return nil, err
	}
	out := &UserUsageRecords{UserUsageRange: r, Currency: currency, Page: page, PageSize: size, Total: total, Items: []UserUsageRecord{}}
	for _, row := range rows {
		item := UserUsageRecord{ID: fmt.Sprintf("request:%d", row.ID), RequestID: row.RequestID, CreatedAt: row.CreatedAt, ModelName: row.ModelName, Outcome: row.Outcome, Consumption: quotaDisplayValue(row.Quota, currency), PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens, TotalTokens: row.PromptTokens + row.CompletionTokens}
		if row.Outcome == "unknown" {
			item.ID = fmt.Sprintf("legacy:%d:%s", row.ID, row.RequestID)
		} else {
			item.StatusCode = &row.StatusCode
			item.RetryCount = &row.RetryCount
			item.IsStream = &row.IsStream
			item.DurationMS = &row.DurationMS
			if row.LastErrorStatusCode > 0 {
				item.LastErrorStatusCode = &row.LastErrorStatusCode
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
