package controller

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func imageUploadFixture(t *testing.T) object_storage.Config {
	t.Helper()
	cfg := object_storage.Defaults()
	cfg.Enabled = true
	cfg.Endpoint = "https://oss-cn-hangzhou.aliyuncs.com"
	cfg.Region = "cn-hangzhou"
	cfg.Bucket = "test-images"
	cfg.AccessKeyID = "fixture-id"
	cfg.AccessKeySecret = "fixture-secret"
	encoded, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	old, exists := common.OptionMap[object_storage.OptionKey]
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	upload, reserve, record := uploadImageObject, reserveImageUpload, recordImageUpload
	requestBudget := reserveImageUploadRequest
	reserveImageUploadRequest = func(context.Context, int, object_storage.Config) (int, error) { return 0, nil }
	reserveImageUpload = func(context.Context, int, int64, object_storage.Config) (int, error) { return 0, nil }
	recordImageUpload = func(*gin.Context, string, int64) {}
	t.Cleanup(func() {
		reserveImageUploadRequest = requestBudget
		uploadImageObject = upload
		reserveImageUpload = reserve
		recordImageUpload = record
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if exists {
			common.OptionMap[object_storage.OptionKey] = old
		} else {
			delete(common.OptionMap, object_storage.OptionKey)
		}
	})
	return cfg
}

func imageUploadRequest(t *testing.T, field string, data []byte) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "photo.jpg")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads/images", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, httptest.NewRecorder()
}

func TestUploadImageContract(t *testing.T) {
	cfg := imageUploadFixture(t)
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	require.NoError(t, png.Encode(&data, img))
	uploadImageObject = func(_ context.Context, got object_storage.Config, key, mime string, body io.Reader, size int64) (string, error) {
		assert.Equal(t, cfg.Bucket, got.Bucket)
		assert.True(t, strings.HasPrefix(key, "uploads/images/"))
		assert.True(t, strings.HasSuffix(key, ".png"))
		assert.Equal(t, "image/png", mime)
		copied, err := io.ReadAll(body)
		require.NoError(t, err)
		assert.Equal(t, data.Bytes(), copied)
		assert.Equal(t, int64(data.Len()), size)
		return "https://files.example.com/image.png?signature=test", nil
	}
	req, w := imageUploadRequest(t, "file", data.Bytes())
	r := gin.New()
	r.POST("/v1/uploads/images", func(c *gin.Context) { c.Set("id", 42) }, UploadImage)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	assert.Len(t, response, 5)
	assert.Equal(t, "photo.jpg", response["filename"])
	assert.Equal(t, "image/png", response["content_type"])
	assert.Equal(t, float64(data.Len()), response["bytes"])
	assert.NotEmpty(t, response["url"])
	assert.Greater(t, response["created_at"].(float64), float64(0))
}

func TestUploadImageRejectsWithoutCallingOSS(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		data        []byte
		status      int
	}{
		{"missing file", "other", []byte("x"), 400},
		{"fake extension", "file", []byte("<svg></svg>"), 400},
		{"oversize", "file", bytes.Repeat([]byte("x"), (20<<20)+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageUploadFixture(t)
			uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
				t.Fatal("OSS must not be called")
				return "", nil
			}
			req, w := imageUploadRequest(t, tc.field, tc.data)
			r := gin.New()
			r.POST("/v1/uploads/images", UploadImage)
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			assert.Contains(t, w.Body.String(), "invalid_request_error")
		})
	}
}

func TestUploadImageRateLimitAndStorageFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		fail   bool
		status int
	}{{"rpm", 1, false, 429}, {"daily bytes", 2, false, 429}, {"OSS failure", 0, true, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			imageUploadFixture(t)
			var data bytes.Buffer
			require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))))
			reserveImageUpload = func(context.Context, int, int64, object_storage.Config) (int, error) { return tc.code, nil }
			uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
				require.True(t, tc.fail)
				return "", fmt.Errorf("sensitive upstream host and credentials")
			}
			req, w := imageUploadRequest(t, "file", data.Bytes())
			r := gin.New()
			r.POST("/v1/uploads/images", UploadImage)
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			assert.NotContains(t, w.Body.String(), "sensitive upstream")
			if tc.status == 429 {
				assert.Equal(t, "60", w.Header().Get("Retry-After"))
			}
		})
	}
}

func TestObjectStorageSecretsHiddenAndGenericUpdateBlocked(t *testing.T) {
	imageUploadFixture(t)
	r := gin.New()
	r.GET("/storage", GetObjectStorageSettings)
	r.PUT("/option", UpdateOption)
	r.GET("/option", GetOptions)
	for _, url := range []string{"/storage", "/option"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		require.Equal(t, 200, w.Code)
		assert.NotContains(t, w.Body.String(), "fixture-secret")
		assert.NotContains(t, w.Body.String(), "fixture-id")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/option", strings.NewReader(`{"key":"object_storage_config","value":"{}"}`)))
	assert.Contains(t, w.Body.String(), `"success":false`)
}

func TestUploadDisabledDoesNotReadBody(t *testing.T) {
	imageUploadFixture(t)
	cfg := object_storage.Defaults()
	encoded, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	r := gin.New()
	r.POST("/upload", UploadImage)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/upload", strings.NewReader("invalid multipart")))
	assert.Equal(t, 503, w.Code)
	assert.Contains(t, w.Body.String(), "disabled")
}

func TestStorageSavePreservesBlankCredentialsAndPersistsAtomically(t *testing.T) {
	original := imageUploadFixture(t)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Log{}, &model.User{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB; require.NoError(t, sqlDB.Close()) })
	request := original
	request.AccessKeyID = ""
	request.AccessKeySecret = ""
	request.MaxFileMB = 10
	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	r := gin.New()
	r.PUT("/storage", SaveObjectStorageSettings)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/storage", bytes.NewReader(encoded)))
	require.Equal(t, 200, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"success":true`)
	saved, err := object_storage.Get()
	require.NoError(t, err)
	assert.Equal(t, original.AccessKeyID, saved.AccessKeyID)
	assert.Equal(t, original.AccessKeySecret, saved.AccessKeySecret)
	assert.Equal(t, 10, saved.MaxFileMB)
	var option model.Option
	require.NoError(t, db.Where("key = ?", object_storage.OptionKey).First(&option).Error)
	var persisted object_storage.Config
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &persisted))
	assert.Equal(t, saved, persisted)
	request.MaxFileMB = 999
	encoded, err = common.Marshal(request)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/storage", bytes.NewReader(encoded)))
	assert.Equal(t, 400, w.Code)
	unchanged, err := object_storage.Get()
	require.NoError(t, err)
	assert.Equal(t, saved, unchanged)
}

func TestUploadConcurrencyLimitRejectsExtraRequestAndReleasesSlots(t *testing.T) {
	cfg := imageUploadFixture(t)
	cfg.MaxConcurrentUploads = 1
	encoded, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	uploadImageObject = func(_ context.Context, _ object_storage.Config, _, _ string, _ io.Reader, _ int64) (string, error) {
		close(started)
		<-release
		return "https://files.example.com/x", nil
	}
	r := gin.New()
	r.POST("/upload", UploadImage)
	r.GET("/model", func(c *gin.Context) { c.Status(200) })
	first, w := imageUploadRequest(t, "file", data.Bytes())
	first.URL.Path = "/upload"
	go func() { defer close(finished); r.ServeHTTP(w, first) }()
	<-started
	second, busy := imageUploadRequest(t, "file", data.Bytes())
	second.URL.Path = "/upload"
	r.ServeHTTP(busy, second)
	assert.Equal(t, 429, busy.Code)
	modelResponse := httptest.NewRecorder()
	r.ServeHTTP(modelResponse, httptest.NewRequest("GET", "/model", nil))
	assert.Equal(t, 200, modelResponse.Code)
	close(release)
	<-finished
	assert.Equal(t, 200, w.Code)
	uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
		return "https://files.example.com/x", nil
	}
	third, available := imageUploadRequest(t, "file", data.Bytes())
	third.URL.Path = "/upload"
	r.ServeHTTP(available, third)
	assert.Equal(t, 200, available.Code)
}

func TestUploadRequestRateLimitRejectsBeforeReadingBody(t *testing.T) {
	imageUploadFixture(t)
	reserveImageUploadRequest = func(context.Context, int, object_storage.Config) (int, error) { return 1, nil }
	r := gin.New()
	r.POST("/upload", UploadImage)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/upload", strings.NewReader("not multipart")))
	assert.Equal(t, 429, w.Code)
	assert.Contains(t, w.Body.String(), "rate_limit_error")
}
