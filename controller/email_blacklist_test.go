package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailBlacklistRejectsVerificationAndPreissuedRegistrationCode(t *testing.T) {
	db := setupPhoneControllerTest(t)
	oldPassword, oldVerify, oldWhitelist := common.PasswordRegisterEnabled, common.EmailVerificationEnabled, common.EmailDomainRestrictionEnabled
	common.PasswordRegisterEnabled, common.EmailVerificationEnabled, common.EmailDomainRestrictionEnabled = true, true, false
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{"EmailDomainBlacklistEnabled": "true", "EmailDomainBlacklist": "maildrop.cc"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.PasswordRegisterEnabled, common.EmailVerificationEnabled, common.EmailDomainRestrictionEnabled = oldPassword, oldVerify, oldWhitelist
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	existing := &model.User{Username: "existing", Email: "existing@example.com"}
	require.NoError(t, db.Create(existing).Error)
	engine := gin.New()
	engine.GET("/verification", SendEmailVerification)
	engine.POST("/register", Register)
	engine.POST("/bind", func(c *gin.Context) { c.Set("id", existing.Id); EmailBind(c) })
	for _, email := range []string{"jvii9y1blgo@maildrop.cc", "a@maildrop.cc.", "a@SUB.MAILDROP.CC."} {
		t.Run(email, func(t *testing.T) {
			// These addresses pass the existing email validator, so the blacklist
			// must reject them before sending mail or consuming a previous code.
			require.NoError(t, common.Validate.Var(email, "required,email"))
			common.RegisterVerificationCodeWithKey(email, "123456", common.EmailVerificationPurpose)
			registration, err := common.Marshal(map[string]string{"username": "blacklist-user", "password": "password123", "email": email, "verification_code": "123456"})
			require.NoError(t, err)
			binding, err := common.Marshal(map[string]string{"email": email, "code": "123456"})
			require.NoError(t, err)
			for _, tc := range []struct{ method, path, body string }{
				{http.MethodGet, "/verification?email=" + email, ""},
				{http.MethodPost, "/register", string(registration)},
				{http.MethodPost, "/bind", string(binding)},
			} {
				response := httptest.NewRecorder()
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Accept-Language", "en")
				engine.ServeHTTP(response, req)
				var data struct {
					Success bool
					Message string
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &data))
				assert.False(t, data.Success, tc.path)
				assert.Contains(t, data.Message, "email domain", tc.path)
			}
			assert.True(t, common.VerifyCodeWithKey(email, "123456", common.EmailVerificationPurpose))
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.First(existing, existing.Id).Error)
	assert.Equal(t, "existing@example.com", existing.Email)
}
