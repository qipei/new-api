package middleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// 短信验证码独立一个限流桶，不与邮箱验证码互相挤占额度。
const (
	SMSVerificationRateLimitMark = "SMS"
	SMSVerificationMaxRequests   = 2  // 30秒内最多2次
	SMSVerificationDuration      = 30 // 30秒时间窗口
)

// rejectThrottledSMSRequest 拒绝本次请求；腾讯云验证码可用时同时要求该 IP 后续先过验证，
// 客户端可以在等待期间完成验证，但被拒绝的这次请求绝不会到达发送逻辑。
func rejectThrottledSMSRequest(c *gin.Context, waitSeconds int) {
	data := gin.H{"resend_after": waitSeconds}
	settings := system_setting.GetSMSCaptchaSettings()
	if common.PhoneLoginEnabled && settings.Enabled && settings.Configured() {
		service.RequireSMSCaptchaForIP(c.ClientIP())
		data["require_captcha"] = true
		data["captcha_app_id"] = settings.CaptchaAppId
	}
	c.Header("Retry-After", strconv.Itoa(waitSeconds))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": fmt.Sprintf("发送过于频繁，请等待 %d 秒后再试", waitSeconds),
		"data":    data,
	})
}

func memorySMSVerificationRateLimiter(c *gin.Context) {
	key := SMSVerificationRateLimitMark + ":" + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, SMSVerificationMaxRequests, SMSVerificationDuration) {
		rejectThrottledSMSRequest(c, SMSVerificationDuration)
		return
	}
	c.Next()
}

func redisSMSVerificationRateLimiter(c *gin.Context) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(
		c.Request.Context(),
		redisIPRateLimitKey(SMSVerificationRateLimitMark, c.ClientIP()),
		SMSVerificationMaxRequests,
		SMSVerificationDuration,
	)
	if err != nil {
		memorySMSVerificationRateLimiter(c)
		return
	}
	if allowed {
		c.Next()
		return
	}
	waitSeconds := int64(SMSVerificationDuration)
	if ttlSeconds > 0 {
		waitSeconds = ttlSeconds
	}
	rejectThrottledSMSRequest(c, int(waitSeconds))
}

// SMSVerificationRateLimit 按 IP 限制短信验证码的请求频率，登录发码与绑定发码共用。
func SMSVerificationRateLimit() gin.HandlerFunc {
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		if common.RedisEnabled {
			redisSMSVerificationRateLimiter(c)
		} else {
			memorySMSVerificationRateLimiter(c)
		}
	}
}
