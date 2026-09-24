package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCheckinCaptchaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Checkin{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB; require.NoError(t, sqlDB.Close()) })
	return db
}

func TestAlreadyCheckedInRequestNeverReachesCaptchaProvider(t *testing.T) {
	db := setupCheckinCaptchaDB(t)
	oldSetting, oldTransport := *operation_setting.GetCheckinSetting(), http.DefaultTransport
	t.Cleanup(func() { *operation_setting.GetCheckinSetting() = oldSetting; http.DefaultTransport = oldTransport })
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, CaptchaEnabled: true}
	require.NoError(t, db.Create(&model.Checkin{UserId: 1, CheckinDate: time.Now().Format("2006-01-02"), QuotaAwarded: 1000}).Error)
	http.DefaultTransport = checkinCaptchaTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("already checked-in requests must not call Tencent")
		return nil, nil
	})
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 1); c.Next() })
	engine.POST("/checkin", CheckinCaptcha(), func(c *gin.Context) { t.Fatal("duplicate check-in reached reward handler") })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", nil))
	assert.Contains(t, response.Body.String(), `"code":"CHECKIN_ALREADY_DONE"`)
	assert.NotContains(t, response.Body.String(), "require_captcha")
	// A failed eligibility lookup must also stop before any paid verification.
	require.NoError(t, db.Migrator().DropTable(&model.Checkin{}))
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", nil))
	assert.Contains(t, response.Body.String(), `"success":false`)
}

type checkinCaptchaTransport func(*http.Request) (*http.Response, error)

func (f checkinCaptchaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCheckinCaptchaBlocksUnverifiedRewardsAndOverridesTurnstile(t *testing.T) {
	setupCheckinCaptchaDB(t)
	useRateLimitMiniRedis(t)
	require.NoError(t, i18n.Init())
	oldSetting, oldSMS := *operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings()
	oldTransport, oldTurnstile := http.DefaultTransport, common.TurnstileCheckEnabled
	t.Cleanup(func() {
		*operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings() = oldSetting, oldSMS
		http.DefaultTransport, common.TurnstileCheckEnabled = oldTransport, oldTurnstile
	})
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, CaptchaEnabled: true}
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{MiniAppID: "456", MiniAppSecretKey: "mini-secret", CaptchaAppId: "123", AppSecretKey: "web-secret", SecretId: "id", SecretKey: "secret"}
	common.TurnstileCheckEnabled = true
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 1); c.Next() })
	issued := 0
	engine.POST("/checkin", CheckinCaptcha(), func(c *gin.Context) { issued++; c.JSON(200, gin.H{"success": true}) })
	for _, tc := range []struct {
		name, body, provider string
		allowed              bool
	}{
		{"missing", "", "", false},
		{"incomplete", `{"captcha_ticket":"ticket"}`, "", false},
		{"invalid-client", `{"captcha_client":"invalid"}`, "", false},
		{"web-rejected", `{"captcha_ticket":"rejected","captcha_randstr":"random"}`, `{"Response":{"CaptchaCode":8}}`, false},
		{"web-pass", `{"captcha_ticket":"checkin-web-test","captcha_randstr":"random"}`, `{"Response":{"CaptchaCode":1}}`, true},
		{"mini-pass", `{"captcha_client":"mini_program","captcha_ticket":"checkin-mini-test"}`, `{"Response":{"CaptchaCode":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			http.DefaultTransport = checkinCaptchaTransport(func(r *http.Request) (*http.Response, error) {
				require.NotEmpty(t, tc.provider, "unverified request must not call the provider")
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.provider))}, nil
			})
			before := issued
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/checkin", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(response, request)
			var payload map[string]interface{}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.Equal(t, tc.allowed, payload["success"])
			if tc.allowed {
				assert.Equal(t, before+1, issued)
			} else {
				assert.Equal(t, before, issued)
			}
			assert.NotContains(t, response.Body.String(), "web-secret")
			assert.NotContains(t, response.Body.String(), "mini-secret")
			if tc.name == "missing" {
				assert.Equal(t, "CAPTCHA_REQUIRED", payload["code"])
				assert.Equal(t, "123", payload["data"].(map[string]interface{})["captcha_app_id"])
			}
		})
	}
	system_setting.GetSMSCaptchaSettings().MiniAppSecretKey = ""
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", strings.NewReader(`{"captcha_client":"mini_program","captcha_ticket":"test"}`)))
	assert.Contains(t, response.Body.String(), "CAPTCHA_NOT_CONFIGURED")
	operation_setting.GetCheckinSetting().CaptchaEnabled = false
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", nil))
	assert.Contains(t, response.Body.String(), "Turnstile")
	common.TurnstileCheckEnabled = false
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", nil))
	assert.JSONEq(t, `{"success":true}`, response.Body.String())
}

func TestCheckinUserLimitCannotBeBypassedByChangingIP(t *testing.T) {
	useRateLimitMiniRedis(t)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 42); c.Next() })
	engine.POST("/checkin", CheckinRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for i := 0; i < 6; i++ {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/checkin", nil))
		require.Equal(t, http.StatusNoContent, response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/checkin", nil)
	request.RemoteAddr = "198.51.100.9:1234"
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.NotEmpty(t, response.Header().Get("Retry-After"))
}

func TestAdaptiveCheckinSkipsPaidVerificationUntilIPRiskRequiresIt(t *testing.T) {
	db := setupCheckinCaptchaDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	useRateLimitMiniRedis(t)
	require.NoError(t, i18n.Init())
	oldSetting, oldSMS := *operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings()
	oldTransport, oldTurnstile := http.DefaultTransport, common.TurnstileCheckEnabled
	t.Cleanup(func() {
		*operation_setting.GetCheckinSetting(), *system_setting.GetSMSCaptchaSettings() = oldSetting, oldSMS
		http.DefaultTransport, common.TurnstileCheckEnabled = oldTransport, oldTurnstile
	})
	*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, CaptchaEnabled: true, CaptchaMode: "adaptive", CaptchaTrustDays: 3, CaptchaIPUserLimit: 2}
	*system_setting.GetSMSCaptchaSettings() = system_setting.SMSCaptchaSettings{CaptchaAppId: "123", AppSecretKey: "secret", SecretId: "id", SecretKey: "key"}
	common.TurnstileCheckEnabled = true
	verifiedAt := time.Now().Unix() - 3600
	for _, user := range []model.User{{Id: 1, Username: "trusted-one", AffCode: "one", CheckinCaptchaVerifiedAt: verifiedAt}, {Id: 2, Username: "trusted-two", AffCode: "two", CheckinCaptchaVerifiedAt: verifiedAt}, {Id: 3, Username: "new-user", AffCode: "three"}} {
		require.NoError(t, db.Create(&user).Error)
	}
	providerCalls, awarded := 0, 0
	providerResult := `{"Response":{"CaptchaCode":1}}`
	http.DefaultTransport = checkinCaptchaTransport(func(*http.Request) (*http.Response, error) {
		providerCalls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(providerResult))}, nil
	})
	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies(nil))
	userID := 1
	engine.Use(func(c *gin.Context) { c.Set("id", userID); c.Next() })
	engine.POST("/checkin", CheckinCaptcha(), func(c *gin.Context) { awarded++; c.JSON(200, gin.H{"success": true}) })
	request := func(ip, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/checkin", strings.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	response := request("192.0.2.99", "")
	require.JSONEq(t, `{"success":true}`, response.Body.String())
	assert.Equal(t, 0, providerCalls)
	stored, err := model.GetCheckinCaptchaVerifiedAt(1)
	require.NoError(t, err)
	assert.Equal(t, verifiedAt, stored)
	userID = 2
	response = request("192.0.2.99", "")
	assert.Contains(t, response.Body.String(), "CAPTCHA_REQUIRED")
	assert.Equal(t, 1, awarded)
	response = request("192.0.2.99", `{"captcha_ticket":"adaptive-checkin-ticket","captcha_randstr":"random"}`)
	require.JSONEq(t, `{"success":true}`, response.Body.String())
	assert.Equal(t, 1, providerCalls)
	stored, err = model.GetCheckinCaptchaVerifiedAt(2)
	require.NoError(t, err)
	assert.Greater(t, stored, verifiedAt)
	userID = 1
	response = request("192.0.2.99", "")
	assert.Contains(t, response.Body.String(), "CAPTCHA_REQUIRED")
	response = request("192.0.2.100", "")
	require.JSONEq(t, `{"success":true}`, response.Body.String())
	assert.Equal(t, 1, providerCalls)
	userID = 3
	providerResult = `{"Response":{"CaptchaCode":8}}`
	response = request("192.0.2.101", `{"captcha_ticket":"adaptive-rejected-ticket","captcha_randstr":"random"}`)
	assert.Contains(t, response.Body.String(), "CAPTCHA_FAILED")
	stored, err = model.GetCheckinCaptchaVerifiedAt(3)
	require.NoError(t, err)
	assert.Zero(t, stored)
	assert.Equal(t, 3, awarded)
	userID = 1
	system_setting.GetSMSCaptchaSettings().AppSecretKey = ""
	response = request("192.0.2.102", "")
	assert.Contains(t, response.Body.String(), "CAPTCHA_NOT_CONFIGURED")
	assert.Equal(t, 3, awarded, "missing credentials cannot fall back to a trust exemption")
}
