package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/good-fish-man/logx"
)

const diskCacheVersion = 2

type persistedEntry struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Report    Report    `json:"report"`
}

// DiskResearchCache persists public evidence reports across Runtime restarts.
// Writes use a temporary file and rename so interrupted processes cannot leave
// a partially written cache entry.
type DiskResearchCache struct{ dir string }

func NewDiskResearchCache(dir string) *DiskResearchCache {
	return &DiskResearchCache{dir: filepath.Clean(strings.TrimSpace(dir))}
}

func (c *DiskResearchCache) Get(key string, ttl time.Duration) (Report, bool) {
	path, ok := c.path(key)
	if !ok || ttl <= 0 {
		return Report{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warnw("read persistent research cache failed", "path", path, "error", err)
		}
		return Report{}, false
	}
	var entry persistedEntry
	if err = json.Unmarshal(data, &entry); err != nil {
		log.Warnw("decode persistent research cache failed", "path", path, "error", err)
		return Report{}, false
	}
	if entry.Version != diskCacheVersion || time.Since(entry.CreatedAt) >= ttl {
		return Report{}, false
	}
	return entry.Report, true
}

func (c *DiskResearchCache) Put(key string, report Report) {
	path, ok := c.path(key)
	if !ok || len(report.Items) == 0 {
		return
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		log.Warnw("create persistent research cache directory failed", "dir", c.dir, "error", err)
		return
	}
	data, err := json.Marshal(persistedEntry{Version: diskCacheVersion, CreatedAt: time.Now().UTC(), Report: report})
	if err != nil {
		log.Warnw("encode persistent research cache failed", "key", key, "error", err)
		return
	}
	tmp, err := os.CreateTemp(c.dir, ".research-*.tmp")
	if err != nil {
		log.Warnw("create persistent research cache temporary file failed", "dir", c.dir, "error", err)
		return
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	_ = tmp.Chmod(0o600)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpPath, path)
	}
	if err != nil {
		log.Warnw("write persistent research cache failed", "path", path, "error", err)
	}
}

func (c *DiskResearchCache) path(key string) (string, bool) {
	if c == nil || c.dir == "" || c.dir == "." || key == "" || strings.ContainsAny(key, `/\\`) {
		return "", false
	}
	return filepath.Join(c.dir, key+".json"), true
}

type cacheStore interface {
	Get(string, time.Duration) (Report, bool)
	Put(string, Report)
}

// LayeredResearchCache checks memory first, then disk, and promotes disk hits
// back into memory.
type LayeredResearchCache struct {
	memory *ResearchCache
	disk   cacheStore
}

func NewLayeredResearchCache(dir string) *LayeredResearchCache {
	cache := &LayeredResearchCache{memory: NewResearchCache()}
	if strings.TrimSpace(dir) != "" {
		cache.disk = NewDiskResearchCache(dir)
	}
	return cache
}

func (c *LayeredResearchCache) Get(key string, ttl time.Duration) (Report, bool) {
	if c == nil {
		return Report{}, false
	}
	if report, ok := c.memory.Get(key, ttl); ok {
		return report, true
	}
	if c.disk == nil {
		return Report{}, false
	}
	report, ok := c.disk.Get(key, ttl)
	if ok {
		c.memory.Put(key, report)
	}
	return report, ok
}

func (c *LayeredResearchCache) Put(key string, report Report) {
	if c == nil {
		return
	}
	c.memory.Put(key, report)
	if c.disk != nil {
		c.disk.Put(key, report)
	}
}

func ValidateCacheDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create research cache directory: %w", err)
	}
	test, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return fmt.Errorf("research cache directory is not writable: %w", err)
	}
	name := test.Name()
	if closeErr := test.Close(); closeErr != nil {
		return closeErr
	}
	if err = os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
