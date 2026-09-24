package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const customerServiceOption = "CustomerService"
const maxCustomerServiceImageBytes = 500 * 1024

var customerServicePhonePattern = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{2,30}$`)

type customerServiceConfig struct {
	Phone         string `json:"phone"`
	QRCodeDataURL string `json:"qrcode_data_url"`
}

// Each process retains only the current configuration's immutable image bytes.
// Compare against OptionMap on reads so database sync and restart work without
// relying on the local save endpoint to invalidate the cache.
type customerServiceSnapshot struct {
	raw      string
	config   customerServiceConfig
	data     []byte
	mime     string
	etag     string
	imageURL string
	err      error
}

var customerServiceAssets struct {
	sync.Mutex
	current *customerServiceSnapshot
}

func newCustomerServiceSnapshot(raw string, config customerServiceConfig, data []byte, mime string) *customerServiceSnapshot {
	snapshot := &customerServiceSnapshot{raw: raw, config: config, data: data, mime: mime}
	if len(data) > 0 {
		hash := sha256.Sum256(data)
		snapshot.etag = fmt.Sprintf(`"%x"`, hash)
		// Preserve the existing URL version scheme.
		version := sha256.Sum256([]byte(config.QRCodeDataURL))
		snapshot.imageURL = fmt.Sprintf("/api/miniapp/customer-service/qrcode?v=%x", version[:8])
	}
	return snapshot
}

func loadCustomerServiceSnapshot() *customerServiceSnapshot {
	customerServiceAssets.Lock()
	defer customerServiceAssets.Unlock()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[customerServiceOption]
	common.OptionMapRWMutex.RUnlock()
	if current := customerServiceAssets.current; current != nil && current.raw == raw {
		return current
	}
	var config customerServiceConfig
	var err error
	if raw != "" {
		err = common.UnmarshalJsonStr(raw, &config)
	}
	var data []byte
	var mime string
	if err == nil {
		// Validate once when loading pre-existing or externally synchronized config.
		// Invalid images stay unavailable and are also memoized until config changes.
		data, mime, _ = decodeCustomerServiceQRCode(config.QRCodeDataURL)
	}
	snapshot := newCustomerServiceSnapshot(raw, config, data, mime)
	snapshot.err = err
	customerServiceAssets.current = snapshot
	return snapshot
}

func readCustomerServiceConfig() (customerServiceConfig, error) {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[customerServiceOption]
	common.OptionMapRWMutex.RUnlock()
	var config customerServiceConfig
	if raw == "" {
		return config, nil
	}
	err := common.UnmarshalJsonStr(raw, &config)
	return config, err
}

// decodeCustomerServiceQRCode only accepts bounded PNG/JPEG images, never SVG
// or arbitrary data URLs that could become active content on this origin.
func decodeCustomerServiceQRCode(value string) ([]byte, string, error) {
	if value == "" {
		return nil, "", nil
	}
	if len(value) > base64.StdEncoding.EncodedLen(maxCustomerServiceImageBytes)+32 {
		return nil, "", fmt.Errorf("二维码图片不能超过 500 KB")
	}
	prefix, encoded, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") {
		return nil, "", fmt.Errorf("二维码仅支持 PNG 或 JPEG 图片")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > maxCustomerServiceImageBytes {
		return nil, "", fmt.Errorf("二维码图片格式或大小无效")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return nil, "", fmt.Errorf("二维码图片无效，尺寸不能超过 2048×2048")
	}
	if prefix != "data:image/"+format+";base64" {
		return nil, "", fmt.Errorf("二维码图片类型不匹配")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return nil, "", fmt.Errorf("二维码图片已损坏")
	}
	return data, "image/" + format, nil
}

func GetMiniAppAbout(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	content := common.OptionMap["About"]
	common.OptionMapRWMutex.RUnlock()
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, gin.H{"content": content, "version": common.Version})
}

func GetCustomerServiceSettings(c *gin.Context) {
	config, err := readCustomerServiceConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, config)
}

func SaveCustomerServiceSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(base64.StdEncoding.EncodedLen(maxCustomerServiceImageBytes)+4096))
	var config customerServiceConfig
	if err := common.DecodeJson(c.Request.Body, &config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "客服配置格式无效或图片过大"})
		return
	}
	config.Phone = strings.TrimSpace(config.Phone)
	if config.Phone != "" && !customerServicePhonePattern.MatchString(config.Phone) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请输入有效的客服电话"})
		return
	}
	data, mime, err := decodeCustomerServiceQRCode(config.QRCodeDataURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	raw, err := common.Marshal(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// One option keeps phone and QR code consistent in storage and memory.
	if err := model.UpdateOptionsBulk(map[string]string{customerServiceOption: string(raw)}); err != nil {
		common.ApiError(c, err)
		return
	}
	// Only publish after persistence succeeds; reuse the already validated bytes.
	snapshot := newCustomerServiceSnapshot(string(raw), config, data, mime)
	customerServiceAssets.Lock()
	customerServiceAssets.current = snapshot
	customerServiceAssets.Unlock()
	common.ApiSuccess(c, config)
}

func GetMiniAppCustomerService(c *gin.Context) {
	snapshot := loadCustomerServiceSnapshot()
	if snapshot.err != nil {
		common.ApiError(c, snapshot.err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, gin.H{"phone": snapshot.config.Phone, "qrcode_url": snapshot.imageURL})
}

func GetMiniAppCustomerServiceQRCode(c *gin.Context) {
	snapshot := loadCustomerServiceSnapshot()
	if snapshot.err != nil {
		common.ApiError(c, snapshot.err)
		return
	}
	if len(snapshot.data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("ETag", snapshot.etag)
	c.Header("Cache-Control", "public, no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == snapshot.etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, snapshot.mime, snapshot.data)
}
