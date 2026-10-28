package dto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 闲时时段是 [Start, End) 的每日窗口，按配置的时区解释；End 早于 Start 表示跨零点。
// 判断不了（未配置、配置无效）时一律按不在时段内，闲时价不会被误用。
func TestOffPeakPricingActiveAt(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 10, 27, hour, minute, 0, 0, shanghai)
	}

	tests := []struct {
		name   string
		window *OffPeakPricing
		at     time.Time
		want   bool
	}{
		{"start is inclusive", &OffPeakPricing{Start: "00:00", End: "07:50"}, at(0, 0), true},
		{"inside window", &OffPeakPricing{Start: "00:00", End: "07:50"}, at(3, 30), true},
		{"end is exclusive", &OffPeakPricing{Start: "00:00", End: "07:50"}, at(7, 50), false},
		{"after window", &OffPeakPricing{Start: "00:00", End: "07:50"}, at(12, 0), false},
		{"cross midnight before 24:00", &OffPeakPricing{Start: "22:00", End: "06:00"}, at(23, 0), true},
		{"cross midnight after 00:00", &OffPeakPricing{Start: "22:00", End: "06:00"}, at(5, 59), true},
		{"cross midnight outside", &OffPeakPricing{Start: "22:00", End: "06:00"}, at(6, 0), false},
		// 02:00 上海时间是 18:00 UTC，不在 UTC 的 00:00~07:50 内。
		{"interprets window in its timezone", &OffPeakPricing{Start: "00:00", End: "07:50", TZ: "UTC"}, at(2, 0), false},
		{"nil window", nil, at(3, 0), false},
		{"invalid clock", &OffPeakPricing{Start: "0:00x", End: "07:50"}, at(3, 0), false},
		{"empty window", &OffPeakPricing{Start: "03:00", End: "03:00"}, at(3, 0), false},
		{"unknown timezone", &OffPeakPricing{Start: "00:00", End: "07:50", TZ: "Mars/Base"}, at(3, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.window.ActiveAt(tt.at))
		})
	}
}

func TestOffPeakPricingValidate(t *testing.T) {
	var unset *OffPeakPricing
	require.NoError(t, unset.Validate())
	require.NoError(t, (&OffPeakPricing{Start: "00:00", End: "07:50"}).Validate())
	require.NoError(t, (&OffPeakPricing{Start: "22:00", End: "06:00", TZ: "UTC"}).Validate())

	assert.Error(t, (&OffPeakPricing{Start: "24:00", End: "07:50"}).Validate())
	assert.Error(t, (&OffPeakPricing{Start: "00:00", End: ""}).Validate())
	assert.Error(t, (&OffPeakPricing{Start: "08:00", End: "08:00"}).Validate())
	assert.Error(t, (&OffPeakPricing{Start: "00:00", End: "07:50", TZ: "Mars/Base"}).Validate())
}
