package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/object_storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageTempCleanupShortenedIntervalDeletesExpiredFilesAtNewBoundary(t *testing.T) {
	m := imageUploadTempFiles{dir: t.TempDir(), active: make(map[string]struct{})}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cfg := object_storage.Defaults()
	cfg.TempCleanupIntervalMinutes = 1440
	encoded, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original, exists := common.OptionMap[object_storage.OptionKey]
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if exists {
			common.OptionMap[object_storage.OptionKey] = original
		} else {
			delete(common.OptionMap, object_storage.OptionKey)
		}
	})
	startupFile := filepath.Join(m.dir, "image-startup")
	require.NoError(t, os.WriteFile(startupFile, []byte("expired at startup"), 0600))
	old := start.Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(startupFile, old, old))
	var schedule imageUploadCleanupSchedule
	schedule.run(&m, start)
	assert.NoFileExists(t, startupFile, "startup cleanup must run immediately")
	leftover := filepath.Join(m.dir, "image-leftover")
	require.NoError(t, os.WriteFile(leftover, []byte("expired leftover"), 0600))
	require.NoError(t, os.Chtimes(leftover, old, old))
	schedule.run(&m, start.Add(time.Minute))
	assert.FileExists(t, leftover, "the old interval has not elapsed")
	cfg.TempCleanupIntervalMinutes = 5
	encoded, err = common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[object_storage.OptionKey] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	schedule.run(&m, start.Add(5*time.Minute-time.Nanosecond))
	assert.FileExists(t, leftover, "do not clean before the new interval elapses")
	schedule.run(&m, start.Add(5*time.Minute))
	assert.NoFileExists(t, leftover, "the new interval must take effect without waiting 24 hours")
	require.NoError(t, os.WriteFile(leftover, []byte("next expired leftover"), 0600))
	require.NoError(t, os.Chtimes(leftover, old, old))
	schedule.run(&m, start.Add(10*time.Minute-time.Nanosecond))
	assert.FileExists(t, leftover, "the interval restarts after each cleanup")
	schedule.run(&m, start.Add(10*time.Minute))
	assert.NoFileExists(t, leftover)
}
