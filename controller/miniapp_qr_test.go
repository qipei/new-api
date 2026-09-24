package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiniAppQRCodeSettingsPublishAndDisable(t *testing.T) {
	db := setupPhoneControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previous := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previous })
	request := func(handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		handler(c)
		return response
	}
	initial := request(GetMiniAppQRCode, "")
	assert.Contains(t, initial.Body.String(), `"enabled":true`)
	assert.Contains(t, initial.Body.String(), `"image_url":"/miniapp-code.jpg"`)
	assert.Equal(t, "no-store", initial.Header().Get("Cache-Control"))
	for _, body := range []string{
		`{"enabled":true,"image_url":"https://example.com/code.png"}`,
		`{"enabled":false,"image_url":"/qr/custom.png"}`,
	} {
		saved := request(SaveMiniAppQRCodeSettings, body)
		require.Equal(t, http.StatusOK, saved.Code)
		require.Contains(t, saved.Body.String(), `"success":true`)
		assert.JSONEq(t, saved.Body.String(), request(GetMiniAppQRCode, "").Body.String())
		var stored model.Option
		require.NoError(t, db.First(&stored, "key = ?", miniAppQRCodeOption).Error)
		assert.JSONEq(t, body, stored.Value)
	}
	// Failed persistence cannot change public settings.
	require.NoError(t, db.Migrator().DropTable(&model.Option{}))
	failed := request(SaveMiniAppQRCodeSettings, `{"enabled":true,"image_url":"/new.png"}`)
	assert.Contains(t, failed.Body.String(), `"success":false`)
	assert.Contains(t, request(GetMiniAppQRCode, "").Body.String(), `"enabled":false`)
}

func TestMiniAppQRCodeSettingsRejectInvalidInputs(t *testing.T) {
	for _, body := range []string{
		`{"image_url":"/qr.png"}`, `{"enabled":true}`, `{"enabled":true,"image_url":"javascript:alert(1)"}`,
		`{"enabled":true,"image_url":"data:image/png;base64,YQ=="}`, `{"enabled":true,"image_url":"//example.com/code.png"}`,
		`{"enabled":true,"image_url":"https://user:pass@example.com/code.png"}`, `{"enabled":true,"image_url":"/\\example.com/code.png"}`,
		`{"enabled":true,"image_url":"` + strings.Repeat("a", 2049) + `"}`,
	} {
		t.Run(body[:min(len(body), 80)], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
			SaveMiniAppQRCodeSettings(c)
			assert.Equal(t, http.StatusBadRequest, c.Writer.Status())
		})
	}
}
