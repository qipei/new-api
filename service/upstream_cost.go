package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

const (
	upstreamCostFetchTimeout = 5 * time.Second
	upstreamCostPageSize     = 1000
	upstreamCostMaxBodyBytes = 8 << 20
	upstreamCostFailureTTL   = 5 * time.Minute
)

var unsupportedUpstreamCostSources sync.Map

type UpstreamCostInfo struct {
	LogId                int     `json:"log_id"`
	UpstreamQuota        int     `json:"upstream_quota"`
	UpstreamQuotaPerUnit float64 `json:"upstream_quota_per_unit"`
	PlatformQuota        int     `json:"platform_quota"`
	PlatformQuotaPerUnit float64 `json:"platform_quota_per_unit"`
	UpstreamAmountUSD    float64 `json:"upstream_amount_usd"`
	PlatformAmountUSD    float64 `json:"platform_amount_usd"`
	ExceedsPlatform      bool    `json:"exceeds_platform"`
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
		logs, logsErr = fetchUpstreamLogs(ctx, group.baseURL, group.apiKey)
	}()
	wg.Wait()
	if statusErr != nil || logsErr != nil || !status.Success || status.Data.QuotaPerUnit <= 0 || common.QuotaPerUnit <= 0 {
		if group.cacheKey != "" {
			unsupportedUpstreamCostSources.Store(group.cacheKey, time.Now().Add(upstreamCostFailureTTL))
		}
		return nil
	}
	unsupportedUpstreamCostSources.Delete(group.cacheKey)

	quotaByRequestId := make(map[string]int, len(logs)*2)
	for _, log := range logs {
		if log.RequestId != "" {
			quotaByRequestId[log.RequestId] = log.Quota
		}
		if log.UpstreamRequestId != "" {
			quotaByRequestId[log.UpstreamRequestId] = log.Quota
		}
	}

	costs := make([]UpstreamCostInfo, 0, len(group.targets))
	for _, target := range group.targets {
		upstreamQuota, ok := quotaByRequestId[target.requestId]
		if !ok || upstreamQuota < 0 || target.platformQuota < 0 {
			continue
		}
		upstreamAmountUSD := float64(upstreamQuota) / status.Data.QuotaPerUnit
		platformAmountUSD := float64(target.platformQuota) / common.QuotaPerUnit
		costs = append(costs, UpstreamCostInfo{
			LogId:                target.logId,
			UpstreamQuota:        upstreamQuota,
			UpstreamQuotaPerUnit: status.Data.QuotaPerUnit,
			PlatformQuota:        target.platformQuota,
			PlatformQuotaPerUnit: common.QuotaPerUnit,
			UpstreamAmountUSD:    upstreamAmountUSD,
			PlatformAmountUSD:    platformAmountUSD,
			ExceedsPlatform:      upstreamAmountUSD > platformAmountUSD,
		})
	}
	return costs
}

func fetchUpstreamLogs(ctx context.Context, baseURL, apiKey string) ([]upstreamLogItem, error) {
	endpoint := fmt.Sprintf("%s/api/log/token?page_size=%d", baseURL, upstreamCostPageSize)
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
		return err
	}
	defer CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return common.DecodeJson(io.LimitReader(resp.Body, upstreamCostMaxBodyBytes), target)
}
