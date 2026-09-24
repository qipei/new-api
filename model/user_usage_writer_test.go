package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUsageWriterDBs(t *testing.T) {
	t.Helper()
	t.Setenv("USER_USAGE_SPOOL_DIR", t.TempDir())
	oldDB, oldLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	logs, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, conn := range []*gorm.DB{db, logs} {
		sqlDB, err := conn.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		require.NoError(t, conn.AutoMigrate(&UserUsageRequest{}, &UserUsageTracking{}))
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	DB, LOG_DB = db, logs
	t.Cleanup(func() { require.NoError(t, StopUserUsageWriter(context.Background())); DB, LOG_DB = oldDB, oldLogDB })
	require.NoError(t, LOG_DB.Create(&UserUsageTracking{ID: 1, StartedAt: 1}).Error)
}

func TestUsageShutdownFailurePreservesPendingAndQueuedRecords(t *testing.T) {
	setupUsageWriterDBs(t)
	var fail atomic.Bool
	fail.Store(true)
	require.NoError(t, LOG_DB.Callback().Create().Before("gorm:create").Register("shutdown_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_usage_requests" && fail.Load() {
			tx.AddError(errors.New("write outage during shutdown"))
		}
	}))
	require.NoError(t, StartUserUsageWriter())
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "pending", CreatedAt: 100, Requests: 1, Quota: 10}))
	require.Error(t, FlushUserUsage(context.Background()))
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "queued", CreatedAt: 101, Requests: 1, Quota: 20}))
	require.ErrorContains(t, StopUserUsageWriter(context.Background()), "saved for next startup")
	fail.Store(false)
	require.NoError(t, StartUserUsageWriter())
	require.NoError(t, StopUserUsageWriter(context.Background()))
	var rows []UserUsageRequest
	require.NoError(t, LOG_DB.Order("request_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(10), rows[0].Quota)
	assert.Equal(t, int64(20), rows[1].Quota)
	complete, err := UserUsageRangeComplete(1, 200)
	require.NoError(t, err)
	assert.False(t, complete)
	// A second startup does not replay an already recovered file.
	require.NoError(t, StartUserUsageWriter())
	require.NoError(t, StopUserUsageWriter(context.Background()))
	var count int64
	require.NoError(t, LOG_DB.Model(&UserUsageRequest{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestUsageEnqueueDoesNotWaitForDatabaseAndShutdownDrains(t *testing.T) {
	setupUsageWriterDBs(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	require.NoError(t, LOG_DB.Callback().Create().Before("gorm:create").Register("block_usage_write", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_usage_requests" {
			enterOnce.Do(func() { close(entered) })
			<-release
		}
	}))
	require.NoError(t, StartUserUsageWriter())
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "first", CreatedAt: 100, Requests: 1, Outcome: "success", Quota: 10}))
	flushed := make(chan error, 1)
	go func() { flushed <- FlushUserUsage(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not enter blocked database")
	}
	// The worker is held inside DB I/O; a new completed request must still enqueue.
	queued := make(chan error, 1)
	go func() {
		queued <- enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "second", CreatedAt: 101, Requests: 1, Outcome: "success", Quota: 20})
	}()
	select {
	case err := <-queued:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("request waited for database")
	}
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-flushed)
	require.NoError(t, StopUserUsageWriter(context.Background()))
	var rows []UserUsageRequest
	require.NoError(t, LOG_DB.Order("request_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(10), rows[0].Quota)
	assert.Equal(t, int64(20), rows[1].Quota)
	var mainCount int64
	require.NoError(t, DB.Model(&UserUsageRequest{}).Count(&mainCount).Error)
	assert.Zero(t, mainCount)
}

func TestUsageBatchRetriesAndMarksCoverageIncomplete(t *testing.T) {
	setupUsageWriterDBs(t)
	var fail atomic.Bool
	fail.Store(true)
	require.NoError(t, LOG_DB.Callback().Create().Before("gorm:create").Register("fail_usage_write", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_usage_requests" && fail.Load() {
			tx.AddError(errors.New("database temporarily unavailable"))
		}
	}))
	require.NoError(t, StartUserUsageWriter())
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "retry", CreatedAt: 100, Requests: 1, Outcome: "success", Quota: 42}))
	require.Error(t, FlushUserUsage(context.Background()))
	complete, err := UserUsageRangeComplete(1, 200)
	require.NoError(t, err)
	assert.False(t, complete)
	fail.Store(false)
	require.NoError(t, FlushUserUsage(context.Background()))
	// An ambiguous retry must not double-count the same request.
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "retry", CreatedAt: 100, Requests: 1, Outcome: "success", Quota: 42}))
	require.NoError(t, StopUserUsageWriter(context.Background()))
	var rows []UserUsageRequest
	require.NoError(t, LOG_DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(42), rows[0].Quota)
	complete, err = UserUsageRangeComplete(1, 200)
	require.NoError(t, err)
	assert.False(t, complete)
	complete, err = UserUsageRangeComplete(101, 200)
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestUsageQueueOverflowRemainsBoundedAndAuditable(t *testing.T) {
	setupUsageWriterDBs(t)
	w := &userUsageWriter{db: LOG_DB, queue: make(chan UserUsageRequest, 1)}
	usageWriter.Store(w)
	// This deliberately paused writer makes queue saturation deterministic.
	t.Cleanup(func() { usageWriter.Store(nil) })
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "kept", CreatedAt: 100}))
	require.NoError(t, enqueueUserUsage(UserUsageRequest{UserID: 1, RequestID: "overflow", CreatedAt: 101}))
	assert.Len(t, w.queue, 1)
	complete, err := UserUsageRangeComplete(1, 200)
	require.NoError(t, err)
	assert.False(t, complete)
	require.NoError(t, w.persistGap(context.Background()))
	usageWriter.Store(nil)
	complete, err = UserUsageRangeComplete(1, 200)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestUsageStorageMigrationAndRetention(t *testing.T) {
	setupUsageWriterDBs(t)
	require.NoError(t, LOG_DB.Where("id = ?", 1).Delete(&UserUsageTracking{}).Error)
	now := time.Now()
	cutoff := now.Add(-31 * 24 * time.Hour).Unix()
	require.NoError(t, DB.Create(&UserUsageTracking{ID: 1, StartedAt: cutoff - 100}).Error)
	oldRows := []UserUsageRequest{
		{UserID: 1, RequestID: "expired", CreatedAt: cutoff - 1, Quota: 1},
		{UserID: 1, RequestID: "retained", CreatedAt: now.Unix(), Requests: 1, Outcome: "success", Quota: 42},
	}
	require.NoError(t, DB.Create(&oldRows).Error)
	require.NoError(t, InitializeUserUsageStorage())
	require.NoError(t, InitializeUserUsageStorage())
	var rows []UserUsageRequest
	require.NoError(t, LOG_DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "retained", rows[0].RequestID)
	epoch, err := GetUserUsageTrackingStart()
	require.NoError(t, err)
	assert.Equal(t, cutoff-100, epoch)
	require.NoError(t, LOG_DB.Create(&UserUsageRequest{UserID: 2, RequestID: "expired", CreatedAt: cutoff - 1}).Error)
	require.NoError(t, LOG_DB.Create(&UserUsageRequest{UserID: 2, RequestID: "boundary", CreatedAt: cutoff}).Error)
	require.NoError(t, CleanupUserUsage(context.Background(), LOG_DB, now))
	rows = nil
	require.NoError(t, LOG_DB.Order("created_at").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "boundary", rows[0].RequestID)
	// Retention of the source copy is safe and independent of migration success.
	require.NoError(t, CleanupUserUsage(context.Background(), DB, now))
	rows = nil
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "retained", rows[0].RequestID)
}
