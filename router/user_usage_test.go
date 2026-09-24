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

func TestUserUsageRoutesAuthValidationAndIsolation(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldLimit := common.RedisEnabled, common.GlobalApiRateLimitEnable
	oldSecret, oldUnit := common.SessionSecret, common.QuotaPerUnit
	oldGeneral := *operation_setting.GetGeneralSetting()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}, &model.UserUsageRequest{}, &model.UserUsageTracking{}))
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.GlobalApiRateLimitEnable = false, false
	common.SessionSecret, common.QuotaPerUnit = "usage-route-test", 500_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = "USD"
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled, common.GlobalApiRateLimitEnable = oldRedis, oldLimit
		common.SessionSecret, common.QuotaPerUnit = oldSecret, oldUnit
		*operation_setting.GetGeneralSetting() = oldGeneral
		_ = sqlDB.Close()
	})
	user := &model.User{Username: "usage-user", Quota: 500_000, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(user).Error)
	bundle, err := service.CreateLoginSession(user.Id, "phone", "127.0.0.1", "mini-program")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.UserUsageTracking{ID: 1, StartedAt: 1}).Error)
	require.NoError(t, model.InitializeUserUsageTracking())
	epoch, err := model.GetUserUsageTrackingStart()
	require.NoError(t, err)
	assert.Equal(t, int64(1), epoch)
	require.NoError(t, db.Create(&model.UserUsageRequest{UserID: user.Id, RequestID: "my-request", CreatedAt: time.Now().Unix(), Requests: 1, Outcome: "failed", StatusCode: 429, Quota: 100}).Error)
	require.NoError(t, db.Create(&model.UserUsageRequest{UserID: user.Id + 1, RequestID: "private-request", CreatedAt: time.Now().Unix(), Requests: 1, Outcome: "success", Quota: 99999}).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	for _, endpoint := range []string{"overview", "records"} {
		t.Run(endpoint, func(t *testing.T) {
			path := "/api/user/self/usage/" + endpoint
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, http.StatusUnauthorized, response.Code)
			request := httptest.NewRequest(http.MethodGet, path+"?period=today&user_id=999&username=other", nil)
			request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
			response = httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, 200, response.Code)
			assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
			assert.NotContains(t, response.Body.String(), "private-request")
			if endpoint == "overview" {
				var payload struct {
					Success bool                      `json:"success"`
					Data    service.UserUsageOverview `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
				require.True(t, payload.Success)
				assert.Equal(t, int64(1), payload.Data.Requests)
				assert.Equal(t, int64(100), payload.Data.Consumption.Quota)
				require.NotNil(t, payload.Data.SuccessRate)
				assert.Zero(t, *payload.Data.SuccessRate)
			} else {
				var payload struct {
					Success bool                     `json:"success"`
					Data    service.UserUsageRecords `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
				require.True(t, payload.Success)
				require.Len(t, payload.Data.Items, 1)
				assert.Equal(t, "my-request", payload.Data.Items[0].RequestID)
			}
			invalid := []string{"period=year"}
			if endpoint == "records" {
				invalid = append(invalid, "page=0", "page_size=101", "page_size=-1", "page=abc", "outcome=other")
			}
			for _, query := range invalid {
				request = httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
				request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
				response = httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				assert.Equal(t, 400, response.Code, query)
			}
		})
	}
}
