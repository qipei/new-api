// CUSTOM: 渠道闲时时段（fork 扩展）。
//
// 有些上游按时段收费（例如 autodl 的 ComfyUI 工作流 00:00~08:00 更便宜）。时段挂在
// 渠道上而不是模型上：同一个模型名可能由多个上游提供，只有确实分时段收费的那条线路
// 才该给闲时价。请求在时段内落到配置了时段的渠道时，价格表里配了闲时价的档位按
// 闲时价计费，其余情况一律按原价。
//
// 时段是 [Start, End) 的每日窗口，End 早于 Start 表示跨零点（例如 22:00~06:00）。
// 我们按发起请求的时刻判断，上游可能按开始执行的时刻计费，排队会让任务跨过时段
// 结束点；配置时应把结束时间比上游提前一些（例如上游 08:00 结束就配 07:50）。
package dto

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	offPeakClockLayout = "15:04"
	offPeakDefaultTZ   = "Asia/Shanghai"
)

type OffPeakPricing struct {
	Start string `json:"start"`
	End   string `json:"end"`
	TZ    string `json:"tz,omitempty"`
}

func (p *OffPeakPricing) timezone() string {
	if tz := strings.TrimSpace(p.TZ); tz != "" {
		return tz
	}
	return offPeakDefaultTZ
}

// Validate 在保存渠道时校验，运行期的 ActiveAt 对坏配置一律按不在时段内处理。
func (p *OffPeakPricing) Validate() error {
	if p == nil {
		return nil
	}
	start, err := time.Parse(offPeakClockLayout, p.Start)
	if err != nil {
		return fmt.Errorf("off_peak_pricing.start %q is not HH:MM", p.Start)
	}
	end, err := time.Parse(offPeakClockLayout, p.End)
	if err != nil {
		return fmt.Errorf("off_peak_pricing.end %q is not HH:MM", p.End)
	}
	if start.Equal(end) {
		return fmt.Errorf("off_peak_pricing.start and end must differ")
	}
	if _, err := time.LoadLocation(p.timezone()); err != nil {
		return fmt.Errorf("off_peak_pricing.tz %q is not a valid timezone", p.timezone())
	}
	return nil
}

// offPeakLocations 缓存已解析的时区：每个按价格表计费的请求都会判断一次，
// time.LoadLocation 不带缓存，每次都要读时区库。
var offPeakLocations sync.Map

func offPeakLocation(tz string) (*time.Location, bool) {
	if cached, ok := offPeakLocations.Load(tz); ok {
		loc, _ := cached.(*time.Location)
		return loc, loc != nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		// 坏时区也缓存（存 nil），免得每次请求都去读一次时区库。
		offPeakLocations.Store(tz, (*time.Location)(nil))
		return nil, false
	}
	offPeakLocations.Store(tz, loc)
	return loc, true
}

// ActiveAt 判断时刻 at 是否落在闲时时段内。未配置或配置无效都返回 false：
// 闲时价只会比原价低，判断不了的时候按原价收才不会亏。
func (p *OffPeakPricing) ActiveAt(at time.Time) bool {
	if p == nil {
		return false
	}
	start, errStart := time.Parse(offPeakClockLayout, p.Start)
	end, errEnd := time.Parse(offPeakClockLayout, p.End)
	if errStart != nil || errEnd != nil || start.Equal(end) {
		return false
	}
	loc, ok := offPeakLocation(p.timezone())
	if !ok {
		return false
	}
	local := at.In(loc)
	minute := local.Hour()*60 + local.Minute()
	startMinute := start.Hour()*60 + start.Minute()
	endMinute := end.Hour()*60 + end.Minute()
	if startMinute < endMinute {
		return minute >= startMinute && minute < endMinute
	}
	return minute >= startMinute || minute < endMinute
}
