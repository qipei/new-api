// Package volcopenapi 实现火山引擎 OpenAPI 的 HMAC-SHA256 请求签名。
// 规则见 https://www.volcengine.com/docs/6369/67269 ：与 AWS SigV4 同构，
// 派生密钥不加前缀，凭据范围为 日期/地域/服务/request。
package volcopenapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Credential struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	Service         string
}

// Sign 为请求写入 Host、X-Date、X-Content-Sha256 与 Authorization 头。
// body 必须与实际发送的请求体逐字节一致。
func Sign(req *http.Request, body []byte, cred Credential, now time.Time) {
	now = now.UTC()
	xDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	payloadHash := sha256Hex(body)

	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", payloadHash)

	signed := map[string]string{
		"content-type":     req.Header.Get("Content-Type"),
		"host":             req.URL.Host,
		"x-content-sha256": payloadHash,
		"x-date":           xDate,
	}
	names := make([]string, 0, len(signed))
	for name := range signed {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + strings.TrimSpace(signed[name]) + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	canonicalRequest := strings.Join([]string{
		req.Method,
		path,
		canonicalQuery(req.URL.Query()),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/%s/request", shortDate, cred.Region, cred.Service)
	stringToSign := strings.Join([]string{"HMAC-SHA256", xDate, scope, sha256Hex([]byte(canonicalRequest))}, "\n")

	key := hmacSHA256([]byte(cred.SecretAccessKey), shortDate)
	key = hmacSHA256(key, cred.Region)
	key = hmacSHA256(key, cred.Service)
	key = hmacSHA256(key, "request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cred.AccessKeyID, scope, signedHeaders, signature))
}

// canonicalQuery 按键排序，并按 RFC 3986 编码（空格编码为 %20 而不是 +）。
func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		sort.Strings(vals)
		for _, val := range vals {
			parts = append(parts, rfc3986Escape(key)+"="+rfc3986Escape(val))
		}
	}
	return strings.Join(parts, "&")
}

func rfc3986Escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
