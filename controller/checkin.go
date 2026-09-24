package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := model.GetUserCheckinStats(userId, month)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	provider, appID := "none", ""
	if common.TurnstileCheckEnabled {
		provider = "turnstile"
	}
	if setting.CaptchaEnabled {
		// Tencent owns check-in protection while enabled, including the
		// exemption period; do not fall back to global Turnstile during it.
		provider = "none"
		if stats["checked_in_today"] != true {
			client := "web"
			if c.Query("captcha_client") == "mini_program" {
				client = "mini_program"
			}
			if !system_setting.GetSMSCaptchaSettings().ConfiguredForClient(client) {
				c.JSON(http.StatusOK, gin.H{"success": false, "code": "CAPTCHA_NOT_CONFIGURED", "message": "签到验证码尚未配置完整，请联系管理员"})
				return
			}
			required, err := service.CheckinCaptchaRequired(c.Request.Context(), userId, c.ClientIP(), false)
			if err != nil {
				common.SysError("check-in CAPTCHA status failed: " + err.Error())
				common.ApiErrorMsg(c, "签到安全检查暂不可用，请稍后重试")
				return
			}
			if required {
				provider = "tencent"
				appID = system_setting.GetSMSCaptchaSettings().CaptchaAppId
				if client == "mini_program" {
					appID = system_setting.GetSMSCaptchaSettings().MiniAppID
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":          setting.Enabled,
			"min_quota":        setting.MinQuota,
			"max_quota":        setting.MaxQuota,
			"captcha_provider": provider,
			"captcha_app_id":   appID,
			"stats":            stats,
		},
	})
}

// DoCheckin 执行用户签到
func DoCheckin(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}

	userId := c.GetInt("id")

	checkin, err := model.UserCheckin(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "签到成功",
		"data": gin.H{
			"quota_awarded": checkin.QuotaAwarded,
			"checkin_date":  checkin.CheckinDate},
	})
}
