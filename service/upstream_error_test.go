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

// 实际遇到的第三方中转 404：原文里带着它的主域名、备用域名和接口清单。
const leakyUpstreamBody = `{"error":{"message":"Invalid URL (POST /v1/text/chatcompletion_v2). 请确认 base_url 为 https://api.relay-example.ai（主域名）或 https://backup.relay-example.ai（备用域名），可调 GET /v1/skills 查看所有可用接口。","type":"invalid_request_error"}}`

func upstreamResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

// 渠道侧问题（鉴权、路径、余额、限流、上游故障、连不上）给用户统一提示；用户能改正的错误
// 保留原文；不是上游返回的错误不受影响。
func TestUpstreamErrorUserMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *types.NewAPIError
		want string
	}{
		{"not found", RelayErrorHandler(context.Background(), upstreamResponse(404, leakyUpstreamBody), false), "上游服务暂时不可用（上游状态码 404），请稍后重试或联系客服"},
		{"unauthorized", RelayErrorHandler(context.Background(), upstreamResponse(401, `{"error":{"message":"invalid api key"}}`), false), "上游服务暂时不可用（上游状态码 401），请稍后重试或联系客服"},
		{"insufficient balance", RelayErrorHandler(context.Background(), upstreamResponse(402, `{"message":"余额不足"}`), false), "上游服务暂时不可用（上游状态码 402），请稍后重试或联系客服"},
		{"rate limited", RelayErrorHandler(context.Background(), upstreamResponse(429, `{"error":{"message":"too many requests"}}`), false), "当前分组上游负载已饱和，请稍后再试"},
		{"bad gateway html", RelayErrorHandler(context.Background(), upstreamResponse(502, `<html>nginx</html>`), false), "上游服务暂时不可用（上游状态码 502），请稍后重试或联系客服"},
		{"network error", types.NewOpenAIError(errors.New(`Post "https://api.relay-example.ai/v1/chat/completions": dial tcp: i/o timeout`), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError), "上游服务连接失败，请稍后重试或联系客服"},
		{"bad request keeps original", RelayErrorHandler(context.Background(), upstreamResponse(400, `{"error":{"message":"max_tokens is too large"}}`), false), ""},
		{"unprocessable keeps original", RelayErrorHandler(context.Background(), upstreamResponse(422, `{"error":{"message":"content policy violation"}}`), false), ""},
		{"local error untouched", types.NewErrorWithStatusCode(errors.New("model price not configured"), types.ErrorCodeModelPriceError, http.StatusInternalServerError), ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UpstreamErrorUserMessage(tt.err))
		})
	}
}

// 返回给用户的错误里不能再出现上游原文，但状态码保持不变（包括渠道状态码映射后的值）；
// 分类依据的是上游原始状态码，不受映射影响。
func TestUserFacingRelayErrorHidesUpstreamText(t *testing.T) {
	upstreamErr := RelayErrorHandler(context.Background(), upstreamResponse(404, leakyUpstreamBody), false)
	require.Equal(t, http.StatusNotFound, upstreamErr.UpstreamStatusCode())
	ResetStatusCode(upstreamErr, `{"404":"500"}`)
	require.Equal(t, http.StatusInternalServerError, upstreamErr.StatusCode)

	userErr := UserFacingRelayError(upstreamErr)

	assert.Equal(t, http.StatusInternalServerError, userErr.StatusCode)
	for _, message := range []string{userErr.ToOpenAIError().Message, userErr.ToClaudeError().Message} {
		assert.Equal(t, "上游服务暂时不可用（上游状态码 404），请稍后重试或联系客服", message)
		assert.NotContains(t, message, "skills")
		assert.NotContains(t, message, ".ai")
	}

	clientErr := RelayErrorHandler(context.Background(), upstreamResponse(400, `{"error":{"message":"max_tokens is too large"}}`), false)
	assert.Same(t, clientErr, UserFacingRelayError(clientErr))
}

// 任务提交错误：本地校验错误原样返回；上游错误中渠道侧问题给提示，其余打码。
func TestUserFacingTaskError(t *testing.T) {
	local := &taskdto.TaskError{Message: "duration must be between 1 and 15 seconds", StatusCode: 400, LocalError: true}
	UserFacingTaskError(local)
	assert.Equal(t, "duration must be between 1 and 15 seconds", local.Message)

	notFound := &taskdto.TaskError{Message: leakyUpstreamBody, StatusCode: 404}
	UserFacingTaskError(notFound)
	assert.Equal(t, "上游服务暂时不可用（上游状态码 404），请稍后重试或联系客服", notFound.Message)

	badRequest := &taskdto.TaskError{Message: "prompt rejected by api.relay-example.ai moderation", StatusCode: 400}
	UserFacingTaskError(badRequest)
	assert.Equal(t, "prompt rejected by ***.***.ai moderation", badRequest.Message)
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
