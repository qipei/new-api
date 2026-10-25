package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按用途区分的素材字段：顶层与 metadata 写法都认，单复数合并去重。
func TestTaskSubmitReqRoleMediaReadsTopLevelAndMetadata(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{
		"prompt": "p",
		"first_frame_url": "https://a/first.png",
		"reference_images": ["https://a/r1.png"],
		"video_url": "https://a/v1.mp4",
		"metadata": {
			"last_frame_url": "https://a/last.png",
			"reference_images": ["https://a/r2.png", "https://a/r1.png"],
			"video_urls": ["https://a/v2.mp4"],
			"audio_url": "https://a/a1.mp3"
		}
	}`), &req))

	assert.Equal(t, TaskMedia{
		FirstFrame:      "https://a/first.png",
		LastFrame:       "https://a/last.png",
		ReferenceImages: []string{"https://a/r1.png", "https://a/r2.png"},
		ReferenceVideos: []string{"https://a/v1.mp4", "https://a/v2.mp4"},
		ReferenceAudios: []string{"https://a/a1.mp3"},
	}, req.RoleMedia())
}

// 只用新字段时补齐旧字段，未适配角色的渠道和计费识别都能拿到素材；
// 调用方自己传的旧字段原样保留，不被覆盖。
func TestTaskSubmitReqFillsLegacyMediaOnlyWhenAbsent(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantImages []string
		wantVideo  string
		wantAudios []string
	}{
		{
			name:       "first and last frame",
			body:       `{"first_frame_url":"f","last_frame_url":"l"}`,
			wantImages: []string{"f", "l"},
		},
		{
			name:       "reference media",
			body:       `{"metadata":{"reference_images":["r1","r2"],"video_urls":["v1","v2"],"audio_urls":["a1"]}}`,
			wantImages: []string{"r1", "r2"},
			wantVideo:  "v1",
			wantAudios: []string{"a1"},
		},
		{
			name: "last frame alone cannot be expressed by legacy fields",
			body: `{"last_frame_url":"l"}`,
		},
		{
			name:       "legacy fields untouched",
			body:       `{"images":["i1"],"video":"v0"}`,
			wantImages: []string{"i1"},
			wantVideo:  "v0",
		},
		{
			name:       "legacy images with reference audio fills audios only",
			body:       `{"images":["i1"],"metadata":{"audio_urls":["a1"]}}`,
			wantImages: []string{"i1"},
			wantAudios: []string{"a1"},
		},
		{
			name:       "openai input_reference object becomes images for image-only channels",
			body:       `{"input_reference":{"image_url":"u1"},"video_urls":["v1"]}`,
			wantImages: []string{"u1"},
			wantVideo:  "v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req TaskSubmitReq
			require.NoError(t, common.Unmarshal([]byte(tt.body), &req))
			assert.Equal(t, tt.wantImages, req.Images)
			assert.Equal(t, tt.wantVideo, req.Video)
			assert.Equal(t, tt.wantAudios, req.Audios)
		})
	}
}

// 百炼老版万相在 metadata 顶层的 first_frame_url 等是原生参数：不补旧字段、不算混用，
// 已有请求的行为不变。
func TestTaskSubmitReqKeepsAliNativeMetadataFields(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"image":"i","metadata":{"first_frame_url":"f","audio_url":"a"}}`), &req))
	assert.Equal(t, "", req.Video)
	assert.Nil(t, req.Audios)
	assert.False(t, req.MixesMediaStyles())
}

// 参考视频、参考音频只有一种用途，旧写法与 video_urls/audio_urls 合并送出，旧写法在前；
// 旧写法传图时仍按渠道原有的张数规则推断，不走按用途组装。
func TestTaskSubmitReqMergesReferenceVideoAndAudio(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"video":"v0","audio":"a0","video_urls":["v1","v0"],"metadata":{"audio_urls":["a1"]},"reference_images":["r"]}`), &req))
	assert.True(t, req.UsesRoleMedia())
	media := req.RoleMedia()
	assert.Equal(t, []string{"v0", "v1"}, media.ReferenceVideos)
	assert.Equal(t, []string{"a0", "a1"}, media.ReferenceAudios)

	var legacyImages TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"images":["i"],"metadata":{"audio_urls":["a1"]}}`), &legacyImages))
	assert.False(t, legacyImages.UsesRoleMedia())
	assert.Equal(t, []string{"a1"}, legacyImages.RoleMedia().ReferenceAudios)
}

// OpenAI 视频接口的 input_reference 支持字符串和 {image_url}/{file_id} 对象；只有 file_id 时
// 网关取不到图，留给透传渠道原样转发，不能让整个请求解析失败。
func TestTaskSubmitReqParsesOpenAIInputReference(t *testing.T) {
	for body, want := range map[string]string{
		`{"input_reference":"https://a/i.png"}`:                       "https://a/i.png",
		`{"input_reference":{"image_url":"https://a/i.png"}}`:         "https://a/i.png",
		`{"input_reference":{"image_url":{"url":"https://a/i.png"}}}`: "https://a/i.png",
		`{"input_reference":{"file_id":"file-1"}}`:                    "",
	} {
		var req TaskSubmitReq
		require.NoError(t, common.Unmarshal([]byte(body), &req), body)
		assert.Equal(t, want, req.InputReference, body)
	}
}

func TestValidateTaskRequestRoleMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"role fields accepted", `{"prompt":"p","first_frame_url":"f","last_frame_url":"l"}`, ""},
		{"mixed with legacy images", `{"prompt":"p","images":["i"],"reference_images":["r"]}`, "not both"},
		{"input_reference mixed with first frame", `{"prompt":"p","input_reference":"i","first_frame_url":"f"}`, "not both"},
		{"legacy images with reference audio and video", `{"prompt":"p","images":["i"],"metadata":{"audio_urls":["a"],"video_urls":["v"]}}`, ""},
		{"legacy video with reference images", `{"prompt":"p","video":"v","reference_images":["r"]}`, ""},
		{"mixed with native content", `{"prompt":"p","video_urls":["v"],"metadata":{"content":[{"type":"text","text":"p"}]}}`, "content already lists"},
		{"mixed with top-level content", `{"prompt":"p","reference_images":["r"],"content":[{"type":"text","text":"p"}]}`, "content already lists"},
		{"top-level content carries the prompt", `{"content":[{"type":"text","text":"p"},{"type":"image_url","image_url":{"url":"f"},"role":"first_frame"}]}`, ""},
		{"too many reference videos", `{"prompt":"p","video_urls":["1","2","3","4","5","6","7","8","9","10","11"]}`, "at most 10"},
		{"media only without prompt for media tasks", `{"metadata":{"reference_images":["r"]}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = request
			taskErr := ValidateMediaTaskRequest(c, &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}, constant.TaskActionGenerate)
			if tt.wantErr == "" {
				require.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Contains(t, taskErr.Message, tt.wantErr)
		})
	}
}
