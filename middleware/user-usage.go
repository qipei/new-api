package middleware

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// The two usage endpoints share a per-user budget, independent of relay limits.
func UserUsageRateLimit() gin.HandlerFunc {
	return userRateLimitFactory(60, 60, "user-usage")
}

// UserUsageTracking observes model calls without altering their response.
func UserUsageTracking() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if c.Request.Method != http.MethodPost && path != "/v1/realtime" {
			c.Next()
			return
		}
		// Match registered routes, not arbitrary model names or query strings.
		switch path {
		case "/suno/fetch", "/mj/task/list-by-condition", "/:mode/mj/task/list-by-condition",
			"/mj/submit/upload-discord-images", "/:mode/mj/submit/upload-discord-images",
			"/v1/files", "/v1/fine-tunes", "/v1/fine-tunes/:id/cancel", "/v1/images/variations":
			c.Next()
			return
		case "/jimeng/":
			if c.Query("Action") == "CVSync2AsyncGetResult" {
				c.Next()
				return
			}
		}
		started := time.Now()
		recorder := model.BeginUserUsage(c)
		defer func() {
			recovered := recover()
			if recovered != nil {
				model.MarkUserUsageFailure(c, 500)
			}
			if c.GetString(RouteTagKey) == "relay" {
				if err := recorder.Finish(c, started); err != nil {
					common.SysError("record user usage: " + err.Error())
				}
			}
			if recovered != nil {
				panic(recovered)
			}
		}()
		c.Next()
	}
}
