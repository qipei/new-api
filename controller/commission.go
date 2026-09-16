package controller

import (
	"strconv"
	"strings"

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

// CommissionOverrideRequest 是新增或更新推广人专属返佣参数的请求体。
type CommissionOverrideRequest struct {
	UserId          int     `json:"user_id"`
	Username        string  `json:"username"`
	Type            string  `json:"type"`
	Value           float64 `json:"value"`
	TopupCountLimit int     `json:"topup_count_limit"`
	Remark          string  `json:"remark"`
}

// GetCommissionOverrides 管理员分页查看全部专属返佣参数。
func GetCommissionOverrides(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	overrides, total, err := model.GetUserCommissionOverrides(keyword, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(overrides)
	common.ApiSuccess(c, pageInfo)
}

// SaveCommissionOverride 新增或更新一个推广人的专属返佣参数。
//
// 允许按用户名指定对象：管理员在界面上认的是用户名而不是内部 id，只给 id
// 会逼着人先去用户列表查一次。
func SaveCommissionOverride(c *gin.Context) {
	var req CommissionOverrideRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	userId := req.UserId
	if userId <= 0 {
		username := strings.TrimSpace(req.Username)
		if username == "" {
			common.ApiErrorMsg(c, "请指定用户")
			return
		}
		id, err := model.GetUserIdByUsername(username)
		if err != nil || id <= 0 {
			common.ApiErrorMsg(c, "用户不存在")
			return
		}
		userId = id
	}

	override := &model.UserCommissionOverride{
		UserId:          userId,
		Type:            req.Type,
		Value:           req.Value,
		TopupCountLimit: req.TopupCountLimit,
		Remark:          req.Remark,
	}
	if err := model.SaveUserCommissionOverride(override); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, override)
}

// DeleteCommissionOverride 删除一条专属返佣参数，该推广人回到全局参数。
func DeleteCommissionOverride(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.DeleteUserCommissionOverride(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
