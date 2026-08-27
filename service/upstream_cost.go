package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

const (
	upstreamCostFetchTimeout = 10 * time.Second
	upstreamCostPageSize     = 1000
	// 上游每页最多返回 100 条，高流量 token 的请求很快被挤出首页，只能逐条点查补齐；
	// 上限用来兜住管理端一次查 100 条历史日志的场景，避免打爆上游接口限流。
	upstreamCostPointLookupLimit = 8
	upstreamCostMaxBodyBytes     = 8 << 20
	// 只有「上游暂时不可用」才熔断，且窗口很短：熔断同时会让管理端的上游计费查询失效，
	// 窗口过长会把一次抖动放大成数分钟的功能静默失效。
	upstreamCostFailureTTL = time.Minute
)

// transientUpstreamError 标记「上游暂时不可用」（连接失败、超时、5xx、429）。
// 其余错误（鉴权失败、响应格式不符）重试再快也不会变好，因此不熔断，
// 而是每次都留下诊断日志，避免问题被静默吞掉。
type transientUpstreamError struct{ err error }

func (e transientUpstreamError) Error() string { return e.err.Error() }

func (e transientUpstreamError) Unwrap() error { return e.err }

func isTransientUpstreamError(err error) bool {
	var transient transientUpstreamError
	return errors.As(err, &transient)
}

var unsupportedUpstreamCostSources sync.Map

// UpstreamCostInfo 描述一次请求在上游的实际成本与本平台已计费金额的对比。
//
// 两个部署的 quota 单位不可直接比较：quota/quota_per_unit 得到的是各自的「系统美元」，
// 而一个系统美元值多少真实货币由各自的充值单价 price 决定（例如上游 6.82 元、本平台 1 元）。
// 因此必须先各自折成真实货币才可比，NormalizedUpstreamQuota 就是折算回本平台额度单位的上游成本。
type UpstreamCostInfo struct {
	LogId                int     `json:"log_id"`
	UpstreamQuota        int     `json:"upstream_quota"`
	UpstreamQuotaPerUnit float64 `json:"upstream_quota_per_unit"`
	UpstreamPrice        float64 `json:"upstream_price"`
	PlatformQuota        int     `json:"platform_quota"`
	PlatformQuotaPerUnit float64 `json:"platform_quota_per_unit"`
	PlatformPrice        float64 `json:"platform_price"`
	// NormalizedUpstreamQuota 是上游成本换算成本平台额度单位后的值，也是补扣的目标额度。
	NormalizedUpstreamQuota int     `json:"normalized_upstream_quota"`
	UpstreamAmount          float64 `json:"upstream_amount"`
	PlatformAmount          float64 `json:"platform_amount"`
	ExceedsPlatform         bool    `json:"exceeds_platform"`
}

type upstreamCostTarget struct {
	logId         int
	requestId     string
	platformQuota int
}

type upstreamCostGroup struct {
	cacheKey string
	baseURL  string
	apiKey   string
	targets  []upstreamCostTarget
}

type upstreamStatusEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
		// Price 是上游的充值单价（1 系统美元 = ? 元），用于把上游 quota 折成真实货币。
		Price float64 `json:"price"`
	} `json:"data"`
}

type upstreamLogEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

type upstreamLogPage struct {
	Items []upstreamLogItem `json:"items"`
}

type upstreamLogItem struct {
	RequestId         string `json:"request_id"`
	UpstreamRequestId string `json:"upstream_request_id"`
	Quota             int    `json:"quota"`
	// Type 是上游日志类型；上游按 token 返回的是全部类型（消费、错误、退款等），
	// 字段缺失时为 0，此时不做类型过滤以兼容不返回该字段的上游。
	Type int `json:"type"`
}

// isUpstreamConsumeCharge 判断一行上游日志是否代表这次请求的实际计费。
// 上游同一个 request_id 可能有多行（预扣费与结算分开记、或额外的错误/退款记录），
// 其中非消费行的 quota 往往是 0；把 0 当成真实成本会让比价得出「上游没花钱」，
// 补扣因此不触发，这一笔的亏损就永远收不回来。
func isUpstreamConsumeCharge(item upstreamLogItem) bool {
	if item.Quota <= 0 {
		return false
	}
	return item.Type == 0 || item.Type == model.LogTypeConsume
}

func GetUpstreamCosts(ctx context.Context, logIds []int) ([]UpstreamCostInfo, error) {
	logs, err := model.GetConsumeLogsByIds(logIds)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return []UpstreamCostInfo{}, nil
	}

	channelIds := make([]int, 0, len(logs))
	seenChannelIds := make(map[int]struct{}, len(logs))
	for _, log := range logs {
		if log.ChannelId <= 0 || log.UpstreamRequestId == "" {
			continue
		}
		if _, ok := seenChannelIds[log.ChannelId]; ok {
			continue
		}
		seenChannelIds[log.ChannelId] = struct{}{}
		channelIds = append(channelIds, log.ChannelId)
	}
	channels, err := model.GetChannelsByIds(channelIds)
	if err != nil {
		return nil, err
	}
	channelById := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}

	groups := make(map[string]*upstreamCostGroup)
	for _, log := range logs {
		channel := channelById[log.ChannelId]
		if channel == nil || channel.BaseURL == nil || log.UpstreamRequestId == "" {
			continue
		}
		apiKey, ok := resolveUpstreamCostKey(channel, log.Other)
		if !ok {
			continue
		}
		baseURL, ok := normalizeUpstreamCostBaseURL(*channel.BaseURL)
		if !ok {
			continue
		}
		fingerprint := sha256.Sum256([]byte(baseURL + "\x00" + apiKey))
		groupKey := hex.EncodeToString(fingerprint[:])
		group := groups[groupKey]
		if group == nil {
			group = &upstreamCostGroup{cacheKey: groupKey, baseURL: baseURL, apiKey: apiKey}
			groups[groupKey] = group
		}
		group.targets = append(group.targets, upstreamCostTarget{
			logId:         log.Id,
			requestId:     log.UpstreamRequestId,
			platformQuota: log.Quota,
		})
	}

	results := make(chan []UpstreamCostInfo, len(groups))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, group := range groups {
		wg.Add(1)
		go func(group *upstreamCostGroup) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			costs := fetchUpstreamCostGroup(ctx, group)
			if len(costs) > 0 {
				results <- costs
			}
		}(group)
	}
	wg.Wait()
	close(results)

	allCosts := make([]UpstreamCostInfo, 0, len(logs))
	for costs := range results {
		allCosts = append(allCosts, costs...)
	}
	return allCosts, nil
}

func resolveUpstreamCostKey(channel *model.Channel, other string) (string, bool) {
	if !channel.ChannelInfo.IsMultiKey {
		key := strings.TrimSpace(channel.Key)
		return key, key != ""
	}
	otherMap, err := common.StrToMap(other)
	if err != nil {
		return "", false
	}
	adminInfo, ok := otherMap["admin_info"].(map[string]interface{})
	if !ok {
		return "", false
	}
	indexValue, ok := adminInfo["multi_key_index"].(float64)
	if !ok || indexValue < 0 || math.Trunc(indexValue) != indexValue {
		return "", false
	}
	keys := channel.GetKeys()
	index := int(indexValue)
	if index >= len(keys) {
		return "", false
	}
	key := strings.TrimSpace(keys[index])
	return key, key != ""
}

func normalizeUpstreamCostBaseURL(rawBaseURL string) (string, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(rawBaseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), true
}

func fetchUpstreamCostGroup(parentCtx context.Context, group *upstreamCostGroup) []UpstreamCostInfo {
	if retryAtValue, ok := unsupportedUpstreamCostSources.Load(group.cacheKey); ok {
		if retryAt, valid := retryAtValue.(time.Time); valid && time.Now().Before(retryAt) {
			common.SysLog(fmt.Sprintf("upstream cost fetch skipped for %s: circuit open until %s", group.baseURL, retryAt.Format(time.RFC3339)))
			return nil
		}
		unsupportedUpstreamCostSources.Delete(group.cacheKey)
	}
	ctx, cancel := context.WithTimeout(parentCtx, upstreamCostFetchTimeout)
	defer cancel()

	var status upstreamStatusEnvelope
	var logs []upstreamLogItem
	var statusErr error
	var logsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		statusErr = fetchUpstreamJSON(ctx, group.baseURL+"/api/status", "", &status)
	}()
	go func() {
		defer wg.Done()
		logs, logsErr = fetchUpstreamLogs(ctx, group.baseURL, group.apiKey, "")
	}()
	wg.Wait()
	failure := ""
	transient := false
	switch {
	case statusErr != nil:
		failure = "/api/status: " + statusErr.Error()
		transient = isTransientUpstreamError(statusErr)
	case logsErr != nil:
		failure = "/api/log/token: " + logsErr.Error()
		transient = isTransientUpstreamError(logsErr)
	case !status.Success:
		failure = "/api/status returned success=false"
	case status.Data.QuotaPerUnit <= 0:
		failure = fmt.Sprintf("/api/status returned quota_per_unit=%v", status.Data.QuotaPerUnit)
	case status.Data.Price <= 0:
		// 没有上游充值单价就无法把两边的 quota 折成同一种真实货币，
		// 此时宁可跳过也不能退回「直接比 quota」——那会因为单位不同得出相反的结论。
		failure = fmt.Sprintf("/api/status returned price=%v, cannot normalize upstream cost", status.Data.Price)
	case common.QuotaPerUnit <= 0:
		failure = fmt.Sprintf("local QuotaPerUnit=%v", common.QuotaPerUnit)
	case operation_setting.Price <= 0:
		failure = fmt.Sprintf("local recharge price=%v", operation_setting.Price)
	}
	if failure != "" {
		common.SysError(fmt.Sprintf("upstream cost fetch failed for %s (%d logs): %s [transient=%v]",
			group.baseURL, len(group.targets), failure, transient))
		if transient && group.cacheKey != "" {
			unsupportedUpstreamCostSources.Store(group.cacheKey, time.Now().Add(upstreamCostFailureTTL))
		}
		return nil
	}
	unsupportedUpstreamCostSources.Delete(group.cacheKey)

	// 上游按新→旧返回，同一个 request_id 取最先出现（最新）的那一行，不被更旧的行覆盖。
	quotaByRequestId := make(map[string]int, len(logs)*2)
	for _, log := range logs {
		if !isUpstreamConsumeCharge(log) {
			continue
		}
		if log.RequestId != "" {
			if _, seen := quotaByRequestId[log.RequestId]; !seen {
				quotaByRequestId[log.RequestId] = log.Quota
			}
		}
		if log.UpstreamRequestId != "" {
			if _, seen := quotaByRequestId[log.UpstreamRequestId]; !seen {
				quotaByRequestId[log.UpstreamRequestId] = log.Quota
			}
		}
	}

	resolveMissingUpstreamQuotas(ctx, group, quotaByRequestId)

	costs := make([]UpstreamCostInfo, 0, len(group.targets))
	missing := make([]string, 0, len(group.targets))
	for _, target := range group.targets {
		upstreamQuota, ok := quotaByRequestId[target.requestId]
		if !ok {
			missing = append(missing, target.requestId)
			continue
		}
		if upstreamQuota < 0 || target.platformQuota < 0 {
			continue
		}
		normalizedQuota, clamp := common.QuotaFromDecimalChecked(normalizeUpstreamQuota(upstreamQuota, status.Data.QuotaPerUnit, status.Data.Price))
		if clamp != nil {
			common.SysError(fmt.Sprintf("upstream cost normalization clamped for log %d: original=%f clamped=%d", target.logId, clamp.Original, clamp.Clamped))
			continue
		}
		costs = append(costs, UpstreamCostInfo{
			LogId:                   target.logId,
			UpstreamQuota:           upstreamQuota,
			UpstreamQuotaPerUnit:    status.Data.QuotaPerUnit,
			UpstreamPrice:           status.Data.Price,
			PlatformQuota:           target.platformQuota,
			PlatformQuotaPerUnit:    common.QuotaPerUnit,
			PlatformPrice:           operation_setting.Price,
			NormalizedUpstreamQuota: normalizedQuota,
			UpstreamAmount:          float64(normalizedQuota) / common.QuotaPerUnit,
			PlatformAmount:          float64(target.platformQuota) / common.QuotaPerUnit,
			ExceedsPlatform:         normalizedQuota > target.platformQuota,
		})
	}
	if len(missing) > 0 {
		// 上游写自己的消费日志是异步的，刚结束的请求可能还查不到；这条日志用于区分
		// 「上游不可达」和「上游还没落库」，是补扣未触发时最需要的诊断信息。
		common.SysLog(fmt.Sprintf("upstream cost lookup: %d/%d request ids not found in %s recent logs (%d returned), first missing=%s",
			len(missing), len(group.targets), group.baseURL, len(logs), missing[0]))
	}
	return costs
}

// normalizeUpstreamQuota 把上游 quota 折算成本平台的额度单位：
//
//	上游真实成本 = upstreamQuota / upstreamQuotaPerUnit * upstreamPrice
//	本平台额度   = 上游真实成本 / Price * QuotaPerUnit
//
// 向上取整，保证补扣后至少覆盖上游成本。两边的 price 都以「1 系统美元折合多少元」计价，
// 这是 new-api 的既定语义；接入非同币种计价的上游会失真，因此 price 缺失时调用方直接跳过。
func normalizeUpstreamQuota(upstreamQuota int, upstreamQuotaPerUnit, upstreamPrice float64) decimal.Decimal {
	return decimal.NewFromInt(int64(upstreamQuota)).
		Mul(decimal.NewFromFloat(upstreamPrice)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Div(decimal.NewFromFloat(upstreamQuotaPerUnit)).
		Div(decimal.NewFromFloat(operation_setting.Price)).
		Ceil()
}

// resolveMissingUpstreamQuotas 对最近一页里没找到的请求逐条点查补齐。
// 上游 /api/log/token 每页只返回 100 条（我们请求的 page_size 会被忽略），
// 高流量 token 的请求几分钟内就会被挤出首页，只靠翻页会永久查不到，
// 从而让这笔已经在上游产生成本的请求永远补不回来。
func resolveMissingUpstreamQuotas(ctx context.Context, group *upstreamCostGroup, quotaByRequestId map[string]int) {
	pending := make([]string, 0, len(group.targets))
	for _, target := range group.targets {
		if _, found := quotaByRequestId[target.requestId]; found || target.requestId == "" {
			continue
		}
		pending = append(pending, target.requestId)
	}
	if len(pending) == 0 {
		return
	}
	if len(pending) > upstreamCostPointLookupLimit {
		common.SysLog(fmt.Sprintf("upstream cost point lookup capped at %d of %d missing request ids for %s",
			upstreamCostPointLookupLimit, len(pending), group.baseURL))
		pending = pending[:upstreamCostPointLookupLimit]
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, requestId := range pending {
		wg.Add(1)
		go func(requestId string) {
			defer wg.Done()
			items, err := fetchUpstreamLogs(ctx, group.baseURL, group.apiKey, requestId)
			if err != nil {
				common.SysError(fmt.Sprintf("upstream cost point lookup failed for %s request %s: %s", group.baseURL, requestId, err.Error()))
				return
			}
			for _, item := range items {
				if item.RequestId != requestId && item.UpstreamRequestId != requestId {
					continue
				}
				if !isUpstreamConsumeCharge(item) {
					continue
				}
				mu.Lock()
				quotaByRequestId[requestId] = item.Quota
				mu.Unlock()
				return
			}
		}(requestId)
	}
	wg.Wait()
}

// fetchUpstreamLogs 拉取上游日志。requestId 非空时按该 id 精确点查，否则拉最近一页。
func fetchUpstreamLogs(ctx context.Context, baseURL, apiKey, requestId string) ([]upstreamLogItem, error) {
	endpoint := fmt.Sprintf("%s/api/log/token?page_size=%d", baseURL, upstreamCostPageSize)
	if requestId != "" {
		endpoint = fmt.Sprintf("%s/api/log/token?page_size=1&request_id=%s", baseURL, url.QueryEscape(requestId))
	}
	var envelope upstreamLogEnvelope
	if err := fetchUpstreamJSON(ctx, endpoint, apiKey, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success || len(envelope.Data) == 0 {
		return nil, fmt.Errorf("upstream log endpoint returned no data")
	}

	if common.GetJsonType(envelope.Data) == "array" {
		var logs []upstreamLogItem
		if err := common.Unmarshal(envelope.Data, &logs); err != nil {
			return nil, err
		}
		return logs, nil
	}
	var page upstreamLogPage
	if err := common.Unmarshal(envelope.Data, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

func fetchUpstreamJSON(ctx context.Context, endpoint, apiKey string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := GetHttpClient()
	if client == nil {
		return fmt.Errorf("http client is not initialized")
	}
	resp, err := client.Do(req)
	if err != nil {
		return transientUpstreamError{err: err}
	}
	defer CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		statusErr := fmt.Errorf("upstream returned status %d", resp.StatusCode)
		// 429 是上游明确要求退避（new-api 的 /api/log/token 默认 20 次/20 分钟限流），
		// 5xx 是上游自身故障，两者稍后重试才有意义。
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return transientUpstreamError{err: statusErr}
		}
		return statusErr
	}
	return common.DecodeJson(io.LimitReader(resp.Body, upstreamCostMaxBodyBytes), target)
}
