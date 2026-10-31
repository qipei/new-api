// CUSTOM: 上游具体错误保留给用户，仅沿用敏感信息脱敏（fork 扩展）。
package service

import (
	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// UserFacingRelayError 保留错误详情、状态码和错误码，仅脱敏消息中的域名等敏感信息。
func UserFacingRelayError(err *types.NewAPIError) *types.NewAPIError {
	if err != nil {
		err.SetMessage(err.MaskSensitiveError())
	}
	return err
}

// UserFacingTaskError 保留任务提交的具体错误；本地错误不变，上游错误仅脱敏。
func UserFacingTaskError(taskErr *taskdto.TaskError) {
	if taskErr == nil || taskErr.LocalError {
		return
	}
	taskErr.Message = common.MaskSensitiveInfo(taskErr.Message)
}
