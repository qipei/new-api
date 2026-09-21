package system_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// SMSSettings 阿里云短信服务配置，用于下发手机号登录验证码。
type SMSSettings struct {
	LocalOnly         bool   `json:"local_only"`          // 调试模式：不真正调用阿里云，直接返回 DebugCode
	DebugCode         string `json:"debug_code"`          // LocalOnly 为 true 时返回给登录流程的验证码
	Endpoint          string `json:"endpoint"`            // 阿里云短信 API 地址
	AccessKeyId       string `json:"access_key_id"`       // 阿里云 AccessKey ID
	AccessKeySecret   string `json:"access_key_secret"`   // 阿里云 AccessKey Secret
	SignName          string `json:"sign_name"`           // 已申请的阿里云短信签名
	TemplateCode      string `json:"template_code"`       // 登录验证码模板，变量为 code
	CodeLength        int    `json:"code_length"`         // 验证码位数
	CodeExpireMinutes int    `json:"code_expire_minutes"` // 验证码有效期（分钟）
	ResendIntervalSec int    `json:"resend_interval_sec"` // 同一手机号两次获取验证码的最小间隔（秒）
	DailyLimit        int    `json:"daily_limit"`         // 全站每天最多发送多少条，0 表示不限
	PhoneDailyLimit   int    `json:"phone_daily_limit"`   // 同一手机号每天最多发送多少条，0 表示不限
	IPDailyLimit      int    `json:"ip_daily_limit"`      // 同一 IP 每天最多发送多少条，0 表示不限
}

var defaultSMSSettings = SMSSettings{
	LocalOnly:         false,
	DebugCode:         "123456",
	Endpoint:          "https://dysmsapi.aliyuncs.com/",
	AccessKeyId:       "",
	AccessKeySecret:   "",
	SignName:          "",
	TemplateCode:      "",
	CodeLength:        6,
	CodeExpireMinutes: 10,
	ResendIntervalSec: 60,
	DailyLimit:        1000,
	PhoneDailyLimit:   10,
	IPDailyLimit:      50,
}

// SMSCaptchaSettings 腾讯云验证码配置，用于短信下发前的防刷校验。
type SMSCaptchaSettings struct {
	Enabled           bool   `json:"enabled"`             // 是否启用腾讯云验证码防刷
	CaptchaAppId      string `json:"captcha_app_id"`      // 腾讯云验证码应用 ID，会下发给前端用于拉起验证码
	AppSecretKey      string `json:"app_secret_key"`      // 腾讯云验证码 AppSecretKey，仅服务端校验票据使用，不下发给前端
	SecretId          string `json:"secret_id"`           // 腾讯云账号 OpenAPI SecretId，用于调用 DescribeCaptchaResult 签名
	SecretKey         string `json:"secret_key"`          // 腾讯云账号 OpenAPI SecretKey，用于调用 DescribeCaptchaResult 签名
	PhoneTriggerCount int    `json:"phone_trigger_count"` // 同一手机号在窗口时间内成功发送达到该次数后，下次获取验证码先弹安全验证
	IPTriggerCount    int    `json:"ip_trigger_count"`    // 同一 IP 在窗口时间内成功发送达到该次数后，下次获取验证码先弹安全验证
	WindowSeconds     int    `json:"window_seconds"`      // 频控计数保留多久
}

var defaultSMSCaptchaSettings = SMSCaptchaSettings{
	Enabled:           false,
	CaptchaAppId:      "",
	AppSecretKey:      "",
	SecretId:          "",
	SecretKey:         "",
	PhoneTriggerCount: 3,
	IPTriggerCount:    8,
	WindowSeconds:     600,
}

func init() {
	config.GlobalConfig.Register("sms", &defaultSMSSettings)
	config.GlobalConfig.Register("sms_captcha", &defaultSMSCaptchaSettings)
}

func GetSMSSettings() *SMSSettings {
	return &defaultSMSSettings
}

func GetSMSCaptchaSettings() *SMSCaptchaSettings {
	return &defaultSMSCaptchaSettings
}

// Configured 返回短信通道是否已经具备下发能力。调试模式下不需要阿里云凭据。
func (s *SMSSettings) Configured() bool {
	if s.LocalOnly {
		return strings.TrimSpace(s.DebugCode) != ""
	}
	return strings.TrimSpace(s.AccessKeyId) != "" &&
		strings.TrimSpace(s.AccessKeySecret) != "" &&
		strings.TrimSpace(s.SignName) != "" &&
		strings.TrimSpace(s.TemplateCode) != ""
}

// EffectiveCodeLength 把配置收敛到阿里云模板可用的位数范围内。
func (s *SMSSettings) EffectiveCodeLength() int {
	if s.CodeLength < 4 {
		return 4
	}
	if s.CodeLength > 8 {
		return 8
	}
	return s.CodeLength
}

// EffectiveExpireMinutes 保证验证码始终有一个可用的有效期。
func (s *SMSSettings) EffectiveExpireMinutes() int {
	if s.CodeExpireMinutes <= 0 {
		return 10
	}
	if s.CodeExpireMinutes > 60 {
		return 60
	}
	return s.CodeExpireMinutes
}

// EffectiveResendInterval 同一手机号重复获取验证码的最小间隔。
func (s *SMSSettings) EffectiveResendInterval() int {
	if s.ResendIntervalSec < 0 {
		return 0
	}
	if s.ResendIntervalSec > 600 {
		return 600
	}
	return s.ResendIntervalSec
}

// EffectiveWindowSeconds 频控计数窗口，0 或负数表示不做窗口统计。
func (c *SMSCaptchaSettings) EffectiveWindowSeconds() int {
	if c.WindowSeconds <= 0 {
		return 600
	}
	return c.WindowSeconds
}

// Configured 腾讯云验证码需要同时具备票据校验凭据才算配置完成。
func (c *SMSCaptchaSettings) Configured() bool {
	return strings.TrimSpace(c.CaptchaAppId) != "" &&
		strings.TrimSpace(c.AppSecretKey) != "" &&
		strings.TrimSpace(c.SecretId) != "" &&
		strings.TrimSpace(c.SecretKey) != ""
}
