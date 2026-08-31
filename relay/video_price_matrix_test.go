package relay

import (
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
