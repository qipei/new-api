package relay

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func audioTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	return c
}

// 音轨维度识别契约：generate_audio 与 OutputConfig.AudioGeneration 两种参数形态。
func TestRequestAudioDimension(t *testing.T) {
	c := audioTestContext(t)

	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     string
	}{
		{"generate_audio true", map[string]interface{}{"generate_audio": true}, video_billing.AudioOn},
		{"generate_audio false", map[string]interface{}{"generate_audio": false}, video_billing.AudioOff},
		{"generate_audio 字符串", map[string]interface{}{"generate_audio": "true"}, video_billing.AudioOn},
		{"Ali parameters.audio", map[string]interface{}{"parameters": map[string]interface{}{"audio": true}}, video_billing.AudioOn},
		{
			"OutputConfig.AudioGeneration Disabled",
			map[string]interface{}{
				"resolution":   "1080p",
				"OutputConfig": map[string]interface{}{"AudioGeneration": "Disabled"},
			},
			video_billing.AudioOff,
		},
		{
			"OutputConfig.AudioGeneration Enabled",
			map[string]interface{}{
				"OutputConfig": map[string]interface{}{"AudioGeneration": "Enabled"},
			},
			video_billing.AudioOn,
		},
		{
			"snake_case 形态",
			map[string]interface{}{
				"output_config": map[string]interface{}{"audio_generation": "disabled"},
			},
			video_billing.AudioOff,
		},
		{
			"未知取值不猜测",
			map[string]interface{}{
				"OutputConfig": map[string]interface{}{"AudioGeneration": "Auto"},
			},
			"",
		},
		{"未指定", map[string]interface{}{"resolution": "1080p"}, ""},
		{"generate_audio 优先于 OutputConfig", map[string]interface{}{
			"generate_audio": true,
			"OutputConfig":   map[string]interface{}{"AudioGeneration": "Disabled"},
		}, video_billing.AudioOn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := relaycommon.TaskSubmitReq{Metadata: tt.metadata}
			assert.Equal(t, tt.want, requestAudioDimension(c, &req))
		})
	}
}

func TestTaskVideoDimsReadsAliNestedProtocol(t *testing.T) {
	c := audioTestContext(t)
	req := relaycommon.TaskSubmitReq{
		Model: "kling/kling-v3-omni-video-generation",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{"type": "feature", "url": "https://example.com/reference.mp4"},
				},
			},
			"parameters": map[string]interface{}{
				"mode":     "pro",
				"duration": 8,
				"audio":    false,
			},
		},
	}
	c.Set("task_request", req)

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	mode, resolution, audio, seconds := taskVideoDims(c, info)

	require.Equal(t, video_billing.ModeVideoToVideo, mode)
	assert.Equal(t, "1080p", resolution)
	assert.Equal(t, video_billing.AudioOff, audio)
	assert.Equal(t, 8, seconds)
}

func TestTaskVideoDimsReadsHappyHorseVideoAndDefaultResolution(t *testing.T) {
	c := audioTestContext(t)
	req := relaycommon.TaskSubmitReq{
		Model: "happyhorse-video-edit",
		Video: "https://example.com/input.mp4",
	}
	c.Set("task_request", req)

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "happyhorse-1.0-video-edit"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	mode, resolution, audio, seconds := taskVideoDims(c, info)

	assert.Equal(t, video_billing.ModeVideoToVideo, mode)
	assert.Equal(t, "1080p", resolution)
	assert.Empty(t, audio)
	assert.Zero(t, seconds)
}

func TestTaskVideoDimsTreatsSingleImageAsImageToVideo(t *testing.T) {
	c := audioTestContext(t)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Image:    "https://example.com/input.png",
		Size:     "1280x720",
		Duration: 6,
	})

	mode, resolution, _, seconds := taskVideoDims(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	assert.Equal(t, video_billing.ModeImageToVideo, mode)
	assert.Equal(t, "1280x720", resolution)
	assert.Equal(t, 6, seconds)
}

func TestTaskVideoDimsReadsTopLevelPassThroughResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{
		"model":"MiniMax-H3",
		"prompt":"test",
		"resolution":"768p",
		"duration":5,
		"ratio":"16:9"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "MiniMax-H3",
		Duration: 5,
	})

	mode, resolution, _, seconds := taskVideoDims(c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	assert.Equal(t, video_billing.ModeTextToVideo, mode)
	assert.Equal(t, "768p", resolution)
	assert.Equal(t, 5, seconds)
}

// Kling expresses its quality tier through parameters.mode while the price matrix
// prices on the resolution dimension. A missing branch silently falls through to the
// default tier, which for these tables is less than half the 4K price.
func TestTaskVideoDims4kModeMapsToTheFourKTier(t *testing.T) {
	c := audioTestContext(t)
	req := relaycommon.TaskSubmitReq{
		Model: "kling/kling-v3-omni-video-generation",
		Metadata: map[string]interface{}{
			"parameters": map[string]interface{}{
				"mode":     "4k",
				"duration": 5,
			},
		},
	}
	c.Set("task_request", req)

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	_, resolution, _, _ := taskVideoDims(c, info)

	assert.Equal(t, "4k", resolution)
}

// The quality tier can arrive as the top-level mode field, not only inside metadata.
// Reading just one of them prices a 4K job at the default tier — less than half.
func TestTaskVideoDimsReadsTopLevelModeForTier(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"4k", "4k"},
		{"pro", "1080p"},
		{"std", "720p"},
	} {
		c := audioTestContext(t)
		c.Set("task_request", relaycommon.TaskSubmitReq{
			Model: "kling/kling-v3-video-generation", Mode: tc.mode, Duration: 5,
		})
		info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

		_, resolution, _, _ := taskVideoDims(c, info)

		assert.Equal(t, tc.want, resolution, "mode=%s", tc.mode)
	}
}

// 万相3.0 的参考类型名和别家不同：漏掉 reference_video / reference_image 会把
// 视频编辑、延长、参考生视频都判成文生视频，落到另一个价格档。
func TestTaskVideoDimsReadsWan3ReferenceMediaTypes(t *testing.T) {
	for _, tc := range []struct {
		mediaType string
		expected  string
	}{
		{"reference_video", video_billing.ModeVideoToVideo},
		{"reference_image", video_billing.ModeImageToVideo},
	} {
		c := audioTestContext(t)
		c.Set("task_request", relaycommon.TaskSubmitReq{
			Model: "wan3.0-video",
			Metadata: map[string]interface{}{
				"input": map[string]interface{}{
					"media": []interface{}{
						map[string]interface{}{"type": tc.mediaType, "url": "https://example.com/x"},
					},
				},
				"parameters": map[string]interface{}{"resolution": "480P", "duration": 12},
			},
		})

		info := &relaycommon.RelayInfo{
			ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "wan3.0-video"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		mode, resolution, _, seconds := taskVideoDims(c, info)

		assert.Equal(t, tc.expected, mode, tc.mediaType)
		assert.Equal(t, "480P", resolution, tc.mediaType)
		assert.Equal(t, 12, seconds, tc.mediaType)
	}
}

// 不传分辨率时上游按 1080P 出片并按 1080P 扣我们，取价必须跟着走，
// 否则每秒少收一档（720P 0.6 对 1080P 1.2）。
func TestTaskVideoDimsDefaultsWan3ToOfficialResolution(t *testing.T) {
	c := audioTestContext(t)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "wan3.0-video-prime",
		Prompt:   "hi",
		Duration: 5,
	})

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "wan3.0-video-prime"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	mode, resolution, _, seconds := taskVideoDims(c, info)

	assert.Equal(t, video_billing.ModeTextToVideo, mode)
	assert.Equal(t, "1080p", resolution)
	assert.Equal(t, 5, seconds)
}

// 自动分组选路时，按请求体里的档位判断该分组是否被排除；没有配置排除的价格表不解析请求。
func TestMatrixTierExcludedForGroupReadsRequestDims(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"video-model": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.1},
			{Resolution: "768p", Price: 0.08},
			{Resolution: "1080p", Price: 0.2, ExcludedGroups: []string{"cheap"}},
		}},
		"image-model": {Unit: video_billing.UnitPerImage, Tiers: []video_billing.PriceTier{
			{Price: 0.1},
			{Resolution: "4k", Price: 0.5, ExcludedGroups: []string{"cheap"}},
		}},
		"plain-model": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.1}}},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	tests := []struct {
		name  string
		path  string
		model string
		group string
		body  string
		want  bool
	}{
		{"excluded video tier", "/v1/video/generations", "video-model", "cheap", `{"model":"video-model","prompt":"p","resolution":"1080p"}`, true},
		{"other group keeps tier", "/v1/video/generations", "video-model", "vip", `{"model":"video-model","prompt":"p","resolution":"1080p"}`, false},
		{"unrelated tier", "/v1/video/generations", "video-model", "cheap", `{"model":"video-model","prompt":"p","resolution":"768p"}`, false},
		{"excluded image size", "/v1/images/generations", "image-model", "cheap", `{"model":"image-model","prompt":"p","size":"4096x4096"}`, true},
		{"no exclusions configured", "/v1/video/generations", "plain-model", "cheap", `{"model":"plain-model","prompt":"p","resolution":"1080p"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tt.path, strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			assert.Equal(t, tt.want, MatrixTierExcludedForGroup(c, tt.model, tt.group))
		})
	}
}

// 闲时价挂在渠道上：只有当前选中的渠道配置了闲时时段、请求时刻落在时段内、命中的档位
// 配了闲时价，三者同时成立才按闲时价计费；其余情况按原价，不会因为别的渠道有闲时价
// 而少收。
func TestResolveTaskPriceDataUsesChannelOffPeakPrice(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	offPeakPrice := 0.04
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"h3-turbo": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.05, OffPeakPrice: &offPeakPrice},
			{Resolution: "768p", Price: 0.08},
		}},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	night := time.Date(2026, 10, 27, 2, 0, 0, 0, shanghai)
	noon := time.Date(2026, 10, 27, 12, 0, 0, 0, shanghai)
	window := &dto.OffPeakPricing{Start: "00:00", End: "07:50"}

	tests := []struct {
		name        string
		window      *dto.OffPeakPricing
		at          time.Time
		resolution  string
		wantPrice   float64
		wantOffPeak bool
	}{
		{"off-peak channel inside window", window, night, "", 0.04, true},
		{"off-peak channel outside window", window, noon, "", 0.05, false},
		{"channel without window", nil, night, "", 0.05, false},
		{"tier without off-peak price", window, night, "768p", 0.08, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/video/generations",
				strings.NewReader(`{"model":"h3-turbo","prompt":"p","resolution":"`+tt.resolution+`"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: "h3-turbo"})
			info := &relaycommon.RelayInfo{
				OriginModelName: "h3-turbo",
				UsingGroup:      "default",
				StartTime:       tt.at,
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelOtherSettings: dto.ChannelOtherSettings{OffPeakPricing: tt.window},
				},
			}

			priceData, err := resolveTaskPriceData(c, info)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPrice, priceData.ModelPrice)
			assert.Equal(t, tt.wantOffPeak, priceData.OffPeak)
		})
	}
}

// autodl 渠道按工作流的档位取价：「宽x高」按短边归档，与适配器发给上游的分辨率选项
// 用同一套规则；不传分辨率时按默认档（上游按工作流默认档出片）。
func TestResolveTaskPriceDataUsesAutoDLResolutionTier(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"h3-turbo": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.05},
			{Resolution: "480p", Price: 0.04},
			{Resolution: "1088p", Price: 0.08},
		}},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	tests := []struct {
		name      string
		body      string
		wantPrice float64
	}{
		{"size by short edge", `{"model":"h3-turbo","prompt":"p","size":"864x480"}`, 0.04},
		{"resolution label", `{"model":"h3-turbo","prompt":"p","resolution":"480P","ratio":"9:16"}`, 0.04},
		{"tall size", `{"model":"h3-turbo","prompt":"p","size":"1088x1920"}`, 0.08},
		{"no resolution uses default tier", `{"model":"h3-turbo","prompt":"p"}`, 0.05},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			var req relaycommon.TaskSubmitReq
			require.NoError(t, common.UnmarshalJsonStr(tt.body, &req))
			c.Set("task_request", req)
			info := &relaycommon.RelayInfo{
				OriginModelName: "h3-turbo",
				UsingGroup:      "default",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAutoDLComfyUI},
			}

			priceData, err := resolveTaskPriceData(c, info)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPrice, priceData.ModelPrice)
		})
	}
}

// WeNex 渠道按适配器发给上游的画质取价：不传为 768p，720p 按 768p，「宽x高」按短边归档；
// 1080p 的档位名与工作流线路一致（1088p），两条线路挂在同一个模型下共用价格表。
func TestResolveTaskPriceDataUsesWeNexQuality(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"h3-wenex": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.08},
			{Resolution: "480p", Price: 0.02},
			{Resolution: "768p", Price: 0.03},
			{Resolution: "1088p", Price: 0.045},
		}},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	tests := []struct {
		name      string
		body      string
		wantPrice float64
	}{
		{"no resolution is 768p", `{"model":"h3-wenex","prompt":"p"}`, 0.03},
		{"720p is 768p", `{"model":"h3-wenex","prompt":"p","resolution":"720p"}`, 0.03},
		{"1080p label", `{"model":"h3-wenex","prompt":"p","resolution":"1080P"}`, 0.045},
		{"1080p size", `{"model":"h3-wenex","prompt":"p","size":"1920x1080"}`, 0.045},
		{"workflow tier name", `{"model":"h3-wenex","prompt":"p","resolution":"1088p","ratio":"9:16"}`, 0.045},
		{"768p size", `{"model":"h3-wenex","prompt":"p","size":"720x1280"}`, 0.03},
		{"metadata quality", `{"model":"h3-wenex","prompt":"p","metadata":{"quality":"1080p"}}`, 0.045},
	}
	run := func(t *testing.T, model string, body string, wantPrice float64) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		var req relaycommon.TaskSubmitReq
		require.NoError(t, common.UnmarshalJsonStr(body, &req))
		c.Set("task_request", req)
		info := &relaycommon.RelayInfo{
			OriginModelName: model,
			UsingGroup:      "default",
			TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeWeNex},
		}

		priceData, err := resolveTaskPriceData(c, info)
		require.NoError(t, err)
		assert.InDelta(t, wantPrice, priceData.ModelPrice, 1e-9)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { run(t, "h3-wenex", tt.body, tt.wantPrice) })
	}

	// 挂在官方 H3 模型下时 1080p 这一档通常写作 1080p，不能落到兜底档。
	settings.PriceTables["h3-official"] = video_billing.ModelPriceTable{Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
		{Price: 0.8},
		{Resolution: "768p", Price: 0.5},
		{Resolution: "1080p", Price: 0.6},
	}}
	t.Run("1080p tier named 1080p", func(t *testing.T) {
		run(t, "h3-official", `{"model":"h3-official","prompt":"p","resolution":"1080p"}`, 0.6)
	})
	t.Run("1088p request uses the 1080p tier", func(t *testing.T) {
		run(t, "h3-official", `{"model":"h3-official","prompt":"p","resolution":"1088p"}`, 0.6)
	})
	t.Run("768P label", func(t *testing.T) {
		run(t, "h3-official", `{"model":"h3-official","prompt":"p","size":"768P"}`, 0.5)
	})
}

// 比价路由按本次请求命中的档位取各分组的单价（含分组价）；价格表没配分组价、或选路时
// 拿不到请求维度（remix）时返回 nil，比价只比倍率。
func TestMatrixGroupTierPricer(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"h3-fast": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.8},
			{Resolution: "768p", Price: 0.6, GroupPrices: map[string]float64{"wenex": 0.04}},
			{Resolution: "1088p", Price: 0.7, GroupPrices: map[string]float64{"wenex": 0.06}},
			{Resolution: "1440p", Price: 0.8, ExcludedGroups: []string{"wenex"}},
		}},
		"plain": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{{Price: 0.5}}},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	newContext := func(path, body string) *gin.Context {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	pricer := MatrixGroupTierPricer(newContext("/v1/video/generations", `{"model":"h3-fast","prompt":"p","resolution":"1088p"}`), "h3-fast")
	require.NotNil(t, pricer)
	price, ok := pricer("wenex")
	assert.True(t, ok)
	assert.InDelta(t, 0.06, price, 1e-9)
	price, ok = pricer("1折")
	assert.True(t, ok)
	assert.InDelta(t, 0.7, price, 1e-9)

	pricer = MatrixGroupTierPricer(newContext("/v1/video/generations", `{"model":"h3-fast","prompt":"p","resolution":"1440p"}`), "h3-fast")
	require.NotNil(t, pricer)
	_, ok = pricer("wenex")
	assert.False(t, ok, "档位排除了该分组时取不到价")

	assert.Nil(t, MatrixGroupTierPricer(newContext("/v1/video/generations", `{"model":"plain","prompt":"p"}`), "plain"))
	assert.Nil(t, MatrixGroupTierPricer(newContext("/v1/videos/abc/remix", `{"prompt":"p"}`), "h3-fast"))
}

// 自动分组（auto、auto_price）的请求要按本次选中的分组取价：分组价和不适用分组都看实际
// 分组，而不是令牌上的 auto_price。否则选中了配分组价的分组却按原价扣费。
func TestResolveTaskPriceDataUsesSelectedAutoGroup(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"h3": {Unit: video_billing.UnitPerSecond, Tiers: []video_billing.PriceTier{
			{Price: 0.8},
			{Resolution: "768p", Price: 0.5, GroupPrices: map[string]float64{"wenex": 0.04}},
			{Resolution: "2k", Price: 0.8, ExcludedGroups: []string{"wenex"}},
		}},
	}
	t.Cleanup(func() { settings.PriceTables = original })
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"wenex":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios)) })

	newContext := func(body string) *gin.Context {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		var req relaycommon.TaskSubmitReq
		require.NoError(t, common.UnmarshalJsonStr(body, &req))
		c.Set("task_request", req)
		c.Set("auto_group", "wenex")
		return c
	}
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName: "h3",
			UsingGroup:      "auto_price",
			TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			ChannelMeta:     &relaycommon.ChannelMeta{},
		}
	}

	info := newInfo()
	priceData, err := resolveTaskPriceData(newContext(`{"model":"h3","prompt":"p","resolution":"768p"}`), info)
	require.NoError(t, err)
	assert.InDelta(t, 0.04, priceData.ModelPrice, 1e-9)
	assert.Equal(t, "wenex", info.UsingGroup)

	_, err = resolveTaskPriceData(newContext(`{"model":"h3","prompt":"p","resolution":"2k"}`), newInfo())
	assert.ErrorIs(t, err, video_billing.ErrTierExcludedForGroup)
}
