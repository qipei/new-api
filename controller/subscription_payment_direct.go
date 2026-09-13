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
	"github.com/shopspring/decimal"
)

const (
	alipaySubscriptionNotifyPath = "/api/subscription/alipay/notify"
	wechatSubscriptionNotifyPath = "/api/subscription/wechat/notify"
)

// SubscriptionDirectPayRequest 是两条直连通道共用的套餐购买请求体。
type SubscriptionDirectPayRequest struct {
	PlanId int `json:"plan_id"`
}

// subscriptionDirectOrder 承载一次套餐下单所需的已校验数据。
type subscriptionDirectOrder struct {
	order     *model.SubscriptionOrder
	planTitle string
}

// prepareSubscriptionDirectOrder 校验套餐并创建待支付订单。
//
// 校验失败时已向客户端写出响应，调用方直接返回即可。
//
// 与充值不同，这里不复用未支付订单：套餐有每人购买上限，复用会让上限校验的
// 时点和实际入账的时点脱节。每次点击生成新单，未支付的旧单由超时兜底。
func prepareSubscriptionDirectOrder(c *gin.Context, paymentProvider string, paymentMethod string) (*subscriptionDirectOrder, bool) {
	if !requirePaymentCompliance(c) {
		return nil, false
	}

	var req SubscriptionDirectPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return nil, false
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return nil, false
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return nil, false
	}

	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return nil, false
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return nil, false
		}
	}

	tradeNo := fmt.Sprintf("SUBUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentMethod,
		PaymentProvider: paymentProvider,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("直连支付 创建套餐订单失败 user_id=%d provider=%s plan_id=%d error=%q", userId, paymentProvider, plan.Id, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return nil, false
	}
	return &subscriptionDirectOrder{order: order, planTitle: plan.Title}, true
}

// subscriptionDirectExpireAt 返回套餐订单的绝对超时时间。
func subscriptionDirectExpireAt(order *model.SubscriptionOrder) time.Time {
	return time.Unix(order.CreateTime, 0).Add(directPayOrderTTL)
}

// subscriptionDirectSubject 生成上游展示的商品标题。
func subscriptionDirectSubject(planTitle string) string {
	cleaned := strings.NewReplacer("/", " ", "=", " ", "&", " ").Replace(planTitle)
	return fmt.Sprintf("套餐 %s", strings.TrimSpace(cleaned))
}

// SubscriptionRequestAlipayPay 用支付宝电脑网站支付购买套餐。
func SubscriptionRequestAlipayPay(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		common.ApiErrorMsg(c, "支付宝支付未启用")
		return
	}

	prepared, ok := prepareSubscriptionDirectOrder(c, model.PaymentProviderAlipay, model.PaymentMethodAlipayDirect)
	if !ok {
		return
	}
	order := prepared.order

	payURL, err := service.CreateAlipayPagePay(c.Request.Context(), &service.AlipayPagePayParams{
		TradeNo:    order.TradeNo,
		Subject:    subscriptionDirectSubject(prepared.planTitle),
		Amount:     decimal.NewFromFloat(order.Money).StringFixed(2),
		NotifyURL:  directPayNotifyURL(alipaySubscriptionNotifyPath),
		ReturnURL:  paymentReturnPath("/wallet?pay=pending"),
		TimeExpire: subscriptionDirectExpireAt(order).Format("2006-01-02 15:04:05"),
		ClientIP:   c.ClientIP(),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 套餐拉起支付失败 trade_no=%s plan_id=%d error=%q", order.TradeNo, order.PlanId, err.Error()))
		if expireErr := model.ExpireSubscriptionOrder(order.TradeNo, model.PaymentProviderAlipay); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 套餐订单置失效失败 trade_no=%s error=%q", order.TradeNo, expireErr.Error()))
		}
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 套餐订单就绪 trade_no=%s plan_id=%d money=%.2f", order.TradeNo, order.PlanId, order.Money))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_url":    payURL,
			"trade_no":   order.TradeNo,
			"expires_at": subscriptionDirectExpireAt(order).Unix(),
		},
	})
}

// SubscriptionRequestWechatPay 用微信 Native 扫码购买套餐。
func SubscriptionRequestWechatPay(c *gin.Context) {
	if !isWechatPayTopUpEnabled() {
		common.ApiErrorMsg(c, "微信支付未启用")
		return
	}

	prepared, ok := prepareSubscriptionDirectOrder(c, model.PaymentProviderWechat, model.PaymentMethodWechatDirect)
	if !ok {
		return
	}
	order := prepared.order

	amountTotal, err := directPayMoneyToCents(order.Money)
	if err != nil || amountTotal <= 0 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 套餐金额换算失败 trade_no=%s money=%.2f", order.TradeNo, order.Money))
		if expireErr := model.ExpireSubscriptionOrder(order.TradeNo, model.PaymentProviderWechat); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 套餐订单置失效失败 trade_no=%s error=%q", order.TradeNo, expireErr.Error()))
		}
		common.ApiErrorMsg(c, "套餐金额无效")
		return
	}

	expireAt := subscriptionDirectExpireAt(order)
	codeURL, err := service.CreateWechatNativeOrder(c.Request.Context(), &service.WechatNativeParams{
		TradeNo:     order.TradeNo,
		Description: subscriptionDirectSubject(prepared.planTitle),
		AmountTotal: amountTotal,
		NotifyURL:   directPayNotifyURL(wechatSubscriptionNotifyPath),
		TimeExpire:  expireAt.Format(time.RFC3339),
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 套餐拉起支付失败 trade_no=%s plan_id=%d error=%q", order.TradeNo, order.PlanId, err.Error()))
		if expireErr := model.ExpireSubscriptionOrder(order.TradeNo, model.PaymentProviderWechat); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 套餐订单置失效失败 trade_no=%s error=%q", order.TradeNo, expireErr.Error()))
		}
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付 套餐订单就绪 trade_no=%s plan_id=%d money=%.2f", order.TradeNo, order.PlanId, order.Money))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"code_url":   codeURL,
			"trade_no":   order.TradeNo,
			"expires_at": expireAt.Unix(),
		},
	})
}

// SubscriptionAlipayNotify 处理套餐订单的支付宝异步通知。
//
// 与充值回调同构：验签之后仍要逐项核对 app_id、seller_id 与金额，任何一项
// 对不上都按异常通知忽略。
func SubscriptionAlipayNotify(c *gin.Context) {
	ctx := c.Request.Context()
	if !isAlipayWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 套餐 webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		writeAlipayNotifyFailure(c)
		return
	}

	bm, err := service.ParseAlipayNotify(c.Request)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 套餐 webhook 验签失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
		writeAlipayNotifyFailure(c)
		return
	}

	tradeNo := bm.GetString("out_trade_no")
	tradeStatus := bm.GetString("trade_status")
	if !service.AlipayTradePaid(tradeStatus) {
		logger.LogInfo(ctx, fmt.Sprintf("支付宝 套餐 webhook 忽略非支付成功事件 trade_no=%s trade_status=%s", tradeNo, tradeStatus))
		writeAlipayNotifySuccess(c)
		return
	}

	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil {
		logger.LogWarn(ctx, fmt.Sprintf("支付宝 套餐回调订单不存在 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		writeAlipayNotifyFailure(c)
		return
	}
	if !verifySubscriptionAlipayIdentity(c, bm, order) {
		writeAlipayNotifyFailure(c)
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	if err := model.CompleteSubscriptionOrder(tradeNo, common.GetJsonString(bm), model.PaymentProviderAlipay, model.PaymentMethodAlipayDirect); err != nil {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐完成失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
		writeAlipayNotifyFailure(c)
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("支付宝 套餐购买成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	writeAlipayNotifySuccess(c)
}

// verifySubscriptionAlipayIdentity 校验套餐回调的来源与金额。
func verifySubscriptionAlipayIdentity(c *gin.Context, bm gopayBodyMapReader, order *model.SubscriptionOrder) bool {
	ctx := c.Request.Context()

	if appID := bm.GetString("app_id"); appID != strings.TrimSpace(setting.AlipayAppID) {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐回调 app_id 不匹配 trade_no=%s notify_app_id=%s", order.TradeNo, appID))
		return false
	}
	if sellerID := bm.GetString("seller_id"); sellerID != strings.TrimSpace(setting.AlipaySellerID) {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐回调 seller_id 不匹配 trade_no=%s notify_seller_id=%s", order.TradeNo, sellerID))
		return false
	}
	if order.PaymentProvider != model.PaymentProviderAlipay {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐回调订单网关不匹配 trade_no=%s provider=%s", order.TradeNo, order.PaymentProvider))
		return false
	}
	upstream, err := decimal.NewFromString(bm.GetString("total_amount"))
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐回调金额无法解析 trade_no=%s amount=%q", order.TradeNo, bm.GetString("total_amount")))
		return false
	}
	if !upstream.Equal(decimal.NewFromFloat(order.Money)) {
		logger.LogError(ctx, fmt.Sprintf("支付宝 套餐回调金额不匹配 trade_no=%s upstream=%s local=%.2f", order.TradeNo, upstream.String(), order.Money))
		return false
	}
	return true
}

// SubscriptionWechatNotify 处理套餐订单的微信支付结果通知。
func SubscriptionWechatNotify(c *gin.Context) {
	ctx := c.Request.Context()
	if !isWechatPayWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 套餐 webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		c.JSON(http.StatusForbidden, gin.H{"code": "FAIL", "message": "webhook disabled"})
		return
	}

	result, err := service.ParseWechatNotify(c.Request)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 套餐 webhook 验签或解密失败 client_ip=%s error=%q", c.ClientIP(), err.Error()))
		c.JSON(http.StatusUnauthorized, gin.H{"code": "FAIL", "message": "invalid signature"})
		return
	}
	if result == nil || result.TradeState != service.WechatTradeStateSuccess {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}

	order := model.GetSubscriptionOrderByTradeNo(result.TradeNo)
	if order == nil {
		logger.LogWarn(ctx, fmt.Sprintf("微信支付 套餐回调订单不存在 trade_no=%s client_ip=%s", result.TradeNo, c.ClientIP()))
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}
	if !verifySubscriptionWechatIdentity(c, result, order) {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}

	LockOrder(result.TradeNo)
	defer UnlockOrder(result.TradeNo)

	if err := model.CompleteSubscriptionOrder(result.TradeNo, common.GetJsonString(result), model.PaymentProviderWechat, model.PaymentMethodWechatDirect); err != nil {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐完成失败 trade_no=%s client_ip=%s error=%q", result.TradeNo, c.ClientIP(), err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": "系统错误"})
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("微信支付 套餐购买成功 trade_no=%s client_ip=%s", result.TradeNo, c.ClientIP()))
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
}

// verifySubscriptionWechatIdentity 校验套餐回调的来源与金额。
func verifySubscriptionWechatIdentity(c *gin.Context, result *service.WechatNotifyResult, order *model.SubscriptionOrder) bool {
	ctx := c.Request.Context()

	if result.AppID != strings.TrimSpace(setting.WechatPayAppID) {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐回调 appid 不匹配 trade_no=%s notify_appid=%s", order.TradeNo, result.AppID))
		return false
	}
	if result.MchID != strings.TrimSpace(setting.WechatPayMchID) {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐回调 mchid 不匹配 trade_no=%s notify_mchid=%s", order.TradeNo, result.MchID))
		return false
	}
	if order.PaymentProvider != model.PaymentProviderWechat {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐回调订单网关不匹配 trade_no=%s provider=%s", order.TradeNo, order.PaymentProvider))
		return false
	}
	expected, err := directPayMoneyToCents(order.Money)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐本地金额换算失败 trade_no=%s money=%.2f", order.TradeNo, order.Money))
		return false
	}
	if expected != result.AmountTotal {
		logger.LogError(ctx, fmt.Sprintf("微信支付 套餐回调金额不匹配 trade_no=%s upstream_cents=%d local_cents=%d", order.TradeNo, result.AmountTotal, expected))
		return false
	}
	return true
}

// SubscriptionDirectOrderStatus 供前端轮询套餐订单的支付结果。
//
// 只返回当前用户自己的订单。本地仍为待支付时主动向上游查单，两家都明确要求
// 商户不能只依赖回调。
func SubscriptionDirectOrderStatus(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	if tradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	userId := c.GetInt("id")
	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil || order.UserId != userId {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}

	if order.Status == common.TopUpStatusPending && reconcileSubscriptionDirectOrder(c, order) {
		order.Status = common.TopUpStatusSuccess
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"trade_no":   order.TradeNo,
			"status":     order.Status,
			"expires_at": subscriptionDirectExpireAt(order).Unix(),
		},
	})
}

// reconcileSubscriptionDirectOrder 向上游查单并在确认已支付时补齐入账。
func reconcileSubscriptionDirectOrder(c *gin.Context, order *model.SubscriptionOrder) bool {
	ctx := c.Request.Context()
	paymentMethod := ""
	upstreamTradeNo := ""

	switch order.PaymentProvider {
	case model.PaymentProviderAlipay:
		if !isAlipayTopUpEnabled() {
			return false
		}
		state, err := service.QueryAlipayTrade(ctx, order.TradeNo)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("支付宝 套餐查单失败 trade_no=%s error=%q", order.TradeNo, err.Error()))
			return false
		}
		if state == nil || !service.AlipayTradePaid(state.TradeStatus) {
			return false
		}
		upstream, err := decimal.NewFromString(state.TotalAmount)
		if err != nil || !upstream.Equal(decimal.NewFromFloat(order.Money)) {
			logger.LogError(ctx, fmt.Sprintf("支付宝 套餐查单金额不匹配 trade_no=%s upstream=%s local=%.2f", order.TradeNo, state.TotalAmount, order.Money))
			return false
		}
		upstreamTradeNo = state.TradeNo
		paymentMethod = model.PaymentMethodAlipayDirect
	case model.PaymentProviderWechat:
		if !isWechatPayTopUpEnabled() {
			return false
		}
		state, err := service.QueryWechatTrade(ctx, order.TradeNo)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("微信支付 套餐查单失败 trade_no=%s error=%q", order.TradeNo, err.Error()))
			return false
		}
		if state == nil || state.TradeState != service.WechatTradeStateSuccess {
			return false
		}
		expected, err := directPayMoneyToCents(order.Money)
		if err != nil || expected != state.AmountTotal {
			logger.LogError(ctx, fmt.Sprintf("微信支付 套餐查单金额不匹配 trade_no=%s upstream_cents=%d local=%.2f", order.TradeNo, state.AmountTotal, order.Money))
			return false
		}
		upstreamTradeNo = state.TransIDWx
		paymentMethod = model.PaymentMethodWechatDirect
	default:
		return false
	}

	LockOrder(order.TradeNo)
	defer UnlockOrder(order.TradeNo)

	// 查单补入账时没有回调报文，至少把上游交易号留下，否则这类订单在
	// provider_payload 里是空的，事后无法与网关账单对账。
	payload := common.GetJsonString(map[string]string{
		"source":            "query",
		"upstream_trade_no": upstreamTradeNo,
	})
	if err := model.CompleteSubscriptionOrder(order.TradeNo, payload, order.PaymentProvider, paymentMethod); err != nil {
		logger.LogError(ctx, fmt.Sprintf("直连支付 套餐查单补入账失败 trade_no=%s provider=%s error=%q", order.TradeNo, order.PaymentProvider, err.Error()))
		return false
	}
	logger.LogInfo(ctx, fmt.Sprintf("直连支付 套餐查单补入账成功 trade_no=%s provider=%s", order.TradeNo, order.PaymentProvider))
	return true
}
