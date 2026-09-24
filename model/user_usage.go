package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserUsageRequest is independent of optional diagnostic/consume logs. Quota is
// observed settled consumption; this table never changes a wallet or billing.
type UserUsageRequest struct {
	ID                  int64  `gorm:"primaryKey;index:idx_usage_records,priority:4"`
	UserID              int    `gorm:"uniqueIndex:idx_usage_request,priority:1;index:idx_usage_user_time,priority:1;index:idx_usage_records,priority:1"`
	RequestID           string `gorm:"size:64;uniqueIndex:idx_usage_request,priority:2"`
	CreatedAt           int64  `gorm:"index:idx_usage_user_time,priority:2;index:idx_usage_retention;index:idx_usage_records,priority:3"`
	ModelName           string `gorm:"size:255"`
	Outcome             string `gorm:"size:16"`
	Requests            int64  `gorm:"index:idx_usage_records,priority:2"`
	Quota               int64
	PromptTokens        int64
	CompletionTokens    int64
	StatusCode          int
	RetryCount          int
	LastErrorStatusCode int
	IsStream            bool
	DurationMS          int64
}

type UserUsageTracking struct {
	ID             int `gorm:"primaryKey"`
	StartedAt      int64
	StorageVersion int   `gorm:"not null;default:0"`
	GapFrom        int64 `gorm:"not null;default:0"`
	GapUntil       int64 `gorm:"not null;default:0"`
}

func InitializeUserUsageTracking() error {
	return userUsageDatabase().Clauses(clause.OnConflict{DoNothing: true}).Create(&UserUsageTracking{ID: 1, StartedAt: time.Now().Unix()}).Error
}

func GetUserUsageTrackingStart() (int64, error) {
	var state UserUsageTracking
	err := userUsageDatabase().First(&state, 1).Error
	return state.StartedAt, err
}

const usageRecorderKey = "user_usage_recorder"

// UserUsageRecorder is request-local and also tolerates concurrent usage events
// in realtime relays. No prompts, keys, channel details or raw errors are saved.
type UserUsageRecorder struct {
	sync.Mutex
	UserUsageRequest
	streamFailed bool
}

func BeginUserUsage(c *gin.Context) *UserUsageRecorder {
	r := &UserUsageRecorder{}
	c.Set(usageRecorderKey, r)
	return r
}

func CaptureUserUsage(c *gin.Context, params RecordConsumeLogParams) {
	value, ok := c.Get(usageRecorderKey)
	if !ok {
		return
	}
	r := value.(*UserUsageRecorder)
	r.Lock()
	defer r.Unlock()
	r.ModelName = params.ModelName
	r.Quota += int64(max(params.Quota, 0))
	r.PromptTokens += int64(max(params.PromptTokens, 0))
	r.CompletionTokens += int64(max(params.CompletionTokens, 0))
	r.IsStream = params.IsStream
	if stream, ok := params.Other["stream_status"].(map[string]interface{}); ok && stream["status"] == "error" {
		r.streamFailed = true
	}
}

func MarkUserUsageFailure(c *gin.Context, statusCode int) {
	value, ok := c.Get(usageRecorderKey)
	if !ok {
		return
	}
	r := value.(*UserUsageRecorder)
	r.Lock()
	defer r.Unlock()
	r.Outcome, r.StatusCode = "failed", statusCode
}

func MarkUserUsageAttemptError(c *gin.Context, statusCode int) {
	value, ok := c.Get(usageRecorderKey)
	if !ok {
		return
	}
	r := value.(*UserUsageRecorder)
	r.Lock()
	defer r.Unlock()
	r.LastErrorStatusCode = statusCode
}

func (r *UserUsageRecorder) Finish(c *gin.Context, started time.Time) error {
	r.Lock()
	defer r.Unlock()
	r.UserID = c.GetInt("id")
	if r.UserID <= 0 {
		return nil
	}
	r.RequestID = c.GetString(common.RequestIdKey)
	if r.RequestID == "" {
		r.RequestID = common.NewRequestId()
	}
	r.CreatedAt = time.Now().Unix()
	r.Requests = 1
	if r.ModelName == "" {
		r.ModelName = c.GetString("original_model")
	}
	r.RetryCount = max(len(c.GetStringSlice("use_channel"))-1, 0)
	r.IsStream = r.IsStream || common.GetContextKeyBool(c, constant.ContextKeyIsStream)
	r.DurationMS = time.Since(started).Milliseconds()
	if r.StatusCode == 0 {
		r.StatusCode = c.Writer.Status()
	}
	if r.Outcome == "" {
		r.Outcome = "success"
		if r.StatusCode >= 400 || r.streamFailed {
			r.Outcome = "failed"
		}
	}
	return enqueueUserUsage(r.UserUsageRequest)
}

func RecordUserUsageAdjustment(params RecordTaskBillingLogParams) {
	if params.LogType != LogTypeConsume || params.Quota <= 0 {
		return
	}
	row := UserUsageRequest{UserID: params.UserId, RequestID: common.NewRequestId(), CreatedAt: time.Now().Unix(), ModelName: params.ModelName, Outcome: "adjustment", Quota: int64(params.Quota)}
	if err := enqueueUserUsage(row); err != nil {
		common.SysError("record usage adjustment: " + err.Error())
	}
}

type UserUsageAggregate struct {
	Bucket    int64
	ModelName string
	Outcome   string
	Requests  int64
	Quota     int64
}

// The derived query folds retries with the same request ID together. Older
// entries without an ID remain separate rather than collapsing into one call.
func legacyUserUsageQuery(userID int, start, end, trackingStart int64) *gorm.DB {
	return LOG_DB.Model(&Log{}).
		Select("MAX(id) AS id, request_id, MAX(created_at) AS created_at, MAX(model_name) AS model_name, SUM(CASE WHEN type = 2 THEN quota ELSE 0 END) AS quota, SUM(CASE WHEN type = 2 THEN prompt_tokens ELSE 0 END) AS prompt_tokens, SUM(CASE WHEN type = 2 THEN completion_tokens ELSE 0 END) AS completion_tokens").
		Where("user_id = ? AND type IN ? AND created_at >= ? AND created_at < ? AND created_at < ?", userID, []int{LogTypeConsume, LogTypeError}, start, end, trackingStart).
		Group("request_id, CASE WHEN request_id = '' THEN id ELSE 0 END")
}

func GetUserUsageAggregates(userID int, start, end, trackingStart, bucketSeconds int64) ([]UserUsageAggregate, error) {
	// All supported SQL databases implement integer remainder. +8h gives
	// Beijing calendar-day boundaries without database timezone functions.
	bucket := fmt.Sprintf("created_at - ((created_at + 28800) %% %d)", bucketSeconds)
	var result []UserUsageAggregate
	err := userUsageDatabase().Model(&UserUsageRequest{}).
		Select(bucket+" AS bucket, model_name, outcome, SUM(requests) AS requests, SUM(quota) AS quota").
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userID, start, end).
		Group(bucket + ", model_name, outcome").Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if start < trackingStart {
		var legacy []UserUsageAggregate
		err = LOG_DB.Table("(?) AS usage_history", legacyUserUsageQuery(userID, start, end, trackingStart)).
			Select(bucket + " AS bucket, model_name, 'unknown' AS outcome, COUNT(*) AS requests, SUM(quota) AS quota").
			Group(bucket + ", model_name").Scan(&legacy).Error
		result = append(result, legacy...)
	}
	return result, err
}

type UserUsageRecordQuery struct {
	UserID                    int
	Start, End, TrackingStart int64
	Model, Outcome            string
	Offset, Limit             int
}

func GetUserUsageRecords(q UserUsageRecordQuery) ([]UserUsageRequest, int64, error) {
	current := userUsageDatabase().Model(&UserUsageRequest{}).Where("user_id = ? AND requests = 1 AND created_at >= ? AND created_at < ?", q.UserID, q.Start, q.End)
	legacy := LOG_DB.Table("(?) AS usage_history", legacyUserUsageQuery(q.UserID, q.Start, q.End, q.TrackingStart))
	if q.Model != "" {
		current = current.Where("model_name = ?", q.Model)
		legacy = legacy.Where("model_name = ?", q.Model)
	}
	if q.Outcome != "" {
		current = current.Where("outcome = ?", q.Outcome)
	}
	var currentCount, legacyCount int64
	if err := current.Count(&currentCount).Error; err != nil {
		return nil, 0, err
	}
	includeLegacy := q.Start < q.TrackingStart && (q.Outcome == "" || q.Outcome == "unknown")
	if includeLegacy {
		if err := legacy.Count(&legacyCount).Error; err != nil {
			return nil, 0, err
		}
	}
	result := make([]UserUsageRequest, 0, q.Limit)
	if int64(q.Offset) < currentCount {
		if err := current.Order("created_at DESC, id DESC").Offset(q.Offset).Limit(q.Limit).Find(&result).Error; err != nil {
			return nil, 0, err
		}
	}
	if includeLegacy && len(result) < q.Limit {
		var rows []UserUsageRequest
		offset := max(int64(q.Offset)-currentCount, 0)
		if err := legacy.Select("id, request_id, created_at, model_name, quota, prompt_tokens, completion_tokens").Order("created_at DESC, id DESC").Offset(int(offset)).Limit(q.Limit - len(result)).Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		for i := range rows {
			rows[i].Outcome = "unknown"
			rows[i].Requests = 1
		}
		result = append(result, rows...)
	}
	return result, currentCount + legacyCount, nil
}
