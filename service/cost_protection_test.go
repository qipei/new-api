package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCostProtectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMemoryCache := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	originalBatchUpdate := common.BatchUpdateEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.MemoryCacheEnabled = originalMemoryCache
		common.RedisEnabled = originalRedisEnabled
		common.BatchUpdateEnabled = originalBatchUpdate
		if originalMemoryCache && originalDB != nil && originalDB.Migrator().HasTable(&model.Channel{}) {
			model.InitChannelCache()
		}
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func createCostProtectionChannel(t *testing.T, db *gorm.DB, id int, group, modelName string) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Type: constant.ChannelTypeOpenAI, Key: fmt.Sprintf("key-%d", id),
		Status: common.ChannelStatusEnabled, Name: fmt.Sprintf("channel-%d", id),
		Weight: &weight, Models: modelName, Group: group, Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
}

func TestCostProtectionRoutesToAlternativeAndFallsBackForContinuity(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	modelName := "cost-route-" + strings.ReplaceAll(t.Name(), "/", "-")
	createCostProtectionChannel(t, db, 7101, "default", modelName)
	createCostProtectionChannel(t, db, 7102, "default", modelName)
	model.InitChannelCache()
	user := model.User{Id: 7103, Username: "cost-route-user", Password: "password123", Quota: 900}
	token := model.Token{Id: 7104, UserId: user.Id, Key: "cost-route-token", Name: "cost-route-token", RemainQuota: 900}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	log := model.Log{
		UserId: user.Id, Type: model.LogTypeConsume, ModelName: modelName, Quota: 100,
		ChannelId: 7101, TokenId: token.Id, Group: "default",
		Other: common.MapToJsonStr(map[string]interface{}{
			"request_path":   "/v1/chat/completions",
			"billing_source": BillingSourceWallet,
		}),
	}
	require.NoError(t, db.Create(&log).Error)
	applyCostProtection(context.Background(), UpstreamCostInfo{
		LogId: log.Id, UpstreamQuota: 150, UpstreamQuotaPerUnit: common.QuotaPerUnit,
		NormalizedUpstreamQuota: 150, UpstreamAmount: 0.0003, PlatformAmount: 0.0002,
		ExceedsPlatform: true,
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "default", ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 7102, selected.Id)
	assert.Equal(t, "default", group)
	var auditedLog model.Log
	require.NoError(t, db.First(&auditedLog, log.Id).Error)
	auditedOther, err := common.StrToMap(auditedLog.Other)
	require.NoError(t, err)
	adminInfo, ok := auditedOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	protection, ok := adminInfo["cost_protection"].(map[string]interface{})
	require.True(t, ok)
	// The upstream already charged us for this request, so switching channels alone
	// would leave the loss on the books: both actions have to happen.
	assert.Equal(t, "surcharge", protection["action"])
	assert.Equal(t, true, protection["rerouted"])
	assert.Equal(t, float64(7102), protection["alternative_channel_id"])
	assert.Equal(t, float64(50), protection["delta_quota"])
	var gotUser model.User
	require.NoError(t, db.First(&gotUser, user.Id).Error)
	assert.Equal(t, 850, gotUser.Quota, "surcharge must be deducted even when a cheaper channel exists")

	require.NoError(t, markCostProtectedChannel("default", modelName, 7102))
	selected, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "default", ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Contains(t, []int{7101, 7102}, selected.Id, "all protected channels must fall back instead of blocking traffic")
}

func TestCostProtectionAutoGroupSearchesLaterGroupBeforeFallback(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	modelName := "cost-auto-" + strings.ReplaceAll(t.Name(), "/", "-")
	createChannelSelectAutoGroupsChannel(t, db, 7151, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 7152, "default", modelName)
	model.InitChannelCache()
	require.NoError(t, markCostProtectedChannel("vip", modelName, 7151))

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: modelName,
		RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 7152, selected.Id)
	assert.Equal(t, "default", group)
}

// Rounding noise in the float amounts must never move money: the surcharge is
// driven by the integer normalized quota, so an equal quota is always a no-op.
func TestCostProtectionIgnoresFloatOnlyDifferenceAtSameNormalizedQuota(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	modelName := "cost-precision-" + strings.ReplaceAll(t.Name(), "/", "-")
	createCostProtectionChannel(t, db, 7181, "default", modelName)
	createCostProtectionChannel(t, db, 7182, "default", modelName)
	model.InitChannelCache()
	log := model.Log{
		Type: model.LogTypeConsume, ModelName: modelName, Quota: 100,
		ChannelId: 7181, Group: "default", Other: "{}",
	}
	require.NoError(t, db.Create(&log).Error)

	applyCostProtection(context.Background(), UpstreamCostInfo{
		LogId: log.Id, UpstreamQuota: 100, UpstreamQuotaPerUnit: common.QuotaPerUnit,
		NormalizedUpstreamQuota: 100,
		UpstreamAmount:          0.2000000000001, PlatformAmount: 0.2, ExceedsPlatform: true,
	})
	assert.Empty(t, getCostProtectedChannelIDs("default", modelName))
}

func TestProcessCostProtectionJobsFetchesInBackgroundBatchAndRoutes(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	InitHttpClient()
	setUpstreamCostBillingUnits(t, common.QuotaPerUnit, 1)
	var statusCalls atomic.Int32
	var logCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			statusCalls.Add(1)
			_, err := fmt.Fprintf(w, `{"success":true,"data":{"quota_per_unit":%f,"price":1}}`, common.QuotaPerUnit)
			assert.NoError(t, err)
		case "/api/log/token":
			logCalls.Add(1)
			assert.Equal(t, "Bearer key-7191", r.Header.Get("Authorization"))
			_, err := fmt.Fprint(w, `{"success":true,"data":{"items":[{"request_id":"upstream-7191","quota":150}]}}`)
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	modelName := "cost-worker-" + strings.ReplaceAll(t.Name(), "/", "-")
	createCostProtectionChannel(t, db, 7191, "default", modelName)
	createCostProtectionChannel(t, db, 7192, "default", modelName)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7191).Update("base_url", server.URL).Error)
	model.InitChannelCache()
	user := model.User{Id: 7193, Username: "cost-worker-user", Password: "password123", Quota: 900}
	require.NoError(t, db.Create(&user).Error)
	log := model.Log{
		UserId: user.Id, Type: model.LogTypeConsume, ModelName: modelName, Quota: 100,
		ChannelId: 7191, Group: "default", UpstreamRequestId: "upstream-7191",
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source": BillingSourceWallet,
			"request_path":   "/v1/chat/completions",
		}),
	}
	require.NoError(t, db.Create(&log).Error)

	processCostProtectionJobs(context.Background(), []costProtectionJob{{logID: log.Id}})
	assert.EqualValues(t, 1, statusCalls.Load())
	assert.EqualValues(t, 1, logCalls.Load())
	assert.Contains(t, getCostProtectedChannelIDs("default", modelName), 7191)
	var auditedLog model.Log
	require.NoError(t, db.First(&auditedLog, log.Id).Error)
	auditedOther, err := common.StrToMap(auditedLog.Other)
	require.NoError(t, err)
	adminInfo, ok := auditedOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	protection, ok := adminInfo["cost_protection"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "surcharge", protection["action"])
	assert.Equal(t, true, protection["rerouted"])
	var gotUser model.User
	require.NoError(t, db.First(&gotUser, user.Id).Error)
	assert.Equal(t, 850, gotUser.Quota)
}

func TestCostProtectionSurchargeAdjustsBalancesAndLogOnce(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	user := model.User{Id: 7201, Username: "cost-user", Password: "password123", Quota: 900, UsedQuota: 100}
	token := model.Token{Id: 7202, UserId: user.Id, Key: "cost-token", Name: "cost-token", RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	createCostProtectionChannel(t, db, 7203, "default", "kimi-k3")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7203).Update("used_quota", 100).Error)
	model.InitChannelCache()
	log := model.Log{
		UserId: user.Id, Type: model.LogTypeConsume, ModelName: "kimi-k3",
		Quota: 100, ChannelId: 7203, TokenId: token.Id, Group: "default",
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source": BillingSourceWallet,
			"request_path":   "/v1/chat/completions",
		}),
	}
	require.NoError(t, db.Create(&log).Error)
	cost := UpstreamCostInfo{
		LogId: log.Id, UpstreamQuota: 150, UpstreamQuotaPerUnit: common.QuotaPerUnit,
		NormalizedUpstreamQuota: 150, UpstreamAmount: 0.0003, PlatformAmount: 0.0002,
		ExceedsPlatform: true,
	}

	applyCostProtection(context.Background(), cost)
	applyCostProtection(context.Background(), cost)

	var gotUser model.User
	var gotToken model.Token
	var gotChannel model.Channel
	var gotLog model.Log
	require.NoError(t, db.First(&gotUser, user.Id).Error)
	require.NoError(t, db.First(&gotToken, token.Id).Error)
	require.NoError(t, db.First(&gotChannel, 7203).Error)
	require.NoError(t, db.First(&gotLog, log.Id).Error)
	assert.Equal(t, 850, gotUser.Quota)
	assert.Equal(t, 150, gotUser.UsedQuota)
	assert.Equal(t, 850, gotToken.RemainQuota)
	assert.Equal(t, 150, gotToken.UsedQuota)
	assert.EqualValues(t, 150, gotChannel.UsedQuota)
	assert.Equal(t, 100, gotLog.Quota, "original request log keeps its own charge; the surcharge is a separate record")

	logOther, err := common.StrToMap(gotLog.Other)
	require.NoError(t, err)
	adminInfo, ok := logOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	protection, ok := adminInfo["cost_protection"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "surcharge", protection["action"])
	assert.Equal(t, float64(50), protection["delta_quota"])

	var surchargeLogs []model.Log
	require.NoError(t, db.Where("type = ? AND id <> ?", model.LogTypeConsume, log.Id).Find(&surchargeLogs).Error)
	require.Len(t, surchargeLogs, 1, "surcharge must show up in the usage log exactly once")
	assert.Equal(t, 50, surchargeLogs[0].Quota)
	assert.Equal(t, user.Id, surchargeLogs[0].UserId)
	assert.Equal(t, "kimi-k3", surchargeLogs[0].ModelName)
	assert.Empty(t, surchargeLogs[0].UpstreamRequestId, "surcharge rows must not be re-checked against upstream cost")
	surchargeOther, err := common.StrToMap(surchargeLogs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, true, surchargeOther["cost_protection_surcharge"])
	assert.Equal(t, float64(log.Id), surchargeOther["source_log_id"])
}

// Production channels are multi-key, so the upstream API key is resolved from the
// log's admin_info.multi_key_index rather than from Channel.Key directly. A break
// here silently disables cost protection for every multi-key upstream.
func TestCostProtectionResolvesUpstreamKeyForMultiKeyChannel(t *testing.T) {
	db := setupCostProtectionTestDB(t)
	InitHttpClient()
	setUpstreamCostBillingUnits(t, common.QuotaPerUnit, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, err := fmt.Fprintf(w, `{"success":true,"data":{"quota_per_unit":%f,"price":1}}`, common.QuotaPerUnit)
			assert.NoError(t, err)
		case "/api/log/token":
			assert.Equal(t, "Bearer second-key", r.Header.Get("Authorization"))
			_, err := fmt.Fprint(w, `{"success":true,"data":{"items":[{"request_id":"upstream-7211","quota":150}]}}`)
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	modelName := "cost-multikey-" + strings.ReplaceAll(t.Name(), "/", "-")
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 7211, Type: constant.ChannelTypeOpenAI, Key: "first-key\nsecond-key",
		Status: common.ChannelStatusEnabled, Name: "channel-7211", BaseURL: &server.URL,
		Weight: &weight, Models: modelName, Group: "default", Priority: &priority,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2},
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: modelName, ChannelId: 7211, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	model.InitChannelCache()
	log := model.Log{
		Type: model.LogTypeConsume, ModelName: modelName, Quota: 100,
		ChannelId: 7211, Group: "default", UpstreamRequestId: "upstream-7211",
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source": BillingSourceWallet,
			"admin_info":     map[string]interface{}{"is_multi_key": true, "multi_key_index": 1},
		}),
	}
	require.NoError(t, db.Create(&log).Error)

	costs, err := GetUpstreamCosts(context.Background(), []int{log.Id})
	require.NoError(t, err)
	require.Len(t, costs, 1)
	assert.True(t, costs[0].ExceedsPlatform)
}
