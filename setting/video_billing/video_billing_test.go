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

func TestNormalizeImageResolutionByShortEdge(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"明确的 1K 标签", "1K", "1k"},
		{"明确的 2K 标签", "2k", "2k"},
		{"明确的 4K 标签", " 4K ", "4k"},
		{"1K 下边界", "768x1024", "1k"},
		{"1K 上边界", "4096x1024", "1k"},
		{"刚进入 2K", "1025x4096", "2k"},
		{"2K 上边界", "2048x4096", "2k"},
		{"刚进入 4K", "4096x2049", "4k"},
		{"横竖图归档一致", "2049x4096", "4k"},
		{"未知标签原样保留", "auto", "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeImageResolution(tt.in))
		})
	}
}

func TestGetPriceMatchesArbitraryImageSizeByShortEdge(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"resolution-priced-image": {
			Unit: UnitPerImage,
			Tiers: []PriceTier{
				{Resolution: "1k", Price: 0.1},
				{Resolution: "2k", Price: 0.2},
				{Resolution: "4k", Price: 0.4},
			},
		},
	})

	tests := []struct {
		resolution string
		want       float64
	}{
		{"1k", 0.1},
		{"2K", 0.2},
		{"4k", 0.4},
		{"4096x1024", 0.1},
		{"4096x1025", 0.2},
		{"2048x4096", 0.2},
		{"2049x4096", 0.4},
	}

	for _, tt := range tests {
		t.Run(tt.resolution, func(t *testing.T) {
			price, unit, ok := GetPrice("resolution-priced-image", ModeTextToImage, tt.resolution, "")
			require.True(t, ok)
			assert.Equal(t, UnitPerImage, unit)
			assert.InDelta(t, tt.want, price, 1e-9)
		})
	}
}

func TestGetPricePrefersConfiguredResolutionBuckets(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"vidu-image": {
			Unit: UnitPerImage,
			ResolutionBuckets: []ResolutionBucket{
				{Name: "1k", Sizes: []string{"1920*1088"}},
				{Name: "2k", Sizes: []string{"3072x2048"}},
				{Name: "4k", Sizes: []string{"3840x1648"}},
			},
			Tiers: []PriceTier{
				{Resolution: "1k", Price: 0.1},
				{Resolution: "2k", Price: 0.2},
				{Resolution: "4k", Price: 0.4},
			},
		},
	})

	tests := []struct {
		name       string
		resolution string
		want       float64
	}{
		{"星号尺寸归入官方 1K", "1920*1088", 0.1},
		{"横竖方向使用同一档位", "1088x1920", 0.1},
		{"官方 2K 覆盖通用 4K 推断", "3072x2048", 0.2},
		{"官方 4K 覆盖通用 2K 推断", "3840x1648", 0.4},
		{"直接档位标签仍然可用", "4K", 0.4},
		{"未配置尺寸回退短边规则", "1500x3000", 0.2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, unit, ok := GetPrice("vidu-image", ModeTextToImage, tt.resolution, "")
			require.True(t, ok)
			assert.Equal(t, UnitPerImage, unit)
			assert.InDelta(t, tt.want, price, 1e-9)
		})
	}
}

func TestGetPriceMatchesImagePixelRanges(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"pixel-priced-image": {
			Unit: UnitPerImage,
			Tiers: []PriceTier{
				{Price: 0.6},
				{MaxPixels: 2_610_000, Price: 0.3},
				{MinPixels: 2_610_001, Price: 0.6},
				{Resolution: "1920x1920", Price: 0.8},
				{MinPixels: 3_000_000, MaxPixels: 2_000_000, Price: 0.1},
			},
		},
	})

	tests := []struct {
		name       string
		resolution string
		want       float64
	}{
		{"低于边界", "1920x1080", 0.3},
		{"等于边界", "2610x1000", 0.3},
		{"高于边界", "1800x1800", 0.6},
		{"精确尺寸优先于像素范围", "1920x1920", 0.8},
		{"无法计算像素时回退默认档", "2k", 0.6},
		{"像素乘法溢出时回退默认档", "9223372036854775807x2", 0.6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, unit, ok := GetPrice("pixel-priced-image", ModeTextToImage, tt.resolution, "")
			require.True(t, ok)
			assert.Equal(t, UnitPerImage, unit)
			assert.InDelta(t, tt.want, price, 1e-9)
		})
	}
}

// The generic short-edge rule only emits 480p/720p/1080p/2k/4k, so a provider tier
// like Vidu's 540P (1024*576 has a 576 short edge) is unreachable by size and the
// request silently prices at the 720p tier — 2.5x the intended rate. Buckets are the
// escape hatch and must work for per_second, not just per_image.
func TestGetPriceUsesResolutionBucketsForPerSecondTables(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"vidu-bucketed": {
			Unit: UnitPerSecond,
			ResolutionBuckets: []ResolutionBucket{
				{Name: "540p", Sizes: []string{"1024*576", "1024*1024", "960*528"}},
			},
			Tiers: []PriceTier{
				{Price: 0.3125, Resolution: "540p"},
				{Price: 0.78125, Resolution: "720p"},
				{Price: 0.9375, Resolution: "1080p"},
			},
		},
	})

	for _, size := range []string{"1024*576", "1024*1024", "960*528", "540p"} {
		price, unit, ok := GetPrice("vidu-bucketed", "", size, "")
		require.True(t, ok, "size %s must match a tier", size)
		assert.Equal(t, UnitPerSecond, unit)
		assert.Equal(t, 0.3125, price, "size %s must price at the 540p tier", size)
	}

	// Sizes outside the bucket keep the generic short-edge classification.
	price, _, ok := GetPrice("vidu-bucketed", "", "1920*1080", "")
	require.True(t, ok)
	assert.Equal(t, 0.9375, price)
}
