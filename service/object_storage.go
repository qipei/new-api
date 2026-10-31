package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// Dedicated reusable pool: upload connections never consume relay HTTP-client slots.
var objectStorageHTTPClient = &http.Client{
	Timeout: 90 * time.Second,
	Transport: &http.Transport{
		Proxy:        http.ProxyFromEnvironment,
		DialContext:  (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns: 8, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 30,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

type ImageObjectStorage struct {
	client *oss.Client
	signer *oss.Client
	config object_storage.Config
}

func NewImageObjectStorage(cfg object_storage.Config) *ImageObjectStorage {
	sdk := oss.LoadDefaultConfig().WithRegion(cfg.Region).WithEndpoint(cfg.Endpoint).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).
		WithConnectTimeout(10 * time.Second).WithReadWriteTimeout(60 * time.Second).WithRetryMaxAttempts(2).WithHttpClient(objectStorageHTTPClient)
	client := oss.NewClient(sdk)
	signer := client
	if cfg.PublicEndpoint != "" {
		publicSDK := oss.LoadDefaultConfig().WithRegion(cfg.Region).WithEndpoint(cfg.PublicEndpoint).WithUseCName(true).
			WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret))
		signer = oss.NewClient(publicSDK)
	} else if strings.Contains(cfg.Endpoint, "-internal.") {
		publicSDK := oss.LoadDefaultConfig().WithRegion(cfg.Region).WithEndpoint(strings.Replace(cfg.Endpoint, "-internal.", ".", 1)).
			WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret))
		signer = oss.NewClient(publicSDK)
	}
	return &ImageObjectStorage{client: client, signer: signer, config: cfg}
}

func (s *ImageObjectStorage) Upload(ctx context.Context, key, contentType string, body io.Reader, size int64) (string, error) {
	_, err := s.client.PutObject(ctx, &oss.PutObjectRequest{Bucket: oss.Ptr(s.config.Bucket), Key: oss.Ptr(key), Body: body, ContentType: oss.Ptr(contentType), ContentLength: oss.Ptr(size), CacheControl: oss.Ptr("private, max-age=0"), ForbidOverwrite: oss.Ptr("true")})
	if err != nil {
		return "", err
	}
	result, err := s.signer.Presign(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(s.config.Bucket), Key: oss.Ptr(key)}, oss.PresignExpires(time.Duration(s.config.URLTTLHours)*time.Hour))
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

// Test validates the actual public download path and removes its own probe even on failure.
func (s *ImageObjectStorage) Test(ctx context.Context) (testErr error) {
	key := s.config.Prefix + "connection-test-" + common.GetUUID() + ".txt"
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := s.client.DeleteObject(cleanup, &oss.DeleteObjectRequest{Bucket: oss.Ptr(s.config.Bucket), Key: oss.Ptr(key)}); err != nil {
			common.SysError("OSS connection test cleanup failed")
			if testErr == nil {
				testErr = fmt.Errorf("OSS 读写成功，但测试文件删除失败，请检查 oss:DeleteObject 权限")
			}
		}
	}()
	payload := []byte("new-api object storage connection test")
	address, err := s.Upload(ctx, key, "text/plain", bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("OSS 上传测试失败，请检查地域、Bucket 和写入权限")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("OSS 下载地址无效")
	}
	resp, err := objectStorageHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("OSS 签名下载测试失败，请检查公网域名和网络")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(data, payload) {
		return fmt.Errorf("OSS 签名下载测试失败，请检查读取权限、自定义域名及防盗链设置")
	}
	return nil
}

// CheckLifecycle reports only unrestricted enabled rules covering the entire upload prefix.
func (s *ImageObjectStorage) CheckLifecycle(ctx context.Context) error {
	result, err := s.client.GetBucketLifecycle(ctx, &oss.GetBucketLifecycleRequest{Bucket: oss.Ptr(s.config.Bucket)})
	if err != nil {
		return fmt.Errorf("无法读取 OSS 生命周期，请检查 oss:GetBucketLifecycle 权限及规则配置")
	}
	if result.LifecycleConfiguration != nil {
		for _, rule := range result.LifecycleConfiguration.Rules {
			if oss.ToString(rule.Status) != "Enabled" || rule.Expiration == nil || rule.Expiration.Days == nil || len(rule.Tags) > 0 || rule.Filter != nil {
				continue
			}
			days := *rule.Expiration.Days
			if strings.HasPrefix(s.config.Prefix, oss.ToString(rule.Prefix)) && days == 3 {
				return nil
			}
		}
	}
	return fmt.Errorf("未找到覆盖上传目录的 3 天过期删除规则，请在 OSS 控制台配置；本系统不会自动修改 Bucket 生命周期")
}
