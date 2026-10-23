package volcopenapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignWritesScopedAuthorization(t *testing.T) {
	body := []byte(`{"ResourceIDs":["seed-tts-2.0"]}`)
	req, err := http.NewRequest(http.MethodPost, "https://open.volcengineapi.com/?Version=2025-05-20&Action=ListSpeakers", nil)
	require.NoError(t, err)
	now := time.Date(2026, 1, 5, 7, 23, 5, 0, time.UTC)
	Sign(req, body, Credential{AccessKeyID: "AK", SecretAccessKey: "SK", Region: "cn-beijing", Service: "speech_saas_prod"}, now)

	sum := sha256.Sum256(body)
	assert.Equal(t, hex.EncodeToString(sum[:]), req.Header.Get("X-Content-Sha256"))
	assert.Equal(t, "20260105T072305Z", req.Header.Get("X-Date"))
	auth := req.Header.Get("Authorization")
	assert.True(t, strings.HasPrefix(auth, "HMAC-SHA256 Credential=AK/20260105/cn-beijing/speech_saas_prod/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature="))

	// 签名对请求体敏感：内容一变签名必须跟着变。
	other, err := http.NewRequest(http.MethodPost, req.URL.String(), nil)
	require.NoError(t, err)
	Sign(other, []byte(`{}`), Credential{AccessKeyID: "AK", SecretAccessKey: "SK", Region: "cn-beijing", Service: "speech_saas_prod"}, now)
	assert.NotEqual(t, auth, other.Header.Get("Authorization"))
}

func TestCanonicalQueryUsesRFC3986(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://x/?b=2&a=hello world&a=x*y", nil)
	require.NoError(t, err)
	assert.Equal(t, "a=hello%20world&a=x%2Ay&b=2", canonicalQuery(req.URL.Query()))
}
