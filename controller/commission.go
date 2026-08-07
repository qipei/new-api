package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetUserCommissionRecords 当前用户作为邀请人的佣金明细(被邀请人用户名脱敏)
func GetUserCommissionRecords(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)

	records, total, err := model.GetInviterCommissionRecords(userId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, r := range records {
		r.InviteeUsername = maskUsername(r.InviteeUsername)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

// maskUsername 用户名脱敏: 保留首(尾)字符, 中间打码
func maskUsername(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "***"
	}
	if len(runes) <= 2 {
		return string(runes[:1]) + "***"
	}
	return string(runes[:1]) + "***" + string(runes[len(runes)-1:])
}
