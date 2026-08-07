package model

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
