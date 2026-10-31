package controller

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

var objectStorageSaveMutex sync.Mutex

// Limit disk I/O, hashing and open uploads independently of model request concurrency.
var activeImageUploads struct {
	sync.Mutex
	count int
}

func GetObjectStorageSettings(c *gin.Context) {
	cfg, err := object_storage.Get()
	if err != nil {
		common.ApiErrorMsg(c, "无法读取文件存储配置")
		return
	}
	configured := cfg.Configured()
	cfg.AccessKeyID = ""
	cfg.AccessKeySecret = ""
	common.ApiSuccess(c, struct {
		object_storage.Config
		CredentialsConfigured bool `json:"credentials_configured"`
	}{Config: cfg, CredentialsConfigured: configured})
}

func SaveObjectStorageSettings(c *gin.Context) {
	var cfg object_storage.Config
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := common.DecodeJson(c.Request.Body, &cfg); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "无效的文件存储配置"})
		return
	}
	objectStorageSaveMutex.Lock()
	defer objectStorageSaveMutex.Unlock()
	current, err := object_storage.Get()
	if err != nil {
		common.ApiErrorMsg(c, "无法读取现有存储配置")
		return
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		cfg.AccessKeyID = current.AccessKeyID
	}
	if strings.TrimSpace(cfg.AccessKeySecret) == "" {
		cfg.AccessKeySecret = current.AccessKeySecret
	}
	if err = cfg.Validate(); err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	encoded, err := common.Marshal(cfg)
	if err != nil {
		common.ApiErrorMsg(c, "无法保存文件存储配置")
		return
	}
	if err = model.UpdateOptionsBulk(map[string]string{object_storage.OptionKey: string(encoded)}); err != nil {
		common.ApiErrorMsg(c, "无法保存文件存储配置")
		return
	}
	recordManageAudit(c, "option.update", map[string]interface{}{"key": object_storage.OptionKey})
	GetObjectStorageSettings(c)
}

func TestObjectStorage(c *gin.Context)           { checkObjectStorage(c, false) }
func CheckObjectStorageLifecycle(c *gin.Context) { checkObjectStorage(c, true) }

func checkObjectStorage(c *gin.Context, lifecycle bool) {
	cfg, err := object_storage.Get()
	if err != nil {
		common.ApiErrorMsg(c, "无法读取文件存储配置")
		return
	}
	if err = cfg.Validate(); err != nil || !cfg.Configured() || cfg.Endpoint == "" || cfg.Region == "" || cfg.Bucket == "" {
		common.ApiErrorMsg(c, "请先保存完整的 OSS 配置")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	store := service.NewImageObjectStorage(cfg)
	if lifecycle {
		err = store.CheckLifecycle(ctx)
	} else {
		err = store.Test(ctx)
	}
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

type uploadedImage struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Bytes       int64  `json:"bytes"`
	CreatedAt   int64  `json:"created_at"`
}

// Boundary functions allow contract tests without external OSS or real credentials.
var uploadImageObject = func(ctx context.Context, cfg object_storage.Config, key, mime string, body io.Reader, size int64) (string, error) {
	return service.NewImageObjectStorage(cfg).Upload(ctx, key, mime, body, size)
}
var reserveImageUpload = service.ReserveImageUploadBytes
var reserveImageUploadRequest = service.ReserveImageUploadRequest

// imageUploadUserHasBalance 只有付费用户能上传：钱包余额大于 0，或有生效中的订阅。
// 上传本身不扣额度，没有这道门槛时零余额的新账号也能把接口当免费图床用。
var imageUploadUserHasBalance = func(userID int) (bool, error) {
	quota, err := model.GetUserQuota(userID, false)
	if err != nil {
		return false, err
	}
	if quota > 0 {
		return true, nil
	}
	return model.HasActiveUserSubscription(userID)
}
var recordImageUpload = func(c *gin.Context, key string, size int64) {
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeSystem, "上传临时图片", map[string]interface{}{"object_key": key, "bytes": size, "token_id": common.GetContextKeyInt(c, constant.ContextKeyTokenId)})
}

func imageUploadError(c *gin.Context, status int, message string) {
	// HTTP/1 otherwise drains an unread request body after the handler returns.
	// Reject slow/invalid uploads without leaving a connection waiting for more bytes.
	if c.Request.ProtoMajor == 1 {
		c.Header("Connection", "close")
		_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now())
	}
	typ := "invalid_request_error"
	if status == 429 {
		typ = "rate_limit_error"
	} else if status >= 500 {
		typ = "server_error"
	}
	detail := gin.H{"message": message, "type": typ}
	if status == 429 {
		detail["code"] = 429
		c.Header("Retry-After", "60")
	}
	if status == http.StatusForbidden {
		detail["code"] = "insufficient_user_quota"
	}
	c.JSON(status, gin.H{"error": detail})
}

func UploadImage(c *gin.Context) {
	cfg, err := object_storage.Get()
	if err != nil {
		imageUploadError(c, 503, "image uploads are unavailable")
		return
	}
	if !cfg.Enabled {
		imageUploadError(c, 503, "image uploads are disabled")
		return
	}
	if err = cfg.Validate(); err != nil {
		imageUploadError(c, 503, "image upload storage is not configured")
		return
	}
	// 在占用每分钟上传次数之前检查，没有余额的账号被拒不消耗次数。
	hasBalance, err := imageUploadUserHasBalance(c.GetInt("id"))
	if err != nil {
		imageUploadError(c, 503, "image uploads are unavailable")
		return
	}
	if !hasBalance {
		imageUploadError(c, http.StatusForbidden, "image uploads require a positive account balance or an active subscription; please top up and try again")
		return
	}
	code, err := reserveImageUploadRequest(c.Request.Context(), c.GetInt("id"), cfg)
	if err != nil {
		imageUploadError(c, 503, "upload rate limit service unavailable")
		return
	}
	if code != 0 {
		imageUploadError(c, 429, "Rate limit exceeded. Please try again later")
		return
	}
	activeImageUploads.Lock()
	if activeImageUploads.count >= cfg.MaxConcurrentUploads {
		activeImageUploads.Unlock()
		imageUploadError(c, 429, "Upload service busy. Please try again later")
		return
	}
	activeImageUploads.count++
	activeImageUploads.Unlock()
	defer func() { activeImageUploads.Lock(); activeImageUploads.count--; activeImageUploads.Unlock() }()

	if encoding := c.GetHeader("Content-Encoding"); encoding != "" && encoding != "identity" {
		imageUploadError(c, 400, "compressed upload requests are not supported")
		return
	}
	// A slow upload cannot hold a slot indefinitely. Reset the HTTP/1 connection deadline
	// after parsing so keep-alive model requests on the same connection remain unaffected.
	responseController := http.NewResponseController(c.Writer)
	_ = responseController.SetReadDeadline(time.Now().Add(60 * time.Second))
	defer func() {
		if c.Request.ProtoMajor == 1 && c.Writer.Status() >= 400 {
			return // Preserve the rejection deadline until net/http closes the body.
		}
		_ = responseController.SetReadDeadline(time.Time{})
	}()
	maxBytes := int64(cfg.MaxFileMB) << 20
	boundedBody := http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(64<<10))
	c.Request.Body = boundedBody
	// The standard multipart parser permits 10 MiB of MIME headers per part.
	// Bound its initial reads separately so a tiny image cannot allocate that much.
	headerBudget := &io.LimitedReader{R: c.Request.Body, N: 64 << 10}
	c.Request.Body = struct {
		io.Reader
		io.Closer
	}{headerBudget, c.Request.Body}
	reader, err := c.Request.MultipartReader()
	if err != nil {
		imageUploadError(c, 400, "missing or invalid file field: multipart/form-data required")
		return
	}
	var file *os.File
	var filename string
	var fileSize int64
	part, partErr := reader.NextPart()
	if partErr != nil {
		imageUploadError(c, 400, "missing or invalid file field: http: no such file")
		return
	}
	if part.FormName() != "file" || part.FileName() == "" {
		// Form parameters are not part of this API; only a file field is accepted.
		imageUploadError(c, 400, "missing or invalid file field: expected file")
		return
	}
	headerBudget.N += maxBytes
	filename = part.FileName()
	file, err = service.CreateImageUploadTemp()
	if err != nil {
		imageUploadError(c, 503, "upload temporary storage unavailable")
		return
	}
	defer service.ReleaseImageUploadTemp(file)
	fileSize, err = io.Copy(file, io.LimitReader(part, maxBytes+1))
	var tooLarge *http.MaxBytesError
	if fileSize > maxBytes || errors.As(err, &tooLarge) {
		imageUploadError(c, 413, fmt.Sprintf("file exceeds maximum %d bytes", maxBytes))
		return
	}
	if err != nil {
		imageUploadError(c, 400, "failed to read image upload")
		return
	}
	// Validate the entire multipart envelope before reserving bytes or writing to OSS.
	// This endpoint accepts one file only, not a batch or extra form parameters.
	headerBudget.N = min(headerBudget.N, 64<<10)
	if _, err = reader.NextPart(); err != io.EOF {
		if errors.As(err, &tooLarge) {
			imageUploadError(c, 413, fmt.Sprintf("file exceeds maximum %d bytes", maxBytes))
		} else {
			imageUploadError(c, 400, "expected exactly one file field and no additional form fields")
		}
		return
	}
	// A MIME closing delimiter does not prove that a chunked/declared HTTP body
	// has finished. Keep the read deadline and upload slot until its transport EOF.
	if _, err = io.Copy(io.Discard, boundedBody); err != nil {
		if errors.As(err, &tooLarge) {
			imageUploadError(c, 413, fmt.Sprintf("request exceeds maximum %d bytes", maxBytes+(64<<10)))
		} else {
			imageUploadError(c, 400, "failed to read complete upload request")
		}
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		imageUploadError(c, 500, "failed to read image")
		return
	}
	config, format, err := image.DecodeConfig(file)
	mime := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp"}[format]
	if err != nil || mime == "" {
		imageUploadError(c, 400, "unsupported or invalid image, allowed: jpeg, png, gif, webp")
		return
	}
	// Avoid delivering decompression bombs to downstream image processors.
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 100_000_000 {
		imageUploadError(c, 400, "image dimensions exceed maximum 100 megapixels")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		imageUploadError(c, 500, "failed to read image")
		return
	}
	code, err = reserveImageUpload(c.Request.Context(), c.GetInt("id"), fileSize, cfg)
	if err != nil {
		imageUploadError(c, 503, "upload rate limit service unavailable")
		return
	}
	if code != 0 {
		message := "Rate limit exceeded. Please try again later"
		if code == 2 {
			message = "Daily upload size limit exceeded"
		}
		imageUploadError(c, 429, message)
		return
	}
	extension := format
	if extension == "jpeg" {
		extension = "jpg"
	}
	now := time.Now().UTC()
	key := cfg.Prefix + now.Format("2006/01/02/") + common.GetUUID() + "." + extension
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	address, err := uploadImageObject(ctx, cfg, key, mime, file, fileSize)
	if err != nil {
		common.SysError("OSS image upload failed")
		imageUploadError(c, 500, "failed to upload image")
		return
	}
	recordImageUpload(c, key, fileSize)
	filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	c.JSON(200, uploadedImage{URL: address, Filename: filename, ContentType: mime, Bytes: fileSize, CreatedAt: now.Unix()})
}

// Only expired leftovers on this instance are removed; this never contacts OSS.
func CleanupObjectStorageTemp(c *gin.Context) {
	cfg, err := object_storage.Get()
	if err != nil || cfg.TempRetentionHours < 1 || cfg.TempRetentionHours > 168 {
		common.ApiErrorMsg(c, "无法读取临时文件清理配置")
		return
	}
	result, err := service.CleanImageUploadTemp(time.Duration(cfg.TempRetentionHours) * time.Hour)
	if err != nil {
		common.ApiErrorMsg(c, "临时文件清理失败，请检查服务器目录权限")
		return
	}
	recordManageAudit(c, "option.update", map[string]interface{}{"key": "object_storage_temp_cleanup", "deleted_files": result.DeletedFiles})
	common.ApiSuccess(c, result)
}
