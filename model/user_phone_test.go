package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPhoneRegistrationAndLegacyAccountBinding(t *testing.T) {
	truncateTables(t)
	oldQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 0
	t.Cleanup(func() { common.QuotaForNewUser = oldQuota })
	user, created, err := RegisterUserByPhone("+86 138-0013-8000", 0)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "13800138000", user.Phone)
	same, created, err := RegisterUserByPhone("13800138000", 0)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, user.Id, same.Id)
	legacy := &User{Username: "legacy", Password: "unchanged-password-hash", Email: "legacy@example.test", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(legacy).Error)
	assert.ErrorIs(t, BindPhoneToUser(legacy, user.Phone), ErrPhoneAlreadyTaken)
	require.NoError(t, BindPhoneToUser(legacy, "13900139000"))
	bound, err := GetUserByPhone("13900139000")
	require.NoError(t, err)
	assert.Equal(t, legacy.Id, bound.Id)
	assert.Equal(t, "unchanged-password-hash", bound.Password)
	assert.Equal(t, "legacy@example.test", bound.Email)
}

func TestPhoneNumberIsReleasedAfterAccountDeletion(t *testing.T) {
	truncateTables(t)
	oldQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 0
	t.Cleanup(func() { common.QuotaForNewUser = oldQuota })
	deleted := &User{Username: "deleted-phone", Phone: "13800138002", AffCode: "del1"}
	require.NoError(t, DB.Create(deleted).Error)
	require.NoError(t, DB.Delete(deleted).Error)

	user, created, err := RegisterUserByPhone(deleted.Phone, 0)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, deleted.Id, user.Id)
	assert.Equal(t, "13800138002", user.Phone)
	assert.NotContains(t, user.Username, user.Phone)

	// 注销记录保留原号码，便于审计。
	var archived User
	require.NoError(t, DB.Unscoped().First(&archived, deleted.Id).Error)
	assert.Equal(t, "13800138002", archived.Phone)

	legacy := &User{Username: "legacy-rebind", Status: common.UserStatusEnabled, AffCode: "leg1"}
	require.NoError(t, DB.Create(legacy).Error)
	assert.ErrorIs(t, BindPhoneToUser(legacy, deleted.Phone), ErrPhoneAlreadyTaken)
}

func TestPhoneBindingPreservesConcurrentAccountChanges(t *testing.T) {
	truncateTables(t)
	user := &User{Username: "stale-phone", Password: "old-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{"password": "new-hash", "status": common.UserStatusDisabled}).Error)
	require.NoError(t, BindPhoneToUser(user, "13800138999"))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "new-hash", stored.Password)
	assert.Equal(t, common.UserStatusDisabled, stored.Status)
	assert.Equal(t, "13800138999", stored.Phone)
}
