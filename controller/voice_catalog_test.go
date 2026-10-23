package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/voice_catalog"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVoiceCatalogControllerTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSettings := *voice_catalog.GetSettings()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SystemTask{}, &model.VoiceCatalogEntry{}))
	model.DB, model.LOG_DB = db, db
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		*voice_catalog.GetSettings() = oldSettings
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, sqlDB.Close())
	})

	engine := gin.New()
	engine.GET("/voice-catalog", GetVoiceCatalogSettings)
	engine.PUT("/voice-catalog", SaveVoiceCatalogSettings)
	engine.GET("/options", GetOptions)
	return engine
}

func serveVoiceCatalog(t *testing.T, engine *gin.Engine, method, path, body string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

// 密钥只写不读：任何接口都不能把 AK/SK 返回给前端；留空表示保持原值。
func TestVoiceCatalogSettingsNeverExposeCredentials(t *testing.T) {
	engine := setupVoiceCatalogControllerTest(t)

	code, body := serveVoiceCatalog(t, engine, http.MethodPut, "/voice-catalog",
		`{"volc_access_key_id":"AKLTsecretid","volc_secret_access_key":"TOPSECRET","volc_resource_ids":["seed-tts-2.0"," seed-tts-1.0 "],"sync_interval_hours":12}`)
	require.Equal(t, http.StatusOK, code, body)
	settings := voice_catalog.GetSettings()
	assert.Equal(t, "AKLTsecretid", settings.VolcAccessKeyID)
	assert.Equal(t, "TOPSECRET", settings.VolcSecretAccessKey)
	assert.Equal(t, []string{"seed-tts-2.0", "seed-tts-1.0"}, settings.ResourceIDs())
	assert.Equal(t, 12, settings.SyncIntervalHoursOrDefault())

	for _, path := range []string{"/voice-catalog", "/options"} {
		_, body := serveVoiceCatalog(t, engine, http.MethodGet, path, "")
		assert.NotContains(t, body, "AKLTsecretid", path)
		assert.NotContains(t, body, "TOPSECRET", path)
	}
	_, body = serveVoiceCatalog(t, engine, http.MethodGet, "/voice-catalog", "")
	assert.Contains(t, body, `"volc_secret_access_key_configured":true`)

	code, _ = serveVoiceCatalog(t, engine, http.MethodPut, "/voice-catalog",
		`{"volc_access_key_id":"","volc_secret_access_key":"","volc_resource_ids":["seed-tts-2.0"],"sync_interval_hours":24}`)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "TOPSECRET", settings.VolcSecretAccessKey, "blank secret keeps the saved value")

	code, _ = serveVoiceCatalog(t, engine, http.MethodPut, "/voice-catalog",
		`{"clear_volc_credentials":true,"volc_resource_ids":["seed-tts-2.0"],"sync_interval_hours":24}`)
	require.Equal(t, http.StatusOK, code)
	assert.False(t, settings.VolcConfigured())
}

func TestVoiceCatalogSettingsValidation(t *testing.T) {
	engine := setupVoiceCatalogControllerTest(t)
	for _, body := range []string{
		`{"volc_resource_ids":["seed-tts-2.0"],"sync_interval_hours":0}`,
		`{"volc_resource_ids":["seed-tts-2.0"],"sync_interval_hours":169}`,
		`{"volc_resource_ids":[" "],"sync_interval_hours":24}`,
	} {
		code, _ := serveVoiceCatalog(t, engine, http.MethodPut, "/voice-catalog", body)
		assert.Equal(t, http.StatusBadRequest, code, body)
	}
}
