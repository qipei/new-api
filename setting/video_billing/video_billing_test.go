package video_billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 默认表必须与迁移前 relay/channel/task/doubao 硬编码 videoPriceTable 行为一致。
func TestGetRatioSeedanceDefaultsMatchLegacyTable(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		mode       string
		resolution string
		want       float64
	}{
		{"基准档 480p 文生", "doubao-seedance-2-0-260128", ModeTextToVideo, "480p", 1.0},
		{"基准档 720p 图生", "doubao-seedance-2-0-260128", ModeImageToVideo, "720p", 1.0},
		{"1080p 文生", "doubao-seedance-2-0-260128", ModeTextToVideo, "1080p", 51.0 / 46.0},
		{"4k 文生", "doubao-seedance-2-0-260128", ModeTextToVideo, "4k", 26.0 / 46.0},
		{"含视频输入 基准档", "doubao-seedance-2-0-260128", ModeVideoToVideo, "720p", 28.0 / 46.0},
		{"含视频输入 1080p", "doubao-seedance-2-0-260128", ModeVideoToVideo, "1080p", 31.0 / 46.0},
		{"含视频输入 4k", "doubao-seedance-2-0-260128", ModeVideoToVideo, "4k", 16.0 / 46.0},
		{"fast 基准档", "doubao-seedance-2-0-fast-260128", ModeTextToVideo, "720p", 1.0},
		{"fast 含视频输入", "doubao-seedance-2-0-fast-260128", ModeVideoToVideo, "720p", 22.0 / 37.0},
		{"fast 未配置的 1080p 档按基准价", "doubao-seedance-2-0-fast-260128", ModeTextToVideo, "1080p", 1.0},
		{"分辨率缺省按基准档", "doubao-seedance-2-0-260128", ModeTextToVideo, "", 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok := GetRatio(tt.model, tt.mode, tt.resolution, "")
			require.True(t, ok)
			assert.InDelta(t, tt.want, ratio, 1e-9)
		})
	}
}

func TestGetRatioUnconfiguredModel(t *testing.T) {
	_, ok := GetRatio("sora-2", ModeTextToVideo, "720p", "")
	assert.False(t, ok)
}

func TestGetRatioAudioAndSpecificity(t *testing.T) {
	original := videoBillingSettings.PriceTables
	t.Cleanup(func() { videoBillingSettings.PriceTables = original })

	videoBillingSettings.PriceTables = map[string]ModelPriceTable{
		"video-model-x": {
			BasePrice: 10.0,
			Tiers: []PriceTier{
				{Audio: AudioOn, Price: 12.0},
				{Resolution: "1080p", Price: 20.0},
				{Resolution: "1080p", Audio: AudioOn, Price: 25.0},
				{Mode: ModeVideoToVideo, Resolution: "1080p", Audio: AudioOn, Price: 30.0},
				{Price: -5}, // 非法价格档必须被忽略
			},
		},
	}

	tests := []struct {
		name       string
		mode       string
		resolution string
		audio      string
		want       float64
	}{
		{"无音轨基准档", ModeTextToVideo, "720p", AudioOff, 1.0},
		{"有音轨通用档", ModeTextToVideo, "720p", AudioOn, 1.2},
		{"1080p 无音轨", ModeTextToVideo, "1080p", AudioOff, 2.0},
		{"1080p 有音轨取更具体档", ModeTextToVideo, "1080p", AudioOn, 2.5},
		{"三维全匹配优先", ModeVideoToVideo, "1080p", AudioOn, 3.0},
		{"尺寸字符串归档到 1080p", ModeTextToVideo, "1792x1024", AudioOff, 2.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok := GetRatio("video-model-x", tt.mode, tt.resolution, tt.audio)
			require.True(t, ok)
			assert.InDelta(t, tt.want, ratio, 1e-9)
		})
	}
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
