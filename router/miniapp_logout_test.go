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

func TestMiniappLogoutSessionIsolation(t *testing.T) {
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousSecret := common.SessionSecret
	previousSecure, previousTrusted := common.SessionCookieSecure, common.SessionCookieTrustedURLs
	previousGlobalLimit, previousCriticalLimit := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB, common.RedisEnabled = db, false
	common.SessionSecret = "miniapp-logout-test-secret"
	common.SessionCookieSecure = true
	common.SessionCookieTrustedURLs = []string{"https://token01.net"}
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.SessionSecret = previousSecret
		common.SessionCookieSecure, common.SessionCookieTrustedURLs = previousSecure, previousTrusted
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousGlobalLimit, previousCriticalLimit
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})

	pat := "12345678901234567890123456789012"
	user := &model.User{Username: "miniapp-logout-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AccessToken: &pat}
	require.NoError(t, db.Create(user).Error)
	miniapp, err := service.CreateLoginSession(user.Id, "phone", "127.0.0.1", "miniapp")
	require.NoError(t, err)
	web, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "web")
	require.NoError(t, err)
	miniappIdentity, err := service.ParseAccessToken(miniapp.AccessToken)
	require.NoError(t, err)
	webIdentity, err := service.ParseAccessToken(web.AccessToken)
	require.NoError(t, err)
	proof, _, err := service.IssueSecurityProof(miniappIdentity, "password", []string{"test"})
	require.NoError(t, err)
	engine := gin.New()
	SetApiRouter(engine)

	// Every request carries a different, valid Web refresh cookie. It must
	// never authenticate the miniapp route or select a session to revoke.
	request := func(path, authorization, expectedSID, referer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://token01.net"+path, strings.NewReader(`{"sid":"`+web.Session.SID+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authorization)
		req.Header.Set("X-Auth-Session", expectedSID)
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		req.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: web.RefreshToken})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	const path = "/api/user/auth/logout/miniapp"
	const referer = "https://servicewechat.com/wx6d1981f3d309e6cd/devtools/page-frame.html"
	for _, test := range []struct {
		name, authorization, sid, code string
		status                         int
	}{
		{"cookie alone", "", "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"malformed bearer", miniapp.AccessToken, "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"invalid token", "Bearer invalid", "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"personal access token", "Bearer " + pat, "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"security proof", "Bearer " + proof, "", "AUTH_UNAUTHORIZED", http.StatusUnauthorized},
		{"session mismatch", "Bearer " + miniapp.AccessToken, web.Session.SID, "AUTH_SESSION_MISMATCH", http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(path, test.authorization, test.sid, referer)
			require.Equal(t, test.status, response.Code, response.Body.String())
			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.code, body.Code)
			assert.Empty(t, response.Result().Cookies())
			for _, identity := range []service.AuthIdentity{miniappIdentity, webIdentity} {
				_, _, err := service.ValidateLoginSession(identity)
				require.NoError(t, err)
			}
		})
	}

	// The existing browser endpoint retains its cross-origin protection.
	response := request("/api/user/auth/logout", "Bearer "+miniapp.AccessToken, "", referer)
	require.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_ORIGIN_FORBIDDEN")

	response = request(path, "Bearer "+miniapp.AccessToken, miniapp.Session.SID, referer)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			RevokedSID string `json:"revoked_sid"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.True(t, body.Success)
	assert.Equal(t, miniapp.Session.SID, body.Data.RevokedSID)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Empty(t, response.Result().Cookies())
	_, _, err = service.ValidateLoginSession(miniappIdentity)
	require.ErrorIs(t, err, service.ErrLoginSessionRevoked)
	_, _, err = service.RefreshLoginSession(miniapp.RefreshToken, miniapp.Session.SID, "127.0.0.1", "miniapp")
	require.Error(t, err)
	_, _, err = service.ValidateLoginSession(webIdentity)
	require.NoError(t, err)

	// Without a Referer, revoked tokens still fail authentication; they
	// cannot fall back to the valid Web cookie attached to the request.
	response = request(path, "Bearer "+miniapp.AccessToken, "", "")
	require.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_SESSION_REVOKED")
	_, _, err = service.ValidateLoginSession(webIdentity)
	require.NoError(t, err)

	// A same-site Web logout continues to revoke its own session normally.
	response = request("/api/user/auth/logout", "Bearer "+web.AccessToken, web.Session.SID, "https://token01.net/console")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	_, _, err = service.ValidateLoginSession(webIdentity)
	require.ErrorIs(t, err, service.ErrLoginSessionRevoked)
}
