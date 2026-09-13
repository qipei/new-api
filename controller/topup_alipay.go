package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const alipayNotifyPath = "/api/alipay/notify"

// RequestAlipayPay 发起支付宝电脑网站支付，返回可直接跳转的收银台链接。
func RequestAlipayPay(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝支付未启用"})
		return
	}

	order, ok := prepareDirectPayOrder(c, model.PaymentProviderAlipay, model.PaymentMethodAlipayDirect, "ALI")
	if !ok {
		return
	}

	payURL, err := service.CreateAlipayPagePay(c.Request.Context(), &service.AlipayPagePayParams{
		TradeNo: order.topUp.TradeNo,
		// 标题不可含 / = & 等特殊字符，支付宝会拒绝。
		Subject:    fmt.Sprintf("账户充值 %d", order.topUp.Amount),
		Amount:     decimal.NewFromFloat(order.payMoney).StringFixed(2),
		NotifyURL:  directPayNotifyURL(alipayNotifyPath),
		ReturnURL:  paymentReturnPath("/usage-logs"),
		TimeExpire: directPayExpireAt(order.topUp).Format("2006-01-02 15:04:05"),
		ClientIP:   c.ClientIP(),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 拉起支付失败 user_id=%d trade_no=%s amount=%d error=%q", order.userID, order.topUp.TradeNo, order.topUp.Amount, err.Error()))
		if !order.reused {
			if updateErr := model.UpdatePendingTopUpStatus(order.topUp.TradeNo, model.PaymentProviderAlipay, common.TopUpStatusFailed); updateErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 标记订单失败状态失败 trade_no=%s error=%q", order.topUp.TradeNo, updateErr.Error()))
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 充值订单就绪 user_id=%d trade_no=%s amount=%d money=%.2f reused=%t", order.userID, order.topUp.TradeNo, order.topUp.Amount, order.payMoney, order.reused))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_url":    payURL,
			"trade_no":   order.topUp.TradeNo,
			"expires_at": directPayExpireAt(order.topUp).Unix(),
		},
	})
}

// AlipayNotify 处理支付宝异步通知。
//
// 支付宝要求应答体为纯文本 success，收到其它内容即视为通知失败并按
// 4m、10m、10m、1h、2h、6h、15h 的节奏重投。因此除了「确实已入账」以外的所有
// 分支都必须回非 success，让支付宝继续重试。
//
// 本端点同时作为开放平台「应用网关」地址：消息服务推送带 msg_method，与支付结果
// 通知共用一个入口可以避免 notify_url 缺失时静默丢单。
func AlipayNotify(c *gin.Context) {
	ctx := c.Request.Context()
	if !isAlipayWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		writeAlipayNotifyFailure(c)
		return
	}

	bm, err := service.ParseAlipayNotify(c.Request)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 webhook 验签失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
		writeAlipayNotifyFailure(c)
		return
	}

	// 消息服务推送（例如退款冲退完成通知）走同一个地址，这里只记录并应答成功。
	if msgMethod := bm.GetString("msg_method"); msgMethod != "" {
		logger.LogInfo(ctx, fmt.Sprintf("支付宝 收到消息服务推送 msg_method=%s client_ip=%s", msgMethod, c.ClientIP()))
		writeAlipayNotifySuccess(c)
		return
	}

	tradeNo := bm.GetString("out_trade_no")
	tradeStatus := bm.GetString("trade_status")
	logger.LogInfo(ctx, fmt.Sprintf("支付宝 webhook 验签成功 trade_no=%s trade_status=%s client_ip=%s", tradeNo, tradeStatus, c.ClientIP()))

	if !service.AlipayTradePaid(tradeStatus) {
		// 交易创建、交易关闭等状态无需处理，但仍要应答 success 阻止重投。
		logger.LogInfo(ctx, fmt.Sprintf("支付宝 webhook 忽略非支付成功事件 trade_no=%s trade_status=%s", tradeNo, tradeStatus))
		writeAlipayNotifySuccess(c)
		return
	}

	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 回调订单不存在 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		writeAlipayNotifyFailure(c)
		return
	}
	if !verifyAlipayNotifyIdentity(c, bm, topUp) {
		writeAlipayNotifyFailure(c)
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	alreadyDone, err := model.RechargeDirectPay(tradeNo, model.PaymentProviderAlipay, c.ClientIP())
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("支付宝 充值处理失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
		writeAlipayNotifyFailure(c)
		return
	}
	if alreadyDone {
		logger.LogInfo(ctx, fmt.Sprintf("支付宝 重复回调幂等忽略 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("支付宝 充值成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	}
	writeAlipayNotifySuccess(c)
}

// verifyAlipayNotifyIdentity 执行支付宝要求的业务校验。
//
// 官方原文：out_trade_no、total_amount、seller_id、app_id 任何一项验证不通过，
// 都表明本次通知是异常通知，务必忽略。验签只证明报文来自支付宝，证明不了这笔钱
// 是付给我们这笔订单的。
func verifyAlipayNotifyIdentity(c *gin.Context, bm gopayBodyMapReader, topUp *model.TopUp) bool {
	ctx := c.Request.Context()

	if appID := bm.GetString("app_id"); appID != strings.TrimSpace(setting.AlipayAppID) {
		logger.LogError(ctx, fmt.Sprintf("支付宝 回调 app_id 不匹配 trade_no=%s notify_app_id=%s", topUp.TradeNo, appID))
		return false
	}
	if sellerID := bm.GetString("seller_id"); sellerID != strings.TrimSpace(setting.AlipaySellerID) {
		logger.LogError(ctx, fmt.Sprintf("支付宝 回调 seller_id 不匹配 trade_no=%s notify_seller_id=%s", topUp.TradeNo, sellerID))
		return false
	}
	if topUp.PaymentProvider != model.PaymentProviderAlipay {
		logger.LogError(ctx, fmt.Sprintf("支付宝 回调订单网关不匹配 trade_no=%s provider=%s", topUp.TradeNo, topUp.PaymentProvider))
		return false
	}
	return directPayAmountMatches(ctx, topUp, bm.GetString("total_amount"))
}

// gopayBodyMapReader 只暴露校验所需的读取能力，便于为业务校验单独写表驱动测试。
type gopayBodyMapReader interface {
	GetString(key string) string
}

func writeAlipayNotifySuccess(c *gin.Context) {
	if _, err := c.Writer.WriteString("success"); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 响应写入失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
	}
}

func writeAlipayNotifyFailure(c *gin.Context) {
	if _, err := c.Writer.WriteString("failure"); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 响应写入失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
	}
}
