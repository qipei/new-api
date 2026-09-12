package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const wechatPayNotifyPath = "/api/wechat/notify"

// RequestWechatPay 发起微信 Native 下单，返回供前端渲染二维码的 code_url。
//
// 复用未过期订单时会用原单号重新下单：微信会返回新的 code_url 并作废旧链接，
// 这正是官方给出的二维码刷新方式。
func RequestWechatPay(c *gin.Context) {
	if !isWechatPayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "微信支付未启用"})
		return
	}

	order, ok := prepareDirectPayOrder(c, model.PaymentProviderWechat, model.PaymentMethodWechatDirect, "WX")
	if !ok {
		return
	}

	amountTotal, err := directPayMoneyToCents(order.payMoney)
	if err != nil || amountTotal <= 0 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 金额换算失败 trade_no=%s money=%.2f", order.topUp.TradeNo, order.payMoney))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额无效"})
		return
	}

	expireAt := directPayExpireAt(order.topUp)
	codeURL, err := service.CreateWechatNativeOrder(c.Request.Context(), &service.WechatNativeParams{
		TradeNo:     order.topUp.TradeNo,
		Description: fmt.Sprintf("账户充值 %d", order.topUp.Amount),
		AmountTotal: amountTotal,
		NotifyURL:   directPayNotifyURL(wechatPayNotifyPath),
		TimeExpire:  expireAt.Format(time.RFC3339),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 拉起支付失败 user_id=%d trade_no=%s amount=%d error=%q", order.userID, order.topUp.TradeNo, order.topUp.Amount, err.Error()))
		if !order.reused {
			if updateErr := model.UpdatePendingTopUpStatus(order.topUp.TradeNo, model.PaymentProviderWechat, common.TopUpStatusFailed); updateErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 标记订单失败状态失败 trade_no=%s error=%q", order.topUp.TradeNo, updateErr.Error()))
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付 充值订单就绪 user_id=%d trade_no=%s amount=%d money=%.2f reused=%t", order.userID, order.topUp.TradeNo, order.topUp.Amount, order.payMoney, order.reused))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"code_url":   codeURL,
			"trade_no":   order.topUp.TradeNo,
			"expires_at": expireAt.Unix(),
		},
	})
}

// WechatPayNotify 处理微信支付结果通知。
//
// 微信要求 5 秒内应答：验签通过返回 200，验签失败返回 4xx/5xx。微信会刻意发送
// 签名无效的探测报文（签名值带 WECHATPAY/SIGNTEST/ 前缀）来检验商户是否真的验签，
// 因此验签失败这条分支必须真实存在且返回错误状态码。
func WechatPayNotify(c *gin.Context) {
	ctx := c.Request.Context()
	if !isWechatPayWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		c.JSON(http.StatusForbidden, gin.H{"code": "FAIL", "message": "webhook disabled"})
		return
	}

	result, err := service.ParseWechatNotify(c.Request)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 webhook 验签或解密失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
		c.JSON(http.StatusUnauthorized, gin.H{"code": "FAIL", "message": "invalid signature"})
		return
	}
	if result == nil {
		// 非支付成功事件，应答 200 即可，微信不会重投。
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("微信支付 webhook 验签成功 trade_no=%s trade_state=%s transaction_id=%s client_ip=%s", result.TradeNo, result.TradeState, result.TransIDWx, c.ClientIP()))

	if result.TradeState != service.WechatTradeStateSuccess {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}

	topUp := model.GetTopUpByTradeNo(result.TradeNo)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 回调订单不存在 trade_no=%s client_ip=%s", result.TradeNo, c.ClientIP()))
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}
	if !verifyWechatNotifyIdentity(c, result, topUp) {
		// 身份或金额对不上属于异常通知，重投也不会变好，应答 200 避免无意义重试。
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}

	LockOrder(result.TradeNo)
	defer UnlockOrder(result.TradeNo)

	alreadyDone, err := model.RechargeDirectPay(result.TradeNo, model.PaymentProviderWechat, c.ClientIP())
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("微信支付 充值处理失败 trade_no=%s client_ip=%s error=%q", result.TradeNo, c.ClientIP(), err.Error()))
		// 入账失败可能是瞬时故障，返回 5xx 让微信按既定节奏重投。
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": "系统错误"})
		return
	}
	if alreadyDone {
		logger.LogInfo(ctx, fmt.Sprintf("微信支付 重复回调幂等忽略 trade_no=%s client_ip=%s", result.TradeNo, c.ClientIP()))
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("微信支付 充值成功 trade_no=%s client_ip=%s", result.TradeNo, c.ClientIP()))
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
}

// verifyWechatNotifyIdentity 校验通知归属与金额。
//
// 验签只证明报文来自微信，证明不了这笔钱是付给我们这笔订单的，appid、mchid 与
// 金额仍要逐项核对。
func verifyWechatNotifyIdentity(c *gin.Context, result *service.WechatNotifyResult, topUp *model.TopUp) bool {
	ctx := c.Request.Context()

	if result.AppID != strings.TrimSpace(setting.WechatPayAppID) {
		logger.LogError(ctx, fmt.Sprintf("微信支付 回调 appid 不匹配 trade_no=%s notify_appid=%s", topUp.TradeNo, result.AppID))
		return false
	}
	if result.MchID != strings.TrimSpace(setting.WechatPayMchID) {
		logger.LogError(ctx, fmt.Sprintf("微信支付 回调 mchid 不匹配 trade_no=%s notify_mchid=%s", topUp.TradeNo, result.MchID))
		return false
	}
	if topUp.PaymentProvider != model.PaymentProviderWechat {
		logger.LogError(ctx, fmt.Sprintf("微信支付 回调订单网关不匹配 trade_no=%s provider=%s", topUp.TradeNo, topUp.PaymentProvider))
		return false
	}
	return directPayCentsMatch(ctx, topUp, result.AmountTotal)
}
