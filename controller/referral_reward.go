package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func GetReferralRewards(c *gin.Context) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || sizeErr != nil || page < 1 || page > 100000 || size < 1 || size > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分页参数无效"})
		return
	}
	result, err := service.GetReferralRewards(c.GetInt("id"), page, size)
	if err != nil {
		common.SysError("referral rewards: " + err.Error())
		common.ApiErrorMsg(c, "推广收益暂不可用，请稍后重试")
		return
	}
	common.ApiSuccess(c, result)
}
