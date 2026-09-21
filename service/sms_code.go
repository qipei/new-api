package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	// SMSPurposeLogin 手机号登录（含首次登录自动注册）使用的验证码。
	SMSPurposeLogin = "login"
	// SMSPurposeBind 已登录用户绑定手机号使用的验证码。
	SMSPurposeBind = "bind"

	smsCodeKeyPrefix     = "sms_code:"
	smsSentAtKeyPrefix   = "sms_sent_at:"
	smsPhoneCountPrefix  = "sms_cnt_phone:"
	smsIPCountKeyPrefix  = "sms_cnt_ip:"
	smsDailyKeyPrefix    = "sms_daily:"
	smsMemoryGCThreshold = 512
)

// memoryEntry 是 Redis 不可用时的内存回退存储单元。
type memoryEntry struct {
	value     string
	expiresAt time.Time
	attempts  int
}

var (
	smsMemoryMutex sync.Mutex
	smsMemoryStore = map[string]memoryEntry{}
)

func memoryGet(key string) (string, bool) {
	smsMemoryMutex.Lock()
	defer smsMemoryMutex.Unlock()
	entry, ok := smsMemoryStore[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(smsMemoryStore, key)
		return "", false
	}
	return entry.value, true
}

// memoryIncr 在固定窗口内累加计数，窗口内首次写入时确定过期时间。
func memoryIncr(key string, ttl time.Duration) int {
	smsMemoryMutex.Lock()
	defer smsMemoryMutex.Unlock()
	now := time.Now()
	entry, ok := smsMemoryStore[key]
	if !ok || now.After(entry.expiresAt) {
		smsMemoryStore[key] = memoryEntry{value: "1", expiresAt: now.Add(ttl)}
		return 1
	}
	count, _ := strconv.Atoi(entry.value)
	count++
	entry.value = strconv.Itoa(count)
	smsMemoryStore[key] = entry
	return count
}

func memoryDecr(key string) {
	smsMemoryMutex.Lock()
	defer smsMemoryMutex.Unlock()
	entry, ok := smsMemoryStore[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return
	}
	count, _ := strconv.Atoi(entry.value)
	if count > 0 {
		entry.value = strconv.Itoa(count - 1)
		smsMemoryStore[key] = entry
	}
}

func memoryCount(key string) int {
	value, ok := memoryGet(key)
	if !ok {
		return 0
	}
	count, _ := strconv.Atoi(value)
	return count
}

func smsRedisUsable() bool {
	return common.RedisEnabled && common.RDB != nil
}

// GenerateSMSCode 生成指定位数的数字验证码，使用密码学安全随机源。
func GenerateSMSCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	digits := make([]byte, length)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = byte('0' + n.Int64())
	}
	return string(digits), nil
}

func smsCodeKey(purpose string, phone string) string {
	return smsCodeKeyPrefix + purpose + ":" + phone
}

func smsSentAtKey(phone string) string {
	return smsSentAtKeyPrefix + phone
}

// StoreSMSCode 保存一条待校验的验证码，同时记录发送时间用于重发间隔判断。
func StoreSMSCode(purpose string, phone string, code string, ttl time.Duration) error {
	_, err := StoreSMSCodeIfReady(purpose, phone, code, ttl, 0)
	return err
}

// StoreSMSCodeIfReady atomically reserves the phone's resend window and stores
// its code. A rejected reservation leaves the previous code unchanged.
func StoreSMSCodeIfReady(purpose, phone, code string, ttl time.Duration, intervalSeconds int) (int, error) {
	codeKey := smsCodeKey(purpose, phone)
	sentKey := smsSentAtKey(phone)
	now := time.Now()
	sentTTL := max(ttl, 10*time.Minute)
	if smsRedisUsable() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return common.RDB.Eval(ctx, `
local sent = redis.call('GET', KEYS[2])
if sent then
 local wait = tonumber(ARGV[4]) - (tonumber(ARGV[2]) - tonumber(sent))
 if wait > 0 then return wait end
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[5])
redis.call('DEL', KEYS[3])
return 0`, []string{codeKey, sentKey, codeKey + ":attempts"}, code, now.Unix(), ttl.Milliseconds(), intervalSeconds, sentTTL.Milliseconds()).Int()
	}
	smsMemoryMutex.Lock()
	defer smsMemoryMutex.Unlock()
	if sent, ok := smsMemoryStore[sentKey]; ok && now.Before(sent.expiresAt) {
		timestamp, _ := strconv.ParseInt(sent.value, 10, 64)
		if wait := int64(intervalSeconds) - (now.Unix() - timestamp); wait > 0 {
			return int(wait), nil
		}
	}
	if len(smsMemoryStore) > smsMemoryGCThreshold {
		for key, entry := range smsMemoryStore {
			if !now.Before(entry.expiresAt) {
				delete(smsMemoryStore, key)
			}
		}
	}
	smsMemoryStore[codeKey] = memoryEntry{value: code, expiresAt: now.Add(ttl)}
	smsMemoryStore[sentKey] = memoryEntry{value: strconv.FormatInt(now.Unix(), 10), expiresAt: now.Add(sentTTL)}
	return 0, nil
}

// VerifySMSCode atomically consumes a matching code. Five wrong attempts
// invalidate the code; neither success nor failure resets the send cooldown.
func VerifySMSCode(purpose string, phone string, code string) bool {
	if code == "" {
		return false
	}
	codeKey := smsCodeKey(purpose, phone)
	if smsRedisUsable() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := common.RDB.Eval(ctx, `
local stored = redis.call('GET', KEYS[1])
if not stored then return 0 end
if stored == ARGV[1] then
 redis.call('DEL', KEYS[1], KEYS[2])
 return 1
end
local attempts = redis.call('INCR', KEYS[2])
if attempts >= 5 then
 redis.call('DEL', KEYS[1], KEYS[2])
else
 redis.call('PEXPIRE', KEYS[2], redis.call('PTTL', KEYS[1]))
end
return 0`, []string{codeKey, codeKey + ":attempts"}, code).Int()
		return err == nil && result == 1
	}
	smsMemoryMutex.Lock()
	defer smsMemoryMutex.Unlock()
	entry, ok := smsMemoryStore[codeKey]
	if !ok {
		return false
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(smsMemoryStore, codeKey)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(entry.value), []byte(code)) == 1 {
		delete(smsMemoryStore, codeKey)
		return true
	}
	entry.attempts++
	if entry.attempts >= 5 {
		delete(smsMemoryStore, codeKey)
	} else {
		smsMemoryStore[codeKey] = entry
	}
	return false
}

// SMSResendWaitSeconds 返回距离下一次允许发送还剩多少秒，0 表示可以立即发送。
func SMSResendWaitSeconds(purpose string, phone string, intervalSeconds int) int {
	if intervalSeconds <= 0 {
		return 0
	}
	sentKey := smsSentAtKey(phone)

	var raw string
	if smsRedisUsable() {
		value, err := common.RedisGet(sentKey)
		if err != nil {
			return 0
		}
		raw = value
	} else {
		value, ok := memoryGet(sentKey)
		if !ok {
			return 0
		}
		raw = value
	}

	sentAt, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	elapsed := time.Now().Unix() - sentAt
	if elapsed >= int64(intervalSeconds) {
		return 0
	}
	return int(int64(intervalSeconds) - elapsed)
}

// smsWindowIncr 在固定窗口内累加一次计数并返回累加后的值。
func smsWindowIncr(key string, window time.Duration) int {
	if smsRedisUsable() {
		ctx := context.Background()
		count, err := common.RDB.Incr(ctx, key).Result()
		if err == nil {
			if count == 1 {
				_ = common.RDB.Expire(ctx, key, window).Err()
			}
			return int(count)
		}
		common.SysError(fmt.Sprintf("sms window counter incr failed, falling back to memory: %v", err))
	}
	return memoryIncr(key, window)
}

func smsWindowCount(key string) int {
	if smsRedisUsable() {
		value, err := common.RedisGet(key)
		if err == nil {
			count, _ := strconv.Atoi(value)
			return count
		}
		// Redis 里没有这个 key（尚未发送过）时按 0 计，不回退内存，
		// 否则集群下各实例的内存计数会把阈值判断拆散。
		return 0
	}
	return memoryCount(key)
}

// SMSSendCounts 返回窗口内该手机号与该 IP 已成功发送的次数。
func SMSSendCounts(phone string, ip string) (phoneCount int, ipCount int) {
	return smsWindowCount(smsPhoneCountPrefix + phone), smsWindowCount(smsIPCountKeyPrefix + ip)
}

// RecordSMSSent 在短信成功下发后累加手机号与 IP 两个维度的窗口计数。
func RecordSMSSent(phone string, ip string, windowSeconds int) {
	window := time.Duration(windowSeconds) * time.Second
	if window <= 0 {
		return
	}
	smsWindowIncr(smsPhoneCountPrefix+phone, window)
	if ip != "" {
		smsWindowIncr(smsIPCountKeyPrefix+ip, window)
	}
}

// SMS daily quota scopes reported by ReserveSMSDailyQuota when a limit is hit.
const (
	SMSDailyScopeTotal = "total"
	SMSDailyScopePhone = "phone"
	SMSDailyScopeIP    = "ip"
)

func smsWindowDecr(key string) {
	if smsRedisUsable() {
		if err := common.RDB.Decr(context.Background(), key).Err(); err == nil {
			return
		}
	}
	memoryDecr(key)
}

// ReserveSMSDailyQuota 在真正发送前占用当天的发送额度，按手机号、IP、全站三个维度计数，
// limit 为 0 的维度不限制。任一维度超限时回滚本次已占用的部分并返回超限维度；
// 成功时返回 release，发送失败或本次未发出时调用它归还额度。
func ReserveSMSDailyQuota(phone string, ip string, totalLimit int, phoneLimit int, ipLimit int) (release func(), exceededScope string) {
	day := time.Now().Format("20060102")
	counters := []struct {
		scope string
		key   string
		limit int
	}{
		{SMSDailyScopePhone, smsDailyKeyPrefix + day + ":phone:" + phone, phoneLimit},
		{SMSDailyScopeIP, smsDailyKeyPrefix + day + ":ip:" + ip, ipLimit},
		{SMSDailyScopeTotal, smsDailyKeyPrefix + day + ":total", totalLimit},
	}

	reserved := make([]string, 0, len(counters))
	release = func() {
		for _, key := range reserved {
			smsWindowDecr(key)
		}
		reserved = nil
	}
	for _, counter := range counters {
		if counter.limit <= 0 || (counter.scope == SMSDailyScopeIP && ip == "") {
			continue
		}
		reserved = append(reserved, counter.key)
		if smsWindowIncr(counter.key, 25*time.Hour) > counter.limit {
			release()
			return func() {}, counter.scope
		}
	}
	return release, ""
}
