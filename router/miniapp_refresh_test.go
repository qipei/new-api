package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMiniappRefreshCredentialsAndSessionIsolation(t *testing.T) {
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousSecret := common.SessionSecret
	previousSecure, previousTrusted := common.SessionCookieSecure, common.SessionCookieTrustedURLs
	previousGlobalLimit, previousCriticalLimit := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB, common.RedisEnabled = db, false
	common.SessionSecret = "miniapp-refresh-test-secret"
	common.SessionCookieSecure = true
	common.SessionCookieTrustedURLs = []string{"https://token01.net"}
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.SessionSecret = previousSecret
		common.SessionCookieSecure, common.SessionCookieTrustedURLs = previousSecure, previousTrusted
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousGlobalLimit, previousCriticalLimit
		require.NoError(t, sqlDB.Close())
	})
	user := &model.User{Username: "miniapp-refresh-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
	require.NoError(t, db.Create(user).Error)
	miniapp, err := service.CreateLoginSession(user.Id, "phone", "127.0.0.1", "miniapp")
	require.NoError(t, err)
	web, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "web")
	require.NoError(t, err)
	webBefore, err := model.GetUserSessionBySID(web.Session.SID)
	require.NoError(t, err)
	miniappBefore, err := model.GetUserSessionBySID(miniapp.Session.SID)
	require.NoError(t, err)
	engine := gin.New()
	SetApiRouter(engine)
	const path = "/api/user/auth/refresh/miniapp"
	const referer = "https://servicewechat.com/wx6d1981f3d309e6cd/devtools/page-frame.html"
	request := func(endpoint, body, contentType, sid, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://token01.net"+endpoint, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-Auth-Session", sid)
		if origin != "" {
			req.Header.Set("Referer", origin)
		}
		// The browser cookie must never authenticate or select a session on
		// the miniapp endpoint, even when the JSON credential is invalid.
		req.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: web.RefreshToken})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	encodeToken := func(token string) string {
		body, err := common.Marshal(map[string]string{"refresh_token": token})
		require.NoError(t, err)
		return string(body)
	}
	for _, test := range []struct {
		name, body, contentType, sid, code string
		status                             int
	}{
		{"cookie only", `{}`, "application/json", "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"access token instead of refresh token", encodeToken(miniapp.AccessToken), "application/json", "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"wrong secret", encodeToken(miniapp.Session.SID + ".wrong-secret"), "application/json", "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"session mismatch", encodeToken(miniapp.RefreshToken), "application/json", web.Session.SID, "AUTH_SESSION_MISMATCH", http.StatusConflict},
		{"malformed JSON", `{"refresh_token":`, "application/json", "", "AUTH_INVALID_REQUEST", http.StatusBadRequest},
		{"form request", "refresh_token=" + miniapp.RefreshToken, "application/x-www-form-urlencoded", "", "AUTH_INVALID_REQUEST", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(path, test.body, test.contentType, test.sid, referer)
			require.Equal(t, test.status, response.Code, response.Body.String())
			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.code, body.Code)
			assert.Empty(t, response.Result().Cookies())
			stored, err := model.GetUserSessionBySID(miniapp.Session.SID)
			require.NoError(t, err)
			assert.Equal(t, miniappBefore.RefreshHash, stored.RefreshHash)
			assert.Equal(t, model.UserSessionStatusActive, stored.Status)
		})
	}

	response := request("/api/user/auth/refresh", "", "application/json", "", referer)
	require.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_ORIGIN_FORBIDDEN")

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken     string                   `json:"access_token"`
			RefreshToken    string                   `json:"refresh_token"`
			TokenType       string                   `json:"token_type"`
			AccessExpiresAt int64                    `json:"access_expires_at"`
			Session         service.LoginSessionView `json:"session"`
			User            struct {
				ID int `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	refreshToken := miniapp.RefreshToken
	// Both the WeChat Referer and an absent Origin/Referer must work. The
	// second request also verifies use of the newly rotated JSON credential.
	for _, origin := range []string{referer, ""} {
		response = request(path, encodeToken(refreshToken), "application/json; charset=utf-8", miniapp.Session.SID, origin)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		assert.True(t, body.Success)
		require.NotEmpty(t, body.Data.RefreshToken)
		assert.NotEqual(t, refreshToken, body.Data.RefreshToken)
		assert.Equal(t, "Bearer", body.Data.TokenType)
		assert.Positive(t, body.Data.AccessExpiresAt)
		assert.Equal(t, miniapp.Session.SID, body.Data.Session.SID)
		assert.Equal(t, miniapp.Session.ExpiresAt, body.Data.Session.ExpiresAt)
		assert.Equal(t, user.Id, body.Data.User.ID)
		assert.Empty(t, response.Result().Cookies())
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		identity, err := service.ParseAccessToken(body.Data.AccessToken)
		require.NoError(t, err)
		_, _, err = service.ValidateLoginSession(identity)
		require.NoError(t, err)
		refreshToken = body.Data.RefreshToken
	}
	webAfter, err := model.GetUserSessionBySID(web.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, webBefore.RefreshHash, webAfter.RefreshHash)
	assert.Equal(t, model.UserSessionStatusActive, webAfter.Status)

	// Logging out through the miniapp route invalidates its latest refresh
	// credential too, without falling back to the valid browser cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/user/auth/logout/miniapp", nil)
	req.Header.Set("Authorization", "Bearer "+body.Data.AccessToken)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	response = request(path, encodeToken(refreshToken), "application/json", "", referer)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_SESSION_REVOKED")
	assert.Empty(t, response.Result().Cookies())

	// The existing browser endpoint still rotates its cookie and does not
	// expose refresh credentials in its JSON response.
	response = request("/api/user/auth/refresh", "", "application/json", web.Session.SID, "https://token01.net/console")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, response.Result().Cookies(), 1)
	assert.Equal(t, service.RefreshCookieName, response.Result().Cookies()[0].Name)
	assert.NotEqual(t, web.RefreshToken, response.Result().Cookies()[0].Value)
	assert.NotContains(t, response.Body.String(), `"refresh_token"`)
}
