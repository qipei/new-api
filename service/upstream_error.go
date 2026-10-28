// CUSTOM: 上游错误对用户隐藏原文（fork 扩展）。
//
// 上游（尤其第三方中转）的错误原文常带着自己的域名、备用域名、接口清单或账户余额提示。
// MaskSensitiveInfo 只能换掉域名，措辞和接口说明仍能认出是哪家上游。按上游原始状态码分类：
//   - 渠道侧问题（鉴权、路径、余额、上游限流、上游故障、连不上上游）用户改不了，原文对他们
//     没用，统一换成提示；
//   - 其余（400/413/422 等参数类、内容审核类）是用户能改正的，保留打码后的原文。
//
// 被隐藏的原文完整留在错误日志的 admin_info.upstream_error 里，只有管理员能看到。
package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// isChannelSideUpstreamStatus 上游返回这些状态码时是渠道或上游自身的问题：
// 401/403 渠道密钥无效或无权限，402 上游余额不足，404/405 渠道地址或路径配错，
// 407/408 代理或超时，429 上游限流，5xx 上游故障。
func isChannelSideUpstreamStatus(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound,
		http.StatusMethodNotAllowed, http.StatusProxyAuthRequired, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return status >= http.StatusInternalServerError
}

func channelSideUpstreamMessage(status int) string {
	if status == http.StatusTooManyRequests {
		return "当前分组上游负载已饱和，请稍后再试"
	}
	return fmt.Sprintf("上游服务暂时不可用（上游状态码 %d），请稍后重试或联系客服", status)
}

// UpstreamErrorUserMessage 渠道侧上游错误给用户看的提示；用户能自己改正的错误返回空串，
// 调用方保留（打码后的）原文。
func UpstreamErrorUserMessage(err *types.NewAPIError) string {
	if err == nil {
		return ""
	}
	if err.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		return "上游服务连接失败，请稍后重试或联系客服"
	}
	if status := err.UpstreamStatusCode(); status != 0 && isChannelSideUpstreamStatus(status) {
		return channelSideUpstreamMessage(status)
	}
	return ""
}

// UserFacingRelayError 返回给用户的错误：渠道侧上游错误换成提示，状态码不变；其余原样返回
// （输出时仍会经过 MaskSensitiveInfo 打码）。
func UserFacingRelayError(err *types.NewAPIError) *types.NewAPIError {
	message := UpstreamErrorUserMessage(err)
	if message == "" {
		return err
	}
	code := types.ErrorCodeBadResponseStatusCode
	if err.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		code = types.ErrorCodeDoRequestFailed
	}
	return types.NewErrorWithStatusCode(fmt.Errorf("%s", message), code, err.StatusCode, types.ErrOptionWithSkipRetry())
}

// UserFacingTaskError 改写返回给用户的任务提交错误。本地校验错误原样返回；上游错误中渠道侧
// 问题换成提示，其余打码。
func UserFacingTaskError(taskErr *taskdto.TaskError) {
	if taskErr == nil || taskErr.LocalError {
		return
	}
	if isChannelSideUpstreamStatus(taskErr.StatusCode) {
		taskErr.Message = channelSideUpstreamMessage(taskErr.StatusCode)
		return
	}
	taskErr.Message = common.MaskSensitiveInfo(taskErr.Message)
}
