package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserUsageTracksFinalRequestWithoutOptionalLogs(t *testing.T) {
	t.Setenv("USER_USAGE_SPOOL_DIR", t.TempDir())
	oldDB, oldLogDB, oldLogs := model.DB, model.LOG_DB, common.LogConsumeEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.UserUsageRequest{}, &model.UserUsageTracking{}))
	model.DB, model.LOG_DB = db, db
	require.NoError(t, model.StartUserUsageWriter())
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		require.NoError(t, model.StopUserUsageWriter(context.Background()))
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.LogConsumeEnabled = oldLogs
		_ = sqlDB.Close()
	})
	for _, tc := range []struct {
		name, path, outcome   string
		status, user, records int
		stream                bool
	}{
		{"retry-success", "/v1/chat/completions", "success", 200, 1, 1, false},
		{"completed-disconnect", "/v1/chat/completions", "success", 200, 1, 1, false},
		{"stream-failure", "/v1/chat/completions", "failed", 200, 1, 1, true},
		{"failed", "/v1/chat/completions", "failed", 429, 1, 1, false},
		{"fetch", "/suno/fetch", "", 200, 1, 0, false},
		{"unauthenticated", "/v1/chat/completions", "", 401, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Where("1 = 1").Delete(&model.UserUsageRequest{}).Error)
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.Use(UserUsageTracking(), RouteTag("relay"))
			engine.POST(tc.path, func(c *gin.Context) {
				c.Set("id", tc.user)
				c.Set(common.RequestIdKey, "req-"+tc.name)
				c.Set("use_channel", []string{"1", "2"})
				model.MarkUserUsageAttemptError(c, 429)
				other := map[string]interface{}{}
				if tc.stream {
					other["stream_status"] = map[string]interface{}{"status": "error"}
				}
				model.RecordConsumeLog(c, tc.user, model.RecordConsumeLogParams{ModelName: "test", Quota: 100, PromptTokens: 20, CompletionTokens: 5, IsStream: tc.stream, Other: other})
				c.String(tc.status, "original-response")
				if tc.name == "completed-disconnect" {
					ctx, cancel := context.WithCancel(c.Request.Context())
					c.Request = c.Request.WithContext(ctx)
					cancel()
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, nil))
			assert.Equal(t, tc.status, response.Code)
			assert.Equal(t, "original-response", response.Body.String())
			require.NoError(t, model.FlushUserUsage(context.Background()))
			var rows []model.UserUsageRequest
			require.NoError(t, db.Find(&rows).Error)
			require.Len(t, rows, tc.records)
			if tc.records > 0 {
				assert.Equal(t, tc.outcome, rows[0].Outcome)
				assert.Equal(t, int64(1), rows[0].Requests)
				assert.Equal(t, 1, rows[0].RetryCount)
				assert.Equal(t, 429, rows[0].LastErrorStatusCode)
				assert.Equal(t, int64(100), rows[0].Quota)
				assert.Equal(t, tc.status, rows[0].StatusCode)
			}
		})
	}
}

func TestUserUsageEndpointRateLimitIsSharedPerUser(t *testing.T) {
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 901137) })
	engine.GET("/overview", UserUsageRateLimit(), func(c *gin.Context) { c.Status(200) })
	engine.GET("/records", UserUsageRateLimit(), func(c *gin.Context) { c.Status(200) })
	for i := 0; i < 60; i++ {
		path := "/overview"
		if i%2 == 1 {
			path = "/records"
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, 200, response.Code)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/records", nil))
	assert.Equal(t, 429, response.Code)
	assert.NotEmpty(t, response.Header().Get("Retry-After"))
}
