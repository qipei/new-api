package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupUserUsageTest(t *testing.T, epoch int64) {
	t.Helper()
	setupQuotaOverviewTest(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.UserUsageRequest{}, &model.UserUsageTracking{}))
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageTracking{ID: 1, StartedAt: epoch}).Error)
}

func TestUserUsageCalendarRanges(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 30, 0, 0, time.FixedZone("CST", 28800))
	for _, tc := range []struct {
		period, start, unit string
		points              int
	}{
		{"today", "2026-10-01", "hour", 1}, {"7d", "2026-09-25", "day", 7}, {"30d", "2026-09-02", "day", 30}, {"", "2026-09-25", "day", 7},
	} {
		t.Run(tc.period, func(t *testing.T) {
			r, err := ResolveUserUsageRange(tc.period, now.UTC())
			require.NoError(t, err)
			assert.Equal(t, tc.start, time.Unix(r.Start, 0).In(now.Location()).Format("2006-01-02"))
			assert.Equal(t, tc.unit, r.Granularity)
			assert.Equal(t, now.Unix()+1, r.End)
			setupUserUsageTest(t, r.Start)
			out, err := GetUserUsageOverview(1, r)
			require.NoError(t, err)
			assert.Len(t, out.Series, tc.points)
			assert.Empty(t, out.Models)
			assert.Nil(t, out.SuccessRate)
			assert.Nil(t, out.Peak)
			assert.Equal(t, "0.000000", out.Consumption.Amount)
		})
	}
	_, err := ResolveUserUsageRange("365d", now)
	require.Error(t, err)
}

func TestUserUsageOverviewFinalResultsAndRanking(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.FixedZone("CST", 28800))
	r, err := ResolveUserUsageRange("7d", now)
	require.NoError(t, err)
	setupUserUsageTest(t, r.Start)
	for i := 1; i <= 7; i++ {
		outcome := "success"
		if i == 7 {
			outcome = "failed"
		}
		require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: fmt.Sprint(i), CreatedAt: r.Start, ModelName: fmt.Sprintf("model-%d", i), Requests: 1, Outcome: outcome, Quota: int64(i * 500_000)}).Error)
	}
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "adjust", CreatedAt: now.Unix(), ModelName: "model-7", Outcome: "adjustment", Quota: 500_000}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 2, RequestID: "private", CreatedAt: r.Start, Requests: 1, Outcome: "failed", Quota: 99_000_000}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "outside", CreatedAt: r.End, Requests: 1, Outcome: "failed", Quota: 99_000_000}).Error)
	out, err := GetUserUsageOverview(1, r)
	require.NoError(t, err)
	assert.Equal(t, int64(7), out.Requests)
	assert.Equal(t, int64(6), out.SuccessfulRequests)
	assert.Equal(t, int64(1), out.FailedRequests)
	require.NotNil(t, out.SuccessRate)
	assert.InDelta(t, 600.0/7, *out.SuccessRate, 1e-8)
	assert.Equal(t, "29.000000", out.Consumption.Amount)
	assert.Len(t, out.Models, 5)
	assert.Equal(t, "model-7", out.Models[0].ModelName)
	assert.Equal(t, "8.000000", out.Models[0].Consumption.Amount)
	assert.InDelta(t, 8.0/29, out.Models[0].Share, 1e-8)
	assert.Equal(t, 2, out.OtherModels.Count)
	assert.Equal(t, "3.000000", out.OtherModels.Consumption.Amount)
	require.NotNil(t, out.Peak)
	assert.Equal(t, r.Start, out.Peak.Timestamp)
	assert.Equal(t, "28.000000", out.Peak.Consumption.Amount)
	assert.Equal(t, "1.000000", out.Series[6].Consumption.Amount)
	assert.Zero(t, out.Series[6].Requests)
	records, err := GetUserUsageRecords(1, r, 1, 100, "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(7), records.Total)
}

func TestUserUsageLegacyAndCurrentPagination(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	r, err := ResolveUserUsageRange("today", now)
	require.NoError(t, err)
	epoch := r.Start + 3600
	setupUserUsageTest(t, epoch)
	logs := []model.Log{
		{UserId: 1, RequestId: "retry", Type: model.LogTypeError, CreatedAt: r.Start + 2, ModelName: "a"},
		{UserId: 1, RequestId: "retry", Type: model.LogTypeConsume, CreatedAt: r.Start + 3, ModelName: "a", Quota: 100, PromptTokens: 10, CompletionTokens: 2},
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: r.Start + 1, ModelName: "b", Quota: 200},
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: r.Start, ModelName: "b", Quota: 300},
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: epoch, ModelName: "a", Quota: 99999},
		{UserId: 2, Type: model.LogTypeConsume, CreatedAt: r.Start, ModelName: "secret", Quota: 99999},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "new", CreatedAt: epoch, ModelName: "a", Requests: 1, Outcome: "success", Quota: 400, StatusCode: 200, RetryCount: 1, LastErrorStatusCode: 429}).Error)
	out, err := GetUserUsageOverview(1, r)
	require.NoError(t, err)
	assert.Equal(t, int64(4), out.Requests)
	assert.Equal(t, int64(1000), out.Consumption.Quota)
	assert.Equal(t, int64(3), out.Coverage.UnknownRequests)
	assert.False(t, out.Coverage.Complete)
	assert.Nil(t, out.SuccessRate)
	page, err := GetUserUsageRecords(1, r, 1, 2, "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(4), page.Total)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "new", page.Items[0].RequestID)
	require.NotNil(t, page.Items[0].RetryCount)
	assert.Equal(t, 1, *page.Items[0].RetryCount)
	assert.Equal(t, "retry", page.Items[1].RequestID)
	assert.Equal(t, "unknown", page.Items[1].Outcome)
	assert.Nil(t, page.Items[1].StatusCode)
	assert.Nil(t, page.Items[1].RetryCount)
	assert.Equal(t, int64(12), page.Items[1].TotalTokens)
	page, err = GetUserUsageRecords(1, r, 2, 2, "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.NotEqual(t, page.Items[0].ID, page.Items[1].ID)
	page, err = GetUserUsageRecords(1, r, 1, 10, "a", "unknown")
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
	page, err = GetUserUsageRecords(1, r, 1, 10, "a", "success")
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
	page, err = GetUserUsageRecords(1, r, 10, 2, "", "")
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	require.NoError(t, model.LOG_DB.Migrator().DropTable(&model.Log{}))
	_, err = GetUserUsageOverview(1, r)
	require.Error(t, err)
}

func TestUserUsageCurrency(t *testing.T) {
	r, err := ResolveUserUsageRange("today", time.Now())
	require.NoError(t, err)
	setupUserUsageTest(t, r.Start)
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "one", CreatedAt: r.Start, Requests: 1, Outcome: "success", Quota: 500_000}).Error)
	for _, tc := range []struct{ mode, symbol, amount string }{{"USD", "$", "1.000000"}, {"CNY", "¥", "7.300000"}, {"CUSTOM", "€", "0.800000"}, {"TOKENS", "", "500000"}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := operation_setting.GetGeneralSetting()
			s.QuotaDisplayType = tc.mode
			s.CustomCurrencySymbol = "€"
			s.CustomCurrencyExchangeRate = 0.8
			out, err := GetUserUsageOverview(1, r)
			require.NoError(t, err)
			assert.Equal(t, tc.symbol, out.Currency.Symbol)
			assert.Equal(t, tc.amount, out.Consumption.Amount)
			list, err := GetUserUsageRecords(1, r, 1, 10, "", "")
			require.NoError(t, err)
			require.Len(t, list.Items, 1)
			assert.Equal(t, out.Consumption, list.Items[0].Consumption)
		})
	}
}
