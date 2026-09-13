package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// directPayOrderTTL 是直连订单的有效期。支付宝与微信都允许更长的支付窗口
// （分别是 15 天和 7 天），这里主动收紧，既减少悬挂订单，也让二维码刷新与
// 订单复用有一个明确的边界。
const directPayOrderTTL = 15 * time.Minute

// DirectPayRequest 是两条直连通道共用的下单请求体。
type DirectPayRequest struct {
	Amount int64 `json:"amount"`
}

// directPayOrder 承载一次下单所需的全部已校验数据。
type directPayOrder struct {
	userID   int
	topUp    *model.TopUp
	payMoney float64
	// reused 为 true 表示复用了此前未支付的同额订单，而不是新建。
	reused bool
}

// prepareDirectPayOrder 完成直连下单前的全部校验并给出待支付订单。
//
// 校验失败时已向客户端写出响应，调用方直接返回即可。
//
// 复用未过期的同额订单是刻意的：支付宝侧 out_trade_no 与单据一一对应，每次点击
// 都新建订单会在支付宝留下多笔各自可付的待付款单，用户重复支付的风险是真实的。
// 微信侧用原单号重新下单则会让旧二维码失效并返回新的，同样是官方推荐做法。
func prepareDirectPayOrder(c *gin.Context, paymentProvider string, paymentMethod string, tradeNoPrefix string) (*directPayOrder, bool) {
	var req DirectPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return nil, false
	}
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return nil, false
	}

	userID := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, userID, req.Amount) {
		return nil, false
	}

	group, err := model.GetUserGroup(userID, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return nil, false
	}

	// 直连通道与易支付同为人民币收款，沿用同一套单价、分组倍率与折扣口径。
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return nil, false
	}

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).
			Div(decimal.NewFromFloat(common.QuotaPerUnit)).
			IntPart()
		if amount < 1 {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值数量无效"})
			return nil, false
		}
	}

	createdAfter := time.Now().Add(-directPayOrderTTL).Unix()
	existing, err := model.FindPendingDirectPayTopUp(userID, paymentProvider, amount, createdAfter)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("直连支付 查询待支付订单失败 user_id=%d provider=%s error=%q", userID, paymentProvider, err.Error()))
	}
	// 价格口径可能在两次点击之间被管理员改动，金额不一致时不复用。
	if existing != nil && decimal.NewFromFloat(existing.Money).Equal(decimal.NewFromFloat(payMoney)) {
		return &directPayOrder{userID: userID, topUp: existing, payMoney: payMoney, reused: true}, true
	}

	tradeNo := fmt.Sprintf("%s%dNO%s%d", tradeNoPrefix, userID, common.GetRandomString(6), time.Now().Unix())
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentMethod,
		PaymentProvider: paymentProvider,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("直连支付 创建充值订单失败 user_id=%d provider=%s trade_no=%s amount=%d error=%q", userID, paymentProvider, tradeNo, amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return nil, false
	}
	return &directPayOrder{userID: userID, topUp: topUp, payMoney: payMoney}, true
}

// directPayNotifyURL 拼接回调地址。地址由「回调域名 + 固定路径」推导，管理员只需
// 在系统设置里维护域名，避免手填完整 URL 出错。
func directPayNotifyURL(path string) string {
	return service.GetCallbackAddress() + path
}

// directPayExpireAt 返回订单的绝对超时时间。
func directPayExpireAt(topUp *model.TopUp) time.Time {
	return time.Unix(topUp.CreateTime, 0).Add(directPayOrderTTL)
}

// DirectPayOrderStatus 供前端轮询扫码支付结果。
//
// 只返回当前用户自己的订单，避免拿着他人订单号探测其充值情况。本地状态仍为待支付
// 时会主动向上游查单：两家都明确要求商户不能只依赖回调，丢通知会让用户看到未支付
// 的订单进而重复付款。
func DirectPayOrderStatus(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	if tradeNo == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	userID := c.GetInt("id")
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != userID {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}

	if topUp.Status == common.TopUpStatusPending {
		if paid := reconcileDirectPayOrder(c, topUp); paid {
			topUp.Status = common.TopUpStatusSuccess
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"trade_no":   topUp.TradeNo,
			"status":     topUp.Status,
			"expires_at": directPayExpireAt(topUp).Unix(),
		},
	})
}

// reconcileDirectPayOrder 向上游查单并在确认已支付时补齐入账，返回是否已支付。
func reconcileDirectPayOrder(c *gin.Context, topUp *model.TopUp) bool {
	ctx := c.Request.Context()
	paid := false
	upstreamTradeNo := ""

	switch topUp.PaymentProvider {
	case model.PaymentProviderAlipay:
		if !isAlipayTopUpEnabled() {
			return false
		}
		state, err := service.QueryAlipayTrade(ctx, topUp.TradeNo)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("支付宝 查单失败 trade_no=%s error=%q", topUp.TradeNo, err.Error()))
			return false
		}
		if state == nil || !service.AlipayTradePaid(state.TradeStatus) {
			return false
		}
		if !directPayAmountMatches(ctx, topUp, state.TotalAmount) {
			return false
		}
		upstreamTradeNo = state.TradeNo
		paid = true
	case model.PaymentProviderWechat:
		if !isWechatPayTopUpEnabled() {
			return false
		}
		state, err := service.QueryWechatTrade(ctx, topUp.TradeNo)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("微信支付 查单失败 trade_no=%s error=%q", topUp.TradeNo, err.Error()))
			return false
		}
		if state == nil || state.TradeState != service.WechatTradeStateSuccess {
			return false
		}
		if !directPayCentsMatch(ctx, topUp, state.AmountTotal) {
			return false
		}
		upstreamTradeNo = state.TransIDWx
		paid = true
	default:
		return false
	}

	if !paid {
		return false
	}

	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	alreadyDone, err := model.RechargeDirectPay(topUp.TradeNo, topUp.PaymentProvider, upstreamTradeNo, c.ClientIP())
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("直连支付 查单补入账失败 trade_no=%s provider=%s error=%q", topUp.TradeNo, topUp.PaymentProvider, err.Error()))
		return false
	}
	if !alreadyDone {
		logger.LogInfo(ctx, fmt.Sprintf("直连支付 查单补入账成功 trade_no=%s provider=%s", topUp.TradeNo, topUp.PaymentProvider))
	}
	return true
}

// directPayAmountMatches 校验上游返回的元金额与本地订单一致。
func directPayAmountMatches(ctx context.Context, topUp *model.TopUp, upstreamAmount string) bool {
	upstream, err := decimal.NewFromString(upstreamAmount)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("直连支付 上游金额无法解析 trade_no=%s amount=%q", topUp.TradeNo, upstreamAmount))
		return false
	}
	if !upstream.Equal(decimal.NewFromFloat(topUp.Money)) {
		logger.LogError(ctx, fmt.Sprintf("直连支付 金额不匹配 trade_no=%s upstream=%s local=%.2f", topUp.TradeNo, upstreamAmount, topUp.Money))
		return false
	}
	return true
}

// directPayCentsMatch 校验上游返回的分金额与本地订单一致。
func directPayCentsMatch(ctx context.Context, topUp *model.TopUp, upstreamCents int) bool {
	expected, err := directPayMoneyToCents(topUp.Money)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("直连支付 本地金额换算失败 trade_no=%s money=%.2f error=%q", topUp.TradeNo, topUp.Money, err.Error()))
		return false
	}
	if expected != upstreamCents {
		logger.LogError(ctx, fmt.Sprintf("直连支付 金额不匹配 trade_no=%s upstream_cents=%d local_cents=%d", topUp.TradeNo, upstreamCents, expected))
		return false
	}
	return true
}

// directPayMoneyToCents 把元金额换算为分。
//
// 走 decimal 与项目统一的饱和转换助手，不用裸 int() 转换：金额最终会变成计费数量，
// 溢出必须失败而不是回绕。
func directPayMoneyToCents(money float64) (int, error) {
	return common.QuotaFromDecimalStrict(
		decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).Round(0),
	)
}
