package hailuo

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 提交响应里拿到任务号才算提交成功；200 响应缺 task_id 时按失败处理，不能留下无法轮询的任务。
func TestDoResponseRequiresTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		body       string
		wantTaskID string
		wantErr    string
	}{
		{
			name:       "v2 submit",
			body:       `{"task_id":"99936297"}`,
			wantTaskID: "99936297",
		},
		{
			name:       "legacy submit",
			body:       `{"task_id":"106916112212032","base_resp":{"status_code":0,"status_msg":"success"}}`,
			wantTaskID: "106916112212032",
		},
		{
			name:    "v2 error body with http 200",
			body:    `{"type":"error","error":{"type":"bad_request_error","message":"缺少 resolution"}}`,
			wantErr: "task_id is empty",
		},
		{
			name:    "legacy success code without task id",
			body:    `{"base_resp":{"status_code":0,"status_msg":"success"}}`,
			wantErr: "task_id is empty",
		},
		{
			name:    "legacy error code",
			body:    `{"base_resp":{"status_code":1004,"status_msg":"authorization failed"}}`,
			wantErr: "authorization failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(tt.body))}
			info := &relaycommon.RelayInfo{
				OriginModelName: "MiniMax-H3",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
			}

			taskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
			if tt.wantErr != "" {
				require.NotNil(t, taskErr)
				assert.Contains(t, taskErr.Message, tt.wantErr)
				assert.Empty(t, taskID)
				assert.Empty(t, recorder.Body.String(), "nothing is written to the client on failure")
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, tt.wantTaskID, taskID)
			assert.Contains(t, recorder.Body.String(), "task_public")
		})
	}
}
