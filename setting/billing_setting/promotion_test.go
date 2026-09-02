package billing_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func atShanghai(t *testing.T, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	require.NoError(t, err)
	return parsed
}

func withPromotions(t *testing.T, promotions map[string][]ModelPromotion) {
	t.Helper()
	original := billingSetting.ModelPromotions
	billingSetting.ModelPromotions = promotions
	t.Cleanup(func() { billingSetting.ModelPromotions = original })
}

// 起止日期含当天，且按活动自己的时区判断——运营填的是"这几天做活动"，
// 差一天就是白送或少送一天的折扣。
func TestActivePromotionCoversBothEndDates(t *testing.T) {
	withPromotions(t, map[string][]ModelPromotion{
		"m": {{Name: "开学季", Start: "2026-08-30", End: "2026-09-10", TZ: "Asia/Shanghai", Ratio: 0.5}},
	})

	cases := []struct {
		at   string
		want bool
	}{
		{"2026-08-29 23:59", false},
		{"2026-08-30 00:00", true},
		{"2026-09-10 23:59", true},
		{"2026-09-11 00:00", false},
	}
	for _, tc := range cases {
		t.Run(tc.at, func(t *testing.T) {
			_, ok := ActivePromotionAt("m", "default", atShanghai(t, tc.at))
			assert.Equal(t, tc.want, ok)
		})
	}
}

// 活动可以只挂在部分分组上；分组列表为空表示该模型所有分组都参与。
func TestActivePromotionScopesByGroup(t *testing.T) {
	withPromotions(t, map[string][]ModelPromotion{
		"m": {
			{Name: "限定", Start: "2026-08-30", End: "2026-09-10", Ratio: 0.5, Groups: []string{"8.8折"}},
		},
		"all": {
			{Name: "全分组", Start: "2026-08-30", End: "2026-09-10", Ratio: 0.5},
		},
	})
	at := atShanghai(t, "2026-09-01 12:00")

	_, ok := ActivePromotionAt("m", "8.8折", at)
	assert.True(t, ok, "指定的分组应当命中")
	_, ok = ActivePromotionAt("m", "default", at)
	assert.False(t, ok, "未列入的分组不该命中")
	_, ok = ActivePromotionAt("all", "任意分组", at)
	assert.True(t, ok, "分组列表为空表示不限分组")
}

// 多条活动同时命中时取倍率最低的一条，结果不能取决于配置顺序。
func TestActivePromotionPicksTheLowestRatioRegardlessOfOrder(t *testing.T) {
	at := atShanghai(t, "2026-09-01 12:00")
	ascending := []ModelPromotion{
		{Name: "五折", Start: "2026-08-30", End: "2026-09-10", Ratio: 0.5},
		{Name: "三折", Start: "2026-09-01", End: "2026-09-02", Ratio: 0.3},
	}
	descending := []ModelPromotion{ascending[1], ascending[0]}

	for _, order := range [][]ModelPromotion{ascending, descending} {
		withPromotions(t, map[string][]ModelPromotion{"m": order})
		promotion, ok := ActivePromotionAt("m", "default", at)
		require.True(t, ok)
		assert.Equal(t, "三折", promotion.Name)
		assert.Equal(t, 0.3, promotion.Ratio)
	}
}

// 倍率大于 1 是借活动之名涨价，配错了没人会发现，必须在写库前挡掉。
func TestValidateModelPromotionsJSON(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "合法配置", payload: `{"m":[{"name":"开学季","start":"2026-08-30","end":"2026-09-10","ratio":0.5}]}`},
		{name: "空配置", payload: `{}`},
		{name: "倍率大于 1", payload: `{"m":[{"name":"x","start":"2026-08-30","end":"2026-09-10","ratio":1.5}]}`, wantErr: true},
		{name: "倍率为 0", payload: `{"m":[{"name":"x","start":"2026-08-30","end":"2026-09-10","ratio":0}]}`, wantErr: true},
		{name: "倍率为负", payload: `{"m":[{"name":"x","start":"2026-08-30","end":"2026-09-10","ratio":-0.5}]}`, wantErr: true},
		{name: "结束早于开始", payload: `{"m":[{"name":"x","start":"2026-09-10","end":"2026-08-30","ratio":0.5}]}`, wantErr: true},
		{name: "日期格式错误", payload: `{"m":[{"name":"x","start":"2026/08/30","end":"2026-09-10","ratio":0.5}]}`, wantErr: true},
		{name: "活动名为空", payload: `{"m":[{"name":"","start":"2026-08-30","end":"2026-09-10","ratio":0.5}]}`, wantErr: true},
		{name: "时区无法识别", payload: `{"m":[{"name":"x","start":"2026-08-30","end":"2026-09-10","tz":"Mars/Olympus","ratio":0.5}]}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelPromotionsJSON(tc.payload)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// IsLiveAt 只看日期窗口。下发和展示必须用它——用 ActivePromotionAt 传空分组会
// 把所有限定了分组的活动误判成未进行中，这是我第一版写错的地方。
func TestIsLiveAtIgnoresGroupScope(t *testing.T) {
	scoped := ModelPromotion{
		Name: "限定", Start: "2026-08-30", End: "2026-09-10",
		TZ: "Asia/Shanghai", Ratio: 0.5, Groups: []string{"8.8折"},
	}
	during := atShanghai(t, "2026-09-01 12:00")

	assert.True(t, scoped.IsLiveAt(during), "限定分组的活动在窗口内也算进行中")
	assert.False(t, scoped.IsLiveAt(atShanghai(t, "2026-09-11 00:00")))

	withPromotions(t, map[string][]ModelPromotion{"m": {scoped}})
	_, ok := ActivePromotionAt("m", "", during)
	assert.False(t, ok, "带分组的查询传空分组不该命中——所以下发不能用它做过滤")
}
