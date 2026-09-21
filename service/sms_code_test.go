package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func useSMSCodeStore(t *testing.T, redisEnabled bool) {
	t.Helper()
	oldEnabled, oldRDB := common.RedisEnabled, common.RDB
	oldStore := smsMemoryStore
	smsMemoryStore = map[string]memoryEntry{}
	common.RedisEnabled = redisEnabled
	if redisEnabled {
		server := miniredis.RunT(t)
		common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
		client := common.RDB
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldRDB; smsMemoryStore = oldStore })
}

func TestSMSCodeLifecycle(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")
			const phone = "13800138000"
			require.NoError(t, StoreSMSCode(SMSPurposeLogin, phone, "123456", time.Minute))
			assert.False(t, VerifySMSCode(SMSPurposeBind, phone, "123456"), "login codes cannot bind an account")
			assert.False(t, VerifySMSCode(SMSPurposeLogin, phone, "000000"))
			assert.True(t, VerifySMSCode(SMSPurposeLogin, phone, "123456"))
			assert.False(t, VerifySMSCode(SMSPurposeLogin, phone, "123456"), "a code can only be consumed once")
			assert.Positive(t, SMSResendWaitSeconds(SMSPurposeLogin, phone, 60), "verification must not bypass the resend cooldown")
		})
	}
}

func TestSMSCodeStopsGuessingAfterFiveFailures(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")
			const phone = "13800138001"
			require.NoError(t, StoreSMSCode(SMSPurposeLogin, phone, "123456", time.Minute))
			for attempt := 0; attempt < 5; attempt++ {
				require.False(t, VerifySMSCode(SMSPurposeLogin, phone, "000000"))
			}
			assert.False(t, VerifySMSCode(SMSPurposeLogin, phone, "123456"), "exhausted codes must be invalidated")
			require.NoError(t, StoreSMSCode(SMSPurposeLogin, phone, "654321", time.Minute))
			assert.True(t, VerifySMSCode(SMSPurposeLogin, phone, "654321"), "a fresh code has a fresh attempt budget")
		})
	}
}

func TestSMSCodeResendReservation(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")
			wait, err := StoreSMSCodeIfReady(SMSPurposeLogin, "13800138900", "123456", time.Minute, 60)
			require.NoError(t, err)
			require.Zero(t, wait)
			wait, err = StoreSMSCodeIfReady(SMSPurposeBind, "13800138900", "654321", time.Minute, 60)
			require.NoError(t, err)
			assert.Positive(t, wait, "resending for another purpose must also observe the same cooldown")
			assert.True(t, VerifySMSCode(SMSPurposeLogin, "13800138900", "123456"), "rejected resends must preserve the original code")
			assert.False(t, VerifySMSCode(SMSPurposeBind, "13800138900", "654321"))
		})
	}
}

func TestSMSCodeConcurrentConsumption(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")
			require.NoError(t, StoreSMSCode(SMSPurposeLogin, "13800138800", "123456", time.Minute))
			start := make(chan struct{})
			results := make(chan bool, 2)
			for request := 0; request < 2; request++ {
				go func() { <-start; results <- VerifySMSCode(SMSPurposeLogin, "13800138800", "123456") }()
			}
			close(start)
			first, second := <-results, <-results
			assert.NotEqual(t, first, second, "exactly one login may consume a code")
		})
	}
}

func TestSMSCodeExpiry(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")
			require.NoError(t, StoreSMSCode(SMSPurposeLogin, "13800138801", "123456", time.Minute))
			if backend == "redis" {
				require.NoError(t, common.RDB.PExpire(context.Background(), smsCodeKey(SMSPurposeLogin, "13800138801"), -time.Second).Err())
			} else {
				entry := smsMemoryStore[smsCodeKey(SMSPurposeLogin, "13800138801")]
				entry.expiresAt = time.Now().Add(-time.Second)
				smsMemoryStore[smsCodeKey(SMSPurposeLogin, "13800138801")] = entry
			}
			assert.False(t, VerifySMSCode(SMSPurposeLogin, "13800138801", "123456"))
		})
	}
}

func TestSMSDailyQuota(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useSMSCodeStore(t, backend == "redis")

			// 同一号码每天 2 条：第 3 条被拒，且被拒的请求不占用 IP 和全站额度。
			for i := 0; i < 2; i++ {
				_, scope := ReserveSMSDailyQuota("13800138000", "192.0.2.1", 10, 2, 10)
				require.Empty(t, scope)
			}
			_, scope := ReserveSMSDailyQuota("13800138000", "192.0.2.1", 10, 2, 10)
			assert.Equal(t, SMSDailyScopePhone, scope)

			// 同一 IP 每天 3 条：换号码也只能再发 1 条。
			_, scope = ReserveSMSDailyQuota("13800138001", "192.0.2.1", 10, 2, 3)
			require.Empty(t, scope)
			_, scope = ReserveSMSDailyQuota("13800138002", "192.0.2.1", 10, 2, 3)
			assert.Equal(t, SMSDailyScopeIP, scope)

			// 全站每天 4 条：前面成功占用了 3 条，换 IP 后只剩 1 条。
			release, scope := ReserveSMSDailyQuota("13800138003", "192.0.2.2", 4, 2, 3)
			require.Empty(t, scope)
			_, scope = ReserveSMSDailyQuota("13800138004", "192.0.2.3", 4, 2, 3)
			assert.Equal(t, SMSDailyScopeTotal, scope)

			// 发送失败归还额度后，全站额度重新可用。
			release()
			_, scope = ReserveSMSDailyQuota("13800138004", "192.0.2.3", 4, 2, 3)
			assert.Empty(t, scope)

			// 0 表示不限。
			for i := 0; i < 5; i++ {
				_, scope = ReserveSMSDailyQuota("13900139000", "192.0.2.9", 0, 0, 0)
				require.Empty(t, scope)
			}
		})
	}
}
