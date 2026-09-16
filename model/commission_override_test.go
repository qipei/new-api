package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func globalCommissionSettingForTest() operation_setting.CommissionSetting {
	return operation_setting.CommissionSetting{
		Enabled:         true,
		Type:            operation_setting.CommissionTypePercent,
		Value:           10,
		TopupCountLimit: 3,
		ScanCursor:      1700000000,
	}
}

func insertUserForOverrideTest(t *testing.T, id int) {
	t.Helper()
	// username 与 aff_code 上都有唯一索引，两者都必须按 id 取值。aff_code 留空
	// 时第一条能插入、第二条起全部撞空串，这类失败的报错信息与本用例无关，
	// 容易把人引到错误的方向。
	user := &User{
		Id:       id,
		Username: fmt.Sprintf("override_user_%d", id),
		AffCode:  fmt.Sprintf("OVR%d", id),
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
}

// 没有专属记录的推广人必须原样走全局参数，这是「未特殊设置的依然按全局」这条
// 需求的核心保证。
func TestResolveCommissionSettingFallsBackToGlobal(t *testing.T) {
	global := globalCommissionSettingForTest()

	assert.Equal(t, global, ResolveCommissionSetting(global, 999999))
	assert.Equal(t, global, ResolveCommissionSetting(global, 0))
	assert.Equal(t, global, ResolveCommissionSetting(global, -1))
}

// 专属记录只覆盖三项，Enabled 与 ScanCursor 必须保持全局值：全局开关是总闸，
// 覆盖了它就失去一键全停的能力。
func TestResolveCommissionSettingOverridesOnlyThreeFields(t *testing.T) {
	const userId = 920001
	insertUserForOverrideTest(t, userId)
	t.Cleanup(func() {
		DB.Where("user_id = ?", userId).Delete(&UserCommissionOverride{})
		DB.Where("id = ?", userId).Delete(&User{})
	})

	require.NoError(t, SaveUserCommissionOverride(&UserCommissionOverride{
		UserId:          userId,
		Type:            operation_setting.CommissionTypeFixed,
		Value:           500,
		TopupCountLimit: 0,
		Remark:          "大推广者",
	}))

	global := globalCommissionSettingForTest()
	resolved := ResolveCommissionSetting(global, userId)

	assert.Equal(t, operation_setting.CommissionTypeFixed, resolved.Type)
	assert.Equal(t, float64(500), resolved.Value)
	assert.Equal(t, 0, resolved.TopupCountLimit)
	assert.Equal(t, global.Enabled, resolved.Enabled, "启用开关不可被专属记录覆盖")
	assert.Equal(t, global.ScanCursor, resolved.ScanCursor, "扫描游标是运行时状态，不可被覆盖")
}

// 同一用户重复保存应更新而非新增，否则会出现两条记录、命中哪条不确定。
func TestSaveUserCommissionOverrideUpsertsByUser(t *testing.T) {
	const userId = 920002
	insertUserForOverrideTest(t, userId)
	t.Cleanup(func() {
		DB.Where("user_id = ?", userId).Delete(&UserCommissionOverride{})
		DB.Where("id = ?", userId).Delete(&User{})
	})

	first := &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypePercent, Value: 20}
	require.NoError(t, SaveUserCommissionOverride(first))

	second := &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypePercent, Value: 35}
	require.NoError(t, SaveUserCommissionOverride(second))

	var count int64
	require.NoError(t, DB.Model(&UserCommissionOverride{}).Where("user_id = ?", userId).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, first.Id, second.Id, "更新应复用原记录")
	assert.Equal(t, first.CreatedTime, second.CreatedTime, "创建时间不应被更新覆盖")

	resolved := ResolveCommissionSetting(globalCommissionSettingForTest(), userId)
	assert.Equal(t, float64(35), resolved.Value)
}

// 非法参数必须在入库前拒绝。这些值会直接参与佣金金额计算，落库即是资损风险。
func TestSaveUserCommissionOverrideRejectsInvalidInput(t *testing.T) {
	const userId = 920003
	insertUserForOverrideTest(t, userId)
	t.Cleanup(func() {
		DB.Where("user_id = ?", userId).Delete(&UserCommissionOverride{})
		DB.Where("id = ?", userId).Delete(&User{})
	})

	cases := []struct {
		name     string
		override *UserCommissionOverride
	}{
		{"类型无效", &UserCommissionOverride{UserId: userId, Type: "bonus", Value: 10}},
		{"类型为空", &UserCommissionOverride{UserId: userId, Type: "", Value: 10}},
		{"数值为负", &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypePercent, Value: -1}},
		{"比例超过 100", &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypePercent, Value: 100.01}},
		{"笔数限制为负", &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypePercent, Value: 10, TopupCountLimit: -1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, SaveUserCommissionOverride(tc.override))
		})
	}

	assert.ErrorIs(t,
		SaveUserCommissionOverride(&UserCommissionOverride{
			UserId: 888888, Type: operation_setting.CommissionTypePercent, Value: 10,
		}),
		ErrCommissionOverrideUserNotFound,
		"不存在的用户不应写入专属参数")
}

// 删除后该推广人立即回到全局参数。
func TestDeleteUserCommissionOverrideRestoresGlobal(t *testing.T) {
	const userId = 920004
	insertUserForOverrideTest(t, userId)
	t.Cleanup(func() {
		DB.Where("user_id = ?", userId).Delete(&UserCommissionOverride{})
		DB.Where("id = ?", userId).Delete(&User{})
	})

	override := &UserCommissionOverride{UserId: userId, Type: operation_setting.CommissionTypeFixed, Value: 999}
	require.NoError(t, SaveUserCommissionOverride(override))

	global := globalCommissionSettingForTest()
	require.Equal(t, float64(999), ResolveCommissionSetting(global, userId).Value)

	require.NoError(t, DeleteUserCommissionOverride(override.Id))
	assert.Equal(t, global, ResolveCommissionSetting(global, userId))
}
