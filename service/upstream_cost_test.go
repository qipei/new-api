package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchUpstreamCostGroupComparesNormalizedCharges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, err := fmt.Fprint(w, `{"success":true,"data":{"quota_per_unit":500000}}`)
			assert.NoError(t, err)
		case "/api/log/token":
			assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
			assert.Equal(t, "1000", r.URL.Query().Get("page_size"))
			_, err := fmt.Fprint(w, `{"success":true,"data":{"items":[{"request_id":"upstream-request","quota":200000}]}}`)
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })

	costs := fetchUpstreamCostGroup(context.Background(), &upstreamCostGroup{
		baseURL: server.URL,
		apiKey:  "upstream-key",
		targets: []upstreamCostTarget{{
			logId:         7,
			requestId:     "upstream-request",
			platformQuota: 100000,
		}},
	})

	require.Len(t, costs, 1)
	assert.Equal(t, 0.4, costs[0].UpstreamAmountUSD)
	assert.Equal(t, 0.2, costs[0].PlatformAmountUSD)
	assert.True(t, costs[0].ExceedsPlatform)
}

func TestFetchUpstreamLogsSupportsLegacyArrayResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprint(w, `{"success":true,"data":[{"request_id":"legacy-request","quota":42}]}`)
		assert.NoError(t, err)
	}))
	defer server.Close()

	logs, err := fetchUpstreamLogs(context.Background(), server.URL, "key")

	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "legacy-request", logs[0].RequestId)
	assert.Equal(t, 42, logs[0].Quota)
}
