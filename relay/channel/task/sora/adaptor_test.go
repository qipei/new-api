package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraBuildRequestBodyReturnsReplayablePassThroughBody(t *testing.T) {
	payload := []byte("opaque-sora-request-body")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/octet-stream")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	replayable, ok := body.(common.ReplayableBody)
	require.True(t, ok)

	sent, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, sent)
	assert.EqualValues(t, len(payload), replayable.Size())

	replayBody, err := replayable.NewReader()
	require.NoError(t, err)
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)
}

// 顶层的参考素材字段在 metadata 里补一份给对接平台；metadata 里已有的同名字段和
// 原顶层字段都保持不动。
func TestSoraBuildRequestBodyMirrorsReferenceMediaIntoMetadata(t *testing.T) {
	payload := []byte(`{"model":"alias","prompt":"p","reference_images":["r1"],"video_urls":["v1"],"metadata":{"video_urls":["v-meta"],"ratio":"16:9"}}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "Doubao-Seedance-2.0"}}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	sent, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(sent, &got))
	assert.Equal(t, "Doubao-Seedance-2.0", got["model"])
	assert.Equal(t, []any{"r1"}, got["reference_images"])
	assert.Equal(t, []any{"v1"}, got["video_urls"])
	assert.Equal(t, map[string]any{
		"reference_images": []any{"r1"},
		"video_urls":       []any{"v-meta"},
		"ratio":            "16:9",
	}, got["metadata"])
}

// 转发到 tokease 等平台时按转换模式的写法补图片字段：首尾帧各平台都认 first_frame_url，且与
// images 互斥，不能补进 images；参考图除 MiniMax-H3 外只认 images。原有字段一律不动。
func TestSupplementPlatformFields(t *testing.T) {
	tests := []struct {
		name  string
		model string
		body  string
		want  map[string]any
	}{
		{
			name:  "seedance frames stay as first_frame_url and last_frame_url",
			model: "seedance2.5",
			body:  `{"prompt":"p","first_frame_url":"f","last_frame_url":"l"}`,
			want:  map[string]any{},
		},
		{
			name:  "seedance reference images from metadata",
			model: "Seedance 2.0",
			body:  `{"prompt":"p","metadata":{"reference_images":["r1","r2"]}}`,
			want:  map[string]any{"images": []string{"r1", "r2"}},
		},
		{
			name:  "openai input_reference object becomes the first frame",
			model: "seedance2.5",
			body:  `{"prompt":"p","input_reference":{"image_url":"u"}}`,
			want:  map[string]any{"first_frame_url": "u"},
		},
		{
			name:  "minimax keeps reference images out of images",
			model: "MiniMax-H3/qy2",
			body:  `{"prompt":"p","resolution":"768P","reference_images":["r"]}`,
			want:  map[string]any{},
		},
		{
			name:  "minimax input_reference becomes first frame",
			model: "MiniMax-H3",
			body:  `{"prompt":"p","resolution":"2K","input_reference":"u"}`,
			want:  map[string]any{"first_frame_url": "u"},
		},
		{
			name:  "minimax text to video gets the billed tier and a default ratio",
			model: "MiniMax-H3",
			body:  `{"prompt":"p","size":"1920x1080"}`,
			want:  map[string]any{"resolution": "2K", "ratio": "16:9"},
		},
		{
			name:  "minimax ratio from metadata.parameters as in the marketplace sample",
			model: "MiniMax-H3",
			body:  `{"prompt":"p","size":"768P","metadata":{"parameters":{"ratio":"9:16"}}}`,
			want:  map[string]any{"resolution": "768P", "ratio": "9:16"},
		},
		{
			name:  "minimax first frame keeps adaptive ratio",
			model: "MiniMax-H3",
			body:  `{"prompt":"p","resolution":"768P","first_frame_url":"f"}`,
			want:  map[string]any{},
		},
		{
			name:  "minimax caller ratio in metadata untouched",
			model: "MiniMax-H3",
			body:  `{"prompt":"p","resolution":"768P","metadata":{"ratio":"1:1"}}`,
			want:  map[string]any{},
		},
		{
			name:  "caller images untouched",
			model: "seedance2.5",
			body:  `{"prompt":"p","images":["i"],"metadata":{"audio_urls":["a"]}}`,
			want:  map[string]any{},
		},
		{
			name:  "native content untouched",
			model: "seedance2.5",
			body:  `{"content":[{"type":"text","text":"p"}]}`,
			want:  map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req relaycommon.TaskSubmitReq
			require.NoError(t, common.Unmarshal([]byte(tt.body), &req))
			bodyMap := map[string]interface{}{}
			require.NoError(t, common.Unmarshal([]byte(tt.body), &bodyMap))
			before := len(bodyMap)
			supplementPlatformFields(bodyMap, req, tt.model)
			for key, value := range tt.want {
				assert.Equal(t, value, bodyMap[key], key)
			}
			assert.Len(t, bodyMap, before+len(tt.want))
		})
	}
}

// 转发的 MiniMax-H3 完成时按「输出秒数 + 参考视频秒数」结算，上游报告的输入图总张数原样透出。
func TestParseTaskResultReadsMiniMaxSecondsUsage(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t","model":"MiniMax-H3","status":"completed","usage":{"total_seconds":13,"input_seconds":8,"output_seconds":5,"input_image_count":7}}`))
	require.NoError(t, err)
	assert.InDelta(t, 13, result.BillingDuration, 1e-9)
	assert.InDelta(t, 8, result.InputVideoSeconds, 1e-9)
	assert.Equal(t, 7, result.InputImageCount)

	seedance, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t","model":"Seedance 2.0","status":"completed","usage":{"completion_tokens":50638,"total_tokens":50638}}`))
	require.NoError(t, err)
	assert.Zero(t, seedance.BillingDuration)
	assert.Equal(t, 50638, seedance.TotalTokens)
}
