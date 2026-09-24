package model

// UserPeriodQuota contains settled consumption from the log database, in quota
// points. Query by immutable user ID so account renames do not change totals.
type UserPeriodQuota struct {
	TodayQuota int64 `gorm:"column:today_quota"`
	MonthQuota int64 `gorm:"column:month_quota"`
}

func GetUserPeriodQuota(userID int, monthStart, todayStart, asOf int64) (UserPeriodQuota, error) {
	var quota UserPeriodQuota
	err := LOG_DB.Model(&Log{}).
		Select("COALESCE(SUM(CASE WHEN created_at >= ? THEN quota ELSE 0 END), 0) AS today_quota, COALESCE(SUM(quota), 0) AS month_quota", todayStart).
		Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, LogTypeConsume, monthStart, asOf).
		Scan(&quota).Error
	return quota, err
}
