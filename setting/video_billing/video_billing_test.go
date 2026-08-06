package video_billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTables(t *testing.T, tables map[string]ModelPriceTable) {
	t.Helper()
	original := videoBillingSettings.PriceTables
	videoBillingSettings.PriceTables = tables
	t.Cleanup(func() { videoBillingSettings.PriceTables = original })
}

// 绝对价语义：命中档位直接返回原价与单位；最具体的档位优先；默认档兜底。
func TestGetPriceMatchingAndFallback(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"seedance-2-0": {
			Unit: UnitPerMillionToken,
			Tiers: []PriceTier{
				{Price: 6.3},                                              // 默认档
				{Resolution: "1080p", Price: 7.0},                         // 分辨率档
				{Mode: ModeVideoToVideo, Price: 3.8},                      // 模式档
				{Mode: ModeVideoToVideo, Resolution: "1080p", Price: 4.2}, // 模式+分辨率
				{Resolution: "1080p", Audio: AudioOn, Price: 7.5},         // 分辨率+音轨
				{Price: -1}, // 非法档，必须忽略
			},
		},
		"sora-2-pro": {
			Unit: UnitPerSecond,
			Tiers: []PriceTier{
				{Price: 0.3},
				{Resolution: "1080p", Price: 0.5},
			},
		},
	})

	tests := []struct {
		name       string
		model      string
		mode       string
		resolution string
		audio      string
		wantPrice  float64
		wantUnit   string
	}{
		{"默认档", "seedance-2-0", ModeTextToVideo, "480p", "", 6.3, UnitPerMillionToken},
		{"分辨率档", "seedance-2-0", ModeTextToVideo, "1080p", "", 7.0, UnitPerMillionToken},
		{"模式档", "seedance-2-0", ModeVideoToVideo, "480p", "", 3.8, UnitPerMillionToken},
		{"模式+分辨率最具体", "seedance-2-0", ModeVideoToVideo, "1080p", "", 4.2, UnitPerMillionToken},
		{"音轨档", "seedance-2-0", ModeTextToVideo, "1080p", AudioOn, 7.5, UnitPerMillionToken},
		{"尺寸字符串归档", "seedance-2-0", ModeTextToVideo, "1792x1024", "", 7.0, UnitPerMillionToken},
		{"每秒模型", "sora-2-pro", ModeTextToVideo, "720x1280", "", 0.3, UnitPerSecond},
		{"每秒模型高清档", "sora-2-pro", ModeImageToVideo, "1024x1792", "", 0.5, UnitPerSecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, unit, ok := GetPrice(tt.model, tt.mode, tt.resolution, tt.audio)
			require.True(t, ok)
			assert.InDelta(t, tt.wantPrice, price, 1e-9)
			assert.Equal(t, tt.wantUnit, unit)
		})
	}
}

func TestGetPriceUnconfiguredOrInvalid(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"bad-unit":     {Unit: "per_hour", Tiers: []PriceTier{{Price: 1}}},
		"no-tiers":     {Unit: UnitPerSecond},
		"no-default":   {Unit: UnitPerSecond, Tiers: []PriceTier{{Resolution: "1080p", Price: 0.5}}},
		"only-invalid": {Unit: UnitPerSecond, Tiers: []PriceTier{{Price: 0}}},
	})

	_, _, ok := GetPrice("missing-model", ModeTextToVideo, "720p", "")
	assert.False(t, ok)

	_, _, ok = GetPrice("bad-unit", ModeTextToVideo, "720p", "")
	assert.False(t, ok, "非法单位视为未配置")

	_, _, ok = GetPrice("no-tiers", ModeTextToVideo, "720p", "")
	assert.False(t, ok)

	// 无默认档且维度未命中 → 拒绝，避免漏配错价
	_, _, ok = GetPrice("no-default", ModeTextToVideo, "480p", "")
	assert.False(t, ok)
	price, _, ok := GetPrice("no-default", ModeTextToVideo, "1080p", "")
	require.True(t, ok)
	assert.InDelta(t, 0.5, price, 1e-9)

	_, _, ok = GetPrice("only-invalid", ModeTextToVideo, "720p", "")
	assert.False(t, ok)
}

func TestNormalizeResolution(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"720x1280", "720p"},
		{"1280x720", "720p"},
		{"1024x1792", "1080p"},
		{"1792x1024", "1080p"},
		{"3840x2160", "4k"},
		{"2560x1440", "2k"},
		{"480P", "480p"},
		{" 4K ", "4k"},
		{"", ""},
		{"abc", "abc"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, NormalizeResolution(tt.in), "input %q", tt.in)
	}
}
