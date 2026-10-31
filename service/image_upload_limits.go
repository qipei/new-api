package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/object_storage"
)

// Requests reserve before body reads; bytes reserve after validation. Failures are not refunded.
// Same Redis hash tag keeps a user's keys in one cluster slot.
const uploadLimitScript = `
local requests = tonumber(redis.call('GET', KEYS[1]) or '0')
local bytes = tonumber(redis.call('GET', KEYS[2]) or '0')
if tonumber(ARGV[4]) == 1 and requests >= tonumber(ARGV[1]) then return 1 end
if bytes + tonumber(ARGV[2]) > tonumber(ARGV[3]) or (tonumber(ARGV[4]) == 1 and bytes >= tonumber(ARGV[3])) then return 2 end
if tonumber(ARGV[4]) == 1 then redis.call('INCR', KEYS[1]); redis.call('EXPIRE', KEYS[1], 120) end
if tonumber(ARGV[2]) > 0 then redis.call('INCRBY', KEYS[2], ARGV[2]); redis.call('EXPIRE', KEYS[2], 172800) end
return 0`

type imageUploadUsage struct {
	minute, day int64
	requests    int
	bytes       int64
	expires     time.Time
}
type imageUploadLimiter struct {
	sync.Mutex
	users       map[int]imageUploadUsage
	nextCleanup time.Time
}

var localImageUploadLimits = imageUploadLimiter{users: make(map[int]imageUploadUsage)}

func ReserveImageUploadRequest(ctx context.Context, userID int, cfg object_storage.Config) (int, error) {
	return reserveImageUploadBudget(ctx, userID, 0, true, cfg)
}
func ReserveImageUploadBytes(ctx context.Context, userID int, size int64, cfg object_storage.Config) (int, error) {
	return reserveImageUploadBudget(ctx, userID, size, false, cfg)
}
func reserveImageUploadBudget(ctx context.Context, userID int, size int64, request bool, cfg object_storage.Config) (int, error) {
	if size < 0 || size > 20<<20 {
		return 0, fmt.Errorf("invalid upload size")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	countRequest := 0
	if request {
		countRequest = 1
	}
	now := time.Now().UTC()
	minute := now.Unix() / 60
	day := now.Unix() / 86400
	if common.RedisEnabled {
		if common.RDB == nil {
			return 0, fmt.Errorf("upload limit storage unavailable")
		}
		keys := []string{fmt.Sprintf("image-upload:{%d}:minute:%d", userID, minute), fmt.Sprintf("image-upload:{%d}:day:%d", userID, day)}
		code, err := common.RDB.Eval(ctx, uploadLimitScript, keys, cfg.RequestsPerMinute, size, int64(cfg.DailyLimitMB)<<20, countRequest).Int()
		return code, err
	}
	return localImageUploadLimits.reserve(userID, size, request, cfg, now), nil
}

func (l *imageUploadLimiter) reserve(userID int, size int64, request bool, cfg object_storage.Config, now time.Time) int {
	l.Lock()
	defer l.Unlock()
	if !now.Before(l.nextCleanup) {
		for id, u := range l.users {
			if !now.Before(u.expires) {
				delete(l.users, id)
			}
		}
		l.nextCleanup = now.Add(time.Minute)
	}
	u := l.users[userID]
	minute := now.Unix() / 60
	day := now.Unix() / 86400
	if u.minute != minute {
		u.requests = 0
		u.minute = minute
	}
	if u.day != day {
		u.bytes = 0
		u.day = day
	}
	if request && u.requests >= cfg.RequestsPerMinute {
		return 1
	}
	if u.bytes+size > int64(cfg.DailyLimitMB)<<20 || (request && u.bytes >= int64(cfg.DailyLimitMB)<<20) {
		return 2
	}
	if request {
		u.requests++
	}
	u.bytes += size
	u.expires = now.Add(48 * time.Hour)
	l.users[userID] = u
	return 0
}
