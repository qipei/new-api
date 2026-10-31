package controller

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadImageRejectsAdditionalMultipartFields(t *testing.T) {
	for _, field := range []string{"file", "model"} {
		t.Run(field, func(t *testing.T) {
			imageUploadFixture(t)
			var imageData bytes.Buffer
			require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			first, err := writer.CreateFormFile("file", "first.png")
			require.NoError(t, err)
			_, err = first.Write(imageData.Bytes())
			require.NoError(t, err)
			if field == "file" {
				second, err := writer.CreateFormFile("file", "second.png")
				require.NoError(t, err)
				_, err = second.Write(imageData.Bytes())
				require.NoError(t, err)
			} else {
				require.NoError(t, writer.WriteField("model", "unused"))
			}
			require.NoError(t, writer.Close())
			stored := false
			uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
				stored = true
				return "https://files.example.com/image.png", nil
			}
			request := httptest.NewRequest("POST", "/upload", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			router := gin.New()
			router.POST("/upload", UploadImage)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, 400, response.Code)
			assert.False(t, stored, "invalid multi-part requests must never upload their first file")
		})
	}
}

func TestUploadTenConcurrentRequestsRespectLoweredLimitAndReleaseSlots(t *testing.T) {
	cfg := imageUploadFixture(t)
	cfg.MaxConcurrentUploads = 10
	encoded, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()

	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	entered := make(chan struct{}, 12)
	releaseOne := make(chan struct{})
	releaseAll := make(chan struct{})
	results := make(chan int, 12)
	var workers sync.WaitGroup
	t.Cleanup(func() { close(releaseAll); workers.Wait() })
	uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
		entered <- struct{}{}
		select {
		case <-releaseOne:
		case <-releaseAll:
		}
		return "https://files.example.com/result.png", nil
	}
	router := gin.New()
	router.POST("/v1/uploads/images", UploadImage)
	for i := 0; i < 10; i++ {
		request, response := imageUploadRequest(t, "file", imageData.Bytes())
		workers.Add(1)
		go func() {
			defer workers.Done()
			router.ServeHTTP(response, request)
			results <- response.Code
		}()
	}
	for i := 0; i < 10; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("all ten uploads must reach object storage")
		}
	}
	extra, busy := imageUploadRequest(t, "file", imageData.Bytes())
	router.ServeHTTP(busy, extra)
	require.Equal(t, 429, busy.Code)
	assert.Equal(t, "60", busy.Header().Get("Retry-After"))
	assert.Contains(t, busy.Body.String(), "Upload service busy")

	cfg.MaxConcurrentUploads = 5
	encoded, err = common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	// Lowering the cap must not cancel existing requests; five completions leave
	// exactly five active uploads, so another request must still be rejected.
	for i := 0; i < 5; i++ {
		releaseOne <- struct{}{}
		require.Equal(t, 200, <-results)
	}
	extra, busy = imageUploadRequest(t, "file", imageData.Bytes())
	router.ServeHTTP(busy, extra)
	require.Equal(t, 429, busy.Code)

	releaseOne <- struct{}{}
	require.Equal(t, 200, <-results)
	request, available := imageUploadRequest(t, "file", imageData.Bytes())
	workers.Add(1)
	go func() {
		defer workers.Done()
		router.ServeHTTP(available, request)
		results <- available.Code
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("capacity must recover when active uploads fall below the new cap")
	}
	// Complete the four older uploads plus the newly admitted fifth upload.
	for i := 0; i < 5; i++ {
		releaseOne <- struct{}{}
		require.Equal(t, 200, <-results)
	}
	workers.Wait()
}

func TestUploadImageRejectsOversizedMultipartHeadersBeforeOSS(t *testing.T) {
	imageUploadFixture(t)
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	boundary := "header-limit-review"
	body := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"image.png\"\r\nX-Padding: " + strings.Repeat("x", 128<<10) + "\r\n\r\n" + imageData.String() + "\r\n--" + boundary + "--\r\n"
	stored := false
	uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
		stored = true
		return "https://files.example.com/image.png", nil
	}
	request := httptest.NewRequest("POST", "/v1/uploads/images", strings.NewReader(body))
	request.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	router := gin.New()
	router.POST("/v1/uploads/images", UploadImage)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, 400, response.Code)
	assert.False(t, stored)
}

func TestUploadRejectsIncompleteInvalidMultipartWithoutWaitingForBody(t *testing.T) {
	imageUploadFixture(t)
	router := gin.New()
	router.POST("/v1/uploads/images", UploadImage)
	server := httptest.NewServer(router)
	defer server.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err = io.WriteString(connection, "POST /v1/uploads/images HTTP/1.1\r\nHost: localhost\r\nContent-Type: multipart/form-data; boundary=slow\r\nContent-Length: 1024\r\n\r\n--slow\r\nContent-Disposition: form-data; name=\"other\"\r\n\r\n")
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	require.NoError(t, err, "an invalid upload must receive its rejection even when the client withholds the remaining body")
	defer response.Body.Close()
	assert.Equal(t, 400, response.StatusCode)
	assert.True(t, response.Close, "a rejected unread HTTP/1 upload must not be reused for a model request")
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "expected file")
}

type unfinishedUploadBody struct {
	data    *bytes.Reader
	endRead chan struct{}
	finish  chan struct{}
	once    sync.Once
}

func (b *unfinishedUploadBody) Read(p []byte) (int, error) {
	if b.data.Len() > 0 {
		return b.data.Read(p)
	}
	b.once.Do(func() { close(b.endRead) })
	<-b.finish
	return 0, io.EOF
}
func (b *unfinishedUploadBody) Close() error { return nil }

func TestUploadDoesNotStoreBeforeHTTPBodyFinishes(t *testing.T) {
	imageUploadFixture(t)
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	request, response := imageUploadRequest(t, "file", imageData.Bytes())
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	pending := &unfinishedUploadBody{data: bytes.NewReader(body), endRead: make(chan struct{}), finish: make(chan struct{})}
	request.Body = pending
	request.ContentLength = -1
	stored := make(chan struct{}, 1)
	uploadImageObject = func(context.Context, object_storage.Config, string, string, io.Reader, int64) (string, error) {
		stored <- struct{}{}
		return "https://files.example.com/result.png", nil
	}
	router := gin.New()
	router.POST("/v1/uploads/images", UploadImage)
	finished := make(chan struct{})
	go func() { defer close(finished); router.ServeHTTP(response, request) }()
	defer func() { close(pending.finish); <-finished; assert.Equal(t, 200, response.Code) }()
	select {
	case <-pending.endRead:
		// The MIME closing delimiter is not the end of the HTTP request body.
	case <-stored:
		t.Fatal("object storage must wait for the HTTP body to finish")
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach its HTTP body boundary")
	}
}

func TestUploadAcceptsLargeSingleImage(t *testing.T) {
	imageUploadFixture(t)
	var imageData bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	require.NoError(t, encoder.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1024, 1024))))
	require.Greater(t, imageData.Len(), 1<<20)
	uploadImageObject = func(_ context.Context, _ object_storage.Config, _, _ string, body io.Reader, size int64) (string, error) {
		copied, err := io.ReadAll(body)
		require.NoError(t, err)
		assert.Equal(t, imageData.Bytes(), copied)
		assert.Equal(t, int64(imageData.Len()), size)
		return "https://files.example.com/large.png", nil
	}
	request, response := imageUploadRequest(t, "file", imageData.Bytes())
	router := gin.New()
	router.POST("/v1/uploads/images", UploadImage)
	router.ServeHTTP(response, request)
	assert.Equal(t, 200, response.Code)
}
