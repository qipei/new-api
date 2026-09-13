package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// stubNotifyBody 用固定 map 模拟支付宝异步通知的已验签参数。
type stubNotifyBody map[string]string

func (s stubNotifyBody) GetString(key string) string { return s[key] }

func newAlipayNotifyTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/alipay/notify", nil)
	return c
}

// 支付宝要求验签之后仍需逐项核对 app_id、seller_id、out_trade_no 与 total_amount，
// 官方措辞是任何一项不通过都「务必忽略」本次通知。验签只证明报文来自支付宝，
// 证明不了这笔钱是付给我们这笔订单的，所以每一项都必须真的拦得住。
func TestVerifyAlipayNotifyIdentity(t *testing.T) {
	const (
		appID    = "2021006160666755"
		sellerID = "2088123456789012"
	)

	originalAppID := setting.AlipayAppID
	originalSellerID := setting.AlipaySellerID
	t.Cleanup(func() {
		setting.AlipayAppID = originalAppID
		setting.AlipaySellerID = originalSellerID
	})
	setting.AlipayAppID = appID
	setting.AlipaySellerID = sellerID

	topUp := &model.TopUp{
		TradeNo:         "ALI1NOabc123",
		Money:           29.97,
		PaymentProvider: model.PaymentProviderAlipay,
	}

	validBody := func() stubNotifyBody {
		return stubNotifyBody{
			"app_id":       appID,
			"seller_id":    sellerID,
			"out_trade_no": topUp.TradeNo,
			"total_amount": "29.97",
			"trade_status": "TRADE_SUCCESS",
		}
	}

	cases := []struct {
		name    string
		mutate  func(stubNotifyBody)
		topUp   *model.TopUp
		wantOk  bool
		comment string
	}{
		{
			name:   "全部匹配",
			mutate: func(stubNotifyBody) {},
			wantOk: true,
		},
		{
			name:   "app_id 不匹配",
			mutate: func(b stubNotifyBody) { b["app_id"] = "2021000000000000" },
			wantOk: false,
		},
		{
			name:   "seller_id 不匹配",
			mutate: func(b stubNotifyBody) { b["seller_id"] = "2088000000000000" },
			wantOk: false,
		},
		{
			name:   "金额被放大",
			mutate: func(b stubNotifyBody) { b["total_amount"] = "0.01" },
			wantOk: false,
		},
		{
			name:   "金额多出一分",
			mutate: func(b stubNotifyBody) { b["total_amount"] = "29.98" },
			wantOk: false,
		},
		{
			name:   "金额无法解析",
			mutate: func(b stubNotifyBody) { b["total_amount"] = "" },
			wantOk: false,
		},
		{
			name:   "尾随零视为等值",
			mutate: func(b stubNotifyBody) { b["total_amount"] = "29.970" },
			wantOk: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validBody()
			tc.mutate(body)
			got := verifyAlipayNotifyIdentity(newAlipayNotifyTestContext(t), body, topUp)
			assert.Equal(t, tc.wantOk, got)
		})
	}
}

// 订单属于其它支付网关时必须拒绝，防止一条通道的回调去结算另一条通道的订单。
func TestVerifyAlipayNotifyIdentityRejectsForeignProvider(t *testing.T) {
	const (
		appID    = "2021006160666755"
		sellerID = "2088123456789012"
	)

	originalAppID := setting.AlipayAppID
	originalSellerID := setting.AlipaySellerID
	t.Cleanup(func() {
		setting.AlipayAppID = originalAppID
		setting.AlipaySellerID = originalSellerID
	})
	setting.AlipayAppID = appID
	setting.AlipaySellerID = sellerID

	epayOrder := &model.TopUp{
		TradeNo:         "USR1NOabc123",
		Money:           29.97,
		PaymentProvider: model.PaymentProviderEpay,
	}
	body := stubNotifyBody{
		"app_id":       appID,
		"seller_id":    sellerID,
		"out_trade_no": epayOrder.TradeNo,
		"total_amount": "29.97",
	}

	assert.False(t, verifyAlipayNotifyIdentity(newAlipayNotifyTestContext(t), body, epayOrder))
}
