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
			price, unit, err := GetPrice(tt.model, "default", tt.mode, tt.resolution, tt.audio)
			require.NoError(t, err)
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

	_, _, err := GetPrice("missing-model", "default", ModeTextToVideo, "720p", "")
	assert.ErrorIs(t, err, ErrNoMatchingTier)

	_, _, err = GetPrice("bad-unit", "default", ModeTextToVideo, "720p", "")
	assert.ErrorIs(t, err, ErrNoMatchingTier, "非法单位视为未配置")

	_, _, err = GetPrice("no-tiers", "default", ModeTextToVideo, "720p", "")
	assert.ErrorIs(t, err, ErrNoMatchingTier)

	// 无默认档且维度未命中 → 拒绝，避免漏配错价
	_, _, err = GetPrice("no-default", "default", ModeTextToVideo, "480p", "")
	assert.ErrorIs(t, err, ErrNoMatchingTier)
	price, _, err := GetPrice("no-default", "default", ModeTextToVideo, "1080p", "")
	require.NoError(t, err)
	assert.InDelta(t, 0.5, price, 1e-9)

	_, _, err = GetPrice("only-invalid", "default", ModeTextToVideo, "720p", "")
	assert.ErrorIs(t, err, ErrNoMatchingTier)
}

// 档位排除分组：被排除的分组请求该档位时拒绝，不能退到默认档按兜底价收费；
// 被排除的档位本来就不匹配的请求不受影响，其他分组照常取价。
func TestGetPriceExcludedGroups(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"video": {
			Unit: UnitPerSecond,
			Tiers: []PriceTier{
				{Price: 9.99}, // 默认档
				{Resolution: "768p", Price: 0.08},
				{Resolution: "1080p", Price: 0.2, ExcludedGroups: []string{"cheap"}},
				{Resolution: "4k", Audio: AudioOn, Price: 0.5, ExcludedGroups: []string{"cheap"}},
				{Resolution: "4k", Price: 0.4},
			},
		},
		"speech": {Unit: UnitPer10kChars, Tiers: []PriceTier{{Price: 3, ExcludedGroups: []string{"cheap"}}}},
	})

	tests := []struct {
		name       string
		model      string
		group      string
		resolution string
		audio      string
		wantPrice  float64
		wantErr    error
	}{
		{"excluded group rejected instead of default tier", "video", "cheap", "1080p", "", 0, ErrTierExcludedForGroup},
		{"other groups keep the tier", "video", "vip", "1080p", "", 0.2, nil},
		{"unrelated tier unaffected", "video", "cheap", "768p", "", 0.08, nil},
		{"more specific excluded tier blocks the wider one", "video", "cheap", "4k", AudioOn, 0, ErrTierExcludedForGroup},
		{"wider tier still serves requests the excluded tier does not match", "video", "cheap", "4k", AudioOff, 0.4, nil},
		{"flat table excluded for group", "speech", "cheap", "", "", 0, ErrTierExcludedForGroup},
		{"flat table for other groups", "speech", "vip", "", "", 3, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, _, err := GetPrice(tt.model, tt.group, ModeTextToVideo, tt.resolution, tt.audio)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.wantPrice, price, 1e-9)
		})
	}
}

// 公开接口只下发查看者可见的不适用分组，不对外的分组名不能泄露。
func TestPublicTablesHidesInvisibleExcludedGroups(t *testing.T) {
	withTables(t, map[string]ModelPriceTable{
		"video": {Unit: UnitPerSecond, Tiers: []PriceTier{
			{Resolution: "1080p", Price: 0.2, ExcludedGroups: []string{"cheap", "internal"}},
		}},
	})
	tables := PublicTables(map[string]string{"cheap": "Cheap", "default": "Default"})
	assert.Equal(t, []string{"cheap"}, tables["video"].Tiers[0].ExcludedGroups)
	assert.Equal(t, []string{"cheap", "internal"}, videoBillingSettings.PriceTables["video"].Tiers[0].ExcludedGroups, "配置本身不能被改写")
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
			price, unit, err := GetPrice("resolution-priced-image", "default", ModeTextToImage, tt.resolution, "")
			require.NoError(t, err)
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
			price, unit, err := GetPrice("vidu-image", "default", ModeTextToImage, tt.resolution, "")
			require.NoError(t, err)
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
			price, unit, err := GetPrice("pixel-priced-image", "default", ModeTextToImage, tt.resolution, "")
			require.NoError(t, err)
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
		price, unit, err := GetPrice("vidu-bucketed", "default", "", size, "")
		require.NoError(t, err, "size %s must match a tier", size)
		assert.Equal(t, UnitPerSecond, unit)
		assert.Equal(t, 0.3125, price, "size %s must price at the 540p tier", size)
	}

	// Sizes outside the bucket keep the generic short-edge classification.
	price, _, err := GetPrice("vidu-bucketed", "default", "", "1920*1080", "")
	require.NoError(t, err)
	assert.Equal(t, 0.9375, price)
}

// 免费张数不填为 0，每张都计费；负数按 0 处理，不会把计费张数放大或变成负数。
func TestBillableInputImages(t *testing.T) {
	for _, tt := range []struct{ free, total, want int }{
		{0, 3, 3}, {5, 7, 2}, {5, 3, 0}, {-2, 3, 3}, {2, 0, 0},
	} {
		assert.Equal(t, tt.want, ModelPriceTable{FreeInputImages: tt.free}.BillableInputImages(tt.total), tt)
	}
}
