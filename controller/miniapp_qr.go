package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const miniAppQRCodeOption = "MiniAppQRCode"

type miniAppQRCodeConfig struct {
	Enabled  bool   `json:"enabled"`
	ImageURL string `json:"image_url"`
}

func validateMiniAppQRCodeURL(value string) error {
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\\\r\n\t") {
		return fmt.Errorf("请填写有效的二维码图片地址（不超过 2048 字符）")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.User != nil {
		return fmt.Errorf("二维码图片地址必须为站内路径或 HTTP(S) 地址")
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") {
		return nil
	}
	if (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" {
		return nil
	}
	return fmt.Errorf("二维码图片地址必须为站内路径或 HTTP(S) 地址")
}

// GetMiniAppQRCode returns only the lightweight display settings. Image bytes
// remain in the configured static asset or image host, never in public status.
func GetMiniAppQRCode(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[miniAppQRCodeOption]
	common.OptionMapRWMutex.RUnlock()
	config := miniAppQRCodeConfig{Enabled: true, ImageURL: "/miniapp-code.jpg"}
	if raw != "" {
		// Do not retain defaults when a synchronized config is malformed/incomplete.
		config = miniAppQRCodeConfig{}
		if err := common.UnmarshalJsonStr(raw, &config); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := validateMiniAppQRCodeURL(config.ImageURL); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, config)
}

func SaveMiniAppQRCodeSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var request struct {
		Enabled  *bool  `json:"enabled"`
		ImageURL string `json:"image_url"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "小程序二维码配置格式无效"})
		return
	}
	request.ImageURL = strings.TrimSpace(request.ImageURL)
	if err := validateMiniAppQRCodeURL(request.ImageURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	config := miniAppQRCodeConfig{Enabled: *request.Enabled, ImageURL: request.ImageURL}
	raw, err := common.Marshal(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{miniAppQRCodeOption: string(raw)}); err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, config)
}
