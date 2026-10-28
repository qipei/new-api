// CUSTOM: autodl ComfyUI 渠道手动刷新工作流定义（fork 扩展）。
//
// 适配器按工作流定义匹配分辨率、mode 的选项名，定义缓存一天。autodl 改了工作流参数时，
// 管理员可以在渠道列表里手动刷新，不用等缓存过期。缓存在每个节点各自的内存里，刷新只
// 作用于处理这次请求的节点，其余节点仍按一天的有效期自行更新。
package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/autodl"

	"github.com/gin-gonic/gin"
)

type workflowDefinitionRefreshResult struct {
	Model      string `json:"model"`
	WorkflowID string `json:"workflow_id"`
	autodl.WorkflowDefinition
	Error string `json:"error,omitempty"`
}

// RefreshChannelWorkflowDefinitions 重新读取 autodl 渠道下每个模型映射到的工作流定义。
func RefreshChannelWorkflowDefinitions(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Type != constant.ChannelTypeAutoDLComfyUI {
		common.ApiError(c, fmt.Errorf("channel %d is not an AutoDL ComfyUI channel", channelId))
		return
	}

	// 模型映射到 workflow_id；没映射的模型名本身就是 workflow_id。
	mapping := map[string]string{}
	if raw := strings.TrimSpace(channel.GetModelMapping()); raw != "" && raw != "{}" {
		if err := common.UnmarshalJsonStr(raw, &mapping); err != nil {
			common.ApiError(c, fmt.Errorf("invalid model mapping: %w", err))
			return
		}
	}
	baseURL := channel.GetBaseURL()
	results := make([]workflowDefinitionRefreshResult, 0)
	failed := 0
	for _, modelName := range channel.GetModels() {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		workflowID := strings.TrimSpace(mapping[modelName])
		if workflowID == "" {
			workflowID = modelName
		}
		result := workflowDefinitionRefreshResult{Model: modelName, WorkflowID: workflowID}
		definition, err := autodl.RefreshWorkflowDefinition(baseURL, workflowID)
		if err != nil {
			failed++
			result.Error = common.MaskSensitiveInfo(err.Error())
		}
		result.WorkflowDefinition = definition
		results = append(results, result)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": failed == 0,
		"message": fmt.Sprintf("refreshed %d workflow definitions, %d failed", len(results)-failed, failed),
		"data":    results,
	})
}
