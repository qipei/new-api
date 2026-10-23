package model

import (
	"errors"

	"gorm.io/gorm"
)

// FinishRewardedAdSession releases the viewer's reservation without discarding
// the session or treating the client's playback report as proof of a reward.
// The original expires_at remains the anchor for the late callback window.
func FinishRewardedAdSession(userID int, sessionID, reason string) (*RewardedAdSession, error) {
	if userID <= 0 || sessionID == "" || len(sessionID) > 64 {
		return nil, errors.New("invalid_request")
	}
	switch reason {
	case "load_failed", "show_failed", "closed", "confirmation_timeout":
	default:
		return nil, errors.New("invalid_request")
	}
	var session RewardedAdSession
	if err := DB.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return nil, err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Settlement and expiration use the same global-day lock, including when
		// a finish request races with a callback on another application instance.
		if err := lockRewardedAdDay(tx, session.Day); err != nil {
			return err
		}
		if err := tx.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
			return err
		}
		if session.Status != "pending" {
			return nil
		}
		endedReason := "client_" + reason
		res := tx.Model(&RewardedAdSession{}).Where("id = ? AND user_id = ? AND status = ?", sessionID, userID, "pending").Updates(map[string]any{"status": "expired", "reason": endedReason})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("reservation_conflict")
		}
		for _, id := range []int{0, userID} {
			res := tx.Model(&RewardedAdDay{}).Where("day = ? AND user_id = ? AND reserved > 0 AND reserved_quota >= ?", session.Day, id, session.RewardQuota).Updates(map[string]any{"reserved": gorm.Expr("reserved - 1"), "reserved_quota": gorm.Expr("reserved_quota - ?", session.RewardQuota)})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errors.New("reservation_conflict")
			}
		}
		session.Status = "expired"
		session.Reason = endedReason
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &session, nil
}
