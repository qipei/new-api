package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReferralRewardsQueryValidationAndUserBoundary(t *testing.T) {
	db := setupPhoneControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.CommissionRecord{}, &model.ReferralRewardRecord{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "viewer", AffCode: "VIEW"}).Error)
	require.NoError(t, db.Create(&model.ReferralRewardRecord{UserID: 2, SourceType: "registration", SourceID: "secret", Quota: 99999}).Error)
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"?page=0", 400}, {"?page=100001", 400}, {"?page_size=101", 400}, {"?page_size=-1", 400}, {"?page=bad", 400}, {"?user_id=2", 200},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("id", 1)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/referral/rewards"+tc.query, nil)
		GetReferralRewards(c)
		require.Equal(t, tc.code, w.Code)
		if tc.code == 200 {
			assert.Contains(t, w.Body.String(), `"items":[]`)
			assert.Contains(t, w.Body.String(), `"total":0`)
			assert.NotContains(t, w.Body.String(), "99999")
		}
	}
}
