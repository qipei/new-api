package controller

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
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

func TestCustomerServiceSavePublishValidateAndRemove(t *testing.T) {
	db := setupPhoneControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 16, 16))))
	config := customerServiceConfig{Phone: "13800138000", QRCodeDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes())}
	request := func(handler gin.HandlerFunc, value any) *httptest.ResponseRecorder {
		body, err := common.Marshal(value)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
		handler(c)
		c.Writer.WriteHeaderNow()
		return w
	}
	w := request(SaveCustomerServiceSettings, config)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	var stored model.Option
	require.NoError(t, db.First(&stored, "key = ?", customerServiceOption).Error)
	assert.Contains(t, stored.Value, config.Phone)
	public := request(GetMiniAppCustomerService, nil)
	assert.Contains(t, public.Body.String(), config.Phone)
	assert.Contains(t, public.Body.String(), "/api/miniapp/customer-service/qrcode?v=")
	assert.NotContains(t, public.Body.String(), "base64")
	imageResponse := request(GetMiniAppCustomerServiceQRCode, nil)
	assert.Equal(t, "image/png", imageResponse.Header().Get("Content-Type"))
	assert.Equal(t, pngData.Bytes(), imageResponse.Body.Bytes())
	cached := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(cached)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("If-None-Match", imageResponse.Header().Get("ETag"))
	GetMiniAppCustomerServiceQRCode(c)
	c.Writer.WriteHeaderNow()
	assert.Equal(t, http.StatusNotModified, cached.Code)
	for _, invalid := range []customerServiceConfig{
		{Phone: "not-a-number"},
		{QRCodeDataURL: "data:image/svg+xml;base64,PHN2Zz4="},
		{QRCodeDataURL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes())},
		{QRCodeDataURL: "data:image/png;base64," + strings.Repeat("A", 700000)},
		{QRCodeDataURL: "data:image/png;base64,YmFk"},
	} {
		assert.Equal(t, http.StatusBadRequest, request(SaveCustomerServiceSettings, invalid).Code)
		current, err := readCustomerServiceConfig()
		require.NoError(t, err)
		assert.Equal(t, config, current)
	}
	assert.Contains(t, request(SaveCustomerServiceSettings, customerServiceConfig{}).Body.String(), `"success":true`)
	assert.Contains(t, request(GetMiniAppCustomerService, nil).Body.String(), `"qrcode_url":""`)
	assert.Equal(t, http.StatusNotFound, request(GetMiniAppCustomerServiceQRCode, nil).Code)
	// A persistence error must not publish an uncommitted contact configuration.
	require.NoError(t, db.Migrator().DropTable(&model.Option{}))
	assert.Contains(t, request(SaveCustomerServiceSettings, config).Body.String(), `"success":false`)
	current, err := readCustomerServiceConfig()
	require.NoError(t, err)
	assert.Equal(t, customerServiceConfig{}, current)
}

func TestCustomerServiceImage500KBBoundary(t *testing.T) {
	db := setupPhoneControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 16, 16))))
	for _, size := range []int{500 * 1024, 500*1024 + 1} {
		data := append(pngData.Bytes(), make([]byte, size-pngData.Len())...)
		body, err := common.Marshal(customerServiceConfig{QRCodeDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
		SaveCustomerServiceSettings(c)
		if size == 500*1024 {
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), `"success":true`)
		} else {
			assert.Equal(t, http.StatusBadRequest, w.Code)
		}
	}
}

func TestCustomerServiceExcludedFromGeneralOptions(t *testing.T) {
	oldOptions := common.OptionMap
	raw, err := common.Marshal(customerServiceConfig{Phone: "13800138000", QRCodeDataURL: "data:image/png;base64," + strings.Repeat("A", 650000)})
	require.NoError(t, err)
	common.OptionMap = map[string]string{"SystemName": "test", customerServiceOption: string(raw)}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	GetOptions(c)
	var response struct {
		Data []model.Option `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	keys := make([]string, 0, len(response.Data))
	for _, option := range response.Data {
		keys = append(keys, option.Key)
	}
	assert.Contains(t, keys, "SystemName")
	assert.NotContains(t, keys, customerServiceOption)
	// The dedicated administrator endpoint must still return the complete config.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	GetCustomerServiceSettings(c)
	assert.Contains(t, w.Body.String(), "qrcode_data_url")
	assert.Contains(t, w.Body.String(), "13800138000")
}

func TestCustomerServiceImageTracksSyncedConfiguration(t *testing.T) {
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	request := func(handler gin.HandlerFunc, etag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.Header.Set("If-None-Match", etag)
		handler(c)
		c.Writer.WriteHeaderNow()
		return w
	}
	publish := func(data []byte) {
		raw, err := common.Marshal(customerServiceConfig{Phone: "13800138000", QRCodeDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)})
		require.NoError(t, err)
		common.OptionMapRWMutex.Lock()
		common.OptionMap[customerServiceOption] = string(raw)
		common.OptionMapRWMutex.Unlock()
	}
	var first, second bytes.Buffer
	require.NoError(t, png.Encode(&first, image.NewRGBA(image.Rect(0, 0, 16, 16))))
	require.NoError(t, png.Encode(&second, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	publish(first.Bytes())
	initial := request(GetMiniAppCustomerServiceQRCode, "")
	require.Equal(t, http.StatusOK, initial.Code)
	assert.Equal(t, first.Bytes(), initial.Body.Bytes())
	initialURL := request(GetMiniAppCustomerService, "").Body.String()
	etag := initial.Header().Get("ETag")
	require.NotEmpty(t, etag)
	assert.Equal(t, http.StatusNotModified, request(GetMiniAppCustomerServiceQRCode, etag).Code)
	// Simulate configuration sync from a different node, without a local PUT.
	publish(second.Bytes())
	replaced := request(GetMiniAppCustomerServiceQRCode, etag)
	require.Equal(t, http.StatusOK, replaced.Code)
	assert.Equal(t, second.Bytes(), replaced.Body.Bytes())
	assert.NotEqual(t, etag, replaced.Header().Get("ETag"))
	assert.NotEqual(t, initialURL, request(GetMiniAppCustomerService, "").Body.String())
	// Invalid legacy data must not be served or retain the preceding image.
	publish([]byte("invalid png"))
	assert.Equal(t, http.StatusNotFound, request(GetMiniAppCustomerServiceQRCode, "").Code)
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, customerServiceOption)
	common.OptionMapRWMutex.Unlock()
	assert.Equal(t, http.StatusNotFound, request(GetMiniAppCustomerServiceQRCode, etag).Code)
	assert.Contains(t, request(GetMiniAppCustomerService, "").Body.String(), `"qrcode_url":""`)
}
