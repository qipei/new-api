package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/setting"

	"github.com/go-pay/gopay"
	wechat "github.com/go-pay/gopay/wechat/v3"
)

// 交易状态。Native 场景下只会出现前四种，后三种属于付款码支付。
const (
	WechatTradeStateSuccess = "SUCCESS"
	WechatTradeStateNotPay  = "NOTPAY"
	WechatTradeStateClosed  = "CLOSED"
	WechatTradeStateRefund  = "REFUND"
)

// 支付成功回调的事件类型。
const wechatTransactionSuccessEvent = "TRANSACTION.SUCCESS"

func newWechatPayClient() (*wechat.ClientV3, error) {
	mchID := strings.TrimSpace(setting.WechatPayMchID)
	serialNo := strings.TrimSpace(setting.WechatPayCertSerialNo)
	apiV3Key := strings.TrimSpace(setting.WechatPayAPIv3Key)
	privateKey := strings.TrimSpace(setting.WechatPayPrivateKey)
	if mchID == "" || serialNo == "" || apiV3Key == "" || privateKey == "" {
		return nil, errors.New("微信支付直连凭证未配置")
	}
	client, err := wechat.NewClientV3(mchID, serialNo, apiV3Key, privateKey)
	if err != nil {
		return nil, fmt.Errorf("初始化微信支付客户端失败: %w", err)
	}

	publicKey := strings.TrimSpace(setting.WechatPayPublicKey)
	publicKeyID := strings.TrimSpace(setting.WechatPayPublicKeyID)
	if publicKey == "" || publicKeyID == "" {
		return nil, errors.New("微信支付公钥或公钥 ID 未配置")
	}
	// 公钥模式验签。公钥 ID 必须保留 PUB_KEY_ID_ 前缀，gopay 用它匹配应答头里的
	// Wechatpay-Serial。若商户平台未点开「开启公钥切换」，微信仍会用平台证书签名，
	// 这里的验签就会全部失败。
	if err := client.AutoVerifySignByPublicKey([]byte(publicKey), publicKeyID); err != nil {
		return nil, fmt.Errorf("加载微信支付公钥失败: %w", err)
	}
	return client, nil
}

// WechatNativeParams 描述一次 Native 下单。
type WechatNativeParams struct {
	TradeNo     string
	Description string
	// AmountTotal 订单总金额，单位为分。
	AmountTotal int
	NotifyURL   string
	// TimeExpire 支付结束时间，RFC3339 格式。超过该时间用户无法再支付。
	TimeExpire string
}

// CreateWechatNativeOrder 发起 Native 下单并返回二维码链接。
//
// 返回的 code_url 需由前端渲染成二维码供用户用微信扫一扫。微信已不支持长按识别
// 或从相册识别二维码，因此该链接只能通过扫码使用。
//
// 用原样的 out_trade_no 重复调用本接口会得到新的 code_url 且旧链接失效，可用于
// 二维码刷新，无需新建订单。
func CreateWechatNativeOrder(ctx context.Context, params *WechatNativeParams) (codeURL string, err error) {
	client, err := newWechatPayClient()
	if err != nil {
		return "", err
	}
	appID := strings.TrimSpace(setting.WechatPayAppID)
	if appID == "" {
		return "", errors.New("微信支付 AppID 未配置")
	}

	amount := make(gopay.BodyMap)
	amount.Set("total", params.AmountTotal).Set("currency", "CNY")

	bm := make(gopay.BodyMap)
	bm.Set("appid", appID).
		Set("description", params.Description).
		Set("out_trade_no", params.TradeNo).
		Set("notify_url", params.NotifyURL).
		Set("amount", amount)
	if params.TimeExpire != "" {
		bm.Set("time_expire", params.TimeExpire)
	}

	rsp, err := client.V3TransactionNative(ctx, bm)
	if err != nil {
		return "", fmt.Errorf("微信下单失败: %w", err)
	}
	if rsp.Code != wechat.Success {
		return "", fmt.Errorf("微信下单返回异常 code=%s message=%s", rsp.ErrResponse.Code, rsp.ErrResponse.Message)
	}
	if rsp.Response == nil || rsp.Response.CodeUrl == "" {
		return "", errors.New("微信下单未返回二维码链接")
	}
	return rsp.Response.CodeUrl, nil
}

// WechatNotifyResult 是验签并解密后的支付结果通知。
type WechatNotifyResult struct {
	AppID       string
	MchID       string
	TradeNo     string
	TransIDWx   string
	TradeState  string
	AmountTotal int
	SuccessTime string
}

// ParseWechatNotify 验签并解密支付结果通知。
//
// 微信会刻意发送签名无效的探测报文来检验商户是否真的验签，这类报文的签名带
// WECHATPAY/SIGNTEST/ 前缀。验签失败必须向微信返回 4xx 或 5xx，不能当作正常通知
// 放过，也不能对探测流量做特殊处理。
//
// 返回 nil 表示事件类型不是支付成功，调用方应答 200 忽略即可。
func ParseWechatNotify(req *http.Request) (*WechatNotifyResult, error) {
	client, err := newWechatPayClient()
	if err != nil {
		return nil, err
	}
	notifyReq, err := wechat.V3ParseNotify(req)
	if err != nil {
		return nil, fmt.Errorf("解析微信通知失败: %w", err)
	}
	if err := notifyReq.VerifySignByPKMap(client.WxPublicKeyMap()); err != nil {
		return nil, fmt.Errorf("微信通知验签失败: %w", err)
	}
	if notifyReq.EventType != wechatTransactionSuccessEvent {
		return nil, nil
	}
	result, err := notifyReq.DecryptPayCipherText(strings.TrimSpace(setting.WechatPayAPIv3Key))
	if err != nil {
		return nil, fmt.Errorf("微信通知解密失败: %w", err)
	}
	parsed := &WechatNotifyResult{
		AppID:       result.Appid,
		MchID:       result.Mchid,
		TradeNo:     result.OutTradeNo,
		TransIDWx:   result.TransactionId,
		TradeState:  result.TradeState,
		SuccessTime: result.SuccessTime,
	}
	if result.Amount != nil {
		parsed.AmountTotal = result.Amount.Total
	}
	return parsed, nil
}

// WechatTradeState 是主动查单返回的交易信息。
type WechatTradeState struct {
	TradeState  string
	AmountTotal int
	TransIDWx   string
}

// QueryWechatTrade 按商户订单号主动查询交易状态。
//
// 微信明确要求商户不能只依赖回调，必须结合查单。订单在微信侧不存在时返回 nil, nil，
// 表示用户尚未扫码支付。
func QueryWechatTrade(ctx context.Context, tradeNo string) (*WechatTradeState, error) {
	client, err := newWechatPayClient()
	if err != nil {
		return nil, err
	}
	rsp, err := client.V3TransactionQueryOrder(ctx, wechat.OutTradeNo, tradeNo)
	if err != nil {
		return nil, fmt.Errorf("微信查单失败: %w", err)
	}
	if rsp.Code != wechat.Success {
		if rsp.ErrResponse.Code == "ORDER_NOT_EXIST" {
			return nil, nil
		}
		return nil, fmt.Errorf("微信查单返回异常 code=%s message=%s", rsp.ErrResponse.Code, rsp.ErrResponse.Message)
	}
	if rsp.Response == nil {
		return nil, errors.New("微信查单返回为空")
	}
	state := &WechatTradeState{
		TradeState: rsp.Response.TradeState,
		TransIDWx:  rsp.Response.TransactionId,
	}
	if rsp.Response.Amount != nil {
		state.AmountTotal = rsp.Response.Amount.Total
	}
	return state, nil
}

// CloseWechatTrade 关闭一笔未支付的订单，使其进入失败终态。
//
// 二维码超时后应当关单，否则用户过很久再扫码仍可能付款成功，造成悬挂订单。
func CloseWechatTrade(ctx context.Context, tradeNo string) error {
	client, err := newWechatPayClient()
	if err != nil {
		return err
	}
	rsp, err := client.V3TransactionCloseOrder(ctx, tradeNo)
	if err != nil {
		return fmt.Errorf("微信关单失败: %w", err)
	}
	// 关单成功返回 204，订单不存在与关单成功等效。
	if rsp.Code != wechat.Success && rsp.ErrResponse.Code != "ORDER_NOT_EXIST" {
		return fmt.Errorf("微信关单返回异常 code=%s message=%s", rsp.ErrResponse.Code, rsp.ErrResponse.Message)
	}
	return nil
}
