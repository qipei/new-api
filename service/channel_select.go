package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx          *gin.Context
	TokenGroup   string
	ModelName    string
	RequestPath  string
	Retry        *int
	resetNextTry bool
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var protectedFallback *model.Channel
	var protectedFallbackGroup string
	protectedFallbackGroupIndex := 0
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	if IsAutoRoutingGroup(param.TokenGroup) {
		// CUSTOM: 比价路由（fork 扩展）。两种自动路由只有候选列表的来法不同——
		// auto 用管理员编排的顺序，auto_price 每次按当前价格排序——选中之后的
		// 重试、优先级遍历、跨组顺延完全共用下面这套逻辑。
		var autoGroups []string
		if param.TokenGroup == AutoPriceGroup {
			// 排序结果缓存在 context 上：一次请求内的多次重试必须看到同一个顺序，
			// 否则跨组顺延的 AutoGroupIndex 会指向一个已经变了的列表。
			autoGroups = requestPriceRankedGroups(param, userGroup)
			if len(autoGroups) == 0 {
				return nil, selectGroup, errors.New("no priced group is available for this token")
			}
		} else {
			autoGroups = GetRequestAutoGroups(param.Ctx, userGroup)
			if len(autoGroups) == 0 {
				return nil, selectGroup, errors.New("auto groups is not enabled")
			}
		}

		// startGroupIndex: the group index to start searching from
		// startGroupIndex: 开始搜索的分组索引
		startGroupIndex := 0
		// CUSTOM: 比价路由本质就是跨分组的——"当前最便宜的分组"失败之后不顺延到
		// 次便宜的，这个功能就没有意义。所以它不看令牌上的开关，恒为跨组。
		crossGroupRetry := param.TokenGroup == AutoPriceGroup ||
			common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Calculate priorityRetry for current group
			// 计算当前分组的 priorityRetry
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry and update startRetryIndex
			// 如果切换到新分组，重置 priorityRetry 并更新 startRetryIndex
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			var fallback *model.Channel
			channel, fallback, err = selectCostProtectedChannel(autoGroup, param.ModelName, priorityRetry, param.RequestPath)
			if err != nil {
				return nil, autoGroup, err
			}
			if channel == nil && fallback != nil && protectedFallback == nil {
				protectedFallback = fallback
				protectedFallbackGroup = autoGroup
				protectedFallbackGroupIndex = i
			}
			if channel == nil {
				// Current group has no available channel for this model, try next group
				// 当前分组没有该模型的可用渠道，尝试下一个分组
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				// 重置状态以尝试下一个分组
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)

			// Prepare state for next retry
			// 为下一次重试准备状态
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				// Current group has exhausted all retries, prepare to switch to next group
				// This request still uses current group, but next retry will use next group
				// 当前分组已用完所有重试次数，准备切换到下一个分组
				// 本次请求仍使用当前分组，但下次重试将使用下一个分组
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", autoGroup, priorityRetry, common.RetryTimes)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				// Stay in current group, save current state
				// 保持在当前分组，保存当前状态
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
		if channel == nil && protectedFallback != nil {
			channel = protectedFallback
			selectGroup = protectedFallbackGroup
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, protectedFallbackGroup)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, protectedFallbackGroupIndex)
			logger.LogWarn(param.Ctx, fmt.Sprintf("All alternative channels exhausted, falling back to cost-protected channel %d for model %s", protectedFallback.Id, param.ModelName))
		}
	} else {
		var fallback *model.Channel
		channel, fallback, err = selectCostProtectedChannel(param.TokenGroup, param.ModelName, param.GetRetry(), param.RequestPath)
		if err != nil {
			return nil, param.TokenGroup, err
		}
		if channel == nil && fallback != nil {
			channel = fallback
			logger.LogWarn(param.Ctx, fmt.Sprintf("No alternative channel available, falling back to cost-protected channel %d for model %s", fallback.Id, param.ModelName))
		}
	}
	return channel, selectGroup, nil
}

// selectCostProtectedChannel prefers channels without a recent loss signal.
// fallback is returned separately so auto groups can search later groups first;
// callers use it only when no healthy alternative exists, preserving continuity.
func selectCostProtectedChannel(group, modelName string, retry int, requestPath string) (channel, fallback *model.Channel, err error) {
	excluded := getCostProtectedChannelIDs(group, modelName)
	if len(excluded) == 0 {
		channel, err = model.GetRandomSatisfiedChannel(group, modelName, retry, requestPath)
		return channel, nil, err
	}
	channel, err = model.GetRandomSatisfiedChannelExcluding(group, modelName, retry, requestPath, excluded)
	if err != nil || channel != nil {
		return channel, nil, err
	}
	fallback, err = model.GetRandomSatisfiedChannel(group, modelName, retry, requestPath)
	return nil, fallback, err
}
