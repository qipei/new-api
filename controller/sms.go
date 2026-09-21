package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type smsCodeRequest struct {
	Phone          string `json:"phone"`
	CaptchaTicket  string `json:"captcha_ticket"`
	CaptchaRandstr string `json:"captcha_randstr"`
}

type phoneLoginRequest struct {
	Phone   string `json:"phone"`
	Code    string `json:"code"`
	AffCode string `json:"aff_code"`
}

type phoneBindRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// SendLoginSMSCode 下发手机号登录（含首次登录自动注册）所需的验证码。
func SendLoginSMSCode(c *gin.Context) {
	sendSMSCode(c, service.SMSPurposeLogin, 0)
}

// SendBindSMSCode 下发已登录用户绑定手机号所需的验证码。
func SendBindSMSCode(c *gin.Context) {
	sendSMSCode(c, service.SMSPurposeBind, c.GetInt("id"))
}

// sendSMSCode 是两种用途共用的验证码下发流程：先做号码与配置校验，
// 再按阈值决定是否要求先过腾讯云验证码，最后才真正调用短信通道并计数。
func sendSMSCode(c *gin.Context, purpose string, currentUserId int) {
	if !common.PhoneLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgPhoneLoginDisabled)
		return
	}

	var req smsCodeRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	phone := model.NormalizePhone(req.Phone)
	if !model.IsValidPhone(phone) {
		common.ApiErrorI18n(c, i18n.MsgPhoneInvalid)
		return
	}

	settings := system_setting.GetSMSSettings()
	if !settings.Configured() {
		common.ApiErrorI18n(c, i18n.MsgSMSNotConfigured)
		return
	}

	// skipDelivery 为 true 时照常走完频控和验证码流程并返回成功，但不真正下发短信。
	// 用于已禁用账号：既不浪费短信，也不让发码接口暴露哪些号码被封禁（登录时仍会被拒绝）。
	skipDelivery := false
	// 号码状态不满足时提前拒绝，避免白白发出一条短信。
	switch purpose {
	case service.SMSPurposeLogin:
		user, err := model.GetUserByPhone(phone)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
		case err != nil:
			common.ApiError(c, err)
			return
		case user.Status != common.UserStatusEnabled:
			skipDelivery = true
		}
	case service.SMSPurposeBind:
		if currentUserId <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "not authenticated"})
			return
		}
		if err := model.EnsurePhoneAvailable(phone, currentUserId); err != nil {
			if errors.Is(err, model.ErrPhoneAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgPhoneAlreadyTaken)
				return
			}
			common.ApiError(c, err)
			return
		}
	default:
		common.ApiErrorI18n(c, i18n.MsgSMSPurposeUnknown)
		return
	}

	if wait := service.SMSResendWaitSeconds(purpose, phone, settings.EffectiveResendInterval()); wait > 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgSMSTooFrequent, map[string]any{"Seconds": wait}), "data": gin.H{"resend_after": wait}})
		return
	}

	captchaSettings := system_setting.GetSMSCaptchaSettings()
	clientIP := c.ClientIP()
	if captchaSettings.Enabled {
		if !captchaSettings.Configured() {
			common.ApiErrorI18n(c, i18n.MsgSMSCaptchaFailed)
			return
		}
		if req.CaptchaTicket != "" {
			if err := service.VerifySMSCaptcha(req.CaptchaTicket, req.CaptchaRandstr, clientIP); err != nil {
				common.SysLog(fmt.Sprintf("sms captcha verification failed for %s: %v", model.MaskPhone(phone), err))
				common.ApiErrorI18n(c, i18n.MsgSMSCaptchaFailed)
				return
			}
		} else if service.SMSCaptchaRequired(phone, clientIP) {
			// 达到频控阈值，先让前端拉起验证码再带着票据重试，本次不发短信。
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": i18n.T(c, i18n.MsgSMSCaptchaRequired),
				"data": gin.H{
					"require_captcha": true,
					"captcha_app_id":  captchaSettings.CaptchaAppId,
				},
			})
			return
		}
	}

	code := settings.DebugCode
	if !settings.LocalOnly {
		generated, err := service.GenerateSMSCode(settings.EffectiveCodeLength())
		if err != nil {
			common.ApiError(c, err)
			return
		}
		code = generated
	}

	// 每日额度只约束真正会产生费用的发送，调试模式和不下发的请求不占用。
	releaseQuota := func() {}
	if !skipDelivery && !settings.LocalOnly {
		release, exceededScope := service.ReserveSMSDailyQuota(phone, clientIP, settings.DailyLimit, settings.PhoneDailyLimit, settings.IPDailyLimit)
		switch exceededScope {
		case "":
			releaseQuota = release
		case service.SMSDailyScopeTotal:
			common.SysError(fmt.Sprintf("sms daily total limit %d reached, further codes are refused until tomorrow", settings.DailyLimit))
			common.ApiErrorI18n(c, i18n.MsgSMSDailyExhausted)
			return
		default:
			common.ApiErrorI18n(c, i18n.MsgSMSDailyLimit)
			return
		}
	}

	expireMinutes := settings.EffectiveExpireMinutes()
	wait, err := service.StoreSMSCodeIfReady(purpose, phone, code, time.Duration(expireMinutes)*time.Minute, settings.EffectiveResendInterval())
	if err != nil {
		releaseQuota()
		common.ApiError(c, err)
		return
	}
	if wait > 0 {
		releaseQuota()
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgSMSTooFrequent, map[string]any{"Seconds": wait}), "data": gin.H{"resend_after": wait}})
		return
	}

	if skipDelivery {
		common.SysLog(fmt.Sprintf("sms code for disabled account %s not delivered", model.MaskPhone(phone)))
	} else if err := service.SendSMSCode(phone, code); err != nil {
		releaseQuota()
		common.SysError(fmt.Sprintf("failed to send sms code to %s: %v", model.MaskPhone(phone), err))
		if errors.Is(err, service.ErrSMSNotConfigured) {
			common.ApiErrorI18n(c, i18n.MsgSMSNotConfigured)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgSMSSendFailed)
		return
	}

	service.RecordSMSSent(phone, clientIP, captchaSettings.EffectiveWindowSeconds())

	data := gin.H{
		"require_captcha": false,
		"expires_in":      expireMinutes * 60,
		"resend_after":    settings.EffectiveResendInterval(),
	}
	// 调试模式不会真的发出短信，把验证码回给前端，否则本地无法完成登录流程。
	if settings.LocalOnly {
		data["debug_code"] = code
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

// PhoneLogin 手机号 + 短信验证码登录。开启手机号登录即允许验证后自动建号，独立于普通注册开关。
func PhoneLogin(c *gin.Context) {
	if !common.PhoneLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgPhoneLoginDisabled)
		return
	}

	var req phoneLoginRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	phone := model.NormalizePhone(req.Phone)
	if !model.IsValidPhone(phone) {
		common.ApiErrorI18n(c, i18n.MsgPhoneInvalid)
		return
	}
	if !service.VerifySMSCode(service.SMSPurposeLogin, phone, req.Code) {
		common.ApiErrorI18n(c, i18n.MsgPhoneCodeError)
		return
	}

	user, err := model.GetUserByPhone(phone)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		inviterId := 0
		if req.AffCode != "" {
			inviterId, _ = model.GetUserIdByAffCode(req.AffCode)
		}
		user, _, err = model.RegisterUserByPhone(phone, inviterId)
		if err != nil {
			if errors.Is(err, model.ErrPhoneAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgPhoneAlreadyTaken)
				return
			}
			common.ApiError(c, err)
			return
		}
	} else if err != nil {
		common.ApiError(c, err)
		return
	}

	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserDisabled)
		return
	}

	if issue2FAChallenge(user, c) {
		return
	}

	setupLogin(user, c)
}

// BindPhone 给当前登录用户绑定手机号，绑定后即可用手机号验证码登录。
func BindPhone(c *gin.Context) {
	if !common.PhoneLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgPhoneLoginDisabled)
		return
	}

	var req phoneBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	phone := model.NormalizePhone(req.Phone)
	if !model.IsValidPhone(phone) {
		common.ApiErrorI18n(c, i18n.MsgPhoneInvalid)
		return
	}
	if !service.VerifySMSCode(service.SMSPurposeBind, phone, req.Code) {
		common.ApiErrorI18n(c, i18n.MsgPhoneCodeError)
		return
	}

	user := model.User{Id: c.GetInt("id")}
	if user.Id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "not authenticated"})
		return
	}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.BindPhoneToUser(&user, phone); err != nil {
		switch {
		case errors.Is(err, model.ErrPhoneAlreadyTaken):
			common.ApiErrorI18n(c, i18n.MsgPhoneAlreadyTaken)
		case errors.Is(err, model.ErrPhoneInvalid):
			common.ApiErrorI18n(c, i18n.MsgPhoneInvalid)
		default:
			common.ApiError(c, err)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
