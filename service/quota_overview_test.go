package service

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupQuotaOverviewTest(t *testing.T) {
	t.Helper()
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldGeneral := *operation_setting.GetGeneralSetting()
	oldQuotaPerUnit, oldExchangeRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	// The main database and log database may be separate in production.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, connection := range []*gorm.DB{db, logDB} {
		sqlDB, err := connection.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	model.DB, model.LOG_DB = db, logDB
	common.QuotaPerUnit = 500_000
	operation_setting.USDExchangeRate = 7.3
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		*operation_setting.GetGeneralSetting() = oldGeneral
		common.QuotaPerUnit, operation_setting.USDExchangeRate = oldQuotaPerUnit, oldExchangeRate
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "renamed-user", Quota: 6_240_000}).Error)
}

func TestUserQuotaOverviewPeriodsAndUserIsolation(t *testing.T) {
	for _, date := range []string{"2026-09-22T16:25:11+08:00", "2026-10-01T00:30:00+08:00", "2026-10-31T23:59:59+08:00"} {
		t.Run(date, func(t *testing.T) {
			setupQuotaOverviewTest(t)
			now, err := time.Parse(time.RFC3339, date)
			require.NoError(t, err)
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			logs := []model.Log{
				{UserId: 1, Username: "old-name", Type: model.LogTypeConsume, CreatedAt: today.Unix(), Quota: 200_000},
				{UserId: 1, Type: model.LogTypeConsume, CreatedAt: now.Unix(), Quota: 10_000},
				{UserId: 1, Type: model.LogTypeConsume, CreatedAt: month.Unix(), Quota: 3_855_000},
				{UserId: 1, Type: model.LogTypeConsume, CreatedAt: month.Unix() - 1, Quota: 999_999},
				{UserId: 1, Type: model.LogTypeConsume, CreatedAt: now.Unix() + 1, Quota: 999_999},
				{UserId: 2, Type: model.LogTypeConsume, CreatedAt: today.Unix(), Quota: 999_999},
				{UserId: 1, Type: model.LogTypeTopup, CreatedAt: today.Unix(), Quota: 999_999},
			}
			require.NoError(t, model.LOG_DB.Create(&logs).Error)
			// UTC input still uses Beijing calendar boundaries.
			result, err := GetUserQuotaOverview(1, now.UTC())
			require.NoError(t, err)
			assert.Equal(t, int64(6_240_000), result.Remaining.Quota)
			assert.Equal(t, "12.480000", result.Remaining.Amount)
			assert.Equal(t, int64(4_065_000), result.Month.Quota)
			assert.Equal(t, "8.130000", result.Month.Amount)
			if now.Day() == 1 {
				assert.Equal(t, result.Month, result.Today)
			} else {
				assert.Equal(t, int64(210_000), result.Today.Quota)
				assert.Equal(t, "0.420000", result.Today.Amount)
			}
			assert.Equal(t, "Asia/Shanghai", result.Timezone)
			assert.Equal(t, now.Unix(), result.AsOf)
			assert.Equal(t, today.Unix(), result.TodayStart)
			assert.Equal(t, month.Unix(), result.MonthStart)
		})
	}
}

func TestUserQuotaOverviewCurrencyAndEmptyUsage(t *testing.T) {
	setupQuotaOverviewTest(t)
	for _, tc := range []struct {
		mode, symbol, amount string
		rate                 float64
	}{
		{"USD", "$", "12.480000", 1},
		{"CNY", "¥", "91.104000", 7.3},
		{"CUSTOM", "€", "9.984000", 0.8},
		{"TOKENS", "", "6240000", 1},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			settings := operation_setting.GetGeneralSetting()
			settings.QuotaDisplayType = tc.mode
			settings.CustomCurrencySymbol = "€"
			settings.CustomCurrencyExchangeRate = 0.8
			result, err := GetUserQuotaOverview(1, time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			assert.Equal(t, tc.mode, result.Currency.DisplayType)
			assert.Equal(t, tc.symbol, result.Currency.Symbol)
			assert.Equal(t, tc.rate, result.Currency.ExchangeRate)
			assert.Equal(t, float64(500_000), result.Currency.QuotaPerUnit)
			assert.Equal(t, tc.amount, result.Remaining.Amount)
			assert.Zero(t, result.Today.Quota)
			assert.Zero(t, result.Month.Quota)
			if tc.mode == "TOKENS" {
				assert.Equal(t, "0", result.Today.Amount)
			} else {
				assert.Equal(t, "0.000000", result.Today.Amount)
			}
		})
	}
}

func TestUserQuotaOverviewDoesNotHideQueryFailures(t *testing.T) {
	setupQuotaOverviewTest(t)
	require.NoError(t, model.LOG_DB.Migrator().DropTable(&model.Log{}))
	_, err := GetUserQuotaOverview(1, time.Now())
	require.Error(t, err)
}

func TestUserQuotaOverviewUsesConfiguredQuotaUnit(t *testing.T) {
	setupQuotaOverviewTest(t)
	common.QuotaPerUnit = 200_000
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", 1).Error)
	result, err := GetUserQuotaOverview(1, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "0.000005", result.Remaining.Amount)
	assert.Equal(t, float64(200_000), result.Currency.QuotaPerUnit)
}

func TestUserQuotaOverviewRejectsInvalidConversion(t *testing.T) {
	setupQuotaOverviewTest(t)
	for _, unit := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		common.QuotaPerUnit = unit
		_, err := GetUserQuotaOverview(1, time.Now())
		require.Error(t, err)
	}
	common.QuotaPerUnit = 500_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = "CNY"
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		operation_setting.USDExchangeRate = rate
		_, err := GetUserQuotaOverview(1, time.Now())
		require.Error(t, err)
	}
}
