package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserGroupsRespectsOwnGroupVisibility(t *testing.T) {
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalUsable := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalOverrides := ratio_setting.GroupGroupRatio2JSONString()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecial := specialGroups.ReadAll()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.SetDatabaseTypes(originalMainType, originalLogType)
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalOverrides))
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecial)
	})
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 8201, Username: "private-partner", Password: "password", Group: "partner",
	}).Error)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"partner":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"partner":{"default":0.8}}`))

	for _, tc := range []struct {
		name    string
		usable  string
		special map[string]string
		wantOwn bool
	}{
		{name: "hidden from its own members", usable: `{"default":"Default"}`},
		{name: "explicitly selectable globally", usable: `{"default":"Default","partner":"Partner"}`, wantOwn: true},
		{name: "explicitly selectable for this user group", usable: `{"default":"Default"}`, special: map[string]string{"+:partner": "Partner"}, wantOwn: true},
		{name: "explicitly removed for this user group", usable: `{"default":"Default","partner":"Partner"}`, special: map[string]string{"-:partner": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(tc.usable))
			specialGroups.Clear()
			if tc.special != nil {
				specialGroups.Set("partner", tc.special)
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/groups", nil)
			ctx.Set("id", 8201)

			GetUserGroups(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    map[string]struct {
					Ratio float64 `json:"ratio"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			_, hasOwn := response.Data["partner"]
			assert.Equal(t, tc.wantOwn, hasOwn)
			require.Contains(t, response.Data, "default")
			assert.Equal(t, 0.8, response.Data["default"].Ratio)
			assert.True(t, service.GroupInUserUsableGroups("partner", "partner"), "existing keys retain access to their owner's group")
		})
	}
}
