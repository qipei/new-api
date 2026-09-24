package model

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const ReferralRewardRegistration = "registration"

// ReferralRewardRecord holds non-topup referral earnings. Recharge commissions
// remain in commission_records; both sources share aff_quota and aff_history.
// SourceType + SourceID identify the originating event for idempotency.
// Future ad rewards must be credited only after server-side event verification.
type ReferralRewardRecord struct {
	ID            int64  `json:"id"`
	UserID        int    `json:"user_id" gorm:"index:idx_referral_reward_user_time,priority:1;uniqueIndex:idx_referral_reward_source,priority:1"`
	SourceType    string `json:"source_type" gorm:"type:varchar(32);uniqueIndex:idx_referral_reward_source,priority:2"`
	SourceID      string `json:"source_id" gorm:"type:varchar(128);uniqueIndex:idx_referral_reward_source,priority:3"`
	RelatedUserID int    `json:"related_user_id"`
	Quota         int64  `json:"quota" gorm:"type:bigint"`
	CreatedAt     int64  `json:"created_at" gorm:"index:idx_referral_reward_user_time,priority:2"`
}

// creditRegistrationReward runs in the account-creation transaction. A failed
// ledger insert or wallet update rolls back registration and its invitation count.
// Ineligible recipients are skipped before creating a ledger entry.
func creditRegistrationReward(tx *gorm.DB, inviterID, inviteeID, quota int) (bool, error) {
	if quota <= 0 || int64(quota) > common.MaxWalletQuota {
		common.SysError(fmt.Sprintf("registration referral reward skipped: inviter=%d invitee=%d invalid quota=%d", inviterID, inviteeID, quota))
		return false, nil
	}
	// Guard before addition, including the cumulative counter, to prevent overflow.
	// The conditional update also serializes competing credits without a prior read.
	result := tx.Model(&User{}).Where("id = ? AND status = ? AND aff_quota <= ? AND aff_history <= ?", inviterID, common.UserStatusEnabled,
		common.MaxWalletQuota-int64(quota), common.MaxWalletQuota-int64(quota)).Updates(map[string]interface{}{
		"aff_quota": gorm.Expr("aff_quota + ?", quota), "aff_history": gorm.Expr("aff_history + ?", quota),
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		common.SysError(fmt.Sprintf("registration referral reward skipped: inviter=%d invitee=%d quota=%d; inviter missing, deleted, disabled, or wallet limit exceeded", inviterID, inviteeID, quota))
		return false, nil
	}
	record := ReferralRewardRecord{UserID: inviterID, SourceType: ReferralRewardRegistration,
		SourceID: strconv.Itoa(inviteeID), RelatedUserID: inviteeID, Quota: int64(quota), CreatedAt: common.GetTimestamp()}
	if err := tx.Create(&record).Error; err != nil {
		return false, err
	}
	return true, nil
}

type ReferralRewardRow struct {
	ID            int64
	SourceType    string
	RelatedUserID int
	Quota         int64
	CreatedAt     int64
}

type ReferralRewardHistory struct {
	Rows       []ReferralRewardRow
	Total      int64
	Available  int64
	Lifetime   int64
	Unitemized int64
}

// GetReferralRewardHistory reads both ledgers in one snapshot. Historical signup
// rewards cannot be reconstructed from formatted system logs, so any legacy
// balance without a ledger is reported separately, never fabricated as an event.
func GetReferralRewardHistory(userID, page, size int) (*ReferralRewardHistory, error) {
	if userID <= 0 || page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, errors.New("invalid referral history query")
	}
	out := &ReferralRewardHistory{Rows: []ReferralRewardRow{}}
	// PostgreSQL's default READ COMMITTED would give each statement a different
	// snapshot. Use repeatable reads on both server databases; SQLite already
	// keeps its read snapshot for the transaction and needs no isolation override.
	opts := &sql.TxOptions{ReadOnly: true}
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		opts.Isolation = sql.LevelRepeatableRead
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id", "aff_quota", "aff_history").First(&user, userID).Error; err != nil {
			return err
		}
		out.Available, out.Lifetime = int64(user.AffQuota), int64(user.AffHistoryQuota)
		var recorded int64
		// UNION ALL and bound parameters work on SQLite, MySQL and PostgreSQL.
		union := `SELECT id, 'topup' AS source_type, invitee_id AS related_user_id, commission_quota AS quota, created_time AS created_at FROM commission_records WHERE inviter_id = ?
   UNION ALL SELECT id, source_type, related_user_id, quota, created_at FROM referral_reward_records WHERE user_id = ?`
		var aggregate struct {
			Total int64
			Quota int64
		}
		if err := tx.Raw("SELECT COUNT(*) AS total, COALESCE(SUM(quota), 0) AS quota FROM ("+union+") AS rewards", userID, userID).Scan(&aggregate).Error; err != nil {
			return err
		}
		out.Total, recorded = aggregate.Total, aggregate.Quota
		if recorded > out.Lifetime {
			common.SysError(fmt.Sprintf("referral reward ledger exceeds lifetime earnings: user=%d recorded=%d lifetime=%d", userID, recorded, out.Lifetime))
		} else {
			out.Unitemized = out.Lifetime - recorded
		}
		return tx.Raw("SELECT * FROM ("+union+") AS rewards ORDER BY created_at DESC, source_type ASC, id DESC LIMIT ? OFFSET ?", userID, userID, size, (page-1)*size).Scan(&out.Rows).Error
	}, opts)
	return out, err
}
