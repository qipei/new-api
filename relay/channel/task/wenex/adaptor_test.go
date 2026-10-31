package wenex

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseTaskRequest(t *testing.T, body string) relaycommon.TaskSubmitReq {
	t.Helper()
	var req relaycommon.TaskSubmitReq
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return req
}

// 网关请求转上游请求体：画质、比例、秒数总是显式发出（上游会把不认识的 size 静默换档，
// 计费按这里的画质和秒数），素材按用途转成 first_frame/last_frame/images/audios。
func TestBuildGenerationBody(t *testing.T) {
	const img = "https://example.com/a.png"
	const img2 = "https://example.com/b.png"
	const audio = "https://example.com/a.mp3"
	base := func(extra map[string]any) map[string]any {
		body := map[string]any{"model": "h3_video", "prompt": "p", "seconds": 5, "resolution": "768p", "aspect_ratio": "9:16"}
		for key, value := range extra {
			body[key] = value
		}
		return body
	}
	tests := []struct {
		name        string
		body        string
		want        map[string]any
		wantSeconds int
	}{
		{
			name:        "text only defaults to 768p portrait like the workflow line",
			body:        `{"prompt":" p "}`,
			want:        base(nil),
			wantSeconds: 5,
		},
		{
			name:        "quality and ratio labels",
			body:        `{"prompt":"p","seconds":"15","resolution":"1080P","ratio":"9:16"}`,
			want:        base(map[string]any{"seconds": 15, "resolution": "1080p", "aspect_ratio": "9:16"}),
			wantSeconds: 15,
		},
		{
			name:        "720p maps to 768p and aspect_ratio is read",
			body:        `{"prompt":"p","duration":4,"resolution":"720p","aspect_ratio":"21:9"}`,
			want:        base(map[string]any{"seconds": 4, "aspect_ratio": "21:9"}),
			wantSeconds: 4,
		},
		{
			name:        "common 720p size",
			body:        `{"prompt":"p","size":"1280x720"}`,
			want:        base(map[string]any{"aspect_ratio": "16:9"}),
			wantSeconds: 5,
		},
		{
			name:        "quality without ratio is landscape",
			body:        `{"prompt":"p","resolution":"768p"}`,
			want:        base(map[string]any{"aspect_ratio": "16:9"}),
			wantSeconds: 5,
		},
		{
			name:        "workflow tier name 1088p is 1080p",
			body:        `{"prompt":"p","resolution":"1088p","ratio":"1:1"}`,
			want:        base(map[string]any{"resolution": "1080p", "aspect_ratio": "1:1"}),
			wantSeconds: 5,
		},
		{
			name:        "1080p portrait size",
			body:        `{"prompt":"p","size":"1080x1920"}`,
			want:        base(map[string]any{"resolution": "1080p", "aspect_ratio": "9:16"}),
			wantSeconds: 5,
		},
		{
			name:        "upstream canvas size picks the nearest ratio",
			body:        `{"prompt":"p","size":"1440x1088"}`,
			want:        base(map[string]any{"resolution": "1080p", "aspect_ratio": "4:3"}),
			wantSeconds: 5,
		},
		{
			name:        "ratio in size and quality in metadata",
			body:        `{"prompt":"p","size":"1:1","metadata":{"quality":"1080p","duration":10}}`,
			want:        base(map[string]any{"seconds": 10, "resolution": "1080p", "aspect_ratio": "1:1"}),
			wantSeconds: 10,
		},
		{
			name:        "tier label in size with ratio in metadata.parameters",
			body:        `{"prompt":"p","size":"1080P","metadata":{"parameters":{"ratio":"3:4"}}}`,
			want:        base(map[string]any{"resolution": "1080p", "aspect_ratio": "3:4"}),
			wantSeconds: 5,
		},
		{
			name:        "adaptive ratio is treated as unset",
			body:        `{"prompt":"p","size":"768P","metadata":{"parameters":{"ratio":"adaptive"}}}`,
			want:        base(map[string]any{"aspect_ratio": "16:9"}),
			wantSeconds: 5,
		},
		{
			name:        "adaptive ratio keeps the ratio of size",
			body:        `{"prompt":"p","size":"720x1280","ratio":"Adaptive"}`,
			want:        base(nil),
			wantSeconds: 5,
		},
		{
			name:        "auto size uses defaults",
			body:        `{"prompt":"p","size":"auto"}`,
			want:        base(nil),
			wantSeconds: 5,
		},
		{
			name:        "top-level martial arts",
			body:        `{"prompt":"p","martial_arts":true,"motion_speed":4}`,
			want:        base(map[string]any{"metadata": map[string]any{"martial_arts": true, "motion_speed": float64(4)}}),
			wantSeconds: 5,
		},
		{
			name:        "seed and martial arts go upstream",
			body:        `{"prompt":"p","seed":42,"metadata":{"martial_arts":true,"motion_speed":-2.5}}`,
			want:        base(map[string]any{"seed": int64(42), "metadata": map[string]any{"martial_arts": true, "motion_speed": -2.5}}),
			wantSeconds: 5,
		},
		{
			name:        "martial arts false sends nothing",
			body:        `{"prompt":"p","metadata":{"martial_arts":false}}`,
			want:        base(nil),
			wantSeconds: 5,
		},
		{
			name:        "first and last frame fields",
			body:        `{"prompt":"p","first_frame_url":"` + img + `","last_frame_url":"` + img2 + `"}`,
			want:        base(map[string]any{"first_frame": img, "last_frame": img2}),
			wantSeconds: 5,
		},
		{
			name:        "one legacy image is the first frame",
			body:        `{"prompt":"p","image":"` + img + `"}`,
			want:        base(map[string]any{"first_frame": img}),
			wantSeconds: 5,
		},
		{
			name: "reference images with audio",
			body: `{"prompt":"p","reference_images":["` + img + `","` + img2 + `"],"audio_urls":["` + audio + `"]}`,
			want: base(map[string]any{
				"images": []map[string]string{{"url": img, "role": "reference_image"}, {"url": img2, "role": "reference_image"}},
				"audios": []map[string]string{{"url": audio, "role": "reference_audio"}},
			}),
			wantSeconds: 5,
		},
		{
			name:        "legacy images with reference mode",
			body:        `{"prompt":"p","mode":"reference","images":["` + img + `"]}`,
			want:        base(map[string]any{"images": []map[string]string{{"url": img, "role": "reference_image"}}}),
			wantSeconds: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, seconds, err := buildGenerationBody(parseTaskRequest(t, tt.body), "h3_video")
			require.NoError(t, err)
			assert.Equal(t, tt.want, body)
			assert.Equal(t, tt.wantSeconds, seconds)
		})
	}
}

// 上游不支持的取值在本地拒绝：秒数和画质决定计费，不能靠上游换档或截断。
func TestBuildGenerationBodyRejects(t *testing.T) {
	const img = "https://example.com/a.png"
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing prompt", `{"prompt":" "}`, "prompt is required"},
		{"too short", `{"prompt":"p","seconds":"3"}`, "between 4 and 15"},
		{"too long", `{"prompt":"p","duration":16}`, "between 4 and 15"},
		{"fractional seconds", `{"prompt":"p","seconds":"5.5"}`, "must be an integer"},
		{"huge metadata duration", `{"prompt":"p","metadata":{"duration":1e300}}`, "must be an integer"},
		{"unsupported quality", `{"prompt":"p","resolution":"2k"}`, "768p or 1080p"},
		{"unsupported tier label in size", `{"prompt":"p","size":"2K"}`, "768p or 1080p"},
		{"size above 1080p", `{"prompt":"p","size":"4096x4096"}`, "larger than 1080p"},
		{"size with unsupported ratio", `{"prompt":"p","size":"1000x700"}`, "does not match a supported ratio"},
		{"unsupported ratio", `{"prompt":"p","ratio":"2:1"}`, "ratio must be one of"},
		{"negative seed", `{"prompt":"p","seed":-1}`, "seed must be an integer"},
		{"seed above upstream range", `{"prompt":"p","seed":9007199254740993}`, "seed must be an integer"},
		{"motion speed without martial arts", `{"prompt":"p","metadata":{"motion_speed":2}}`, "requires martial_arts"},
		{"motion speed off step", `{"prompt":"p","metadata":{"martial_arts":true,"motion_speed":0.3}}`, "steps of 0.5"},
		{"reference video", `{"prompt":"p","video_url":"https://example.com/a.mp4"}`, "reference videos"},
		{"frames with references", `{"prompt":"p","first_frame_url":"` + img + `","reference_images":["` + img + `"]}`, "cannot be combined"},
		{"audio without image", `{"prompt":"p","audio_urls":["https://example.com/a.mp3"]}`, "require at least one reference image"},
		{"native content", `{"prompt":"p","content":[{"type":"text","text":"p"}]}`, "content is not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := buildGenerationBody(parseTaskRequest(t, tt.body), "h3_video")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// 查询结果：完成时取视频直链，失败时带上上游错误码；只有 error 没有 status 的是查询
// 本身出错（鉴权、查不到），返回错误让轮询下一轮重试，而不是判任务失败。
func TestParseTaskResult(t *testing.T) {
	adaptor := &TaskAdaptor{}

	result, err := adaptor.ParseTaskResult([]byte(`{"id":"t1","status":"in_progress","progress":40}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusInProgress, result.Status)
	assert.Equal(t, "40%", result.Progress)

	result, err = adaptor.ParseTaskResult([]byte(`{"id":"t1","status":"completed","progress":100,"url":"https://vod.example.com/out.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Equal(t, "https://vod.example.com/out.mp4", result.Url)

	result, err = adaptor.ParseTaskResult([]byte(`{"id":"t1","status":"completed","progress":100}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, result.Status)

	result, err = adaptor.ParseTaskResult([]byte(`{"id":"t1","status":"failed","error":{"code":"content_rejected","message":"提示词未通过审核"}}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, result.Status)
	assert.Equal(t, "content_rejected: 提示词未通过审核", result.Reason)

	_, err = adaptor.ParseTaskResult([]byte(`{"error":{"code":"not_found","message":"任务不存在"}}`))
	require.Error(t, err)
}

// 自动分组选路用它判断本线路能否处理请求，与提交前的校验同一套规则：参考视频、2K 这类
// 本线路处理不了的请求要换到其他分组，而不是派过来再在本地失败。
func TestSupportsTaskRequest(t *testing.T) {
	adaptor := &TaskAdaptor{}
	assert.True(t, adaptor.SupportsTaskRequest(parseTaskRequest(t, `{"prompt":"p","resolution":"768p","metadata":{"parameters":{"ratio":"adaptive"}}}`)))
	assert.False(t, adaptor.SupportsTaskRequest(parseTaskRequest(t, `{"prompt":"p","reference_images":["https://example.com/a.png"],"video_urls":["https://example.com/a.mp4"]}`)))
	assert.False(t, adaptor.SupportsTaskRequest(parseTaskRequest(t, `{"prompt":"p","size":"2K"}`)))
	assert.False(t, adaptor.SupportsTaskRequest(parseTaskRequest(t, `{"prompt":"p","content":[{"type":"text","text":"p"}]}`)))
}
