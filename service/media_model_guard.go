// CUSTOM: 媒体模型误走对话接口时在本地拒绝（fork 扩展）。
//
// 配了价格矩阵的模型都是视频、图片或语音模型，没有对话能力。经对话类接口调用时，渠道会
// 把请求拼成对话地址发给上游（例如 MiniMax 渠道拼成 /v1/text/chatcompletion_v2），上游只能
// 返回 404 之类的错误，还会触发重试和渠道错误统计。线上数据里这类调用全部失败，没有一次
// 成功扣费，在本地直接返回 400 并指明正确的接口。
package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"
)

// MediaModelOnChatEndpointError 模型配了价格矩阵（视频、图片、语音）时返回 400 错误，
// 告诉调用方该用哪个接口；其余模型返回 nil。
func MediaModelOnChatEndpointError(modelName string) *types.NewAPIError {
	table, ok := video_billing.GetPriceTable(modelName)
	if !ok {
		return nil
	}
	kind, endpoint := "视频", "POST /v1/video/generations"
	switch {
	case table.Unit == video_billing.UnitPerImage:
		kind, endpoint = "图片", "POST /v1/images/generations"
	case table.Unit == video_billing.UnitPer10kChars || common.IsAudioSpeechModel(modelName):
		kind, endpoint = "语音", "POST /v1/audio/speech 或 POST /v1/audio/speech/tasks"
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("模型 %s 是%s模型，不能通过对话接口调用，请改用 %s（具体参数见模型广场里该模型的接口说明）", modelName, kind, endpoint),
		types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}
