package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID)
	}
	return false
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	var count int64
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, modelName, channelID, true).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, normalized, channelID, true).
		Count(&count).Error
	return err == nil && count > 0
}

func isChannelIDInList(list []int, channelID int) bool {
	for _, id := range list {
		if id == channelID {
			return true
		}
	}
	return false
}

// GroupServesModel 报告该分组下有没有可用渠道提供这个模型。比价路由用它把候选
// 分组先筛一遍：站点可能配了几十个分组，而单个模型往往只在其中几个里，不筛的话
// 每次请求都要在排序里扫过一堆注定选不中的分组。
//
// 内存缓存关闭时退回数据库判断，语义与 IsChannelEnabledForGroupModel 一致。
func GroupServesModel(group string, modelName string) bool {
	if group == "" || modelName == "" {
		return false
	}
	if !common.MemoryCacheEnabled {
		return groupServesModelDB(group, modelName)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false
	}
	if len(group2model2channels[group][modelName]) > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		return len(group2model2channels[group][normalized]) > 0
	}
	return false
}

func groupServesModelDB(group string, modelName string) bool {
	var count int64
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? AND model = ? AND enabled = "+commonTrueVal, group, modelName).
		Limit(1).Count(&count).Error
	if err != nil {
		common.SysError("group serves model lookup failed: " + err.Error())
		return true // 查不出来就别把分组筛掉，宁可多扫一个也不要漏掉可用分组
	}
	return count > 0
}
