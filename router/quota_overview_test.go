package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaOverviewRoute(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldLimit := common.RedisEnabled, common.GlobalApiRateLimitEnable
	oldSecret, oldUnit := common.SessionSecret, common.QuotaPerUnit
	oldGeneral := *operation_setting.GetGeneralSetting()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.GlobalApiRateLimitEnable = false, false
	common.SessionSecret, common.QuotaPerUnit = "quota-overview-route-test", 500_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = "USD"
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled, common.GlobalApiRateLimitEnable = oldRedis, oldLimit
		common.SessionSecret, common.QuotaPerUnit = oldSecret, oldUnit
		*operation_setting.GetGeneralSetting() = oldGeneral
		_ = sqlDB.Close()
	})
	user := &model.User{Username: "overview-user", Quota: 6_240_000, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(user).Error)
	bundle, err := service.CreateLoginSession(user.Id, "phone", "127.0.0.1", "mini-program-test")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Log{UserId: user.Id, Type: model.LogTypeConsume, CreatedAt: time.Now().Unix(), Quota: 210_000}).Error)
	require.NoError(t, db.Create(&model.Log{UserId: user.Id + 1, Type: model.LogTypeConsume, CreatedAt: time.Now().Unix(), Quota: 999_999}).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	// Anonymous callers cannot read financial data.
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/self/quota/overview", nil))
	assert.Equal(t, http.StatusUnauthorized, response.Code)

	// Caller-supplied identity and dates must not override the authenticated user
	// or the server's calendar periods.
	request := httptest.NewRequest(http.MethodGet, "/api/user/self/quota/overview?user_id=999&username=other&start_timestamp=1&end_timestamp=2", nil)
	request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	var payload struct {
		Success bool                      `json:"success"`
		Message string                    `json:"message"`
		Data    service.UserQuotaOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, "12.480000", payload.Data.Remaining.Amount)
	assert.Equal(t, "0.420000", payload.Data.Month.Amount)
	assert.Equal(t, "$", payload.Data.Currency.Symbol)

	require.NoError(t, db.Migrator().DropTable(&model.Log{}))
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.NotEmpty(t, payload.Message)
}
