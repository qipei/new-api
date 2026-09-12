package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 元转分是直连通道唯一的金额换算点，回调校验依赖它判断上游金额是否与订单一致。
// 换算一旦出错，要么把合法支付判成金额不符而拒绝入账，要么放过金额被篡改的通知。
func TestDirectPayMoneyToCents(t *testing.T) {
	cases := []struct {
		name  string
		money float64
		want  int
	}{
		{name: "整数元", money: 10, want: 1000},
		{name: "两位小数", money: 0.01, want: 1},
		{name: "浮点误差 1.1", money: 1.1, want: 110},
		{name: "浮点误差 8.7", money: 8.7, want: 870},
		{name: "浮点误差 29.97", money: 29.97, want: 2997},
		{name: "三位小数向上取整", money: 1.005, want: 101},
		{name: "三位小数向下取整", money: 1.004, want: 100},
		{name: "大额", money: 100000, want: 10000000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := directPayMoneyToCents(tc.money)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 超出可表示范围的金额必须返回错误，而不是回绕成一个小额甚至负数。
func TestDirectPayMoneyToCentsRejectsOutOfRange(t *testing.T) {
	overflow := float64(common.MaxWalletQuota)

	_, err := directPayMoneyToCents(overflow)
	assert.Error(t, err, "超出上限的金额必须换算失败")
}
