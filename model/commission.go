package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// CommissionRecord 推广返佣流水: 被邀请人一笔成功付费订单对应至多一条返佣记录。
// topup_id 唯一索引是幂等兜底: 无论扫描重复多少次, 同一订单只可能发一次佣金。
type CommissionRecord struct {
	Id              int     `json:"id"`
	TopUpId         int     `json:"topup_id" gorm:"column:topup_id;uniqueIndex"`
	InviterId       int     `json:"inviter_id" gorm:"index"`
	InviteeId       int     `json:"invitee_id" gorm:"index"`
	TopUpMoney      float64 `json:"topup_money"`      // 订单实付金额快照(审计)
	CreditedQuota   int     `json:"credited_quota"`   // 订单到账额度快照(percent 基数)
	CommissionQuota int     `json:"commission_quota"` // 实际发放佣金额度
	CommissionType  string  `json:"commission_type" gorm:"type:varchar(20)"`
	CommissionValue float64 `json:"commission_value"`
	CreatedTime     int64   `json:"created_time" gorm:"index"`
}

func (CommissionRecord) TableName() string {
	return "commission_records"
}

// creditedTopUpQuota 计算订单的到账额度, 镜像 ManualCompleteTopUp 的换算规则
// (model/topup.go)。合并上游时若该规则变化(如新增支付渠道分支), 需同步此处。
func creditedTopUpQuota(topUp *TopUp) int {
	var quota int
	var clamp *common.QuotaClamp
	switch topUp.PaymentMethod {
	case PaymentMethodStripe:
		quota, clamp = common.QuotaFromFloatChecked(topUp.Money * common.QuotaPerUnit)
	case PaymentMethodCreem:
		quota, clamp = common.QuotaFromFloatChecked(float64(topUp.Amount))
	default:
		quota, clamp = common.QuotaFromFloatChecked(float64(topUp.Amount) * common.QuotaPerUnit)
	}
	if clamp != nil {
		common.SysError(fmt.Sprintf("commission: credited quota clamped for topup %d: %s", topUp.Id, clamp.Error()))
	}
	return quota
}

// ProcessCommissionForTopUp 对单笔成功付费订单执行返佣判定与发放。
// 幂等: 已存在 commission_records 记录的订单直接跳过, topup_id 唯一索引兜底。
func ProcessCommissionForTopUp(topUp *TopUp, setting operation_setting.CommissionSetting) error {
	if topUp.Status != common.TopUpStatusSuccess {
		return nil
	}
	// balance 是余额内部消费; Amount=0 是订阅账单条目(见 upsertSubscriptionTopUpTx), 均不产生新的付费入账
	if topUp.PaymentMethod == PaymentMethodBalance || topUp.Amount <= 0 {
		return nil
	}

	invitee, err := GetUserById(topUp.UserId, false)
	if err != nil {
		return err
	}
	if invitee.InviterId == 0 || invitee.InviterId == invitee.Id {
		return nil
	}

	var existing int64
	if err := DB.Model(&CommissionRecord{}).Where("topup_id = ?", topUp.Id).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	if setting.TopupCountLimit > 0 {
		var seq int64
		err := DB.Model(&TopUp{}).
			Where("user_id = ? AND status = ? AND amount > 0 AND payment_method <> ? AND id <= ?",
				topUp.UserId, common.TopUpStatusSuccess, PaymentMethodBalance, topUp.Id).
			Count(&seq).Error
		if err != nil {
			return err
		}
		if seq > int64(setting.TopupCountLimit) {
			return nil
		}
	}

	credited := creditedTopUpQuota(topUp)
	var commission int
	var clamp *common.QuotaClamp
	if setting.Type == operation_setting.CommissionTypeFixed {
		commission, clamp = common.QuotaFromFloatChecked(setting.Value)
	} else {
		commission, clamp = common.QuotaFromFloatChecked(float64(credited) * setting.Value / 100.0)
	}
	if clamp != nil {
		common.SysError(fmt.Sprintf("commission: quota clamped for topup %d: %s", topUp.Id, clamp.Error()))
	}
	if commission <= 0 {
		return nil
	}

	record := &CommissionRecord{
		TopUpId:         topUp.Id,
		InviterId:       invitee.InviterId,
		InviteeId:       invitee.Id,
		TopUpMoney:      topUp.Money,
		CreditedQuota:   credited,
		CommissionQuota: commission,
		CommissionType:  setting.Type,
		CommissionValue: setting.Value,
		CreatedTime:     common.GetTimestamp(),
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", record.InviterId).Updates(map[string]interface{}{
			"aff_quota":   gorm.Expr("aff_quota + ?", record.CommissionQuota),
			"aff_history": gorm.Expr("aff_history + ?", record.CommissionQuota),
		}).Error
	})
	if err != nil {
		return err
	}

	RecordLog(record.InviterId, LogTypeSystem, fmt.Sprintf("邀请用户充值返佣 %s(订单到账 %s)",
		logger.LogQuota(record.CommissionQuota), logger.LogQuota(record.CreditedQuota)))
	return nil
}
