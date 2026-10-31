package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestObjectStorageSaveWriteFailurePreservesConfiguration(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "insert"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			original := imageUploadFixture(t)
			redisEnabled := common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() { common.RedisEnabled = redisEnabled })
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Log{}, &model.User{}))
			oldDB, oldLogDB := model.DB, model.LOG_DB
			model.DB, model.LOG_DB = db, db
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLogDB
				require.NoError(t, sqlDB.Close())
			})
			encodedOriginal, err := common.Marshal(original)
			require.NoError(t, err)
			if existing {
				require.NoError(t, db.Create(&model.Option{Key: object_storage.OptionKey, Value: string(encodedOriginal)}).Error)
			}
			require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
			changed := original
			changed.Enabled = false
			changed.MaxFileMB = 10
			encoded, err := common.Marshal(changed)
			require.NoError(t, err)
			router := gin.New()
			router.PUT("/storage", SaveObjectStorageSettings)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/storage", bytes.NewReader(encoded)))
			var result struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success, response.Body.String())
			current, err := object_storage.Get()
			require.NoError(t, err)
			assert.Equal(t, original, current, "failed persistence must not publish new settings")
			var persisted []model.Option
			require.NoError(t, db.Where("key = ?", object_storage.OptionKey).Find(&persisted).Error)
			if existing {
				require.Len(t, persisted, 1)
				assert.Equal(t, string(encodedOriginal), persisted[0].Value)
			} else {
				assert.Empty(t, persisted)
			}
		})
	}
}
