// CUSTOM: 模型广场的预置音色目录（fork 扩展），设计说明见 setting/voice_catalog。
package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/voice_catalog"

	"github.com/gin-gonic/gin"
)

// GetModelVoices 返回模型可用的预置音色，与模型广场同样的访问权限。
func GetModelVoices(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	catalog, ok, err := service.GetModelVoiceCatalog(modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "model has no voice catalog"})
		return
	}
	common.ApiSuccess(c, catalog)
}

// GetVoiceCatalogSettings 返回音色同步配置与最近一次同步结果。密钥只返回是否已配置。
func GetVoiceCatalogSettings(c *gin.Context) {
	settings := voice_catalog.GetSettings()
	data := gin.H{
		"volc_access_key_id_configured":     strings.TrimSpace(settings.VolcAccessKeyID) != "",
		"volc_secret_access_key_configured": strings.TrimSpace(settings.VolcSecretAccessKey) != "",
		"volc_resource_ids":                 settings.ResourceIDs(),
		"sync_interval_hours":               settings.SyncIntervalHoursOrDefault(),
	}
	tasks, err := model.GetLatestSystemTasks([]string{model.SystemTaskTypeVoiceCatalogSync})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task := tasks[model.SystemTaskTypeVoiceCatalogSync]; task != nil {
		data["last_sync"] = task.ToResponse()
	}
	common.ApiSuccess(c, data)
}

type voiceCatalogSettingsRequest struct {
	VolcAccessKeyID     string   `json:"volc_access_key_id"`
	VolcSecretAccessKey string   `json:"volc_secret_access_key"`
	VolcResourceIDs     []string `json:"volc_resource_ids"`
	SyncIntervalHours   int      `json:"sync_interval_hours"`
	// ClearVolcCredentials 清空已保存的 AK/SK（留空的密钥字段表示不修改）。
	ClearVolcCredentials bool `json:"clear_volc_credentials"`
}

func SaveVoiceCatalogSettings(c *gin.Context) {
	var req voiceCatalogSettingsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	if req.SyncIntervalHours < 1 || req.SyncIntervalHours > 24*7 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "sync_interval_hours must be between 1 and 168"})
		return
	}
	resourceIDs := make([]string, 0, len(req.VolcResourceIDs))
	for _, id := range req.VolcResourceIDs {
		if id = strings.TrimSpace(id); id != "" {
			resourceIDs = append(resourceIDs, id)
		}
	}
	if len(resourceIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "volc_resource_ids is required"})
		return
	}
	encodedIDs, err := common.Marshal(resourceIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	updates := map[string]string{
		"voice_catalog.volc_resource_ids":   string(encodedIDs),
		"voice_catalog.sync_interval_hours": strconv.Itoa(req.SyncIntervalHours),
	}
	if req.ClearVolcCredentials {
		updates["voice_catalog.volc_access_key_id"] = ""
		updates["voice_catalog.volc_secret_access_key"] = ""
	} else {
		if key := strings.TrimSpace(req.VolcAccessKeyID); key != "" {
			updates["voice_catalog.volc_access_key_id"] = key
		}
		if secret := strings.TrimSpace(req.VolcSecretAccessKey); secret != "" {
			updates["voice_catalog.volc_secret_access_key"] = secret
		}
	}
	if err := model.UpdateOptionsBulk(updates); err != nil {
		common.ApiError(c, err)
		return
	}
	GetVoiceCatalogSettings(c)
}

// SyncVoiceCatalog 立即触发一次火山音色同步，走系统任务队列执行。
func SyncVoiceCatalog(c *gin.Context) {
	if !voice_catalog.GetSettings().VolcConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "volcengine access key is not configured"})
		return
	}
	task, _, err := service.EnqueueSystemTask(model.SystemTaskTypeVoiceCatalogSync, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, task.ToResponse())
}
