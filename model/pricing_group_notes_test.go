package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道的对外说明按 模型 -> 分组 汇总到模型广场：同一分组下多个渠道的说明去重后都展示，
// 没写说明的渠道不占位，说明只出现在该渠道实际提供的模型和分组上。
func TestPricingGroupNotesCollectChannelPublicNotes(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertChannel := func(id int, note string) {
		channel := &Channel{Id: id, Type: constant.ChannelTypeAutoDLComfyUI, Key: "key", Status: common.ChannelStatusEnabled, Name: "autodl"}
		channel.SetOtherSettings(dto.ChannelOtherSettings{PublicNote: note})
		require.NoError(t, DB.Create(channel).Error)
	}
	insertAbility := func(channelID int, group, modelName string) {
		require.NoError(t, DB.Create(&Ability{Group: group, Model: modelName, ChannelId: channelID, Enabled: true}).Error)
	}
	insertChannel(201, "加速版，画质略低于官方")
	insertChannel(202, "  闲时 00:00-07:50 更便宜  ")
	insertChannel(203, "")
	insertChannel(204, "加速版，画质略低于官方")
	insertAbility(201, "default", "h3-turbo")
	insertAbility(202, "default", "h3-turbo")
	insertAbility(204, "default", "h3-turbo")
	insertAbility(203, "vip", "h3-turbo")
	insertAbility(201, "default", "h3-ref")
	insertAbility(203, "default", "h3-plain")

	InitChannelCache()
	byModel := make(map[string]map[string][]string)
	for _, pricing := range GetPricing() {
		byModel[pricing.ModelName] = pricing.GroupNotes
	}

	assert.Equal(t, map[string][]string{
		"default": {"加速版，画质略低于官方", "闲时 00:00-07:50 更便宜"},
	}, byModel["h3-turbo"])
	assert.Equal(t, map[string][]string{"default": {"加速版，画质略低于官方"}}, byModel["h3-ref"])
	assert.Nil(t, byModel["h3-plain"])
}

// 只由 ComfyUI 工作流渠道提供的模型才标记 workflow_api：同时挂在官方渠道上的模型参数
// 以官方为准，不能套用工作流的说明。
func TestPricingMarksWorkflowOnlyModels(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertChannel := func(id, channelType int) {
		require.NoError(t, DB.Create(&Channel{Id: id, Type: channelType, Key: "key", Status: common.ChannelStatusEnabled, Name: "channel"}).Error)
	}
	insertAbility := func(channelID int, modelName string) {
		require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: channelID, Enabled: true}).Error)
	}
	insertChannel(301, constant.ChannelTypeAutoDLComfyUI)
	insertChannel(302, constant.ChannelTypeAutoDLComfyUI)
	insertChannel(303, constant.ChannelTypeMiniMax)
	insertAbility(301, "MiniMax-H3-Turbo")
	insertAbility(302, "MiniMax-H3-Turbo")
	insertAbility(301, "MiniMax-H3")
	insertAbility(303, "MiniMax-H3")

	InitChannelCache()
	byModel := make(map[string]bool)
	for _, pricing := range GetPricing() {
		byModel[pricing.ModelName] = pricing.WorkflowAPI
	}

	assert.True(t, byModel["MiniMax-H3-Turbo"])
	assert.False(t, byModel["MiniMax-H3"])
}
