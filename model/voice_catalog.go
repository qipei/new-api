package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// CUSTOM: 从厂商接口同步来的预置音色（fork 扩展），目前只有火山豆包语音。
// 每个 (provider, resource_id) 是一份完整快照，整体替换；音色详情以 JSON 存在 TEXT 列，
// 只用于展示，不参与查询条件，三种数据库都兼容。
type VoiceCatalogEntry struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	Provider   string `json:"provider" gorm:"type:varchar(32);uniqueIndex:idx_voice_catalog_entry,priority:1"`
	ResourceID string `json:"resource_id" gorm:"type:varchar(64);uniqueIndex:idx_voice_catalog_entry,priority:2"`
	VoiceID    string `json:"voice_id" gorm:"type:varchar(191);uniqueIndex:idx_voice_catalog_entry,priority:3"`
	SortOrder  int    `json:"sort_order"`
	Data       string `json:"data" gorm:"type:text"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
}

// ReplaceVoiceCatalog 用一份完整快照替换某个模型的音色。空快照视为上游异常而拒绝，
// 不能因为一次失败的同步把展示中的音色清空。
func ReplaceVoiceCatalog(provider, resourceID string, entries []VoiceCatalogEntry) error {
	if len(entries) == 0 {
		return errors.New("refuse to replace voice catalog with an empty snapshot")
	}
	now := common.GetTimestamp()
	for i := range entries {
		entries[i].ID = 0
		entries[i].Provider = provider
		entries[i].ResourceID = resourceID
		entries[i].SortOrder = i
		entries[i].UpdatedAt = now
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("provider = ? AND resource_id = ?", provider, resourceID).Delete(&VoiceCatalogEntry{}).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(entries, 100).Error
	})
}

func ListVoiceCatalog(provider, resourceID string) ([]VoiceCatalogEntry, error) {
	var entries []VoiceCatalogEntry
	err := DB.Where("provider = ? AND resource_id = ?", provider, resourceID).Order("sort_order").Find(&entries).Error
	return entries, err
}
