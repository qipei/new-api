package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captchaTestTransport func(*http.Request) (*http.Response, error)

func (f captchaTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTencentCaptchaUsesClientSpecificVerificationAndRejectsReplay(t *testing.T) {
	usedCaptchaTickets.Lock()
	previousTickets := usedCaptchaTickets.expires
	usedCaptchaTickets.expires = make(map[string]time.Time)
	usedCaptchaTickets.Unlock()
	t.Cleanup(func() {
		usedCaptchaTickets.Lock()
		usedCaptchaTickets.expires = previousTickets
		usedCaptchaTickets.Unlock()
	})
	oldTransport, oldRedis := http.DefaultTransport, common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { http.DefaultTransport, common.RedisEnabled = oldTransport, oldRedis })
	credentials := TencentCaptchaCredentials{AppID: "123", AppSecretKey: "app-secret", SecretID: "account-id", SecretKey: "account-secret"}
	for _, clientType := range []string{"web", "mini_program"} {
		t.Run(clientType, func(t *testing.T) {
			http.DefaultTransport = captchaTestTransport(func(r *http.Request) (*http.Response, error) {
				action := "DescribeCaptchaResult"
				if clientType == "mini_program" {
					action = "DescribeCaptchaMiniResult"
				}
				assert.Equal(t, action, r.Header.Get("X-TC-Action"))
				var body map[string]interface{}
				require.NoError(t, common.DecodeJson(r.Body, &body))
				assert.Equal(t, "198.51.100.1", body["UserIp"])
				assert.Equal(t, float64(123), body["CaptchaAppId"])
				if clientType == "web" {
					assert.Equal(t, "random", body["Randstr"])
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Response":{"CaptchaCode":1,"CaptchaMsg":"OK","RequestId":"test"}}`))}, nil
			})
			ticket := "ticket-" + t.Name()
			err := VerifyTencentCaptcha(context.Background(), credentials, clientType, ticket, "random", "198.51.100.1")
			require.NoError(t, err)
			require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, clientType, ticket, "random", "198.51.100.1"))
		})
	}
}

func TestTencentCaptchaSharedReplayStoreFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	oldTransport, oldRedis, oldClient := http.DefaultTransport, common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		http.DefaultTransport, common.RedisEnabled, common.RDB = oldTransport, oldRedis, oldClient
		_ = client.Close()
	})
	http.DefaultTransport = captchaTestTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Response":{"CaptchaCode":1}}`))}, nil
	})
	credentials := TencentCaptchaCredentials{AppID: "123", AppSecretKey: "secret", SecretID: "id", SecretKey: "key"}
	require.NoError(t, VerifyTencentCaptcha(context.Background(), credentials, "mini_program", "shared-ticket", "", "198.51.100.1"))
	require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, "mini_program", "shared-ticket", "", "198.51.100.2"))
	server.SetError("ERR unavailable")
	require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, "mini_program", "new-ticket", "", "198.51.100.1"))
}

func TestTencentCaptchaFailsClosedOnInvalidProviderResults(t *testing.T) {
	oldTransport, oldRedis := http.DefaultTransport, common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { http.DefaultTransport, common.RedisEnabled = oldTransport, oldRedis })
	credentials := TencentCaptchaCredentials{AppID: "123", AppSecretKey: "app-secret", SecretID: "account-id", SecretKey: "account-secret"}
	for _, response := range []string{`{"Response":{"CaptchaCode":8}}`, `{"Response":{}}`, `{bad json`, `{"Response":{"CaptchaCode":1,"EvilLevel":100}}`} {
		http.DefaultTransport = captchaTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		})
		require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, "web", "invalid-"+response, "random", "198.51.100.1"))
	}
	// Invalid and fallback tickets must not even reach the provider.
	http.DefaultTransport = captchaTestTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unexpected provider call")
		return nil, nil
	})
	require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, "web", "trerror_fallback", "random", "198.51.100.1"))
	require.Error(t, VerifyTencentCaptcha(context.Background(), credentials, "invalid", "ticket", "random", "198.51.100.1"))
}
