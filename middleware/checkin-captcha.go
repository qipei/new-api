package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// CheckinRateLimit limits both challenge requests and verification attempts,
// regardless of which CAPTCHA provider is enabled.
func CheckinRateLimit() gin.HandlerFunc {
	return userRateLimitFactory(6, 60, "checkin")
}

func CheckinIPRateLimit() gin.HandlerFunc {
	return rateLimitFactory(60, 60, "checkin")
}

// CheckinCaptcha replaces the global Turnstile check only when the independent
// Tencent switch is enabled. No client parameter can disable verification.
func CheckinCaptcha() gin.HandlerFunc {
	turnstile := TurnstileCheck()
	return func(c *gin.Context) {
		setting := operation_setting.GetCheckinSetting()
		if !setting.Enabled {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "message": "签到功能未启用"})
			return
		}
		// Avoid paid verification for an already completed check-in. Settlement
		// still checks again and the unique constraint protects concurrent awards.
		checked, err := model.HasCheckedInToday(c.GetInt("id"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "message": "签到状态查询失败，请稍后重试"})
			return
		}
		if checked {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "code": "CHECKIN_ALREADY_DONE", "message": "今日已签到"})
			return
		}
		if !setting.CaptchaEnabled {
			turnstile(c)
			return
		}
		var request struct {
			Client  string `json:"captcha_client"`
			Ticket  string `json:"captcha_ticket"`
			Randstr string `json:"captcha_randstr"`
		}
		if c.Request.Body != nil && c.Request.ContentLength != 0 {
			if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024), &request); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "CAPTCHA_INVALID_REQUEST", "message": "验证码请求格式错误"})
				return
			}
		}
		if request.Client == "" {
			request.Client = "web"
		}
		if request.Client != "web" && request.Client != "mini_program" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "CAPTCHA_INVALID_REQUEST", "message": "不支持的验证码客户端类型"})
			return
		}
		shared := system_setting.GetSMSCaptchaSettings()
		credentials := service.TencentCaptchaCredentials{AppID: shared.CaptchaAppId, AppSecretKey: shared.AppSecretKey, SecretID: shared.SecretId, SecretKey: shared.SecretKey}
		if request.Client == "mini_program" {
			credentials.AppID, credentials.AppSecretKey = system_setting.GetSMSCaptchaSettings().MiniAppID, system_setting.GetSMSCaptchaSettings().MiniAppSecretKey
		}
		if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.AppSecretKey) == "" || strings.TrimSpace(credentials.SecretID) == "" || strings.TrimSpace(credentials.SecretKey) == "" {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "code": "CAPTCHA_NOT_CONFIGURED", "message": "签到验证码尚未配置完整，请联系管理员"})
			return
		}
		challenge := gin.H{"require_captcha": true, "captcha_provider": "tencent", "captcha_app_id": credentials.AppID, "captcha_client": request.Client}
		if strings.TrimSpace(request.Ticket) == "" || (request.Client == "web" && strings.TrimSpace(request.Randstr) == "") {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "code": "CAPTCHA_REQUIRED", "message": i18n.T(c, i18n.MsgSMSCaptchaRequired), "data": challenge})
			return
		}
		if err := service.VerifyTencentCaptcha(c.Request.Context(), credentials, request.Client, request.Ticket, request.Randstr, c.ClientIP()); err != nil {
			c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "code": "CAPTCHA_FAILED", "message": i18n.T(c, i18n.MsgSMSCaptchaFailed), "data": challenge})
			return
		}
		c.Next()
	}
}
