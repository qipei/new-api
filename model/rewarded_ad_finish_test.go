package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFinishRewardedAdSessionReleasesReservationOnceAndAllowsNewView(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	session, err := CreateRewardedAdSession(1, "first", cfg, now)
	require.NoError(t, err)
	finished, err := FinishRewardedAdSession(1, session.ID, "load_failed")
	require.NoError(t, err)
	assert.Equal(t, "expired", finished.Status)
	assert.Equal(t, "client_load_failed", finished.Reason)
	assert.Equal(t, session.ExpiresAt, finished.ExpiresAt, "preserve the original late-notification deadline")
	_, err = FinishRewardedAdSession(1, session.ID, "closed")
	require.NoError(t, err)
	for _, id := range []int{0, 1} {
		var day RewardedAdDay
		require.NoError(t, DB.Where("day = ? AND user_id = ?", session.Day, id).First(&day).Error)
		assert.Zero(t, day.Reserved)
		assert.Zero(t, day.ReservedQuota)
		assert.Zero(t, day.Credited)
	}
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 100, user.Quota, "finishing cannot credit a reward")
	same, err := CreateRewardedAdSession(1, "first", cfg, now.Add(3*time.Second))
	require.NoError(t, err)
	assert.Equal(t, session.ID, same.ID)
	assert.Equal(t, "expired", same.Status)
	next, err := CreateRewardedAdSession(1, "next", cfg, now.Add(3*time.Second))
	require.NoError(t, err)
	assert.NotEqual(t, session.ID, next.ID)
}

func TestFinishRewardedAdSessionPreservesLateRewardAndCreditedSession(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	session, err := CreateRewardedAdSession(1, "first", cfg, now)
	require.NoError(t, err)
	_, err = FinishRewardedAdSession(1, session.ID, "confirmation_timeout")
	require.NoError(t, err)
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: operation_setting.RewardedAdOptionKey, Value: string(raw)}).Error)
	event := RewardedAdCallback{TransID: "late-finished", UserID: "1", Extra: session.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.NoError(t, SettleRewardedAd(event, now.Add(2*time.Minute)))
	credited, err := FinishRewardedAdSession(1, session.ID, "closed")
	require.NoError(t, err)
	assert.Equal(t, "credited", credited.Status)
	assert.Empty(t, credited.Reason, "a paid reward must not retain the earlier client failure reason")
	require.NoError(t, SettleRewardedAd(event, now.Add(4*time.Minute)))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 150, user.Quota)
	for _, id := range []int{0, 1} {
		var day RewardedAdDay
		require.NoError(t, DB.Where("day = ? AND user_id = ?", session.Day, id).First(&day).Error)
		assert.Zero(t, day.Reserved)
		assert.Equal(t, 1, day.Credited)
	}
}

func TestFinishRewardedAdSessionRejectsOtherOwnerAndRollsBackBrokenReservation(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	session, err := CreateRewardedAdSession(1, "first", cfg, now)
	require.NoError(t, err)
	_, err = FinishRewardedAdSession(2, session.ID, "closed")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = FinishRewardedAdSession(1, session.ID, "invented")
	require.ErrorContains(t, err, "invalid_request")
	require.NoError(t, DB.Model(&RewardedAdDay{}).Where("day = ? AND user_id = ?", session.Day, 1).Update("reserved", 0).Error)
	_, err = FinishRewardedAdSession(1, session.ID, "closed")
	require.ErrorContains(t, err, "reservation_conflict")
	var stored RewardedAdSession
	require.NoError(t, DB.First(&stored, "id = ?", session.ID).Error)
	assert.Equal(t, "pending", stored.Status)
	var global RewardedAdDay
	require.NoError(t, DB.Where("day = ? AND user_id = 0", session.Day).First(&global).Error)
	assert.Equal(t, 1, global.Reserved, "global counter release must also roll back")
}

func TestFinishRewardedAdSessionLateCallbackRespectsReplacementReservation(t *testing.T) {
	cfg, now := rewardedAdDB(t)
	cfg.DailyLimit = 1
	first, err := CreateRewardedAdSession(1, "first", cfg, now)
	require.NoError(t, err)
	_, err = FinishRewardedAdSession(1, first.ID, "closed")
	require.NoError(t, err)
	next, err := CreateRewardedAdSession(1, "next", cfg, now.Add(time.Second))
	require.NoError(t, err)
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: operation_setting.RewardedAdOptionKey, Value: string(raw)}).Error)
	event := RewardedAdCallback{TransID: "late-after-replacement", UserID: "1", Extra: first.ID, Adpid: cfg.Adpid, Platform: "weixin-mp", Provider: "china"}
	require.ErrorContains(t, SettleRewardedAd(event, now.Add(time.Minute)), "review_required")
	var stored RewardedAdSession
	require.NoError(t, DB.First(&stored, "id = ?", next.ID).Error)
	assert.Equal(t, "pending", stored.Status)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	assert.Equal(t, 100, user.Quota)
	for _, id := range []int{0, 1} {
		var day RewardedAdDay
		require.NoError(t, DB.Where("day = ? AND user_id = ?", first.Day, id).First(&day).Error)
		assert.Equal(t, 1, day.Reserved)
		assert.EqualValues(t, 50, day.ReservedQuota)
		assert.Zero(t, day.Credited)
	}
	_, err = FinishRewardedAdSession(1, first.ID, "closed")
	require.NoError(t, err)
}
