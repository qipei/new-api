package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckinCaptchaTrustExpiresWithoutSlidingOnCheckin(t *testing.T) {
	user := setupAuthSessionTestDB(t)
	old := *operation_setting.GetCheckinSetting()
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{CaptchaMode: "adaptive", CaptchaTrustDays: 3, CaptchaIPUserLimit: 5}
	t.Cleanup(func() { *operation_setting.GetCheckinSetting() = old })
	const now = int64(1800000000)
	for _, tc := range []struct {
		name       string
		verifiedAt int64
		want       bool
	}{
		{"never verified", 0, true},
		{"within three days", now - 3*86400 + 1, false},
		{"exactly three days", now - 3*86400, true},
		{"future timestamp is not trusted", now + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("checkin_captcha_verified_at", tc.verifiedAt).Error)
			required, err := checkinCaptchaRequired(context.Background(), user.Id, "192.0.2.81", true, now)
			require.NoError(t, err)
			assert.Equal(t, tc.want, required)
			verified, err := model.GetCheckinCaptchaVerifiedAt(user.Id)
			require.NoError(t, err)
			assert.Equal(t, tc.verifiedAt, verified, "ordinary requests must not extend trust")
		})
	}
	operation_setting.GetCheckinSetting().CaptchaMode = "always"
	required, err := checkinCaptchaRequired(context.Background(), user.Id, "192.0.2.81", false, now)
	require.NoError(t, err)
	assert.True(t, required)
}

func TestCheckinIPRiskWindowAndReadOnlyStatus(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[useRedis], func(t *testing.T) {
			oldEnabled, oldClient := common.RedisEnabled, common.RDB
			oldTracker := checkinLocalIPUsers
			checkinLocalIPUsers = &checkinIPUsers{entries: make(map[string]map[int]int64)}
			common.RedisEnabled = useRedis
			t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldClient; checkinLocalIPUsers = oldTracker })
			if useRedis {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { require.NoError(t, common.RDB.Close()) })
			}
			const now = int64(1800000000)
			ctx := context.Background()
			// Reads predict the current account but never register an attempt.
			for _, id := range []int{11, 12, 13, 14, 15} {
				count, err := checkinIPAccountCount(ctx, id, "192.0.2.82", false, now)
				require.NoError(t, err)
				assert.Equal(t, 1, count)
			}
			for _, id := range []int{1, 2, 3, 4} {
				count, err := checkinIPAccountCount(ctx, id, "192.0.2.82", true, now)
				require.NoError(t, err)
				assert.Equal(t, id, count)
			}
			count, err := checkinIPAccountCount(ctx, 4, "192.0.2.82", true, now)
			require.NoError(t, err)
			assert.Equal(t, 4, count)
			count, err = checkinIPAccountCount(ctx, 5, "192.0.2.82", false, now)
			require.NoError(t, err)
			assert.Equal(t, 5, count)
			count, err = checkinIPAccountCount(ctx, 5, "192.0.2.83", true, now)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
			count, err = checkinIPAccountCount(ctx, 5, "192.0.2.82", false, now+86399)
			require.NoError(t, err)
			assert.Equal(t, 5, count, "accounts from earlier the same day still count")
			count, err = checkinIPAccountCount(ctx, 5, "192.0.2.82", true, now+86400)
			require.NoError(t, err)
			assert.Equal(t, 1, count, "attempts exactly 24 hours old expire")
			// Separate authenticated accounts cannot all pass the threshold
			// through a concurrent read-then-write race.
			counts := make(chan int, 5)
			errs := make(chan error, 5)
			var wg sync.WaitGroup
			for id := 1; id <= 5; id++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					n, e := checkinIPAccountCount(ctx, id, "192.0.2.84", true, now)
					counts <- n
					errs <- e
				}(id)
			}
			wg.Wait()
			close(counts)
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			got := []int{}
			for n := range counts {
				got = append(got, n)
			}
			assert.ElementsMatch(t, []int{1, 2, 3, 4, 5}, got)
		})
	}
}

func TestCheckinIPRiskOverridesTrustedUserAndRedisFailureDoesNotAllow(t *testing.T) {
	user := setupAuthSessionTestDB(t)
	old := *operation_setting.GetCheckinSetting()
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{CaptchaMode: "adaptive", CaptchaTrustDays: 3, CaptchaIPUserLimit: 5}
	t.Cleanup(func() { *operation_setting.GetCheckinSetting() = old })
	now := time.Now().Unix()
	require.NoError(t, model.RecordCheckinCaptchaVerification(user.Id, now))
	for _, id := range []int{21, 22, 23, 24} {
		_, err := checkinIPAccountCount(context.Background(), id, "192.0.2.85", true, now)
		require.NoError(t, err)
	}
	required, err := checkinCaptchaRequired(context.Background(), user.Id, "192.0.2.85", true, now)
	require.NoError(t, err)
	assert.True(t, required)
	oldClient := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() { common.RDB = oldClient })
	required, err = checkinCaptchaRequired(context.Background(), user.Id, "192.0.2.86", true, now)
	require.Error(t, err)
	assert.True(t, required)
}

func TestCheckinIPRiskGroupsIPv6ByNetwork(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[useRedis], func(t *testing.T) {
			oldEnabled, oldClient := common.RedisEnabled, common.RDB
			oldTracker := checkinLocalIPUsers
			checkinLocalIPUsers = &checkinIPUsers{entries: make(map[string]map[int]int64)}
			common.RedisEnabled = useRedis
			t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldClient; checkinLocalIPUsers = oldTracker })
			if useRedis {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { require.NoError(t, common.RDB.Close()) })
			}
			const now = int64(1800000000)
			ctx := context.Background()
			// Rotating addresses inside one /64 is the same source.
			for i, ip := range []string{"2001:db8:1:2::1", "2001:db8:1:2:aaaa::9", "2001:db8:1:2:ffff:ffff:ffff:ffff"} {
				count, err := checkinIPAccountCount(ctx, i+1, ip, true, now)
				require.NoError(t, err)
				assert.Equal(t, i+1, count)
			}
			// A neighbouring /64 and IPv4 are tracked separately.
			count, err := checkinIPAccountCount(ctx, 9, "2001:db8:1:3::1", true, now)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
			count, err = checkinIPAccountCount(ctx, 9, "::ffff:192.0.2.90", true, now)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
			count, err = checkinIPAccountCount(ctx, 10, "192.0.2.90", true, now)
			require.NoError(t, err)
			assert.Equal(t, 2, count, "IPv4-mapped IPv6 and plain IPv4 are the same address")
		})
	}
}

func TestCheckinIPRiskFullMemoryTableRequiresCaptcha(t *testing.T) {
	oldEnabled, oldTracker := common.RedisEnabled, checkinLocalIPUsers
	common.RedisEnabled = false
	checkinLocalIPUsers = &checkinIPUsers{entries: make(map[string]map[int]int64)}
	t.Cleanup(func() { common.RedisEnabled = oldEnabled; checkinLocalIPUsers = oldTracker })
	const now = int64(1800000000)
	for i := 0; i < 4096; i++ {
		checkinLocalIPUsers.entries[fmt.Sprintf("10.%d.%d.1", i/256, i%256)] = map[int]int64{1: now}
	}
	// A new IP cannot be tracked: it must be challenged, not rejected outright.
	count, err := checkinIPAccountCount(context.Background(), 7, "203.0.113.7", true, now)
	require.NoError(t, err)
	assert.Equal(t, 100, count)
	// IPs already in the table keep being counted normally.
	count, err = checkinIPAccountCount(context.Background(), 8, "10.0.1.1", true, now)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
