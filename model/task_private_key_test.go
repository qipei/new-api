package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	commonRelay "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

// 上游任务只能用提交它的账号查询：多 key 渠道必须记下这次选中的 key，轮询和刷新结果链接
// 才不会拿整串 key 去查；单 key 渠道直接用渠道的 key，不重复存一份。
func TestInitTaskStoresSubmittingKey(t *testing.T) {
	tests := []struct {
		name     string
		meta     commonRelay.ChannelMeta
		expected string
	}{
		{"multi-key autodl", commonRelay.ChannelMeta{ChannelType: constant.ChannelTypeAutoDLComfyUI, ChannelIsMultiKey: true, ApiKey: "key-b"}, "key-b"},
		{"single-key autodl", commonRelay.ChannelMeta{ChannelType: constant.ChannelTypeAutoDLComfyUI, ApiKey: "key-a"}, ""},
		{"gemini", commonRelay.ChannelMeta{ChannelType: constant.ChannelTypeGemini, ApiKey: "gemini-key"}, "gemini-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := tt.meta
			task := InitTask(constant.TaskPlatform("61"), &commonRelay.RelayInfo{ChannelMeta: &meta})
			assert.Equal(t, tt.expected, task.PrivateData.Key)
		})
	}
}
