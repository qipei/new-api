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
