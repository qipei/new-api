package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 自动选路按分组里提供该模型的可用渠道类型判断能不能处理请求：同类型去重，停用的渠道
// 不算；内存缓存关闭时从数据库得出同样的结果。
func TestGroupModelChannelTypes(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertChannel := func(id, channelType, status int, group string) {
		require.NoError(t, DB.Create(&Channel{Id: id, Type: channelType, Key: "key", Status: status, Name: "c", Group: group, Models: "h3"}).Error)
		require.NoError(t, DB.Create(&Ability{Group: group, Model: "h3", ChannelId: id, Enabled: status == common.ChannelStatusEnabled}).Error)
	}
	insertChannel(401, constant.ChannelTypeWeNex, common.ChannelStatusEnabled, "we")
	insertChannel(402, constant.ChannelTypeWeNex, common.ChannelStatusEnabled, "we")
	insertChannel(403, constant.ChannelTypeMiniMax, common.ChannelStatusManuallyDisabled, "we")
	insertChannel(404, constant.ChannelTypeMiniMax, common.ChannelStatusEnabled, "mixed")
	insertChannel(405, constant.ChannelTypeWeNex, common.ChannelStatusEnabled, "mixed")
	InitChannelCache()

	assert.Equal(t, []int{constant.ChannelTypeWeNex}, GroupModelChannelTypes("we", "h3"))
	assert.ElementsMatch(t, []int{constant.ChannelTypeMiniMax, constant.ChannelTypeWeNex}, GroupModelChannelTypes("mixed", "h3"))
	assert.Empty(t, GroupModelChannelTypes("other", "h3"))

	common.MemoryCacheEnabled = false
	assert.Equal(t, []int{constant.ChannelTypeWeNex}, GroupModelChannelTypes("we", "h3"))
	assert.ElementsMatch(t, []int{constant.ChannelTypeMiniMax, constant.ChannelTypeWeNex}, GroupModelChannelTypes("mixed", "h3"))
}
