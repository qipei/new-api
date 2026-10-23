package controller

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

func GetRewardedAdSettings(c *gin.Context) {
	cfg, err := model.GetRewardedAdSettingFromDB()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data := map[string]any{"s2s_auth_mode": cfg.EffectiveBridgeAuthMode(), "s2s_key_configured": cfg.S2SAuthKey != "", "enabled": cfg.Enabled, "reward_quota": cfg.RewardQuota, "daily_limit": cfg.DailyLimit, "cooldown_seconds": cfg.CooldownSeconds, "daily_budget_quota": cfg.DailyBudgetQuota, "adpid": cfg.Adpid, "wechat_app_id": cfg.WechatAppID, "dcloud_app_id": cfg.DCloudAppID, "bridge_url": cfg.BridgeURL, "bridge_auth_mode": cfg.EffectiveBridgeAuthMode(), "secret_configured": cfg.Secret != "", "callback_key_configured": cfg.CallbackAuthKey != "", "wechat_secret_configured": cfg.WechatAppSecret != "", "bridge_key_configured": cfg.BridgeAuthKey != "", "callback_url": strings.TrimRight(system_setting.ServerAddress, "/") + "/api/rewarded-ad/dcloud/callback", "session_ttl_seconds": operation_setting.RewardedAdSessionTTL}
	common.ApiSuccess(c, data)
}
func SaveRewardedAdSettings(c *gin.Context) {
	var cfg operation_setting.RewardedAdSetting
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16384), &cfg); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "配置格式无效"})
		return
	}
	if err := model.ExpireRewardedAdSessions(time.Now()); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SaveRewardedAdSetting(cfg); err != nil {
		status := 400
		if errors.Is(err, model.ErrRewardedAdInFlight) {
			status = 409
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	GetRewardedAdSettings(c)
}
func rewardedAdError(c *gin.Context, err error) {
	code := err.Error()
	status := 503
	message := "广告服务暂不可用，请稍后重试"
	switch code {
	case "disabled":
		status = 409
		message = "广告奖励未启用"
	case "invalid_request":
		status = 400
		message = "请求参数无效"
	case "account_unavailable":
		status = 403
		message = "账号暂不可用"
	case "identity_required":
		status = 409
		message = "请先初始化微信广告身份"
	case "daily_limit":
		status = 409
		message = "今日奖励次数已用完"
	case "cooldown":
		status = 409
		message = "请稍后再领取"
	case "session_pending":
		status = 409
		message = "请先查询未结束的观看会话"
	case "budget_exhausted":
		status = 409
		message = "今日广告奖励暂不可领取"
	default:
		code = "service_unavailable"
	}
	response := gin.H{"success": false, "code": code, "message": message}
	if code == "cooldown" || code == "session_pending" {
		state, e := service.GetRewardedAdState(c.GetInt("id"), time.Now())
		if e == nil {
			if code == "cooldown" {
				response["data"] = gin.H{"retry_after": state["cooldown_seconds"]}
			} else if pending, ok := state["pending_session"].(map[string]any); ok {
				response["data"] = gin.H{"session_id": pending["session_id"]}
			} else if state["reason"] == "session_pending" {
				response["message"] = "同一微信账号在其他账号下有未结束的观看，请等待该会话结束"
			}
		}
	}
	c.JSON(status, response)
}
func GetRewardedAdState(c *gin.Context) {
	data, err := service.GetRewardedAdState(c.GetInt("id"), time.Now())
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
func InitializeRewardedAdIdentity(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 2048), &req); err != nil || req.Code == "" {
		rewardedAdError(c, errors.New("invalid_request"))
		return
	}
	cfg, err := model.GetRewardedAdSettingFromDB()
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	if !cfg.Enabled {
		rewardedAdError(c, errors.New("disabled"))
		return
	}
	data, err := service.InitializeRewardedAdIdentity(c.Request.Context(), c.GetInt("id"), req.Code)
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
func CreateRewardedAdSession(c *gin.Context) {
	var req struct {
		RequestID string `json:"request_id"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 2048), &req); err != nil {
		rewardedAdError(c, errors.New("invalid_request"))
		return
	}
	cfg := operation_setting.GetRewardedAdSetting()
	cfg.Enabled = cfg.Enabled && cfg.Configured()
	session, err := model.CreateRewardedAdSession(c.GetInt("id"), req.RequestID, cfg, time.Now())
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	data, err := service.RewardedAdSessionView(session, true)
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
func GetRewardedAdSession(c *gin.Context) {
	var session model.RewardedAdSession
	err := model.DB.Where("id = ? AND user_id = ?", c.Param("id"), c.GetInt("id")).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"success": false, "code": "session_not_found", "message": "观看会话不存在"})
		return
	}
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	data, err := service.RewardedAdSessionView(&session, false)
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

// FinishRewardedAdSession ends only the authenticated viewer's reservation.
func FinishRewardedAdSession(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 2048), &req); err != nil {
		rewardedAdError(c, errors.New("invalid_request"))
		return
	}
	session, err := model.FinishRewardedAdSession(c.GetInt("id"), c.Param("id"), req.Reason)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"success": false, "code": "session_not_found", "message": "观看会话不存在"})
		return
	}
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	data, err := service.RewardedAdSessionView(session, false)
	if err != nil {
		rewardedAdError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

func RewardedAdCallback(c *gin.Context) {
	cfg, err := model.GetRewardedAdSettingFromDB()
	if err != nil {
		c.JSON(503, gin.H{"isValid": false})
		return
	}
	event, err := service.ValidateRewardedAdCallback(c.Request, cfg, time.Now())
	if err != nil {
		c.JSON(400, gin.H{"isValid": false})
		return
	}
	err = model.SettleRewardedAd(event, time.Now())
	// The verified event is stored for review; acknowledge so DCloud stops redelivering.
	if err != nil && err.Error() == "review_required" {
		c.JSON(http.StatusOK, gin.H{"isValid": true})
		return
	}
	if err != nil {
		status := 503
		switch err.Error() {
		case "invalid_request", "event_conflict", "session_conflict", "account_unavailable", "invalid_reward":
			status = 400
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = 400
		}
		c.JSON(status, gin.H{"isValid": false})
		return
	}
	// The credit is committed with its log in the outbox. Deliver the log after
	// answering so a slow log database cannot delay DCloud into retrying.
	c.JSON(http.StatusOK, gin.H{"isValid": true})
	gopool.Go(func() {
		if err := model.DeliverRewardLogs(); err != nil {
			common.SysError("reward log delivery deferred to maintenance: " + err.Error())
		}
	})
}
