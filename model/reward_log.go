package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// RewardLogOutbox lives in the primary database. Enqueue it in the same
// transaction as the balance credit; delivery to a separate log DB is retriable.
type RewardLogOutbox struct {
	ID              int    `gorm:"primaryKey"`
	EventKey        string `gorm:"type:varchar(128);not null;uniqueIndex"`
	Payload         string `gorm:"type:text;not null"`
	CreatedAt       int64  `gorm:"bigint"`
	DeliveredAt     int64  `gorm:"bigint;index"`
	DeliveringUntil int64  `gorm:"bigint"`
}

// RewardLogDelivery lives in the relational log database and commits together
// with the log entry, preventing duplicates after an outbox acknowledgement fails.
type RewardLogDelivery struct {
	EventKey  string `gorm:"type:varchar(128);primaryKey"`
	CreatedAt int64  `gorm:"bigint"`
}

func EnqueueRewardLog(tx *gorm.DB, entry *Log, eventKey string) error {
	if entry == nil || eventKey == "" || len(eventKey) > 128 || (entry.Type != LogTypeCheckin && entry.Type != LogTypeRewardedAd) || entry.Quota < 0 {
		return errors.New("invalid reward log")
	}
	entry.RequestId = eventKey
	payload, err := common.Marshal(entry)
	if err != nil {
		return err
	}
	return tx.Create(&RewardLogOutbox{EventKey: eventKey, Payload: string(payload), CreatedAt: common.GetTimestamp()}).Error
}

func DeliverRewardLogs() error {
	var pending []RewardLogOutbox
	if err := DB.Where("delivered_at = ? AND delivering_until < ?", 0, common.GetTimestamp()).Order("id").Limit(100).Find(&pending).Error; err != nil {
		return err
	}
	// One undeliverable event must not block the events queued behind it: record
	// the failure, release its lease for the next round and keep going.
	var failures []error
	for _, event := range pending {
		claim := DB.Model(&RewardLogOutbox{}).Where("id = ? AND delivered_at = ? AND delivering_until < ?", event.ID, 0, common.GetTimestamp()).Update("delivering_until", common.GetTimestamp()+300)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			continue
		}
		var entry Log
		if err := common.UnmarshalJsonStr(event.Payload, &entry); err != nil {
			_ = DB.Model(&RewardLogOutbox{}).Where("id = ?", event.ID).Update("delivering_until", 0).Error
			failures = append(failures, fmt.Errorf("decode reward log %s: %w", event.EventKey, err))
			continue
		}
		var err error
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			// The lease serializes normal workers and a stable key detects acknowledged
			// inserts. ClickHouse insert timeout/visibility delay can still duplicate a
			// display log; the primary reward ledger remains the accounting authority.
			var count int64
			err = LOG_DB.Model(&Log{}).Where("request_id = ?", event.EventKey).Count(&count).Error
			if err == nil && count == 0 {
				entry.Id = 0
				err = LOG_DB.Create(&entry).Error
			}
		} else {
			err = LOG_DB.Transaction(func(tx *gorm.DB) error {
				var receipt RewardLogDelivery
				err := tx.Where("event_key = ?", event.EventKey).First(&receipt).Error
				if err == nil {
					return nil
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				// A plain unique insert arbitrates competing deliveries. Do not use
				// an upsert's RowsAffected: MySQL clientFoundRows may report a no-op
				// duplicate as one affected row. A conflict rolls back this transaction;
				// the next retry observes the committed receipt and skips the log.
				receipt = RewardLogDelivery{EventKey: event.EventKey, CreatedAt: common.GetTimestamp()}
				if err := tx.Create(&receipt).Error; err != nil {
					return err
				}
				entry.Id = 0
				return tx.Create(&entry).Error
			})
		}
		if err != nil {
			_ = DB.Model(&RewardLogOutbox{}).Where("id = ?", event.ID).Update("delivering_until", 0).Error
			failures = append(failures, fmt.Errorf("deliver reward log %s: %w", event.EventKey, err))
			continue
		}
		// A lost acknowledgement is retried after the lease; the receipt prevents a duplicate log.
		if err := DB.Model(&RewardLogOutbox{}).Where("id = ? AND delivered_at = ?", event.ID, 0).Updates(map[string]interface{}{"delivered_at": common.GetTimestamp(), "delivering_until": 0}).Error; err != nil {
			failures = append(failures, fmt.Errorf("acknowledge reward log %s: %w", event.EventKey, err))
		}
	}
	return errors.Join(failures...)
}

// rewardLogRetentionSeconds keeps delivered outbox rows and delivery receipts long
// enough to absorb delayed retries, then lets them be pruned so neither grows forever.
const rewardLogRetentionSeconds = 7 * 24 * 3600

func PruneDeliveredRewardLogs(now int64) error {
	cutoff := now - rewardLogRetentionSeconds
	if err := DB.Where("delivered_at > 0 AND delivered_at < ?", cutoff).Delete(&RewardLogOutbox{}).Error; err != nil {
		return err
	}
	// ClickHouse deduplicates by request_id and keeps no receipt table.
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return nil
	}
	// A committed receipt may still have an unacknowledged primary outbox row.
	// Keep it until that row is delivered; otherwise recovery inserts a second log.
	cursor := ""
	for {
		var receipts []RewardLogDelivery
		if err := LOG_DB.Where("created_at < ? AND event_key > ?", cutoff, cursor).Order("event_key").Limit(500).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) == 0 {
			return nil
		}
		keys := make([]string, 0, len(receipts))
		for _, receipt := range receipts {
			keys = append(keys, receipt.EventKey)
		}
		var pending []string
		if err := DB.Model(&RewardLogOutbox{}).Where("event_key IN ? AND delivered_at = ?", keys, 0).Pluck("event_key", &pending).Error; err != nil {
			return err
		}
		pendingKeys := make(map[string]bool, len(pending))
		for _, key := range pending {
			pendingKeys[key] = true
		}
		deletable := make([]string, 0, len(keys))
		for _, key := range keys {
			if !pendingKeys[key] {
				deletable = append(deletable, key)
			}
		}
		if len(deletable) > 0 {
			if err := LOG_DB.Where("event_key IN ? AND created_at < ?", deletable, cutoff).Delete(&RewardLogDelivery{}).Error; err != nil {
				return err
			}
		}
		cursor = receipts[len(receipts)-1].EventKey
	}
}
