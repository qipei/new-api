package service

import (
	"fmt"
	"time"

	"github.com/samber/hot"
	"golang.org/x/sync/singleflight"
)

// Bounded per-process cache. Concurrent misses for one user/period share one
// query; no database or Redis round trip is required on a cache hit.
var userUsageOverviewCache = hot.NewHotCache[string, *UserUsageOverview](hot.LRU, 1024).Build()
var userUsageOverviewQueries singleflight.Group

func GetCachedUserUsageOverview(userID int, r UserUsageRange) (*UserUsageOverview, error) {
	currency, err := getQuotaDisplayCurrency()
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%d:%s:%d:%q:%q:%g:%g", userID, r.Period, r.Start, currency.DisplayType, currency.Symbol, currency.ExchangeRate, currency.QuotaPerUnit)
	if out, ok, err := userUsageOverviewCache.Get(key); err == nil && ok {
		return out, nil
	}
	value, err, _ := userUsageOverviewQueries.Do(key, func() (interface{}, error) {
		if out, ok, err := userUsageOverviewCache.Get(key); err == nil && ok {
			return out, nil
		}
		out, err := GetUserUsageOverview(userID, r)
		if err != nil {
			return nil, err
		}
		if out.Currency == currency {
			userUsageOverviewCache.SetWithTTL(key, out, time.Minute)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*UserUsageOverview), nil
}
