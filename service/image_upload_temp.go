package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/object_storage"
)

// This directory is exclusively owned by image uploads, never shared with model body caches.
type imageUploadTempFiles struct {
	sync.Mutex
	dir    string
	active map[string]struct{}
}

var imageUploadTemps = imageUploadTempFiles{dir: filepath.Join(os.TempDir(), "new-api-image-uploads"), active: make(map[string]struct{})}

type ImageUploadCleanupResult struct {
	DeletedFiles int   `json:"deleted_files"`
	DeletedBytes int64 `json:"deleted_bytes"`
	ActiveFiles  int   `json:"active_files"`
}

func CreateImageUploadTemp() (*os.File, error) { return imageUploadTemps.create() }
func ReleaseImageUploadTemp(file *os.File)     { imageUploadTemps.release(file) }

func (m *imageUploadTempFiles) create() (*os.File, error) {
	m.Lock()
	defer m.Unlock()
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(m.dir, "image-*")
	if err == nil {
		m.active[file.Name()] = struct{}{}
	}
	return file, err
}

func (m *imageUploadTempFiles) release(file *os.File) {
	m.Lock()
	defer m.Unlock()
	_ = file.Close()
	if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		common.SysError("failed to remove image upload temporary file")
	}
	delete(m.active, file.Name())
}

func CleanImageUploadTemp(retention time.Duration) (ImageUploadCleanupResult, error) {
	return imageUploadTemps.clean(time.Now().Add(-retention))
}

func (m *imageUploadTempFiles) clean(cutoff time.Time) (ImageUploadCleanupResult, error) {
	var result ImageUploadCleanupResult
	dir, err := os.Open(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer dir.Close()
	// Read a bounded batch rather than allocating an entry for every leftover file.
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "image-") || !entry.Type().IsRegular() {
				continue
			}
			name := filepath.Join(m.dir, entry.Name())
			m.Lock()
			if _, active := m.active[name]; active {
				result.ActiveFiles++
				m.Unlock()
				continue
			}
			info, statErr := entry.Info()
			if statErr == nil && info.ModTime().Before(cutoff) {
				if removeErr := os.Remove(name); removeErr == nil {
					result.DeletedFiles++
					result.DeletedBytes += info.Size()
				} else if !errors.Is(removeErr, os.ErrNotExist) {
					m.Unlock()
					return result, removeErr
				}
			}
			m.Unlock()
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return result, statErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
		if readErr != nil {
			return result, readErr
		}
	}
}

var imageUploadCleanupOnce sync.Once

type imageUploadCleanupSchedule struct {
	lastRun time.Time
}

func (s *imageUploadCleanupSchedule) run(m *imageUploadTempFiles, now time.Time) {
	// Re-read the interval on every poll so shorter settings do not wait for an old deadline.
	cfg, err := object_storage.Get()
	if err != nil || cfg.TempRetentionHours < 1 || cfg.TempCleanupIntervalMinutes < 5 {
		return
	}
	interval := time.Duration(cfg.TempCleanupIntervalMinutes) * time.Minute
	if !s.lastRun.IsZero() && now.Before(s.lastRun.Add(interval)) {
		return
	}
	if _, err = m.clean(now.Add(-time.Duration(cfg.TempRetentionHours) * time.Hour)); err != nil {
		common.SysError("image upload temporary cleanup failed")
	}
	s.lastRun = now
}

func StartImageUploadCleanup() {
	imageUploadCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			var schedule imageUploadCleanupSchedule
			for {
				schedule.run(&imageUploadTemps, time.Now())
				<-ticker.C
			}
		}()
	})
}
