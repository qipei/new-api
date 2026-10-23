package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 音频生成的 text_prompt 长度与参考条数是网关侧边界；参考资源的组合规则交给上游。
func TestGetAndValidAudioRequestAudioGenerationBounds(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"valid with undocumented reference type", `{"model":"seed-audio-1.0","text_prompt":"你好","references":[{"video_url":"https://v"}]}`, ""},
		{"missing prompt", `{"model":"seed-audio-1.0"}`, "text_prompt is required"},
		{"prompt over limit", `{"model":"seed-audio-1.0","text_prompt":"` + strings.Repeat("字", relaycommon.MaxAudioGenerationPromptChars+1) + `"}`, "at most 3000 characters"},
		{"too many references", `{"model":"seed-audio-1.0","text_prompt":"x","references":[{},{},{},{}]}`, "at most 3 entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/generations", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			request, err := GetAndValidAudioRequest(c, relayconstant.RelayModeAudioGeneration)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, "你好", request.Input)
				return
			}
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// 百炼的原生请求把参数放在 input 下：音频生成用 text_prompt，语音合成用 text。
func TestGetAndValidAudioRequestQwenAudioShapes(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantInput string
		wantErr   string
	}{
		{"generation prompt", `{"model":"qwen-audio-3.1-tts-next","input":{"text_prompt":"你好","references":[{"audio_url":"https://a"}]}}`, "你好", ""},
		{"synthesis text", `{"model":"qwen-audio-3.1-tts-plus","input":{"text":"你好","voice":"v"}}`, "你好", ""},
		{"missing input text", `{"model":"qwen-audio-3.1-tts-next","input":{}}`, "", "input.text_prompt is required"},
		{"generation prompt over limit", `{"model":"qwen-audio-3.1-tts-next","input":{"text_prompt":"` + strings.Repeat("字", relaycommon.MaxAudioGenerationPromptChars+1) + `"}}`, "", "input.text_prompt must be at most 3000"},
		{"synthesis text over limit", `{"model":"qwen-audio-3.1-tts-plus","input":{"text":"` + strings.Repeat("a", relaycommon.MaxSpeechSynthesisTextChars+1) + `"}}`, "", "input.text must be at most 20000"},
		{"too many references", `{"model":"qwen-audio-3.1-tts-next","input":{"text_prompt":"x","references":[{},{},{},{}]}}`, "", "at most 3 entries"},
		{"text and prompt together", `{"model":"qwen-audio-3.1-tts-next","input":{"text":"a","text_prompt":"b"}}`, "", "mutually exclusive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/generations", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			request, err := GetAndValidAudioRequest(c, relayconstant.RelayModeAudioGeneration)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.wantInput, request.Input)
				return
			}
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
