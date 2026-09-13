package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/setting"

	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
)

// 支付宝「电脑网站支付」产品码，该场景下支付宝仅接受这一个值。
const alipayPagePayProductCode = "FAST_INSTANT_TRADE_PAY"

// 交易状态。TRADE_SUCCESS 是支持退款的产品付款成功后的状态，电脑网站支付正常
// 只会收到这一种；TRADE_FINISHED 出现在不支持退款或已过退款期限的场景。
const (
	AlipayTradeStatusWaitBuyerPay = "WAIT_BUYER_PAY"
	AlipayTradeStatusSuccess      = "TRADE_SUCCESS"
	AlipayTradeStatusFinished     = "TRADE_FINISHED"
	AlipayTradeStatusClosed       = "TRADE_CLOSED"
)

// AlipayTradePaid 判断交易状态是否代表买家已付款。
func AlipayTradePaid(tradeStatus string) bool {
	return tradeStatus == AlipayTradeStatusSuccess || tradeStatus == AlipayTradeStatusFinished
}

func newAlipayClient() (*alipay.Client, error) {
	appID := strings.TrimSpace(setting.AlipayAppID)
	privateKey := strings.TrimSpace(setting.AlipayPrivateKey)
	if appID == "" || privateKey == "" {
		return nil, errors.New("支付宝直连凭证未配置")
	}
	client, err := alipay.NewClient(appID, privateKey, !setting.AlipaySandbox)
	if err != nil {
		return nil, fmt.Errorf("初始化支付宝客户端失败: %w", err)
	}
	client.SetCharset(alipay.UTF8).SetSignType(alipay.RSA2)
	return client, nil
}

// AlipayPagePayParams 描述一次电脑网站支付下单。
type AlipayPagePayParams struct {
	TradeNo   string
	Subject   string
	Amount    string // 元，两位小数
	NotifyURL string
	ReturnURL string
	// TimeExpire 订单绝对超时时间，格式 yyyy-MM-dd HH:mm:ss，范围 1m~15d。
	TimeExpire string
	// ClientIP 用户在本站下单时的客户端 IP，随风控信息回传。
	ClientIP string
}

// CreateAlipayPagePay 发起电脑网站支付并返回可直接跳转的收银台链接。
//
// gopay 的 TradePagePay 返回的是 GET 形态的支付 URL 而非自动提交表单，前端
// 直接 window.open 即可，无需沿用易支付那套表单提交路径。
func CreateAlipayPagePay(ctx context.Context, params *AlipayPagePayParams) (payURL string, err error) {
	client, err := newAlipayClient()
	if err != nil {
		return "", err
	}

	bm := make(gopay.BodyMap)
	bm.Set("out_trade_no", params.TradeNo).
		Set("subject", params.Subject).
		Set("total_amount", params.Amount).
		Set("product_code", alipayPagePayProductCode)
	if params.TimeExpire != "" {
		bm.Set("time_expire", params.TimeExpire)
	}
	// 风险联防数据回传。虚拟充值是支付宝重点治理的被动赌博 / 刷单场景，
	// 不回传容易触发交易拦截或压低收款额度。
	if params.ClientIP != "" {
		riskParams := make(gopay.BodyMap)
		riskParams.Set("mc_create_trade_ip", params.ClientIP)
		bm.Set("business_params", riskParams.JsonBody())
	}

	client.SetNotifyUrl(params.NotifyURL)
	if params.ReturnURL != "" {
		client.SetReturnUrl(params.ReturnURL)
	}

	payURL, err = client.TradePagePay(ctx, bm)
	if err != nil {
		return "", fmt.Errorf("支付宝下单失败: %w", err)
	}
	return payURL, nil
}

// ParseAlipayNotify 解析异步通知报文并完成验签。
//
// 验签只证明报文确实来自支付宝，业务侧的四项校验（out_trade_no、total_amount、
// seller_id、app_id）由调用方在拿到 BodyMap 后完成，缺任何一项都必须忽略通知。
func ParseAlipayNotify(req *http.Request) (gopay.BodyMap, error) {
	publicKey := strings.TrimSpace(setting.AlipayPublicKey)
	if publicKey == "" {
		return nil, errors.New("支付宝公钥未配置")
	}
	bm, err := alipay.ParseNotifyToBodyMap(req)
	if err != nil {
		return nil, fmt.Errorf("解析支付宝通知失败: %w", err)
	}
	ok, err := alipay.VerifySign(publicKey, bm)
	if err != nil {
		return nil, fmt.Errorf("支付宝通知验签失败: %w", err)
	}
	if !ok {
		return nil, errors.New("支付宝通知验签未通过")
	}
	return bm, nil
}

// AlipayTradeState 是主动查单返回的交易信息。
type AlipayTradeState struct {
	TradeStatus string
	TotalAmount string
	TradeNo     string
}

// QueryAlipayTrade 主动查询交易状态。
//
// 支付宝要求商户必须同时接入异步通知与本接口：丢失通知会让用户看到未支付的订单，
// 进而重复支付。订单不存在时返回 nil, nil，调用方据此判断用户尚未发起支付。
func QueryAlipayTrade(ctx context.Context, tradeNo string) (*AlipayTradeState, error) {
	client, err := newAlipayClient()
	if err != nil {
		return nil, err
	}
	bm := make(gopay.BodyMap)
	bm.Set("out_trade_no", tradeNo)

	rsp, err := client.TradeQuery(ctx, bm)
	if err != nil {
		return nil, fmt.Errorf("支付宝查单失败: %w", err)
	}
	if rsp == nil || rsp.Response == nil {
		return nil, errors.New("支付宝查单返回为空")
	}
	// ACQ.TRADE_NOT_EXIST 表示用户还没在支付宝侧发起过这笔交易，属于正常情况。
	if rsp.Response.Code != "10000" {
		if rsp.Response.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return nil, nil
		}
		return nil, fmt.Errorf("支付宝查单返回异常 code=%s sub_code=%s sub_msg=%s",
			rsp.Response.Code, rsp.Response.SubCode, rsp.Response.SubMsg)
	}
	return &AlipayTradeState{
		TradeStatus: rsp.Response.TradeStatus,
		TotalAmount: rsp.Response.TotalAmount,
		TradeNo:     rsp.Response.TradeNo,
	}, nil
}

// CloseAlipayTrade 关闭一笔尚未支付的交易，使其不可再被支付。
//
// 用户重新发起支付前先关掉旧单，可以避免支付宝侧遗留多笔各自可付的待付款单据。
func CloseAlipayTrade(ctx context.Context, tradeNo string) error {
	client, err := newAlipayClient()
	if err != nil {
		return err
	}
	bm := make(gopay.BodyMap)
	bm.Set("out_trade_no", tradeNo)

	rsp, err := client.TradeClose(ctx, bm)
	if err != nil {
		return fmt.Errorf("支付宝关单失败: %w", err)
	}
	if rsp == nil || rsp.Response == nil {
		return errors.New("支付宝关单返回为空")
	}
	// 交易不存在说明用户从未发起支付，与关单成功等效。
	if rsp.Response.Code != "10000" && rsp.Response.SubCode != "ACQ.TRADE_NOT_EXIST" {
		return fmt.Errorf("支付宝关单返回异常 code=%s sub_code=%s sub_msg=%s",
			rsp.Response.Code, rsp.Response.SubCode, rsp.Response.SubMsg)
	}
	return nil
}
