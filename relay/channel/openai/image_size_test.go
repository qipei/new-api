package openai

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 转发给 OpenAI 兼容线路的图片生成请求，百炼原生的「宽*高」换成规范的「宽x高」：
// 只认 x 的中转收到 * 会报计费档位匹配失败。其他写法原样转发。
func TestConvertImageRequestNormalizesAsteriskSize(t *testing.T) {
	tests := []struct {
		size string
		want string
	}{
		{"1024*1024", "1024x1024"},
		{"1664*928", "1664x928"},
		{"1024x1024", "1024x1024"},
		{"2K", "2K"},
		{"", ""},
		{"1*2*3", "1*2*3"},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}

			converted, err := (&Adaptor{}).ConvertImageRequest(c, info, dto.ImageRequest{Model: "qwen-image-3.0", Prompt: "p", Size: tt.size})

			require.NoError(t, err)
			request, ok := converted.(dto.ImageRequest)
			require.True(t, ok)
			assert.Equal(t, tt.want, request.Size)
		})
	}
}
