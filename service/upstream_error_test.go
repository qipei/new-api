package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func upstreamResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

// 上游即使使用 5xx 返回参数错误，也必须保留用户可据以修正请求的说明。
func TestUserFacingRelayErrorPreservesDetails(t *testing.T) {
	for _, status := range []int{400, 401, 402, 404, 429, 500, 502} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			message := "图片模型无法匹配当前分辨率计费档位，可用分辨率：1K、2K，详情 https://api.relay-example.ai/help"
			upstreamErr := RelayErrorHandler(context.Background(), upstreamResponse(status, `{"error":{"message":"`+message+`","type":"invalid_request_error"}}`), false)
			require.NotNil(t, upstreamErr)
			ResetStatusCode(upstreamErr, `{"404":"500"}`)
			wantStatus := upstreamErr.StatusCode
			wantCode := upstreamErr.GetErrorCode()
			userErr := UserFacingRelayError(upstreamErr)
			for _, got := range []string{userErr.ToOpenAIError().Message, userErr.ToClaudeError().Message, userErr.MaskSensitiveErrorWithStatusCode()} {
				assert.Contains(t, got, "可用分辨率：1K、2K")
				assert.NotContains(t, got, "api.relay-example.ai")
			}
			assert.Equal(t, wantStatus, userErr.StatusCode)
			assert.Equal(t, wantCode, userErr.GetErrorCode())
		})
	}
	assert.Nil(t, UserFacingRelayError(nil))
}

func TestUserFacingRelayNetworkErrorPreservesCause(t *testing.T) {
	err := types.NewOpenAIError(errors.New(`Post "https://api.relay-example.ai/v1/chat/completions": dial tcp: i/o timeout`), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	got := UserFacingRelayError(err).ToOpenAIError().Message
	assert.Contains(t, got, "i/o timeout")
	assert.NotContains(t, got, "api.relay-example.ai")
}

func TestUserFacingTaskError(t *testing.T) {
	for _, status := range []int{400, 404, 429, 500, 502} {
		taskErr := &taskdto.TaskError{Message: "分辨率仅支持 1K、2K，来自 api.relay-example.ai", StatusCode: status}
		UserFacingTaskError(taskErr)
		assert.Equal(t, "分辨率仅支持 1K、2K，来自 ***.***.ai", taskErr.Message)
		assert.Equal(t, status, taskErr.StatusCode)
	}
	local := &taskdto.TaskError{Message: "duration must be between 1 and 15 seconds", StatusCode: 400, LocalError: true}
	UserFacingTaskError(local)
	assert.Equal(t, "duration must be between 1 and 15 seconds", local.Message)
	UserFacingTaskError(nil)
}

// 任务预扣费时用户余额不足：用户要看到余额提示，不能被当成上游 403 换成「上游服务暂时不可用」。
func TestTaskPreConsumeErrorKeepsBalanceMessage(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("预扣费额度失败, 用户剩余额度: ¥1.369062, 需要预扣费额度: ¥4.207500"),
		types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())

	taskErr := TaskErrorFromAPIError(apiErr)
	require.NotNil(t, taskErr)
	assert.True(t, taskErr.LocalError)

	UserFacingTaskError(taskErr)
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.Equal(t, "预扣费额度失败, 用户剩余额度: ¥1.369062, 需要预扣费额度: ¥4.207500", taskErr.Message)
}
