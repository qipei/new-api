package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenRequest 内嵌 model.Token，外层同名字段会遮蔽内嵌的那个。整套默认值逻辑
// 都建立在这个前提上，所以先把它钉死：内嵌字段必须一直是零值，解析结果只在
// 外层的 crossGroupRetryInput 里。
func TestTokenRequestCrossGroupRetryParsing(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantSet  bool
		wantVal  bool
		wantAuto bool // 按 AddToken 的规则算出来的最终值（group=auto 时）
	}{
		{name: "字段缺失时默认开启", body: `{"name":"k","group":"auto"}`, wantSet: false, wantVal: false, wantAuto: true},
		{name: "显式 true 保持开启", body: `{"name":"k","group":"auto","cross_group_retry":true}`, wantSet: true, wantVal: true, wantAuto: true},
		{name: "显式 false 必须被尊重", body: `{"name":"k","group":"auto","cross_group_retry":false}`, wantSet: true, wantVal: false, wantAuto: false},
		{name: "null 视为已传且为 false", body: `{"name":"k","group":"auto","cross_group_retry":null}`, wantSet: true, wantVal: false, wantAuto: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var request tokenRequest
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &request))

			assert.Equal(t, tc.wantSet, request.CrossGroupRetry.Set)
			assert.Equal(t, tc.wantVal, request.CrossGroupRetry.Value)
			assert.False(t, request.Token.CrossGroupRetry, "内嵌字段被遮蔽，必须保持零值")

			// AddToken 里 group=auto 分支的取值规则
			assert.Equal(t, tc.wantAuto, !request.CrossGroupRetry.Set || request.CrossGroupRetry.Value)
		})
	}
}
