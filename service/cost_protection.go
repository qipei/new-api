package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/samber/hot"
	"github.com/shopspring/decimal"
)

const (
	costProtectionCacheNamespace = "new-api:cost-protection:v1"
	costProtectionAvoidTTL       = 10 * time.Minute
	costProtectionBatchDelay     = time.Second
	costProtectionBatchSize      = 100
	costProtectionMaxAttempts    = 3
	costProtectionRetryDelay     = 3 * time.Second
)

type costProtectionState struct {
	ChannelIDs []int `json:"channel_ids"`
}

type costProtectionJob struct {
	logID   int
	attempt int
}

var (
	costProtectionCacheOnce sync.Once
	costProtectionCache     *cachex.HybridCache[costProtectionState]
	costProtectionLocks     [64]sync.Mutex
	costProtectionQueue     = make(chan costProtectionJob, 4096)
)

func init() {
	model.RegisterConsumeLogCreatedHook(func(logID int) {
		enqueueCostProtectionJob(costProtectionJob{logID: logID})
	})
	go runCostProtectionWorker()
}

func getCostProtectionCache() *cachex.HybridCache[costProtectionState] {
	costProtectionCacheOnce.Do(func() {
		costProtectionCache = cachex.NewHybridCache(cachex.HybridCacheConfig[costProtectionState]{
			Namespace: cachex.Namespace(costProtectionCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[costProtectionState]{},
			Memory: func() *hot.HotCache[string, costProtectionState] {
				return hot.NewHotCache[string, costProtectionState](hot.LRU, 100_000).
					WithTTL(costProtectionAvoidTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return costProtectionCache
}

func costProtectionKey(group, modelName string) string {
	sum := sha256.Sum256([]byte(group + "\x00" + modelName))
	return fmt.Sprintf("%x", sum[:])
}

func costProtectionLock(key string) *sync.Mutex {
	sum := sha256.Sum256([]byte(key))
	return &costProtectionLocks[int(sum[0])%len(costProtectionLocks)]
}

func getCostProtectedChannelIDs(group, modelName string) map[int]struct{} {
	state, found, err := getCostProtectionCache().Get(costProtectionKey(group, modelName))
	if err != nil {
		common.SysError("cost protection cache get failed: " + err.Error())
		return nil
	}
	if !found || len(state.ChannelIDs) == 0 {
		return nil
	}
	excluded := make(map[int]struct{}, len(state.ChannelIDs))
	for _, channelID := range state.ChannelIDs {
		if channelID > 0 {
			excluded[channelID] = struct{}{}
		}
	}
	return excluded
}

func markCostProtectedChannel(group, modelName string, channelID int) error {
	if group == "" || modelName == "" || channelID <= 0 {
		return nil
	}
	key := costProtectionKey(group, modelName)
	lock := costProtectionLock(key)
	lock.Lock()
	defer lock.Unlock()

	state, _, err := getCostProtectionCache().Get(key)
	if err != nil {
		return err
	}
	for _, existing := range state.ChannelIDs {
		if existing == channelID {
			return getCostProtectionCache().SetWithTTL(key, state, costProtectionAvoidTTL)
		}
	}
	state.ChannelIDs = append(state.ChannelIDs, channelID)
	sort.Ints(state.ChannelIDs)
	return getCostProtectionCache().SetWithTTL(key, state, costProtectionAvoidTTL)
}

func enqueueCostProtectionJob(job costProtectionJob) {
	if job.logID <= 0 {
		return
	}
	select {
	case costProtectionQueue <- job:
	default:
		common.SysError(fmt.Sprintf("cost protection queue full, skipped log %d", job.logID))
	}
}

func runCostProtectionWorker() {
	for first := range costProtectionQueue {
		jobs := []costProtectionJob{first}
		timer := time.NewTimer(costProtectionBatchDelay)
	collect:
		for len(jobs) < costProtectionBatchSize {
			select {
			case job := <-costProtectionQueue:
				jobs = append(jobs, job)
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		processCostProtectionJobs(context.Background(), jobs)
	}
}

func processCostProtectionJobs(ctx context.Context, jobs []costProtectionJob) {
	jobByLogID := make(map[int]costProtectionJob, len(jobs))
	logIDs := make([]int, 0, len(jobs))
	for _, job := range jobs {
		if previous, exists := jobByLogID[job.logID]; exists && previous.attempt >= job.attempt {
			continue
		}
		if _, exists := jobByLogID[job.logID]; !exists {
			logIDs = append(logIDs, job.logID)
		}
		jobByLogID[job.logID] = job
	}
	if len(logIDs) == 0 {
		return
	}
	logs, err := model.GetConsumeLogsByIds(logIDs)
	if err != nil {
		common.SysError("cost protection eligibility lookup failed: " + err.Error())
		return
	}
	eligible := make(map[int]struct{}, len(logs))
	eligibleLogIDs := make([]int, 0, len(logs))
	for _, log := range logs {
		if log.ChannelId <= 0 || log.UpstreamRequestId == "" {
			continue
		}
		other, mapErr := common.StrToMap(log.Other)
		if mapErr != nil {
			continue
		}
		billingSource, _ := other["billing_source"].(string)
		if billingSource != BillingSourceWallet && billingSource != BillingSourceSubscription {
			continue
		}
		eligible[log.Id] = struct{}{}
		eligibleLogIDs = append(eligibleLogIDs, log.Id)
	}
	if len(eligibleLogIDs) == 0 {
		return
	}

	costs, err := GetUpstreamCosts(ctx, eligibleLogIDs)
	if err != nil {
		common.SysError("cost protection upstream query failed: " + err.Error())
		costs = nil
	}
	costByLogID := make(map[int]UpstreamCostInfo, len(costs))
	for _, cost := range costs {
		costByLogID[cost.LogId] = cost
	}

	for _, logID := range logIDs {
		if _, ok := eligible[logID]; !ok {
			continue
		}
		cost, found := costByLogID[logID]
		if found {
			if cost.ExceedsPlatform {
				applyCostProtection(ctx, cost)
			}
			continue
		}
		job := jobByLogID[logID]
		if job.attempt+1 >= costProtectionMaxAttempts {
			continue
		}
		next := costProtectionJob{logID: logID, attempt: job.attempt + 1}
		time.AfterFunc(costProtectionRetryDelay, func() {
			enqueueCostProtectionJob(next)
		})
	}
}

func applyCostProtection(ctx context.Context, cost UpstreamCostInfo) {
	logs, err := model.GetConsumeLogsByIds([]int{cost.LogId})
	if err != nil || len(logs) != 1 {
		if err != nil {
			common.SysError("cost protection log lookup failed: " + err.Error())
		}
		return
	}
	log := logs[0]
	if cost.UpstreamQuota < 0 || cost.UpstreamQuotaPerUnit <= 0 || common.QuotaPerUnit <= 0 {
		return
	}
	targetDecimal := decimal.NewFromInt(int64(cost.UpstreamQuota)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Div(decimal.NewFromFloat(cost.UpstreamQuotaPerUnit)).
		Ceil()
	targetQuota, clamp := common.QuotaFromDecimalChecked(targetDecimal)
	if clamp != nil || targetQuota <= log.Quota {
		if clamp != nil {
			common.SysError(fmt.Sprintf("cost protection target clamped for log %d: original=%f clamped=%d", log.Id, clamp.Original, clamp.Clamped))
		}
		return
	}
	other, _ := common.StrToMap(log.Other)
	if other == nil {
		other = make(map[string]interface{})
	}
	requestPath, _ := other["request_path"].(string)

	marked := true
	if err := markCostProtectedChannel(log.Group, log.ModelName, log.ChannelId); err != nil {
		marked = false
		common.SysError("cost protection route update failed: " + err.Error())
	}
	alternative, alternativeGroup, selectErr := findCostProtectionAlternative(log, requestPath)
	if selectErr != nil {
		common.SysError(fmt.Sprintf("cost protection alternative lookup failed for log %d: %s", log.Id, selectErr.Error()))
		return
	}
	if marked && alternative != nil {
		adminInfo, _ := other["admin_info"].(map[string]interface{})
		if adminInfo == nil {
			adminInfo = make(map[string]interface{})
			other["admin_info"] = adminInfo
		}
		adminInfo["cost_protection"] = map[string]interface{}{
			"action":                   "reroute",
			"costly_channel_id":        log.ChannelId,
			"alternative_channel_id":   alternative.Id,
			"alternative_group":        alternativeGroup,
			"upstream_amount_usd":      cost.UpstreamAmountUSD,
			"original_platform_usd":    cost.PlatformAmountUSD,
			"avoidance_ttl_in_seconds": int(costProtectionAvoidTTL.Seconds()),
		}
		if _, err := model.UpdateConsumeLogQuotaAndOther(log.Id, log.Quota, log.Quota, common.MapToJsonStr(other)); err != nil {
			common.SysError("cost protection reroute audit update failed: " + err.Error())
		}
		logger.LogWarn(ctx, fmt.Sprintf("cost protection reroute: log=%d model=%s group=%s costly_channel=%d alternative_group=%s alternative_channel=%d upstream=%f platform=%f",
			log.Id, log.ModelName, log.Group, log.ChannelId, alternativeGroup, alternative.Id, cost.UpstreamAmountUSD, cost.PlatformAmountUSD))
		return
	}

	if err := settleCostProtectionSurcharge(log, other, cost, targetQuota); err != nil {
		common.SysError(fmt.Sprintf("cost protection surcharge failed for log %d: %s", log.Id, err.Error()))
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("cost protection surcharge: log=%d model=%s channel=%d original_quota=%d final_quota=%d",
		log.Id, log.ModelName, log.ChannelId, log.Quota, targetQuota))
}

func findCostProtectionAlternative(log *model.Log, requestPath string) (*model.Channel, string, error) {
	groups := []string{log.Group}
	if log.TokenId > 0 {
		if token, err := model.GetTokenById(log.TokenId); err == nil && token != nil && token.Group == "auto" {
			autoGroups, autoErr := token.GetAutoGroups()
			if autoErr != nil {
				return nil, "", autoErr
			}
			userGroup, groupErr := model.GetUserGroup(token.UserId, false)
			if groupErr == nil {
				if token.AutoGroups == "" {
					autoGroups = GetUserAutoGroup(userGroup)
				} else {
					autoGroups = FilterUserTokenAutoGroups(userGroup, autoGroups)
				}
			}
			if len(autoGroups) > 0 {
				groups = autoGroups
			}
		}
	}

	seenGroups := make(map[string]struct{}, len(groups)+1)
	orderedGroups := make([]string, 0, len(groups)+1)
	orderedGroups = append(orderedGroups, log.Group)
	seenGroups[log.Group] = struct{}{}
	for _, group := range groups {
		if _, exists := seenGroups[group]; exists {
			continue
		}
		seenGroups[group] = struct{}{}
		orderedGroups = append(orderedGroups, group)
	}
	for _, group := range orderedGroups {
		excluded := getCostProtectedChannelIDs(group, log.ModelName)
		if excluded == nil {
			excluded = make(map[int]struct{})
		}
		excluded[log.ChannelId] = struct{}{}
		alternative, err := model.GetRandomSatisfiedChannelExcluding(group, log.ModelName, 0, requestPath, excluded)
		if err != nil {
			return nil, "", err
		}
		if alternative != nil {
			return alternative, group, nil
		}
	}
	return nil, "", nil
}

func settleCostProtectionSurcharge(log *model.Log, other map[string]interface{}, cost UpstreamCostInfo, targetQuota int) error {
	latest, err := model.GetConsumeLogsByIds([]int{log.Id})
	if err != nil {
		return err
	}
	if len(latest) != 1 {
		return fmt.Errorf("consume log %d not found", log.Id)
	}
	if latest[0].Quota >= targetQuota {
		return nil
	}
	log = latest[0]
	other, err = common.StrToMap(log.Other)
	if err != nil {
		return err
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	delta := targetQuota - log.Quota
	if delta <= 0 {
		return nil
	}

	billingSource, _ := other["billing_source"].(string)
	if billingSource != BillingSourceWallet && billingSource != BillingSourceSubscription {
		return fmt.Errorf("unsupported or missing billing source %q", billingSource)
	}
	if log.UserId <= 0 {
		return fmt.Errorf("invalid user id %d", log.UserId)
	}
	subscriptionID := 0
	if value, ok := other["subscription_id"].(float64); ok && value > 0 && value <= float64(math.MaxInt) && math.Trunc(value) == value {
		subscriptionID = int(value)
	}
	if billingSource == BillingSourceSubscription && subscriptionID <= 0 {
		return fmt.Errorf("subscription billing source is missing subscription id")
	}
	if billingSource == BillingSourceSubscription {
		if err := model.PostConsumeUserSubscriptionDelta(subscriptionID, int64(delta)); err != nil {
			return err
		}
	} else if err := model.DecreaseUserQuota(log.UserId, delta, false); err != nil {
		return err
	}

	var token *model.Token
	if log.TokenId > 0 {
		token, err = model.GetTokenById(log.TokenId)
		if err == nil && token != nil && !token.UnlimitedQuota {
			err = model.DecreaseTokenQuota(token.Id, token.Key, delta)
		}
	}
	if err != nil {
		if billingSource == BillingSourceSubscription && subscriptionID > 0 {
			_ = model.PostConsumeUserSubscriptionDelta(subscriptionID, -int64(delta))
		} else {
			_ = model.IncreaseUserQuota(log.UserId, delta, false)
		}
		return err
	}

	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["cost_protection"] = map[string]interface{}{
		"action":                  "surcharge",
		"original_quota":          log.Quota,
		"final_quota":             targetQuota,
		"delta_quota":             delta,
		"upstream_amount_usd":     cost.UpstreamAmountUSD,
		"original_platform_usd":   cost.PlatformAmountUSD,
		"upstream_quota_per_unit": cost.UpstreamQuotaPerUnit,
	}
	updated, updateErr := model.UpdateConsumeLogQuotaAndOther(log.Id, log.Quota, targetQuota, common.MapToJsonStr(other))
	if updateErr != nil || !updated {
		if token != nil && !token.UnlimitedQuota {
			_ = model.IncreaseTokenQuota(token.Id, token.Key, delta)
		}
		if billingSource == BillingSourceSubscription && subscriptionID > 0 {
			_ = model.PostConsumeUserSubscriptionDelta(subscriptionID, -int64(delta))
		} else {
			_ = model.IncreaseUserQuota(log.UserId, delta, false)
		}
		if updateErr != nil {
			return updateErr
		}
		return fmt.Errorf("consume log changed before surcharge settlement")
	}
	model.UpdateUserUsedQuota(log.UserId, delta)
	model.UpdateChannelUsedQuota(log.ChannelId, delta)
	return nil
}
