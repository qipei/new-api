package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

// UserCommissionOverride 是针对单个推广人的返佣参数覆盖。
//
// 按「推广人」而非「被推广人」匹配：返佣金额发给推广人，给特定推广人配更高比例
// 是这个功能的实际用途。判定时看的是充值用户的邀请人是谁。
//
// 只覆盖类型、数值与笔数限制三项。全局的启用开关仍是总闸，全局关闭时所有人都
// 不返佣，专属设置也不生效——保留一键全停的能力。
type UserCommissionOverride struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"uniqueIndex"`

	Type            string  `json:"type" gorm:"type:varchar(16)"`
	Value           float64 `json:"value"`
	TopupCountLimit int     `json:"topup_count_limit"`

	Remark      string `json:"remark" gorm:"type:varchar(255)"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

// UserCommissionOverrideView 附带用户名，供管理端列表直接展示。
type UserCommissionOverrideView struct {
	UserCommissionOverride
	Username string `json:"username" gorm:"-"`
}

var ErrCommissionOverrideUserNotFound = errors.New("用户不存在")

// normalizeCommissionOverride 归一化并校验一条覆盖记录。
//
// 归一化规则与 operation_setting.GetCommissionSetting 保持一致，避免同一份参数
// 在全局与专属两条路径上表现不同。
func normalizeCommissionOverride(o *UserCommissionOverride) error {
	o.Type = strings.TrimSpace(o.Type)
	if o.Type != operation_setting.CommissionTypeFixed && o.Type != operation_setting.CommissionTypePercent {
		return errors.New("返佣类型无效")
	}
	if o.Value < 0 {
		return errors.New("返佣数值不能为负")
	}
	if o.Type == operation_setting.CommissionTypePercent && o.Value > 100 {
		return errors.New("返佣比例不能超过 100")
	}
	if o.TopupCountLimit < 0 {
		return errors.New("返佣笔数限制不能为负")
	}
	o.Remark = strings.TrimSpace(o.Remark)
	if len([]rune(o.Remark)) > 100 {
		return errors.New("备注过长")
	}
	return nil
}

// ResolveCommissionSetting 返回该推广人实际适用的返佣参数。
//
// 没有专属记录时原样返回全局参数。全局的 Enabled 与 ScanCursor 始终沿用，
// 专属记录不参与这两项。
func ResolveCommissionSetting(global operation_setting.CommissionSetting, inviterId int) operation_setting.CommissionSetting {
	if inviterId <= 0 {
		return global
	}
	var override UserCommissionOverride
	err := DB.Where("user_id = ?", inviterId).First(&override).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError("commission: load override failed: " + err.Error())
		}
		return global
	}

	resolved := global
	resolved.Type = override.Type
	resolved.Value = override.Value
	resolved.TopupCountLimit = override.TopupCountLimit
	// 专属记录入库时已归一化，这里再过一遍是为了兜住历史脏数据。
	if resolved.Type != operation_setting.CommissionTypeFixed && resolved.Type != operation_setting.CommissionTypePercent {
		return global
	}
	if resolved.Value < 0 {
		resolved.Value = 0
	}
	if resolved.Type == operation_setting.CommissionTypePercent && resolved.Value > 100 {
		resolved.Value = 100
	}
	if resolved.TopupCountLimit < 0 {
		resolved.TopupCountLimit = 0
	}
	return resolved
}

// SaveUserCommissionOverride 新增或更新一个推广人的专属参数。
//
// 按 user_id upsert：同一用户只允许一条记录，由唯一索引兜底。
func SaveUserCommissionOverride(o *UserCommissionOverride) error {
	if o.UserId <= 0 {
		return errors.New("未指定用户")
	}
	if err := normalizeCommissionOverride(o); err != nil {
		return err
	}

	var count int64
	if err := DB.Model(&User{}).Where("id = ?", o.UserId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrCommissionOverrideUserNotFound
	}

	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing UserCommissionOverride
		err := tx.Where("user_id = ?", o.UserId).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			o.CreatedTime = now
			o.UpdatedTime = now
			return tx.Create(o).Error
		}
		if err != nil {
			return err
		}
		o.Id = existing.Id
		o.CreatedTime = existing.CreatedTime
		o.UpdatedTime = now
		return tx.Model(&UserCommissionOverride{}).Where("id = ?", existing.Id).Updates(map[string]interface{}{
			"type":              o.Type,
			"value":             o.Value,
			"topup_count_limit": o.TopupCountLimit,
			"remark":            o.Remark,
			"updated_time":      now,
		}).Error
	})
}

// DeleteUserCommissionOverride 删除一条专属参数，该推广人随即回到全局参数。
func DeleteUserCommissionOverride(id int) error {
	if id <= 0 {
		return errors.New("参数错误")
	}
	result := DB.Where("id = ?", id).Delete(&UserCommissionOverride{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("记录不存在")
	}
	return nil
}

// GetUserCommissionOverrides 分页返回全部专属参数，按更新时间倒序。
//
// keyword 非空时按用户名模糊匹配，便于在推广人较多时定位。
func GetUserCommissionOverrides(keyword string, pageInfo *common.PageInfo) ([]*UserCommissionOverrideView, int64, error) {
	query := DB.Model(&UserCommissionOverride{})

	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		pattern, err := sanitizeLikePattern(keyword)
		if err != nil {
			return nil, 0, err
		}
		var userIds []int
		if err := DB.Model(&User{}).Where("username LIKE ? ESCAPE '!'", pattern).
			Limit(1000).Pluck("id", &userIds).Error; err != nil {
			return nil, 0, err
		}
		if len(userIds) == 0 {
			return []*UserCommissionOverrideView{}, 0, nil
		}
		query = query.Where("user_id IN ?", userIds)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var overrides []*UserCommissionOverride
	err := query.Order("updated_time desc, id desc").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&overrides).Error
	if err != nil {
		return nil, 0, err
	}

	userIds := make([]int, 0, len(overrides))
	seen := make(map[int]bool, len(overrides))
	for _, o := range overrides {
		if !seen[o.UserId] {
			seen[o.UserId] = true
			userIds = append(userIds, o.UserId)
		}
	}
	usernames := make(map[int]string, len(userIds))
	if len(userIds) > 0 {
		var users []*User
		if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
			return nil, 0, err
		}
		for _, u := range users {
			usernames[u.Id] = u.Username
		}
	}

	views := make([]*UserCommissionOverrideView, 0, len(overrides))
	for _, o := range overrides {
		views = append(views, &UserCommissionOverrideView{
			UserCommissionOverride: *o,
			Username:               usernames[o.UserId],
		})
	}
	return views, total, nil
}
