package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"

	captcha "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/captcha/v20190722"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// tencentCaptchaTypeSlide 是腾讯云滑块验证码的 CaptchaType，网页端接入用的就是这一种。
const tencentCaptchaTypeSlide = 9

// tencentCaptchaPassCode 是 DescribeCaptchaResult 返回的验证通过码。
const tencentCaptchaPassCode = 1

// ErrCaptchaNotConfigured 表示管理员开启了验证码防刷但没有填全腾讯云凭据。
var ErrCaptchaNotConfigured = errors.New("captcha is not configured")

// RequireSMSCaptchaForIP keeps a throttled IP behind verification even after
// the short request-rate window expires. It does not relax any sending limits.
func RequireSMSCaptchaForIP(ip string) {
	smsWindowIncr("sms_captcha_required_ip:"+ip, time.Duration(system_setting.GetSMSCaptchaSettings().EffectiveWindowSeconds())*time.Second)
}

// VerifySMSCaptcha 向腾讯云校验前端回传的验证码票据。票据一次性有效，
// 校验不通过时返回带原因的错误，由调用方转成面向用户的提示。
func VerifySMSCaptcha(ticket string, randstr string, userIP string) error {
	settings := system_setting.GetSMSCaptchaSettings()
	if !settings.Configured() {
		return ErrCaptchaNotConfigured
	}
	ticket = strings.TrimSpace(ticket)
	randstr = strings.TrimSpace(randstr)
	if ticket == "" || randstr == "" {
		return errors.New("captcha ticket is missing")
	}

	appId, err := strconv.ParseUint(strings.TrimSpace(settings.CaptchaAppId), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid captcha app id: %w", err)
	}

	credential := tccommon.NewCredential(strings.TrimSpace(settings.SecretId), strings.TrimSpace(settings.SecretKey))
	client, err := captcha.NewClient(credential, "", profile.NewClientProfile())
	if err != nil {
		return err
	}

	request := captcha.NewDescribeCaptchaResultRequest()
	request.CaptchaType = tccommon.Uint64Ptr(tencentCaptchaTypeSlide)
	request.Ticket = tccommon.StringPtr(ticket)
	request.Randstr = tccommon.StringPtr(randstr)
	request.UserIp = tccommon.StringPtr(userIP)
	request.CaptchaAppId = tccommon.Uint64Ptr(appId)
	request.AppSecretKey = tccommon.StringPtr(strings.TrimSpace(settings.AppSecretKey))

	response, err := client.DescribeCaptchaResult(request)
	if err != nil {
		return err
	}
	if response == nil || response.Response == nil || response.Response.CaptchaCode == nil {
		return errors.New("empty response from captcha provider")
	}
	if *response.Response.CaptchaCode != tencentCaptchaPassCode {
		reason := ""
		if response.Response.CaptchaMsg != nil {
			reason = *response.Response.CaptchaMsg
		}
		return fmt.Errorf("captcha verification failed: code=%d %s", *response.Response.CaptchaCode, reason)
	}
	return nil
}

// SMSCaptchaRequired 判断这次获取验证码是否必须先过腾讯云验证码：
// IP 触发请求限流，或手机号、IP 的成功发送次数达到窗口阈值后，要求先验证。
func SMSCaptchaRequired(phone string, ip string) bool {
	settings := system_setting.GetSMSCaptchaSettings()
	if !settings.Enabled || !settings.Configured() {
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
