package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	CommissionTypeFixed   = "fixed"
	CommissionTypePercent = "percent"
)

// CommissionSetting 推广赚佣(邀请充值返佣)配置
type CommissionSetting struct {
	Enabled         bool    `json:"enabled"`           // 是否启用充值返佣
	Type            string  `json:"type"`              // fixed(固定额度) / percent(按到账额度比例)
	Value           float64 `json:"value"`             // fixed: 每笔固定返佣额度; percent: 0-100 百分比
	TopupCountLimit int     `json:"topup_count_limit"` // 0=不限次数; N>0=仅被邀请人前 N 笔付费订单返佣
	ScanCursor      int64   `json:"scan_cursor"`       // worker 扫描水位(unix 秒), 运行时状态, 非管理员配置
}

var commissionSetting = CommissionSetting{
	Enabled:         false,
	Type:            CommissionTypePercent,
	Value:           0,
	TopupCountLimit: 0,
	ScanCursor:      0,
}

func init() {
	config.GlobalConfig.Register("commission_setting", &commissionSetting)
}

// GetCommissionSetting 返回归一化后的配置副本:
// 非法 type 视为未启用; 负值归零; percent 值截断到 [0,100]。
// 归一化放在读取侧而非保存侧, 是为了不给上游 controller/option.go 增加校验分支。
func GetCommissionSetting() CommissionSetting {
	s := commissionSetting
	if s.Type != CommissionTypeFixed && s.Type != CommissionTypePercent {
		s.Enabled = false
	}
	if s.Value < 0 {
		s.Value = 0
	}
	if s.Type == CommissionTypePercent && s.Value > 100 {
		s.Value = 100
	}
	if s.TopupCountLimit < 0 {
		s.TopupCountLimit = 0
	}
	return s
}
