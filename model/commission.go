package model

import (
	"fmt"
	"strconv"

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

const (
	commissionScanOverlapSeconds = int64(600) // 回看窗口: 容忍订单乱序完成/时钟偏移, 重复扫到靠唯一索引去重
	commissionScanBatchSize      = 200
)

// ScanAndProcessCommissions 供后台 worker 调用: 读取当前配置执行一轮扫描。
func ScanAndProcessCommissions() (int, error) {
	return scanAndProcessCommissions(operation_setting.GetCommissionSetting(), common.GetTimestamp())
}

func scanAndProcessCommissions(setting operation_setting.CommissionSetting, now int64) (int, error) {
	// 游标未初始化(首次部署): 从当前时间开始, 历史订单不补发。
	// 功能关闭时游标照常推进, 避免重新开启后回溯补发关闭期间的订单。
	if setting.ScanCursor == 0 || !setting.Enabled {
		return 0, saveCommissionScanCursor(now)
	}

	since := setting.ScanCursor - commissionScanOverlapSeconds
	lastId := 0
	processed := 0
	for {
		var topUps []*TopUp
		err := DB.Where(
			"status = ? AND id > ? AND (complete_time >= ? OR (complete_time = 0 AND create_time >= ?))",
			common.TopUpStatusSuccess, lastId, since, since,
		).Order("id asc").Limit(commissionScanBatchSize).Find(&topUps).Error
		if err != nil {
			// 查询失败不推进游标, 下一轮从原水位重试
			return processed, err
		}
		if len(topUps) == 0 {
			break
		}
		for _, topUp := range topUps {
			lastId = topUp.Id
			if err := ProcessCommissionForTopUp(topUp, setting); err != nil {
				// 单笔失败只记日志不阻塞: 回看窗口内下一轮会重试, 超窗后可据日志人工处理
				common.SysError(fmt.Sprintf("commission: process topup %d failed: %s", topUp.Id, err.Error()))
			} else {
				processed++
			}
		}
	}
	return processed, saveCommissionScanCursor(now)
}

func saveCommissionScanCursor(ts int64) error {
	// 写 options 表并经 handleConfigUpdate 回填 commissionSetting.ScanCursor, 多节点经 SyncOptions 同步
	return UpdateOption("commission_setting.scan_cursor", strconv.FormatInt(ts, 10))
}

// CommissionRecordView 佣金流水 + 被邀请人用户名(供用户端明细展示)
type CommissionRecordView struct {
	CommissionRecord
	InviteeUsername string `json:"invitee_username" gorm:"-"`
}

// GetInviterCommissionRecords 分页查询某邀请人的佣金流水, 按 id 倒序
func GetInviterCommissionRecords(inviterId int, pageInfo *common.PageInfo) ([]*CommissionRecordView, int64, error) {
	var total int64
	if err := DB.Model(&CommissionRecord{}).Where("inviter_id = ?", inviterId).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []*CommissionRecord
	err := DB.Where("inviter_id = ?", inviterId).
		Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}

	inviteeIds := make([]int, 0, len(records))
	seen := make(map[int]bool)
	for _, r := range records {
		if !seen[r.InviteeId] {
			seen[r.InviteeId] = true
			inviteeIds = append(inviteeIds, r.InviteeId)
		}
	}
	usernames := make(map[int]string)
	if len(inviteeIds) > 0 {
		var users []*User
		if err := DB.Select("id", "username").Where("id IN ?", inviteeIds).Find(&users).Error; err != nil {
			return nil, 0, err
		}
		for _, u := range users {
			usernames[u.Id] = u.Username
		}
	}

	views := make([]*CommissionRecordView, 0, len(records))
	for _, r := range records {
		views = append(views, &CommissionRecordView{CommissionRecord: *r, InviteeUsername: usernames[r.InviteeId]})
	}
	return views, total, nil
}

// EnsureCommissionScanIndex 为扫描查询补建 (status, complete_time) 复合索引。
// topups 表由上游 TopUp 结构体定义, 不在其 gorm tag 上加索引以免产生上游 diff。
// 幂等, 失败仅告警(无索引时扫描退化为过滤全表, 功能仍正确)。
func EnsureCommissionScanIndex() {
	stmt := &gorm.Statement{DB: DB}
	if err := stmt.Parse(&TopUp{}); err != nil {
		common.SysError("commission: parse topup table name failed: " + err.Error())
		return
	}
	table := stmt.Schema.Table
	const indexName = "idx_topups_commission_scan"

	var err error
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		// MySQL 5.7 不支持 CREATE INDEX IF NOT EXISTS, 先查 information_schema
		var cnt int64
		DB.Raw("SELECT COUNT(1) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			table, indexName).Scan(&cnt)
		if cnt == 0 {
			// status 在 MySQL 下是 TEXT 列, 索引必须指定前缀长度(错误 1170)
			err = DB.Exec(fmt.Sprintf("CREATE INDEX %s ON %s (status(16), complete_time)", indexName, table)).Error
		}
	} else {
		// SQLite / PostgreSQL 均支持 IF NOT EXISTS
		err = DB.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (status, complete_time)", indexName, table)).Error
	}
	if err != nil {
		common.SysError("commission: create scan index failed: " + err.Error())
	}
}
