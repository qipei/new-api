package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// RequireSMSCaptchaForIP keeps a throttled IP behind verification even after
// the short request-rate window expires. It does not relax any sending limits.
func RequireSMSCaptchaForIP(ip string) {
	smsWindowIncr("sms_captcha_required_ip:"+ip, time.Duration(system_setting.GetSMSCaptchaSettings().EffectiveWindowSeconds())*time.Second)
}

// VerifySMSCaptcha 向腾讯云校验前端回传的验证码票据。票据一次性有效，
// 校验不通过时返回带原因的错误，由调用方转成面向用户的提示。
func VerifySMSCaptcha(ticket string, randstr string, userIP string) error {
	settings := system_setting.GetSMSCaptchaSettings()
	return VerifyTencentCaptcha(context.Background(), TencentCaptchaCredentials{
		AppID: settings.CaptchaAppId, AppSecretKey: settings.AppSecretKey,
		SecretID: settings.SecretId, SecretKey: settings.SecretKey,
	}, "web", ticket, randstr, userIP)
}

// SMSCaptchaRequired 判断这次获取验证码是否必须先过腾讯云验证码：
// IP 触发请求限流，或手机号、IP 的成功发送次数达到窗口阈值后，要求先验证。
func SMSCaptchaRequired(phone string, ip string) bool {
	settings := system_setting.GetSMSCaptchaSettings()
	if !settings.Enabled {
		return false
	}
	if smsWindowCount("sms_captcha_required_ip:"+ip) > 0 {
		return true
	}
	phoneCount, ipCount := SMSSendCounts(phone, ip)
	if settings.PhoneTriggerCount > 0 && phoneCount >= settings.PhoneTriggerCount {
		return true
	}
	if settings.IPTriggerCount > 0 && ipCount >= settings.IPTriggerCount {
		return true
	}
	return false
}
