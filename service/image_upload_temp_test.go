package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageTempCleanupPreservesActiveRecentAndUnrelatedFiles(t *testing.T) {
	m := imageUploadTempFiles{dir: t.TempDir(), active: make(map[string]struct{})}
	now := time.Now()
	expired := now.Add(-2 * time.Hour)
	active, err := m.create()
	require.NoError(t, err)
	t.Cleanup(func() { m.release(active) })
	_, err = active.WriteString("active")
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(active.Name(), expired, expired))
	for name, data := range map[string]string{"image-expired": "expired", "image-recent": "recent", "other-file": "other"} {
		path := filepath.Join(m.dir, name)
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
		if name != "image-recent" {
			require.NoError(t, os.Chtimes(path, expired, expired))
		}
	}
	require.NoError(t, os.Symlink(filepath.Join(m.dir, "other-file"), filepath.Join(m.dir, "image-symlink")))
	result, err := m.clean(now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, result.DeletedFiles)
	assert.Equal(t, int64(7), result.DeletedBytes)
	assert.Equal(t, 1, result.ActiveFiles)
	assert.NoFileExists(t, filepath.Join(m.dir, "image-expired"))
	for _, name := range []string{active.Name(), filepath.Join(m.dir, "image-recent"), filepath.Join(m.dir, "other-file"), filepath.Join(m.dir, "image-symlink")} {
		assert.FileExists(t, name)
	}
	m.release(active)
	assert.NoFileExists(t, active.Name())
}

func TestImageTempReleaseDeletesFailedUploadImmediately(t *testing.T) {
	m := imageUploadTempFiles{dir: t.TempDir(), active: make(map[string]struct{})}
	file, err := m.create()
	require.NoError(t, err)
	_, err = file.WriteString("partial upload")
	require.NoError(t, err)
	m.release(file)
	assert.NoFileExists(t, file.Name())
	result, err := m.clean(time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, result.ActiveFiles)
	assert.Zero(t, result.DeletedFiles)
}
