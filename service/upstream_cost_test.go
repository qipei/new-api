package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setUpstreamCostBillingUnits pins both sides of the comparison, because a raw
// quota is only meaningful together with the deployment's quota_per_unit and
// recharge price.
func setUpstreamCostBillingUnits(t *testing.T, quotaPerUnit, price float64) {
	t.Helper()
	previousQuotaPerUnit := common.QuotaPerUnit
	previousPrice := operation_setting.Price
	common.QuotaPerUnit = quotaPerUnit
	operation_setting.Price = price
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		operation_setting.Price = previousPrice
	})
}

func newUpstreamCostStub(t *testing.T, quotaPerUnit, price float64, itemsJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, err := fmt.Fprintf(w, `{"success":true,"data":{"quota_per_unit":%v,"price":%v}}`, quotaPerUnit, price)
			assert.NoError(t, err)
		case "/api/log/token":
			_, err := fmt.Fprintf(w, `{"success":true,"data":{"items":[%s]}}`, itemsJSON)
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
}

// The two deployments price a quota unit differently (upstream ¥6.82 per system
// dollar, ours ¥1), so comparing raw quota/quota_per_unit reports the upstream as
// six times cheaper when it actually costs more. Numbers are a real production
// request: we billed 922255 (¥1.8445) while the upstream billed 154357 (¥2.1054).
func TestFetchUpstreamCostGroupComparesRealCurrencyNotRawQuota(t *testing.T) {
	InitHttpClient()
	server := newUpstreamCostStub(t, 500000, 6.82, `{"request_id":"upstream-request","quota":154357}`)
	defer server.Close()
	setUpstreamCostBillingUnits(t, 500000, 1)

	costs := fetchUpstreamCostGroup(context.Background(), &upstreamCostGroup{
		baseURL: server.URL,
		apiKey:  "upstream-key",
		targets: []upstreamCostTarget{{
			logId:         7,
			requestId:     "upstream-request",
			platformQuota: 922255,
		}},
	})

	require.Len(t, costs, 1)
	assert.Equal(t, 1052715, costs[0].NormalizedUpstreamQuota)
	assert.InDelta(t, 2.10543, costs[0].UpstreamAmount, 1e-5)
	assert.InDelta(t, 1.84451, costs[0].PlatformAmount, 1e-5)
	assert.True(t, costs[0].ExceedsPlatform, "upstream charged more real money and must be flagged")
}

// A cheaper upstream must not be flagged once both sides are normalized.
func TestFetchUpstreamCostGroupIgnoresCheaperUpstream(t *testing.T) {
	InitHttpClient()
	server := newUpstreamCostStub(t, 500000, 6.82, `{"request_id":"upstream-request","quota":10000}`)
	defer server.Close()
	setUpstreamCostBillingUnits(t, 500000, 1)

	costs := fetchUpstreamCostGroup(context.Background(), &upstreamCostGroup{
		baseURL: server.URL,
		apiKey:  "upstream-key",
		targets: []upstreamCostTarget{{logId: 7, requestId: "upstream-request", platformQuota: 922255}},
	})

	require.Len(t, costs, 1)
	assert.Equal(t, 68200, costs[0].NormalizedUpstreamQuota)
	assert.False(t, costs[0].ExceedsPlatform)
}

// Without the upstream recharge price the two quota units cannot be reconciled;
// falling back to a raw quota comparison would invert the verdict, so the group
// must be skipped instead.
func TestFetchUpstreamCostGroupSkipsUpstreamWithoutPrice(t *testing.T) {
	InitHttpClient()
	server := newUpstreamCostStub(t, 500000, 0, `{"request_id":"upstream-request","quota":154357}`)
	defer server.Close()
	setUpstreamCostBillingUnits(t, 500000, 1)

	costs := fetchUpstreamCostGroup(context.Background(), &upstreamCostGroup{
		baseURL: server.URL,
		apiKey:  "upstream-key",
		targets: []upstreamCostTarget{{logId: 7, requestId: "upstream-request", platformQuota: 922255}},
	})

	assert.Empty(t, costs)
}

func TestFetchUpstreamLogsSupportsLegacyArrayResponse(t *testing.T) {
	InitHttpClient()
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
