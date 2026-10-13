package middleware

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"net/http/httptest"
	"testing"
)

func TestRewardedAdAccessLogDoesNotExposeCallbackSignature(t *testing.T) {
	var output bytes.Buffer
	previous := gin.DefaultWriter
	gin.DefaultWriter = &output
	t.Cleanup(func() { gin.DefaultWriter = previous })
	router := gin.New()
	SetUpLogger(router)
	router.GET("/api/rewarded-ad/dcloud/callback", func(c *gin.Context) { c.Status(400) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/rewarded-ad/dcloud/callback?sign=secret-signature&user_id=123", nil))
	assert.NotContains(t, output.String(), "secret-signature")
	assert.NotContains(t, output.String(), "user_id=123")
	assert.Contains(t, output.String(), "/api/rewarded-ad/dcloud/callback")
}
