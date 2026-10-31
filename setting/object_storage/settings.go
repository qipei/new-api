// CUSTOM: Temporary image upload storage, independent of model channels.
package object_storage

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const OptionKey = "object_storage_config"

type Config struct {
	TempRetentionHours         int    `json:"temp_retention_hours"`
	TempCleanupIntervalMinutes int    `json:"temp_cleanup_interval_minutes"`
	Enabled                    bool   `json:"enabled"`
	MaxConcurrentUploads       int    `json:"max_concurrent_uploads"`
	Endpoint                   string `json:"endpoint"`
	Region                     string `json:"region"`
	Bucket                     string `json:"bucket"`
	AccessKeyID                string `json:"access_key_id"`
	AccessKeySecret            string `json:"access_key_secret"`
	PublicEndpoint             string `json:"public_endpoint"`
	Prefix                     string `json:"prefix"`
	URLTTLHours                int    `json:"url_ttl_hours"`
	MaxFileMB                  int    `json:"max_file_mb"`
	RequestsPerMinute          int    `json:"requests_per_minute"`
	DailyLimitMB               int    `json:"daily_limit_mb"`
}

func Defaults() Config {
	return Config{TempRetentionHours: 1, TempCleanupIntervalMinutes: 30, MaxConcurrentUploads: 2, Prefix: "uploads/images/", URLTTLHours: 24, MaxFileMB: 20, RequestsPerMinute: 10, DailyLimitMB: 1024}
}

// Get copies one atomic persisted option, so concurrent saves cannot mix credentials.
func Get() (Config, error) {
	cfg := Defaults()
	common.OptionMapRWMutex.RLock()
	raw := common.Interface2String(common.OptionMap[OptionKey])
	common.OptionMapRWMutex.RUnlock()
	if raw != "" {
		if err := common.UnmarshalJsonStr(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("invalid object storage configuration")
		}
	}
	return cfg, nil
}

func (c Config) Configured() bool { return c.AccessKeyID != "" && c.AccessKeySecret != "" }

// Validate also runs for disabled configurations; an empty disabled setup may be saved.
func (c *Config) Validate() error {
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	c.PublicEndpoint = strings.TrimRight(strings.TrimSpace(c.PublicEndpoint), "/")
	c.Region = strings.TrimSpace(c.Region)
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.AccessKeySecret = strings.TrimSpace(c.AccessKeySecret)
	if c.TempRetentionHours < 1 || c.TempRetentionHours > 168 || c.TempCleanupIntervalMinutes < 5 || c.TempCleanupIntervalMinutes > 1440 {
		return fmt.Errorf("temporary file retention must be 1-168 hours and cleanup interval 5-1440 minutes")
	}
	if c.MaxConcurrentUploads < 1 || c.MaxConcurrentUploads > 30 {
		return fmt.Errorf("maximum concurrent uploads per instance must be between 1 and 30")
	}
	if c.URLTTLHours < 1 || c.URLTTLHours > 168 || c.MaxFileMB < 1 || c.MaxFileMB > 20 || c.RequestsPerMinute < 1 || c.RequestsPerMinute > 120 || c.DailyLimitMB < 1 || c.DailyLimitMB > 10240 {
		return fmt.Errorf("invalid upload limits: URL 1-168 hours, file 1-20 MB, requests 1-120/minute, daily 1-10240 MB")
	}
	c.Prefix = strings.TrimSpace(c.Prefix)
	if c.Prefix == "" || strings.HasPrefix(c.Prefix, "/") || strings.Contains(c.Prefix, "\\") || strings.Contains(c.Prefix, "..") || strings.ContainsAny(c.Prefix, "?#\r\n") || path.Clean(c.Prefix) == "." {
		return fmt.Errorf("storage prefix must be a relative directory without ..")
	}
	c.Prefix = strings.TrimSuffix(path.Clean(c.Prefix), "/") + "/"
	if c.Endpoint != "" {
		if !strings.Contains(c.Endpoint, "://") {
			c.Endpoint = "https://" + c.Endpoint
		}
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !regexp.MustCompile(`^oss-[a-z0-9-]+\.aliyuncs\.com$`).MatchString(u.Hostname()) {
			return fmt.Errorf("endpoint must be an OSS HTTPS endpoint, for example oss-cn-hangzhou.aliyuncs.com")
		}
	}
	if c.PublicEndpoint != "" {
		u, err := url.Parse(c.PublicEndpoint)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !strings.Contains(u.Hostname(), ".") {
			return fmt.Errorf("public endpoint must be an HTTPS domain without a path")
		}
	}
	if c.Region != "" && !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(c.Region) {
		return fmt.Errorf("invalid OSS region")
	}
	if c.Bucket != "" && !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) {
		return fmt.Errorf("invalid OSS bucket name")
	}
	if c.Enabled && (c.Endpoint == "" || c.Region == "" || c.Bucket == "" || !c.Configured()) {
		return fmt.Errorf("OSS endpoint, region, bucket and credentials are required")
	}
	return nil
}
