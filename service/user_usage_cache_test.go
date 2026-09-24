package service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/samber/hot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUsageOverviewCacheAvoidsRepeatedQueriesAndSeparatesUsersAndCurrency(t *testing.T) {
	r, err := ResolveUserUsageRange("today", time.Now())
	require.NoError(t, err)
	setupUserUsageTest(t, r.Start)
	oldCache := userUsageOverviewCache
	userUsageOverviewCache = hot.NewHotCache[string, *UserUsageOverview](hot.LRU, 8).Build()
	t.Cleanup(func() { userUsageOverviewCache = oldCache })
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "one", CreatedAt: r.Start, Requests: 1, Outcome: "success", Quota: 500000}).Error)
	var reads atomic.Int64
	require.NoError(t, model.LOG_DB.Callback().Query().Before("gorm:query").Register("usage_cache_queries", func(*gorm.DB) { reads.Add(1) }))
	first, err := GetCachedUserUsageOverview(1, r)
	require.NoError(t, err)
	assert.Equal(t, "1.000000", first.Consumption.Amount)
	queries := reads.Load()
	require.Positive(t, queries)
	require.NoError(t, model.LOG_DB.Create(&model.UserUsageRequest{UserID: 1, RequestID: "two", CreatedAt: r.Start, Requests: 1, Outcome: "success", Quota: 500000}).Error)
	cached, err := GetCachedUserUsageOverview(1, r)
	require.NoError(t, err)
	assert.Equal(t, first, cached)
	assert.Equal(t, queries, reads.Load())
	other, err := GetCachedUserUsageOverview(2, r)
	require.NoError(t, err)
	assert.Zero(t, other.Requests)
	operation_setting.GetGeneralSetting().QuotaDisplayType = "CNY"
	changed, err := GetCachedUserUsageOverview(1, r)
	require.NoError(t, err)
	assert.Equal(t, "14.600000", changed.Consumption.Amount)
	assert.Equal(t, "¥", changed.Currency.Symbol)
}
