package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Check-in is a daily action, so the shared-IP account count covers a rolling
// 24 hours. A short window lets a farm stay under the threshold by spacing out
// its accounts through the day.
const checkinIPWindowSeconds = int64(86400)

// Each member is a distinct authenticated user. The overflow marker bounds
// storage while conservatively keeping a saturated IP risky for a full window.
const checkinIPUsersScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local user = ARGV[3]
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local known = redis.call('ZSCORE', key, user)
local count = redis.call('ZCARD', key)
if ARGV[4] == '1' then
  if known or count < 100 then
    redis.call('ZADD', key, now, user)
  else
    redis.call('ZADD', key, now, 'overflow')
  end
  redis.call('EXPIRE', key, window)
  count = redis.call('ZCARD', key)
elseif not known then
  count = count + 1
end
if redis.call('ZSCORE', key, 'overflow') or count >= 100 then return 100 end
return count
`

type checkinIPUsers struct {
	sync.Mutex
	entries     map[string]map[int]int64
	nextCleanup int64
}

// User ID 0 is reserved as the local equivalent of the Redis overflow marker.
var checkinLocalIPUsers = &checkinIPUsers{entries: make(map[string]map[int]int64)}

func CheckinCaptchaRequired(ctx context.Context, userID int, ip string, recordAttempt bool) (bool, error) {
	return checkinCaptchaRequired(ctx, userID, ip, recordAttempt, time.Now().Unix())
}

func checkinCaptchaRequired(ctx context.Context, userID int, ip string, recordAttempt bool, now int64) (bool, error) {
	setting := *operation_setting.GetCheckinSetting()
	if setting.CaptchaMode != "adaptive" {
		return true, nil
	}
	if setting.CaptchaTrustDays < 1 || setting.CaptchaTrustDays > 30 || setting.CaptchaIPUserLimit < 2 || setting.CaptchaIPUserLimit > 100 {
		return true, errors.New("invalid check-in CAPTCHA policy")
	}
	count, err := checkinIPAccountCount(ctx, userID, ip, recordAttempt, now)
	if err != nil {
		return true, err
	}
	if count >= setting.CaptchaIPUserLimit {
		return true, nil
	}
	verifiedAt, err := model.GetCheckinCaptchaVerifiedAt(userID)
	if err != nil {
		return true, err
	}
	return verifiedAt <= 0 || verifiedAt > now || now-verifiedAt >= int64(setting.CaptchaTrustDays)*86400, nil
}

func checkinIPAccountCount(ctx context.Context, userID int, ip string, recordAttempt bool, now int64) (int, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || userID <= 0 {
		return 0, errors.New("invalid check-in user or IP")
	}
	// An IPv6 subscriber normally controls a whole /64 and can rotate addresses
	// freely inside it, so count the network rather than the single address.
	if parsedIP.To4() == nil {
		parsedIP = parsedIP.Mask(net.CIDRMask(64, 128))
	}
	ip = parsedIP.String()
	if common.RedisEnabled {
		if common.RDB == nil {
			return 0, errors.New("check-in risk Redis is unavailable")
		}
		key := fmt.Sprintf("checkin:ip-users:v1:%x", sha256.Sum256([]byte(ip)))
		record := "0"
		if recordAttempt {
			record = "1"
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return common.RDB.Eval(ctx, checkinIPUsersScript, []string{key}, now, checkinIPWindowSeconds, strconv.Itoa(userID), record).Int()
	}
	return checkinLocalIPUsers.accountCount(userID, ip, recordAttempt, now)
}

func (tracker *checkinIPUsers) accountCount(userID int, ip string, recordAttempt bool, now int64) (int, error) {
	tracker.Lock()
	defer tracker.Unlock()
	if now >= tracker.nextCleanup {
		for key, users := range tracker.entries {
			for id, seen := range users {
				if seen <= now-checkinIPWindowSeconds {
					delete(users, id)
				}
			}
			if len(users) == 0 {
				delete(tracker.entries, key)
			}
		}
		tracker.nextCleanup = now + 60
	}
	users := tracker.entries[ip]
	for id, seen := range users {
		if seen <= now-checkinIPWindowSeconds {
			delete(users, id)
		}
	}
	_, known := users[userID]
	if recordAttempt {
		if users == nil {
			// Never evict an active IP and accidentally grant a clean risk state.
			// When the table is full, treat the new IP as saturated: the user
			// solves a CAPTCHA instead of being unable to check in at all.
			if len(tracker.entries) >= 4096 {
				common.SysError("check-in risk cache is full; requiring CAPTCHA for untracked IPs")
				return 100, nil
			}
			users = make(map[int]int64)
			tracker.entries[ip] = users
		}
		if known || len(users) < 100 {
			users[userID] = now
		} else {
			users[0] = now
		}
	}
	if _, overflow := users[0]; overflow || len(users) >= 100 {
		return 100, nil
	}
	count := len(users)
	if !recordAttempt && !known {
		count++
	}
	return count, nil
}
