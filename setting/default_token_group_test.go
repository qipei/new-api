package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 老部署只有 DefaultUseAutoGroup。升级后新设置项是空的，此时必须沿用旧开关的
// 行为，否则所有新建令牌会突然从 auto 掉回用户自己的分组。
func TestDefaultTokenGroupFallsBackToTheLegacySwitch(t *testing.T) {
	prevGroup := GetDefaultTokenGroup()
	prevLegacy := DefaultUseAutoGroup
	t.Cleanup(func() {
		SetDefaultTokenGroup(prevGroup)
		DefaultUseAutoGroup = prevLegacy
	})

	SetDefaultTokenGroup(DefaultTokenGroupInherit)

	DefaultUseAutoGroup = true
	assert.Equal(t, DefaultTokenGroupAuto, GetDefaultTokenGroup(),
		"新设置项为空且旧开关为真时按 auto 处理")

	DefaultUseAutoGroup = false
	assert.Equal(t, DefaultTokenGroupInherit, GetDefaultTokenGroup())
}

// 管理员在新界面保存之后，新设置项成为唯一事实来源，旧开关跟着同步——
// 免得降级回旧版本或仍在读旧开关的代码看到相反的状态。
func TestSetDefaultTokenGroupSyncsTheLegacySwitch(t *testing.T) {
	prevGroup := GetDefaultTokenGroup()
	prevLegacy := DefaultUseAutoGroup
	t.Cleanup(func() {
		SetDefaultTokenGroup(prevGroup)
		DefaultUseAutoGroup = prevLegacy
	})

	cases := []struct {
		group      string
		wantGroup  string
		wantLegacy bool
	}{
		{group: DefaultTokenGroupAuto, wantGroup: DefaultTokenGroupAuto, wantLegacy: true},
		{group: DefaultTokenGroupAutoPrice, wantGroup: DefaultTokenGroupAutoPrice, wantLegacy: false},
		{group: DefaultTokenGroupInherit, wantGroup: DefaultTokenGroupInherit, wantLegacy: false},
		// 无法识别的值不能让默认分组变成一个不存在的分组
		{group: "不存在的分组", wantGroup: DefaultTokenGroupInherit, wantLegacy: false},
	}
	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			SetDefaultTokenGroup(tc.group)
			assert.Equal(t, tc.wantGroup, GetDefaultTokenGroup())
			assert.Equal(t, tc.wantLegacy, DefaultUseAutoGroup)
		})
	}
}

// 选了 auto_price 之后，旧开关必须是关的：否则同时读两处的代码会以为默认是 auto。
func TestAutoPriceDoesNotLeaveTheLegacySwitchOn(t *testing.T) {
	prevGroup := GetDefaultTokenGroup()
	prevLegacy := DefaultUseAutoGroup
	t.Cleanup(func() {
		SetDefaultTokenGroup(prevGroup)
		DefaultUseAutoGroup = prevLegacy
	})

	SetDefaultTokenGroup(DefaultTokenGroupAuto)
	require.True(t, DefaultUseAutoGroup)

	SetDefaultTokenGroup(DefaultTokenGroupAutoPrice)
	assert.False(t, DefaultUseAutoGroup)
	assert.Equal(t, DefaultTokenGroupAutoPrice, GetDefaultTokenGroup(),
		"旧开关关掉之后不能被兼容逻辑当成 inherit 又倒回去")
}

func TestValidateDefaultTokenGroup(t *testing.T) {
	for _, valid := range []string{DefaultTokenGroupAuto, DefaultTokenGroupAutoPrice, DefaultTokenGroupInherit} {
		assert.NoError(t, ValidateDefaultTokenGroup(valid), valid)
	}
	assert.Error(t, ValidateDefaultTokenGroup("default"))
	assert.Error(t, ValidateDefaultTokenGroup("8.8折"))
}
