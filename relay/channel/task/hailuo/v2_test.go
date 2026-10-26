package hailuo

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseTaskReq(t *testing.T, body string) relaycommon.TaskSubmitReq {
	t.Helper()
	var req relaycommon.TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	return req
}

// 三种素材写法都组装成带 role 的 content；文生视频默认 16:9，其他参数按网关写法换算。
func TestBuildV2RequestAssemblesContent(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		body      string
		wantRoles []string
		wantRatio string
		wantRes   string
	}{
		{
			name:      "text to video defaults ratio",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","size":"2K","duration":6}`,
			wantRatio: "16:9",
			wantRes:   "2K",
		},
		{
			name:      "role fields",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","first_frame_url":"f","last_frame_url":"l"}`,
			wantRoles: []string{"first_frame", "last_frame"},
			wantRes:   "768P",
		},
		{
			name:      "metadata reference fields",
			model:     "MiniMax-H3-Max",
			body:      `{"prompt":"p","metadata":{"reference_images":["r"],"video_urls":["v"],"audio_url":"a","parameters":{"ratio":"9:16"}}}`,
			wantRoles: []string{"reference_image", "reference_video", "reference_audio"},
			wantRatio: "9:16",
			wantRes:   "768P",
		},
		{
			name:      "legacy single image is first frame",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","image":"i","size":"1920x1080"}`,
			wantRoles: []string{"first_frame"},
			wantRes:   "2K",
		},
		{
			name:      "legacy images with video become references",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","images":["i1","i2"],"video":"v"}`,
			wantRoles: []string{"reference_image", "reference_image", "reference_video"},
			wantRes:   "768P",
		},
		{
			name:      "legacy images with reference audio become references",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","image":"i","metadata":{"audio_urls":["a"],"video_urls":["v1","v2"]}}`,
			wantRoles: []string{"reference_image", "reference_video", "reference_video", "reference_audio"},
			wantRes:   "768P",
		},
		{
			name:      "top-level resolution and ratio as on tokease",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","resolution":"2K","ratio":"9:16"}`,
			wantRatio: "9:16",
			wantRes:   "2K",
		},
		{
			name:      "openai input_reference object is the first frame",
			model:     "MiniMax-H3",
			body:      `{"prompt":"p","input_reference":{"image_url":"i"}}`,
			wantRoles: []string{"first_frame"},
			wantRes:   "768P",
		},
		{
			name:      "lite reference requires explicit ratio",
			model:     "MiniMax-H3-Lite",
			body:      `{"prompt":"p","reference_images":["r"],"size":"480P"}`,
			wantRoles: []string{"reference_image"},
			wantRatio: "16:9",
			wantRes:   "480P",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := buildV2Request(parseTaskReq(t, tt.body), tt.model)
			require.NoError(t, err)
			assert.Equal(t, tt.model, request.Model)
			assert.Equal(t, tt.wantRes, request.Resolution)
			assert.Equal(t, tt.wantRatio, request.Ratio)
			require.NotEmpty(t, request.Content)
			text, ok := request.Content[0].(v2ContentItem)
			require.True(t, ok)
			assert.Equal(t, v2ContentItem{Type: "text", Text: "p"}, text)
			var roles []string
			for _, item := range request.Content[1:] {
				roles = append(roles, item.(v2ContentItem).Role)
			}
			assert.Equal(t, tt.wantRoles, roles)
		})
	}
}

func TestBuildV2RequestPassesNativeContentThrough(t *testing.T) {
	request, err := buildV2Request(parseTaskReq(t, `{"prompt":"p","metadata":{"content":[{"type":"image_url","image_url":{"url":"x"},"role":"last_frame"}],"extra":{"prompt_expansion_mode":"quality"},"aigc_watermark":true}}`), "MiniMax-H3-Max")
	require.NoError(t, err)
	require.Len(t, request.Content, 2)
	assert.Equal(t, v2ContentItem{Type: "text", Text: "p"}, request.Content[0])
	assert.Equal(t, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "x"}, "role": "last_frame"}, request.Content[1])
	assert.Equal(t, map[string]any{"prompt_expansion_mode": "quality"}, request.Extra)
	require.NotNil(t, request.AigcWatermark)
	assert.True(t, *request.AigcWatermark)
}

// 与官方一致的顶层 content 原样透传，text 项就是提示词，不要求再写 prompt。
func TestBuildV2RequestPassesTopLevelContentThrough(t *testing.T) {
	request, err := buildV2Request(parseTaskReq(t, `{"resolution":"768P","duration":5,"ratio":"16:9","content":[{"type":"text","text":"boy"},{"type":"image_url","image_url":{"url":"s"},"role":"first_frame"}]}`), "MiniMax-H3")
	require.NoError(t, err)
	require.Len(t, request.Content, 2)
	assert.Equal(t, map[string]any{"type": "text", "text": "boy"}, request.Content[0])
	assert.Equal(t, "16:9", request.Ratio)
}

// 上游规则与计费边界在网关拒绝，返回 400 而不是交给上游失败。
func TestBuildV2RequestRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name  string
		model string
		body  string
		want  string
	}{
		{"missing prompt", "MiniMax-H3", `{"first_frame_url":"f"}`, "non-empty prompt"},
		{"frames and references mixed", "MiniMax-H3", `{"prompt":"p","first_frame_url":"f","reference_images":["r"]}`, "cannot combine"},
		{"2K on highspeed", "MiniMax-H3-Highspeed", `{"prompt":"p","size":"2K"}`, "supports resolution 768P"},
		{"duration too short for max", "MiniMax-H3-Max", `{"prompt":"p","duration":4}`, "between 5 and 15"},
		{"duration too long", "MiniMax-H3", `{"prompt":"p","duration":30}`, "between 4 and 15"},
		{"adaptive text to video", "MiniMax-H3", `{"prompt":"p","metadata":{"ratio":"adaptive"}}`, "not adaptive"},
		{"too many reference videos", "MiniMax-H3", `{"prompt":"p","video_urls":["1","2","3","4"]}`, "at most 3 reference videos"},
		{"prompt expansion outside max", "MiniMax-H3", `{"prompt":"p","metadata":{"prompt_expansion_mode":"quality"}}`, "does not support prompt_expansion_mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildV2Request(parseTaskReq(t, tt.body), tt.model)
			require.Error(t, err)
			assert.True(t, relaycommon.IsInvalidRequest(err))
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// 计费用量：参考视频按同档单价计时长，输入图扣掉该型号的免费额度后计费。
func TestParseV2TaskResultBillingUsage(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStatus     string
		wantBilling    float64
		wantImages     int
		wantReason     string
		wantURL        string
		wantRetryError bool
	}{
		{
			name:        "succeeded with reference video and images",
			body:        `{"task":{"id":"1","model":"MiniMax-H3","status":"succeeded","content":{"url":"https://v/out.mp4"},"usage":{"output_seconds":5,"input_seconds":6,"input_image_count":7}}}`,
			wantStatus:  model.TaskStatusSuccess,
			wantBilling: 11,
			wantImages:  7,
			wantURL:     "https://v/out.mp4",
		},
		{
			name:        "max reports total input images",
			body:        `{"task":{"id":"1","model":"MiniMax-H3-Max","status":"succeeded","content":{"url":"u"},"usage":{"output_seconds":5,"input_image_count":3}}}`,
			wantStatus:  model.TaskStatusSuccess,
			wantBilling: 5,
			wantImages:  3,
			wantURL:     "u",
		},
		{
			name:        "absurd seconds are clamped",
			body:        `{"task":{"id":"1","model":"MiniMax-H3","status":"succeeded","content":{"url":"u"},"usage":{"output_seconds":99999,"input_seconds":99999}}}`,
			wantStatus:  model.TaskStatusSuccess,
			wantBilling: 2 * relaycommon.MaxTaskDurationSeconds,
			wantURL:     "u",
		},
		{
			name:       "failed",
			body:       `{"task":{"id":"1","status":"failed","error":{"message":"sensitive content"}}}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: "sensitive content",
		},
		{
			name:       "cancelled",
			body:       `{"task":{"id":"1","status":"cancelled"}}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: "task cancelled",
		},
		{
			name:       "running",
			body:       `{"task":{"id":"1","status":"running"}}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:           "query error is retried",
			body:           `{"type":"error","error":{"type":"rate_limit_error","message":"rate limit"}}`,
			wantRetryError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tt.body))
			if tt.wantRetryError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.InDelta(t, tt.wantBilling, result.BillingDuration, 1e-9)
			assert.Equal(t, tt.wantImages, result.InputImageCount)
			assert.Equal(t, tt.wantReason, result.Reason)
			assert.Equal(t, tt.wantURL, result.Url)
		})
	}
}

func TestResolutionTier(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "768p": "768P", "2k": "2K", "480": "480P", "854x480": "480P",
		"1280*720": "768P", "1366x768": "2K", "1920x1080": "2K",
	} {
		assert.Equal(t, want, ResolutionTier(input), input)
	}
}
