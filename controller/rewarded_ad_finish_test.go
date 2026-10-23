package controller

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFinishRewardedAdSessionHTTPPreservesOwnershipAndTerminalState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "finish.db")), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	require.NoError(t, db.AutoMigrate(&model.RewardedAdSession{}, &model.RewardedAdDay{}))
	now := time.Now()
	session := model.RewardedAdSession{ID: "owned-session", UserID: 1, Status: "pending", RewardQuota: 50, Day: model.RewardedAdDate(now), ExpiresAt: now.Unix() + 900}
	require.NoError(t, db.Create(&session).Error)
	for _, id := range []int{0, 1} {
		require.NoError(t, db.Create(&model.RewardedAdDay{Day: session.Day, UserID: id, Reserved: 1, ReservedQuota: 50}).Error)
	}
	router := gin.New()
	router.POST("/sessions/:id/finish", func(c *gin.Context) { c.Set("id", 1); FinishRewardedAdSession(c) })
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"missing reason", "owned-session", `{}`, 400},
		{"invalid reason", "owned-session", `{"reason":"credited"}`, 400},
		{"unknown session", "unknown", `{"reason":"closed"}`, 404},
		{"finish own session", "owned-session", `{"reason":"show_failed"}`, 200},
		{"retry is idempotent", "owned-session", `{"reason":"closed"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/sessions/"+tc.path+"/finish", bytes.NewBufferString(tc.body))
			router.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			if tc.status == 200 {
				var response struct {
					Success bool `json:"success"`
					Data    struct {
						Status string `json:"status"`
						Reason string `json:"reason"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
				assert.True(t, response.Success)
				assert.Equal(t, "expired", response.Data.Status)
				assert.Equal(t, "client_show_failed", response.Data.Reason)
			}
		})
	}
	// Knowing a foreign session ID must not release another account's reservation.
	require.NoError(t, db.Model(&session).Update("user_id", 2).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/sessions/owned-session/finish", bytes.NewBufferString(`{"reason":"closed"}`)))
	assert.Equal(t, 404, w.Code)
}
