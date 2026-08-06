// CUSTOM: 视频价格矩阵的按 token 完成结算（fork 扩展）。
package taskcommon

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/video_billing"
)

// VideoMatrixQuotaOnComplete 按提交时快照的矩阵档价与分组倍率，
// 用实际 token 用量计算最终额度；不适用（非矩阵 token 计费、无 usage）时返回 0，
// 由调用方回退到默认结算逻辑。
//
// 提交期 resolveTaskPriceData 已把命中的档价（USD/百万 token）写入
// PriceData.ModelPrice 并随 BillingContext 落库，因此矩阵中途改价不影响在途任务。
func VideoMatrixQuotaOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	if task == nil || taskResult == nil {
		return 0
	}
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.PerCallBilling || bc.ModelPrice <= 0 {
		return 0
	}

	modelName := bc.OriginModelName
	if modelName == "" {
		modelName = task.Properties.OriginModelName
	}
	table, ok := video_billing.GetPriceTable(modelName)
	if !ok || table.Unit != video_billing.UnitPerMillionToken {
		return 0
	}

	tokens := taskResult.TotalTokens
	if tokens <= 0 {
		tokens = taskResult.CompletionTokens
	}
	if tokens <= 0 {
		return 0
	}

	groupRatio := bc.GroupRatio
	if groupRatio <= 0 {
		return 0
	}

	return common.QuotaFromFloat(float64(tokens) / 1_000_000 * bc.ModelPrice * common.QuotaPerUnit * groupRatio)
}
