// CUSTOM: 新建令牌的默认分组（fork 扩展）。
//
// 原本只有一个 DefaultUseAutoGroup 布尔开关。加入比价路由之后要表达的是「默认用
// 哪种路由」这个三选一的语义，两个布尔表达不了——都打开时得靠额外的优先级规则
// 解释，以后再加一种路由还要再加一个开关。改成一个字符串设置项。
package setting

import (
	"errors"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

const (
	// DefaultTokenGroupInherit 表示沿用用户自己的分组，即不做任何指定。
	DefaultTokenGroupInherit = ""
	DefaultTokenGroupAuto    = "auto"
	// DefaultTokenGroupAutoPrice 与 service.AutoPriceGroup 是同一个值。这里不引用
	// service 是为了避免 setting -> service 的反向依赖。
	DefaultTokenGroupAutoPrice = "auto_price"
)

var defaultTokenGroup atomic.Value

func init() {
	defaultTokenGroup.Store(DefaultTokenGroupInherit)
}

// GetDefaultTokenGroup 返回新建令牌应当使用的分组。
//
// 兼容处理：老部署只有 DefaultUseAutoGroup，升级后这个设置项是空的。空值且旧开关
// 为真时按 auto 处理，这样升级不改变任何现有行为；管理员在新界面里保存一次之后，
// 这个设置项就成为唯一事实来源。
func GetDefaultTokenGroup() string {
	group, _ := defaultTokenGroup.Load().(string)
	if group == DefaultTokenGroupInherit && DefaultUseAutoGroup {
		return DefaultTokenGroupAuto
	}
	return group
}

// SetDefaultTokenGroup 同时把旧开关同步过去，让还在读 DefaultUseAutoGroup 的代码
// （以及降级回旧版本的部署）看到一致的状态。
func SetDefaultTokenGroup(group string) {
	switch group {
	case DefaultTokenGroupAuto, DefaultTokenGroupAutoPrice, DefaultTokenGroupInherit:
	default:
		common.SysError("unknown default token group, falling back to inherit: " + group)
		group = DefaultTokenGroupInherit
	}
	defaultTokenGroup.Store(group)
	DefaultUseAutoGroup = group == DefaultTokenGroupAuto
}

// ValidateDefaultTokenGroup 在写库前校验。
func ValidateDefaultTokenGroup(value string) error {
	switch value {
	case DefaultTokenGroupAuto, DefaultTokenGroupAutoPrice, DefaultTokenGroupInherit:
		return nil
	default:
		return errors.New("默认令牌分组只能是 auto、auto_price 或留空")
	}
}
