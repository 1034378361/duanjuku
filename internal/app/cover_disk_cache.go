package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// coverDiskCache persists cover images to disk so the in-memory coverImageCache
// can be warm on restart and serves as a long-lived (7-day) backing store.
//
// Layout:  <dataDir>/covers/<xx>/<sha256hex>.jpg
// where <xx> is the first two hex chars of the SHA256 for directory sharding.
type coverDiskCache struct {
	dir string // root covers directory
	ttl time.Duration
}

func newCoverDiskCache(dataDir string) *coverDiskCache {
	return &coverDiskCache{
		dir: filepath.Join(dataDir, "covers"),
		ttl: 7 * 24 * time.Hour,
	}
}

// key converts a remote URL + referer to a deterministic file path.
func (c *coverDiskCache) key(cacheKey string) string {
	sum := sha256.Sum256([]byte(cacheKey))
	hex := fmt.Sprintf("%x", sum[:])
	return filepath.Join(c.dir, hex[:2], hex+".jpg")
}

// load returns cached bytes if the file exists and is not stale.
// Returns (nil, nil) on cache miss so callers can fall through to the fetch function.
func (c *coverDiskCache) load(cacheKey string) ([]byte, error) {
	path := c.key(cacheKey)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // cache miss
	}
	if err != nil {
		return nil, err
	}
	if time.Since(info.ModTime()) > c.ttl {
		_ = os.Remove(path)
		return nil, nil // stale
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Touch mtime to extend TTL for hot entries.
	_ = os.Chtimes(path, time.Now(), time.Now())
	return data, nil
}

// store writes bytes to disk. Silently ignores write errors so a full disk
// degrades gracefully (images fetched from remote but not persisted).
func (c *coverDiskCache) store(cacheKey string, data []byte) {
	if len(data) == 0 {
		return
	}
	path := c.key(cacheKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, path)
}

// prune deletes cover files older than TTL. Safe to call in a background goroutine.
func (c *coverDiskCache) prune() {
	root := c.dir
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-c.ttl)
	var removed, kept int
	for _, shard := range entries {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		shardDir := filepath.Join(root, shard.Name())
		files, err := os.ReadDir(shardDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jpg") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(deadline) {
				_ = os.Remove(filepath.Join(shardDir, f.Name()))
				removed++
			} else {
				kept++
			}
		}
	}
	if removed > 0 {
		logInfo("封面磁盘缓存清理完成", "removed", removed, "kept", kept)
	}
}

// GET /api/ui/cover/stats — disk cache statistics
func (a *UIApp) handleCoverCacheStats(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", "GET")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	disk := a.coverDisk
	if disk == nil {
		writeJSON(writer, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	entries, _ := os.ReadDir(disk.dir)
	var totalFiles, totalBytes int64
	for _, shard := range entries {
		if !shard.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(disk.dir, shard.Name()))
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jpg") {
				continue
			}
			if info, err := f.Info(); err == nil {
				totalFiles++
				totalBytes += info.Size()
			}
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"enabled":    true,
		"dir":        disk.dir,
		"ttlHours":   int(disk.ttl.Hours()),
		"fileCount":  totalFiles,
		"totalBytes": totalBytes,
		"totalMB":    fmt.Sprintf("%.1f", float64(totalBytes)/1024/1024),
	})
}

// POST /api/ui/cover/prune — trigger a manual prune of stale cache files
func (a *UIApp) handleCoverCachePrune(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	disk := a.coverDisk
	if disk == nil {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "disabled"})
		return
	}
	go disk.prune()
	writeJSON(writer, http.StatusOK, map[string]string{"status": "pruning"})
}
