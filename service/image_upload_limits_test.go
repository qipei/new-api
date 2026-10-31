package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageUploadLimitUserWindows(t *testing.T) {
	cfg := object_storage.Defaults()
	cfg.RequestsPerMinute = 1
	cfg.DailyLimitMB = 1
	l := imageUploadLimiter{users: make(map[int]imageUploadUsage)}
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 0, l.reserve(1, 600000, true, cfg, now))
	assert.Equal(t, 1, l.reserve(1, 1, true, cfg, now))
	assert.Equal(t, 0, l.reserve(2, 1, true, cfg, now))
	assert.Equal(t, 2, l.reserve(1, 600000, true, cfg, now.Add(time.Minute)))
	assert.Equal(t, 0, l.reserve(1, 600000, true, cfg, now.Add(24*time.Hour)))
}

func TestImageUploadRedisCountersSharedAndAtomic(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB = old; common.RedisEnabled = enabled })
	cfg := object_storage.Defaults()
	cfg.RequestsPerMinute = 2
	cfg.DailyLimitMB = 1
	ctx := context.Background()
	code, err := reserveImageUploadBudget(ctx, 1, 600000, true, cfg)
	require.NoError(t, err)
	assert.Zero(t, code)
	code, err = reserveImageUploadBudget(ctx, 1, 600000, true, cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	code, err = reserveImageUploadBudget(ctx, 1, 1, true, cfg)
	require.NoError(t, err)
	assert.Zero(t, code)
	code, err = reserveImageUploadBudget(ctx, 1, 1, true, cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	code, err = reserveImageUploadBudget(ctx, 2, 1, true, cfg)
	require.NoError(t, err)
	assert.Zero(t, code)
}

func TestImageUploadSeparateRequestAndByteReservations(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB = old; common.RedisEnabled = enabled })
	cfg := object_storage.Defaults()
	cfg.RequestsPerMinute = 1
	cfg.DailyLimitMB = 1
	ctx := context.Background()
	code, err := ReserveImageUploadRequest(ctx, 7, cfg)
	require.NoError(t, err)
	assert.Zero(t, code)
	code, err = ReserveImageUploadBytes(ctx, 7, 100, cfg)
	require.NoError(t, err)
	assert.Zero(t, code)
	code, err = ReserveImageUploadRequest(ctx, 7, cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
}
