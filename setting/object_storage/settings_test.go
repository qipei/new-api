package object_storage

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	cfg := Defaults()
	require.NoError(t, cfg.Validate())
	cfg.Enabled = true
	require.Error(t, cfg.Validate())
	cfg.Endpoint = "oss-cn-hangzhou.aliyuncs.com"
	cfg.Region = "cn-hangzhou"
	cfg.Bucket = "test-images"
	cfg.AccessKeyID = "test-id"
	cfg.AccessKeySecret = "test-secret"
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "https://oss-cn-hangzhou.aliyuncs.com", cfg.Endpoint)
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"oversize", func(c *Config) { c.MaxFileMB = 21 }},
		{"concurrency", func(c *Config) { c.MaxConcurrentUploads = 31 }},
		{"cleanup retention", func(c *Config) { c.TempRetentionHours = 0 }},
		{"cleanup interval", func(c *Config) { c.TempCleanupIntervalMinutes = 4 }},
		{"ttl", func(c *Config) { c.URLTTLHours = 169 }},
		{"traversal", func(c *Config) { c.Prefix = "../images/" }},
		{"endpoint", func(c *Config) { c.Endpoint = "https://127.0.0.1" }},
		{"domain path", func(c *Config) { c.PublicEndpoint = "https://files.example.com/path" }},
	} {
		t.Run(tc.name, func(t *testing.T) { bad := cfg; tc.change(&bad); assert.Error(t, bad.Validate()) })
	}
}

func TestUploadConcurrencyUpperBoundary(t *testing.T) {
	cfg := Defaults()
	cfg.MaxConcurrentUploads = 30
	require.NoError(t, cfg.Validate())
	cfg.MaxConcurrentUploads = 31
	require.Error(t, cfg.Validate())
}
