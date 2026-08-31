// CUSTOM: 视频价格矩阵的按 token 完成结算（fork 扩展）。
package taskcommon

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/video_billing"
)

// maxBillableImageCount 兜住上游返回的异常张数：它是计费乘数，
// 未经约束的大数会把一次结算放大到不可接受的额度。
const maxBillableImageCount = 100

func clampBillableImageCount(count int) int {
	if count <= 0 {
		return 0
	}
	if count > maxBillableImageCount {
		common.SysError(fmt.Sprintf("upstream reported %d billable input images, clamped to %d", count, maxBillableImageCount))
		return maxBillableImageCount
	}
	return count
}

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
	if bc == nil || bc.ModelPrice <= 0 {
		return 0
	}

	modelName := bc.OriginModelName
	if modelName == "" {
		modelName = task.Properties.OriginModelName
	}
	table, ok := video_billing.GetPriceTable(modelName)
	if !ok {
		return 0
	}
	if table.Unit == video_billing.UnitPerSecond {
		billingDuration := taskResult.BillingDuration
		if billingDuration <= 0 {
			billingDuration = float64(taskResult.Duration)
		}
		if !bc.SettleOnComplete || billingDuration <= 0 || bc.GroupRatio <= 0 {
			return 0
		}
		// 输入参考图是独立于输出秒数的一笔上游成本，只按秒计价会漏掉它。
		// 上游返回的张数已扣除免费额度，这里不再重复减免。
		inputImagePrice, _ := table.AdditiveInputPrices()
		imageCost := float64(clampBillableImageCount(taskResult.BillableImageCount)) * inputImagePrice
		quota, clamp := common.QuotaFromFloatChecked((billingDuration*bc.ModelPrice + imageCost) * common.QuotaPerUnit * bc.GroupRatio)
		taskResult.QuotaClamp = clamp
		return quota
	}
	if table.Unit != video_billing.UnitPerMillionToken || bc.PerCallBilling {
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

	quota, clamp := common.QuotaFromFloatChecked(float64(tokens) / 1_000_000 * bc.ModelPrice * common.QuotaPerUnit * groupRatio)
	taskResult.QuotaClamp = clamp
	return quota
}
