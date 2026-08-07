package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCommissionSettingNormalization(t *testing.T) {
	orig := commissionSetting
	t.Cleanup(func() { commissionSetting = orig })

	// 非法 type 视为未启用
	commissionSetting = CommissionSetting{Enabled: true, Type: "bogus", Value: 50}
	assert.False(t, GetCommissionSetting().Enabled)

	// percent 超过 100 截断到 100
	commissionSetting = CommissionSetting{Enabled: true, Type: CommissionTypePercent, Value: 150}
	s := GetCommissionSetting()
	assert.True(t, s.Enabled)
	assert.Equal(t, float64(100), s.Value)

	// 负值归零
	commissionSetting = CommissionSetting{Enabled: true, Type: CommissionTypeFixed, Value: -5, TopupCountLimit: -1}
	s = GetCommissionSetting()
	assert.Equal(t, float64(0), s.Value)
	assert.Equal(t, 0, s.TopupCountLimit)

	// 正常配置原样返回
	commissionSetting = CommissionSetting{Enabled: true, Type: CommissionTypePercent, Value: 10, TopupCountLimit: 3}
	s = GetCommissionSetting()
	assert.True(t, s.Enabled)
	assert.Equal(t, float64(10), s.Value)
	assert.Equal(t, 3, s.TopupCountLimit)
}
