package relay

import (
	"net/http/httptest"
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
