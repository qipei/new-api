package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ossTestTransport func(*http.Request) (*http.Response, error)

func (f ossTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestImageStorageUploadAndCustomDomainSigning(t *testing.T) {
	cfg := object_storage.Defaults()
	cfg.Region = "cn-hangzhou"
	cfg.Endpoint = "https://oss-cn-hangzhou.aliyuncs.com"
	cfg.Bucket = "test-bucket"
	cfg.AccessKeyID = "test-id"
	cfg.AccessKeySecret = "test-secret"
	cfg.PublicEndpoint = "https://files.example.com"
	store := NewImageObjectStorage(cfg)
	transport := ossTestTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "test-bucket.oss-cn-hangzhou.aliyuncs.com", r.URL.Host)
		assert.Equal(t, "/uploads/images/photo.png", r.URL.Path)
		assert.Equal(t, "image/png", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "image-bytes", string(body))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	sdk := oss.LoadDefaultConfig().WithEndpoint(cfg.Endpoint).WithRegion(cfg.Region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).WithHttpClient(&http.Client{Transport: transport})
	store.client = oss.NewClient(sdk)
	signed, err := store.Upload(context.Background(), "uploads/images/photo.png", "image/png", strings.NewReader("image-bytes"), 11)
	require.NoError(t, err)
	parsed, err := url.Parse(signed)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "files.example.com", parsed.Host)
	assert.Equal(t, "/uploads/images/photo.png", parsed.Path)
	assert.NotEmpty(t, parsed.Query().Get("x-oss-signature"))
	assert.Equal(t, "86400", parsed.Query().Get("x-oss-expires"))
	assert.NotContains(t, signed, "test-secret")
}

func TestImageStorageLifecycleCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		ok         bool
	}{
		{"prefix covered", `<Status>Enabled</Status><Prefix>uploads/</Prefix><Expiration><Days>3</Days></Expiration>`, true},
		{"different prefix", `<Status>Enabled</Status><Prefix>other/</Prefix><Expiration><Days>3</Days></Expiration>`, false},
		{"disabled", `<Status>Disabled</Status><Prefix>uploads/</Prefix><Expiration><Days>3</Days></Expiration>`, false},
		{"tag restricted", `<Status>Enabled</Status><Prefix>uploads/</Prefix><Tag><Key>type</Key><Value>temp</Value></Tag><Expiration><Days>3</Days></Expiration>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := object_storage.Defaults()
			cfg.Bucket = "test-bucket"
			store := NewImageObjectStorage(cfg)
			transport := ossTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<LifecycleConfiguration><Rule>` + tc.rule + `</Rule></LifecycleConfiguration>`)), Request: r}, nil
			})
			store.client = oss.NewClient(oss.LoadDefaultConfig().WithRegion("cn-hangzhou").WithCredentialsProvider(credentials.NewStaticCredentialsProvider("id", "secret")).WithHttpClient(&http.Client{Transport: transport}))
			err := store.CheckLifecycle(context.Background())
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
