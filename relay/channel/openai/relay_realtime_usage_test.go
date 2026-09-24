package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func realtimeUsageSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	peer := <-accepted
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(5*time.Second)))
	return client, peer
}

func TestRealtimeUsageFinalOutcomeOnSocketClose(t *testing.T) {
	t.Setenv("USER_USAGE_SPOOL_DIR", t.TempDir())
	oldDB, oldLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.UserUsageRequest{}, &model.UserUsageTracking{}))
	model.DB, model.LOG_DB = db, db
	require.NoError(t, model.StartUserUsageWriter())
	t.Cleanup(func() {
		require.NoError(t, model.StopUserUsageWriter(context.Background()))
		model.DB, model.LOG_DB = oldDB, oldLogDB
		_ = sqlDB.Close()
	})
	for _, tc := range []struct {
		name, outcome string
		normal        bool
	}{{"abnormal", "failed", false}, {"normal", "success", true}} {
		t.Run(tc.name, func(t *testing.T) {
			clientConn, _ := realtimeUsageSocketPair(t)
			targetConn, targetPeer := realtimeUsageSocketPair(t)
			if tc.normal {
				require.NoError(t, targetPeer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			} else {
				require.NoError(t, targetPeer.Close())
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			c.Set("id", 1)
			recorder := model.BeginUserUsage(c)
			apiErr, usage := OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{ClientWs: clientConn, TargetWs: targetConn})
			// Observation must not change the existing websocket handler response.
			assert.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.NoError(t, recorder.Finish(c, time.Now()))
			require.NoError(t, model.FlushUserUsage(context.Background()))
			var row model.UserUsageRequest
			require.NoError(t, db.Order("id DESC").First(&row).Error)
			assert.Equal(t, tc.outcome, row.Outcome)
		})
	}
}
