package autodl

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseTaskRequest(t *testing.T, body string) relaycommon.TaskSubmitReq {
	t.Helper()
	var req relaycommon.TaskSubmitReq
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return req
}

// 网关请求转工作流参数：只放调用方给了的字段（工作流拒收未定义的参数），分辨率转成
// 上游的中文档位，素材转成工作流的字段名。
func TestBuildWorkflowParams(t *testing.T) {
	const img = "https://example.com/a.png"
	const img2 = "https://example.com/b.png"
	const audio = "https://example.com/a.mp3"
	tests := []struct {
		name         string
		body         string
		want         map[string]any
		wantDuration int
	}{
		{
			name:         "text only defaults duration and omits resolution",
			body:         `{"prompt":" cat "}`,
			want:         map[string]any{"prompt": "cat", "duration": 5},
			wantDuration: 5,
		},
		{
			name:         "resolution with portrait ratio",
			body:         `{"prompt":"p","seconds":"8","resolution":"480P","ratio":"9:16"}`,
			want:         map[string]any{"prompt": "p", "duration": 8, "resolution": "480p竖"},
			wantDuration: 8,
		},
		{
			name:         "resolution defaults to landscape",
			body:         `{"prompt":"p","duration":3,"resolution":"768p"}`,
			want:         map[string]any{"prompt": "p", "duration": 3, "resolution": "768p横"},
			wantDuration: 3,
		},
		{
			name:         "size picks tier by short edge and orientation by shape",
			body:         `{"prompt":"p","size":"1920x1088"}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "resolution": "1088p横"},
			wantDuration: 5,
		},
		{
			name:         "square size",
			body:         `{"prompt":"p","size":"480x480","metadata":{"seed":42}}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "resolution": "480p(1:1)", "seed": int64(42)},
			wantDuration: 5,
		},
		{
			name:         "first and last frame fields",
			body:         `{"prompt":"p","first_frame_url":"` + img + `","last_frame_url":"` + img2 + `","seed":7}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "first_frame": img, "last_frame": img2, "seed": int64(7)},
			wantDuration: 5,
		},
		{
			name:         "two legacy images are first and last frame",
			body:         `{"prompt":"p","images":["` + img + `","` + img2 + `"]}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "first_frame": img, "last_frame": img2},
			wantDuration: 5,
		},
		{
			name:         "reference images and audios",
			body:         `{"prompt":"p","reference_images":["` + img + `","` + img2 + `"],"audio_urls":["` + audio + `"]}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "ref_image_0": img, "ref_image_1": img2, "ref_audio_0": audio},
			wantDuration: 5,
		},
		{
			name:         "legacy images with audio are references",
			body:         `{"prompt":"p","image":"` + img + `","audio":"` + audio + `"}`,
			want:         map[string]any{"prompt": "p", "duration": 5, "ref_image_0": img, "ref_audio_0": audio},
			wantDuration: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, duration, err := buildWorkflowParams(parseTaskRequest(t, tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.want, params)
			assert.Equal(t, tt.wantDuration, duration)
		})
	}
}

func TestBuildWorkflowParamsRejectsUnsupportedInput(t *testing.T) {
	const img = "https://example.com/a.png"
	tests := []struct {
		name string
		body string
	}{
		{"duration above upstream max", `{"prompt":"p","duration":16}`},
		{"negative duration", `{"prompt":"p","seconds":"-1"}`},
		{"unsupported ratio", `{"prompt":"p","resolution":"480p","ratio":"4:3"}`},
		{"ratio without resolution", `{"prompt":"p","ratio":"9:16"}`},
		{"resolution label not a tier", `{"prompt":"p","resolution":"hd"}`},
		{"fractional seed", `{"prompt":"p","seed":1.5}`},
		{"string seed", `{"prompt":"p","metadata":{"seed":"1"}}`},
		{"frames with references", `{"prompt":"p","first_frame_url":"` + img + `","audio_urls":["https://example.com/a.mp3"]}`},
		{"reference video", `{"prompt":"p","video_urls":["https://example.com/a.mp4"]}`},
		{"too many reference images", `{"prompt":"p","reference_images":["1","2","3","4","5","6","7","8","9","10"]}`},
		{"too many reference audios", `{"prompt":"p","audio_urls":["1","2","3","4"]}`},
		{"native content", `{"prompt":"p","content":[{"type":"text","text":"p"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := buildWorkflowParams(parseTaskRequest(t, tt.body))
			assert.Error(t, err)
		})
	}
}

// 计费与组装请求按同一套规则归档分辨率，价格表的档位名才能和上游选项对上。
func TestResolutionTier(t *testing.T) {
	assert.Equal(t, "480p", ResolutionTier("864x480"))
	assert.Equal(t, "480p", ResolutionTier("480*864"))
	assert.Equal(t, "768p", ResolutionTier("768P"))
	assert.Equal(t, "1440p", ResolutionTier("2560x1440"))
	assert.Equal(t, "", ResolutionTier(""))
}

func TestParseTaskResult(t *testing.T) {
	adaptor := &TaskAdaptor{}
	const videoURL = "https://cdn.example.com/out.mp4?X-Tos-Date=20261027T134746Z&X-Tos-Expires=86400"
	tests := []struct {
		name       string
		body       string
		wantStatus string
		wantURL    string
		wantReason string
	}{
		{"queued", `{"code":"Success","data":{"task_id":"t","status":"QUEUED","results":[]}}`, model.TaskStatusQueued, "", ""},
		{"running", `{"code":"Success","data":{"task_id":"t","status":"RUNNING","results":[]}}`, model.TaskStatusInProgress, "", ""},
		{
			"success picks the video result",
			`{"code":"Success","data":{"task_id":"t","status":"SUCCESS","results":[{"type":"image","url":"https://cdn.example.com/cover.png"},{"type":"video","url":"` + videoURL + `"}],"cost":0.2}}`,
			model.TaskStatusSuccess, videoURL, "",
		},
		{
			"success without a result fails so the task is refunded",
			`{"code":"Success","data":{"task_id":"t","status":"SUCCESS","results":[]}}`,
			model.TaskStatusFailure, "", "autodl task succeeded without a video or audio result",
		},
		{"failed", `{"code":"Success","data":{"task_id":"t","status":"FAILED"},"msg":"显存不足"}`, model.TaskStatusFailure, "", "显存不足"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adaptor.ParseTaskResult([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantURL, result.Url)
			assert.Equal(t, tt.wantReason, result.Reason)
		})
	}

	// 查询本身出错时任务状态未知，返回错误交给下一轮轮询，而不是判失败退款。
	_, err := adaptor.ParseTaskResult([]byte(`{"code":"InternalError","data":null,"msg":"工作流不可用"}`))
	assert.Error(t, err)
}

// 提交失败的分类：参数不合工作流定义是请求的问题（400、不记渠道错误、不换渠道重试）；
// 其他业务错误和缺任务号都算上游失败，可以换渠道重试。
func TestDoResponseClassifiesSubmitErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantLocal  bool
		wantStatus int
	}{
		{"invalid parameters", `{"code":"RequestParameterIsWrong","data":null,"msg":"缺少必填参数：ref_image_0"}`, true, http.StatusBadRequest},
		{"workflow unavailable", `{"code":"BadRequest","data":null,"msg":"工作流不可用"}`, false, http.StatusInternalServerError},
		{"missing task id", `{"code":"Success","data":{"status":"QUEUED"}}`, false, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			resp := &http.Response{StatusCode: http.StatusOK, Body: httpBody(tt.body)}
			_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{})
			require.NotNil(t, taskErr)
			assert.Equal(t, tt.wantLocal, taskErr.LocalError)
			assert.Equal(t, tt.wantStatus, taskErr.StatusCode)
		})
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusOK, Body: httpBody(`{"code":"Success","data":{"task_id":"up-1","status":"QUEUED"}}`)}
	taskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{
		OriginModelName: "h3-turbo",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	})
	require.Nil(t, taskErr)
	assert.Equal(t, "up-1", taskID)
	assert.Contains(t, recorder.Body.String(), `"id":"task_public"`)
}

func TestResultURLExpired(t *testing.T) {
	const signed = "https://cdn.example.com/out.mp4?X-Tos-Date=20261027T134746Z&X-Tos-Expires=86400"
	signedAt := time.Date(2026, 10, 27, 13, 47, 46, 0, time.UTC)

	assert.False(t, ResultURLExpired(signed, signedAt.Add(time.Hour)))
	// 过期前 5 分钟就当作过期，给调用方留出下载时间。
	assert.True(t, ResultURLExpired(signed, signedAt.Add(24*time.Hour-4*time.Minute)))
	assert.True(t, ResultURLExpired(signed, signedAt.Add(25*time.Hour)))
	// 不认识的地址不触发刷新。
	assert.False(t, ResultURLExpired("https://cdn.example.com/out.mp4", signedAt.Add(48*time.Hour)))
	assert.False(t, ResultURLExpired("", signedAt))
}

func httpBody(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}

// 语音工作流的参数各不相同：请求体去掉 model 后原样作为工作流参数，网关只要求有
// 要合成的文本（预扣按它估算）。
func TestSpeechWorkflowParams(t *testing.T) {
	params, err := speechWorkflowParams(map[string]any{
		"model":              "indextts2",
		"prompt_text":        "你好",
		"prompt_simple":      "https://example.com/voice.wav",
		"emo_control_method": "与音色参考音频相同",
		"emo_happy":          0.5,
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"prompt_text":        "你好",
		"prompt_simple":      "https://example.com/voice.wav",
		"emo_control_method": "与音色参考音频相同",
		"emo_happy":          0.5,
	}, params)

	for _, body := range []map[string]any{
		nil,
		{"model": "indextts2"},
		{"model": "indextts2", "prompt_text": "  "},
		{"model": "indextts2", "prompt_text": 123},
	} {
		_, err := speechWorkflowParams(body)
		assert.Error(t, err)
	}
}

func TestEstimateSpeechSeconds(t *testing.T) {
	assert.Equal(t, 1.0, EstimateSpeechSeconds("你好"))
	assert.Equal(t, 4.0, EstimateSpeechSeconds("你好，这是一段测试文本。"))
	assert.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), EstimateSpeechSeconds(strings.Repeat("字", 100000)))
}

// wavHeader 拼一个 PCM WAV 头：fmt 块之后可选插入其他块，再接 data 块的头。
func wavHeader(byteRate uint32, dataSize uint32, extraChunk []byte) []byte {
	le32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	head := []byte("RIFF")
	head = append(head, le32(0)...)
	head = append(head, []byte("WAVEfmt ")...)
	head = append(head, le32(16)...)
	head = append(head, 1, 0, 1, 0)        // PCM, 单声道
	head = append(head, le32(22050)...)    // 采样率
	head = append(head, le32(byteRate)...) // 每秒字节数
	head = append(head, 2, 0, 16, 0)       // block align, 位深
	head = append(head, extraChunk...)     // LIST 等扩展块
	head = append(head, []byte("data")...)
	return append(head, le32(dataSize)...)
}

// 实测 IndexTTS2 输出 22050Hz 单声道 16 位 PCM WAV（每秒 44100 字节）。时长按 data 块
// 字节数计；流式写出的文件 data 长度为 0 或 0xFFFFFFFF，按文件总大小推算。
func TestWavDurationSeconds(t *testing.T) {
	listChunk := append([]byte("LIST"), 4, 0, 0, 0, 'I', 'N', 'F', 'O')
	tests := []struct {
		name  string
		head  []byte
		total int64
		want  float64
	}{
		{"plain wav", wavHeader(44100, 118800, nil), 118844, 118800.0 / 44100},
		{"extra chunk before data", wavHeader(44100, 88200, listChunk), 0, 2},
		{"streamed data size", wavHeader(44100, 0xFFFFFFFF, nil), 44 + 44100*3, 3},
		{"data size beyond file", wavHeader(44100, 1<<30, nil), 44 + 44100, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := wavDurationSeconds(tt.head, tt.total)
			require.True(t, ok)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}

	for name, head := range map[string][]byte{
		"not riff":       []byte("ID3\x04 mp3 data here"),
		"zero byte rate": wavHeader(0, 1000, nil),
		"truncated":      wavHeader(44100, 1000, nil)[:30],
	} {
		_, ok := wavDurationSeconds(head, 0)
		assert.False(t, ok, name)
	}
}

// 语音结果按实际音频时长结算：读出的时长写进 BillingDuration，异常时长按单次上限钳制，
// 读不出时为 0（完成结算保持预扣额度）。
func TestParseTaskResultReadsAudioDuration(t *testing.T) {
	original := fetchAudioHead
	t.Cleanup(func() { fetchAudioHead = original })
	const body = `{"code":"Success","data":{"task_id":"t","status":"SUCCESS","results":[{"type":"audio","url":"https://cdn.example.com/a.wav"}],"cost":0.01}}`

	fetchAudioHead = func(string) ([]byte, int64, error) { return wavHeader(44100, 118800, nil), 118844, nil }
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Equal(t, "https://cdn.example.com/a.wav", result.Url)
	assert.InDelta(t, 118800.0/44100, result.BillingDuration, 1e-9)

	fetchAudioHead = func(string) ([]byte, int64, error) { return wavHeader(1, 0xFFFFFFF0, nil), 0, nil }
	result, err = (&TaskAdaptor{}).ParseTaskResult([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), result.BillingDuration)

	fetchAudioHead = func(string) ([]byte, int64, error) { return nil, 0, io.ErrUnexpectedEOF }
	result, err = (&TaskAdaptor{}).ParseTaskResult([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Zero(t, result.BillingDuration)
}

// 查询结果与豆包语音的字段一致：成功给音频地址与过期时间，失败给错误原因。
func TestBuildSpeechTaskResponse(t *testing.T) {
	task := &model.Task{TaskID: "task_1", Status: model.TaskStatusSuccess, Progress: "100%", SubmitTime: 100, FinishTime: 105}
	task.Properties.OriginModelName = "indextts2"
	task.PrivateData.ResultURL = "https://cdn.example.com/a.wav?X-Tos-Date=20261027T155457Z&X-Tos-Expires=86400"

	data, err := BuildSpeechTaskResponse(task)
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, common.Unmarshal(data, &resp))
	assert.Equal(t, "succeeded", resp["status"])
	assert.Equal(t, task.PrivateData.ResultURL, resp["audio_url"])
	assert.Equal(t, float64(time.Date(2026, 10, 28, 15, 54, 57, 0, time.UTC).Unix()), resp["url_expire_time"])
	assert.Nil(t, resp["error"])

	failed := &model.Task{TaskID: "task_2", Status: model.TaskStatusFailure, FailReason: "显存不足"}
	data, err = BuildSpeechTaskResponse(failed)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &resp))
	assert.Equal(t, "failed", resp["status"])
	assert.Equal(t, map[string]any{"message": "显存不足"}, resp["error"])
}

// 语音任务的提交结果与豆包语音的异步接口字段一致，而不是视频对象。
func TestDoResponseSpeechSubmit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusOK, Body: httpBody(`{"code":"Success","data":{"task_id":"up-2","status":"QUEUED"}}`)}
	taskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{
		OriginModelName: "indextts2",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public", Action: constant.TaskActionSpeech},
	})
	require.Nil(t, taskErr)
	assert.Equal(t, "up-2", taskID)

	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "task_public", body["task_id"])
	assert.Equal(t, "indextts2", body["model"])
	assert.Equal(t, "queued", body["status"])
	assert.Nil(t, body["object"])
}

// 各工作流的分辨率选项写法不同（有的带像素尺寸），按工作流定义匹配；工作流没有的档位
// 在本地拒绝并列出支持的档位；取不到定义时原样发送，由上游校验。
// stubWorkflowDefinitions 用本地的工作流定义代替 autodl 的定义接口，选项名照抄线上定义。
func stubWorkflowDefinitions(t *testing.T) {
	original := fetchWorkflowDefinition
	t.Cleanup(func() {
		fetchWorkflowDefinition = original
		clearWorkflowDefinitionCache()
	})
	definitions := map[string]WorkflowDefinition{
		"minimax_h3_803": {
			ResolutionOptions: []string{"480p竖(480*864)", "768p竖(768*1376)", "1088p横(1920*1088)"},
			HasMode:           true,
			ModeOptions:       []string{"高速模式", "高质模式"},
		},
		"minimax_h3_zm_u24": {ResolutionOptions: []string{"480p横", "480p竖", "768p竖", "480p(1:1)"}},
	}
	fetchWorkflowDefinition = func(_ string, workflowID string) (WorkflowDefinition, error) {
		definition, ok := definitions[workflowID]
		if !ok {
			return WorkflowDefinition{}, errors.New("暂未找到该工作流详情")
		}
		return definition, nil
	}
}

func TestWorkflowResolutionOption(t *testing.T) {
	stubWorkflowDefinitions(t)
	adaptor := &TaskAdaptor{baseURL: "https://test.autodl.example"}

	tests := []struct {
		workflow string
		label    string
		want     string
	}{
		{"minimax_h3_803", "768p竖", "768p竖(768*1376)"},
		{"minimax_h3_803", "1088p横", "1088p横(1920*1088)"},
		{"minimax_h3_zm_u24", "480p竖", "480p竖"},
		{"minimax_h3_zm_u24", "480p(1:1)", "480p(1:1)"},
		{"unknown_workflow", "768p竖", "768p竖"},
	}
	for _, tt := range tests {
		got, err := adaptor.workflowResolutionOption(tt.workflow, tt.label)
		require.NoError(t, err, tt.workflow+" "+tt.label)
		assert.Equal(t, tt.want, got, tt.workflow+" "+tt.label)
	}

	_, err := adaptor.workflowResolutionOption("minimax_h3_zm_u24", "1088p竖")
	require.Error(t, err)
	assert.True(t, relaycommon.IsInvalidRequest(err))
	assert.Contains(t, err.Error(), "480p竖")
}

// 速度模式：fast/quality 和中文选项名都换成工作流的选项；工作流没有 mode 参数时本地拒绝，
// 不让上游以参数错误拒单；取不到定义时按常见选项名发送。
func TestWorkflowSpeedMode(t *testing.T) {
	stubWorkflowDefinitions(t)
	adaptor := &TaskAdaptor{baseURL: "https://test.autodl.example"}

	tests := []struct {
		workflow string
		mode     string
		want     string
	}{
		{"minimax_h3_803", "fast", "高速模式"},
		{"minimax_h3_803", "Quality", "高质模式"},
		{"minimax_h3_803", "高速模式", "高速模式"},
		{"minimax_h3_803", "高质", "高质模式"},
		{"unknown_workflow", "fast", "高速模式"},
	}
	for _, tt := range tests {
		got, err := adaptor.workflowSpeedMode(tt.workflow, tt.mode)
		require.NoError(t, err, tt.workflow+" "+tt.mode)
		assert.Equal(t, tt.want, got, tt.workflow+" "+tt.mode)
	}

	for _, tt := range []struct{ workflow, mode string }{
		{"minimax_h3_zm_u24", "fast"},
		{"minimax_h3_803", "turbo"},
	} {
		_, err := adaptor.workflowSpeedMode(tt.workflow, tt.mode)
		require.Error(t, err, tt.workflow+" "+tt.mode)
		assert.True(t, relaycommon.IsInvalidRequest(err))
	}
}

// 顶层 mode 的 reference 提示只决定素材用法，不作为速度模式发给工作流。
func TestBuildWorkflowParamsMode(t *testing.T) {
	params, _, err := buildWorkflowParams(relaycommon.TaskSubmitReq{Prompt: "p", Mode: "fast"})
	require.NoError(t, err)
	assert.Equal(t, "fast", params["mode"])

	params, _, err = buildWorkflowParams(relaycommon.TaskSubmitReq{Prompt: "p", Mode: "reference", Images: []string{"https://example.com/a.png"}})
	require.NoError(t, err)
	assert.NotContains(t, params, "mode")
	assert.Equal(t, "https://example.com/a.png", params["ref_image_0"])
}

func clearWorkflowDefinitionCache() {
	workflowDefinitionCache.Range(func(key, _ any) bool {
		workflowDefinitionCache.Delete(key)
		return true
	})
}

// 定义取到后缓存一天：再次提交不重新读取；读取失败时沿用旧定义，失败后短时间内不再重取，
// 免得上游定义接口出问题时每次提交都等超时；后台手动刷新会立即重新读取。
func TestWorkflowDefinitionCache(t *testing.T) {
	original := fetchWorkflowDefinition
	t.Cleanup(func() {
		fetchWorkflowDefinition = original
		clearWorkflowDefinitionCache()
	})
	const baseURL = "https://cache.autodl.example"
	fetches := 0
	var next WorkflowDefinition
	var nextErr error
	fetchWorkflowDefinition = func(string, string) (WorkflowDefinition, error) {
		fetches++
		return next, nextErr
	}

	next = WorkflowDefinition{ResolutionOptions: []string{"480p竖"}}
	got, err := CachedWorkflowDefinition(baseURL, "wf")
	require.NoError(t, err)
	assert.Equal(t, []string{"480p竖"}, got.ResolutionOptions)
	_, err = CachedWorkflowDefinition(baseURL, "wf")
	require.NoError(t, err)
	assert.Equal(t, 1, fetches, "cached definition is reused")

	nextErr = errors.New("definition api down")
	got, err = RefreshWorkflowDefinition(baseURL, "wf")
	require.NoError(t, err, "a failed refresh keeps the cached definition")
	assert.Equal(t, []string{"480p竖"}, got.ResolutionOptions)

	_, err = CachedWorkflowDefinition(baseURL, "missing")
	require.Error(t, err)
	_, err = CachedWorkflowDefinition(baseURL, "missing")
	require.Error(t, err)
	assert.Equal(t, 3, fetches, "a failed fetch is not retried right away")

	next, nextErr = WorkflowDefinition{ResolutionOptions: []string{"768p竖"}}, nil
	got, err = RefreshWorkflowDefinition(baseURL, "wf")
	require.NoError(t, err)
	assert.Equal(t, []string{"768p竖"}, got.ResolutionOptions)
	got, err = CachedWorkflowDefinition(baseURL, "wf")
	require.NoError(t, err)
	assert.Equal(t, []string{"768p竖"}, got.ResolutionOptions)
}
