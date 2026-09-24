package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const usageBatchSize = 128

// One bounded queue and one writer per process; request handlers only enqueue
// copied values. They never hold a gin.Context or wait for database I/O.
type userUsageWriter struct {
	db                *gorm.DB
	queue             chan UserUsageRequest
	commands          chan usageWriteCommand
	done              chan struct{}
	mu                sync.Mutex
	closed            bool
	gapFrom, gapUntil int64
}

type usageWriteCommand struct {
	ctx    context.Context
	stop   bool
	result chan error
}

var usageWriter atomic.Pointer[userUsageWriter]

// ClickHouse's append-only log adapter cannot implement the relational unique
// keys/updates used here. Keep that deployment working with batched SQL storage.
func userUsageDatabase() *gorm.DB {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return DB
	}
	return LOG_DB
}

func StartUserUsageWriter() error {
	if usageWriter.Load() != nil {
		return nil
	}
	if err := restoreUserUsageSpool(userUsageDatabase()); err != nil {
		return err
	}
	w := &userUsageWriter{db: userUsageDatabase(), queue: make(chan UserUsageRequest, 8192), commands: make(chan usageWriteCommand, 1), done: make(chan struct{})}
	if !usageWriter.CompareAndSwap(nil, w) {
		return nil
	}
	go w.run()
	return nil
}

func enqueueUserUsage(row UserUsageRequest) error {
	w := usageWriter.Load()
	if w == nil {
		return errors.New("user usage writer is not running")
	}
	// Invalid model names must not poison an entire batch on MySQL/PostgreSQL.
	if name := []rune(row.ModelName); len(name) > 255 {
		row.ModelName = string(name[:255])
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("user usage writer is stopping")
	}
	select {
	case w.queue <- row:
		return nil
	default:
		// Do not fall back to synchronous writes or unbounded goroutines under load.
		if w.gapFrom == 0 || row.CreatedAt < w.gapFrom {
			w.gapFrom = row.CreatedAt
		}
		w.gapUntil = max(w.gapUntil, row.CreatedAt)
		return nil
	}
}

// FlushUserUsage is a barrier for shutdown/tests, never used by relay handlers.
func FlushUserUsage(ctx context.Context) error      { return commandUserUsageWriter(ctx, false) }
func StopUserUsageWriter(ctx context.Context) error { return commandUserUsageWriter(ctx, true) }

func commandUserUsageWriter(ctx context.Context, stop bool) error {
	w := usageWriter.Load()
	if w == nil {
		return nil
	}
	if stop {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
	}
	command := usageWriteCommand{ctx: ctx, stop: stop, result: make(chan error, 1)}
	select {
	case w.commands <- command:
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	if stop {
		// The DB drain observes ctx. Wait for its local-disk fallback too before
		// the process closes DB pools and exits; a canceled ctx must not skip it.
		err := <-command.result
		usageWriter.CompareAndSwap(w, nil)
		return err
	}
	select {
	case err := <-command.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *userUsageWriter) persistGap(ctx context.Context) error {
	w.mu.Lock()
	from, until := w.gapFrom, w.gapUntil
	w.mu.Unlock()
	if from == 0 {
		return nil
	}
	common.SysError("user usage statistics incomplete; persisting affected interval")
	err := w.db.WithContext(ctx).Model(&UserUsageTracking{}).Where("id = ?", 1).Updates(map[string]interface{}{
		"gap_from":  gorm.Expr("CASE WHEN gap_from = 0 OR gap_from > ? THEN ? ELSE gap_from END", from, from),
		"gap_until": gorm.Expr("CASE WHEN gap_until < ? THEN ? ELSE gap_until END", until, until),
	}).Error
	if err == nil {
		w.mu.Lock()
		if w.gapFrom == from && w.gapUntil == until {
			w.gapFrom, w.gapUntil = 0, 0
		}
		w.mu.Unlock()
	}
	return err
}

func (w *userUsageWriter) write(ctx context.Context, rows []UserUsageRequest) error {
	if len(rows) > 0 {
		for i := range rows {
			rows[i].ID = 0
		} // retry by request ID, not rolled-back generated IDs
		// 50 rows also fit SQLite builds with the older 999-parameter limit.
		if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 50).Error; err != nil {
			w.mu.Lock()
			for _, row := range rows {
				if w.gapFrom == 0 || row.CreatedAt < w.gapFrom {
					w.gapFrom = row.CreatedAt
				}
				w.gapUntil = max(w.gapUntil, row.CreatedAt)
			}
			w.mu.Unlock()
			return err
		}
	}
	return w.persistGap(ctx)
}

func (w *userUsageWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	pending := make([]UserUsageRequest, 0, usageBatchSize)
	for {
		input := w.queue
		if len(pending) >= usageBatchSize {
			input = nil
		} // retain failed batches, bounded memory
		select {
		case row := <-input:
			pending = append(pending, row)
			if len(pending) < usageBatchSize {
				continue
			}
		case <-ticker.C:
		case command := <-w.commands:
			err := w.drain(command.ctx, &pending)
			if command.stop && err != nil {
				if spoolErr := w.saveSpool(pending); spoolErr != nil {
					err = fmt.Errorf("usage drain: %v; preserve pending usage: %w", err, spoolErr)
				} else {
					err = fmt.Errorf("usage drain: %v; pending statistics saved for next startup", err)
				}
			}
			command.result <- err
			if command.stop {
				return
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := w.write(ctx, pending)
		cancel()
		if err == nil {
			pending = pending[:0]
		} else {
			common.SysError("flush user usage batch (will retry): " + err.Error())
		}
	}
}

type userUsageSpool struct {
	Rows     []UserUsageRequest
	GapFrom  int64
	GapUntil int64
}

func userUsageSpoolDir() string {
	if path := os.Getenv("USER_USAGE_SPOOL_DIR"); path != "" {
		return path
	}
	return "./user-usage-spool"
}

// This fallback runs only in the worker after shutdown DB failure, not on any
// request path. Docker's working directory /data is a persistent volume.
func (w *userUsageWriter) saveSpool(pending []UserUsageRequest) error {
	w.mu.Lock()
	spool := userUsageSpool{Rows: append([]UserUsageRequest(nil), pending...), GapFrom: w.gapFrom, GapUntil: w.gapUntil}
	w.mu.Unlock()
	for len(w.queue) > 0 {
		spool.Rows = append(spool.Rows, <-w.queue)
	}
	data, err := common.Marshal(spool)
	if err != nil {
		return err
	}
	dir := userUsageSpoolDir()
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "pending-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), file.Name()+".json")
}

func restoreUserUsageSpool(db *gorm.DB) error {
	files, err := filepath.Glob(filepath.Join(userUsageSpoolDir(), "pending-*.tmp.json"))
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var spool userUsageSpool
		if err = common.Unmarshal(data, &spool); err != nil {
			return err
		}
		w := userUsageWriter{db: db, gapFrom: spool.GapFrom, gapUntil: spool.GapUntil}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = w.write(ctx, spool.Rows)
		cancel()
		if err != nil {
			return err
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (w *userUsageWriter) drain(ctx context.Context, pending *[]UserUsageRequest) error {
	for {
		for len(*pending) < usageBatchSize {
			select {
			case row := <-w.queue:
				*pending = append(*pending, row)
			default:
				goto write
			}
		}
	write:
		if err := w.write(ctx, *pending); err != nil {
			return err
		}
		*pending = (*pending)[:0]
		if len(w.queue) == 0 {
			return nil
		}
	}
}

func UserUsageRangeComplete(start, end int64) (bool, error) {
	var state UserUsageTracking
	if err := userUsageDatabase().First(&state, 1).Error; err != nil {
		return false, err
	}
	complete := start >= state.StartedAt && !(state.GapFrom > 0 && start <= state.GapUntil && end > state.GapFrom)
	if w := usageWriter.Load(); w != nil {
		w.mu.Lock()
		from, until := w.gapFrom, w.gapUntil
		w.mu.Unlock()
		complete = complete && !(from > 0 && start <= until && end > from)
	}
	return complete, nil
}

// InitializeUserUsageStorage keeps the original tracking epoch. Upgrades from
// main-DB storage copy retained rows by unique request ID, so interrupted copies
// can safely resume. Source rows expire normally; do not delete here because
// two different connection pools may actually point to the same database.
func InitializeUserUsageStorage() error {
	storage := userUsageDatabase()
	if err := storage.AutoMigrate(&UserUsageRequest{}, &UserUsageTracking{}); err != nil {
		return err
	}
	var state UserUsageTracking
	err := storage.First(&state, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if state.StorageVersion >= 2 {
		return nil
	}
	if state.StartedAt == 0 && DB.Migrator().HasTable(&UserUsageTracking{}) {
		if err := DB.First(&state, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if state.StartedAt == 0 {
		state = UserUsageTracking{ID: 1, StartedAt: time.Now().Unix()}
	}
	if storage != DB && DB.Migrator().HasTable(&UserUsageRequest{}) {
		var cursor int64
		for {
			var rows []UserUsageRequest
			if err := DB.Where("id > ? AND created_at >= ?", cursor, time.Now().Add(-31*24*time.Hour).Unix()).Order("id").Limit(usageBatchSize).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			cursor = rows[len(rows)-1].ID
			for i := range rows {
				rows[i].ID = 0
			}
			if err := storage.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 50).Error; err != nil {
				return fmt.Errorf("migrate user usage: %w", err)
			}
		}
	}
	state.StorageVersion = 2
	return storage.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]interface{}{"storage_version": 2})}).Create(&state).Error
}

// Delete in indexed batches to avoid a large, long-running deletion transaction.
func CleanupUserUsage(ctx context.Context, db *gorm.DB, now time.Time) error {
	for {
		var ids []int64
		if err := db.WithContext(ctx).Model(&UserUsageRequest{}).Where("created_at < ?", now.Add(-31*24*time.Hour).Unix()).Order("created_at, id").Limit(500).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("id IN ?", ids).Delete(&UserUsageRequest{}).Error; err != nil {
			return err
		}
	}
}

func RunUserUsageRetention(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		storage := userUsageDatabase()
		err := CleanupUserUsage(cleanupCtx, storage, time.Now())
		if err == nil && DB != storage && DB.Migrator().HasTable(&UserUsageRequest{}) {
			err = CleanupUserUsage(cleanupCtx, DB, time.Now())
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			common.SysError("cleanup user usage: " + err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
