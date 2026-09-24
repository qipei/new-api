package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetUserUsageOverview(c *gin.Context) {
	r, err := service.ResolveUserUsageRange(c.Query("period"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "INVALID_USAGE_QUERY", "message": err.Error()})
		return
	}
	result, err := service.GetCachedUserUsageOverview(c.GetInt("id"), r)
	if err != nil {
		common.ApiErrorMsg(c, "用量统计暂不可用，请稍后重试")
		return
	}
	common.ApiSuccess(c, result)
}

func GetUserUsageRecords(c *gin.Context) {
	r, err := service.ResolveUserUsageRange(c.Query("period"), time.Now())
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	outcome := c.Query("outcome")
	modelName := c.Query("model_name")
	if err != nil || pageErr != nil || sizeErr != nil || page < 1 || page > 100000 || size < 1 || size > 100 || len(modelName) > 255 || (outcome != "" && outcome != "success" && outcome != "failed" && outcome != "unknown") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "INVALID_USAGE_QUERY", "message": "invalid period, page, page_size, model_name or outcome"})
		return
	}
	result, err := service.GetUserUsageRecords(c.GetInt("id"), r, page, size, modelName, outcome)
	if err != nil {
		common.ApiErrorMsg(c, "调用记录暂不可用，请稍后重试")
		return
	}
	common.ApiSuccess(c, result)
}
