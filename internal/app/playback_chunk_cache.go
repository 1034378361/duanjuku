package app

import (
	"container/list"
	"sync"
	"time"
)

const maxChunkSizeBytes = 8 << 20 // 8 MB max per individual segment

type chunkCacheEntry struct {
	key       string
	data      []byte
	createdAt time.Time
}

// playbackChunkCache is an LRU memory cache for video/audio segments.
type playbackChunkCache struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	items    map[string]*list.Element
	lru      *list.List
}

var globalChunkCache = newPlaybackChunkCache(64 << 20) // 64 MB LRU segment cache

func newPlaybackChunkCache(maxBytes int64) *playbackChunkCache {
	return &playbackChunkCache{
		maxBytes: maxBytes,
		items:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

func (c *playbackChunkCache) Get(key string) ([]byte, bool) {
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, found := c.items[key]
	if !found {
		return nil, false
	}
	// Expire entries older than 30 minutes
	entry := elem.Value.(*chunkCacheEntry)
	if time.Since(entry.createdAt) > 30*time.Minute {
		c.removeElement(elem)
		return nil, false
	}
	c.lru.MoveToFront(elem)
	return entry.data, true
}

func (c *playbackChunkCache) Put(key string, data []byte) {
	size := int64(len(data))
	if key == "" || size == 0 || size > maxChunkSizeBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// If entry already exists, update
	if elem, found := c.items[key]; found {
		entry := elem.Value.(*chunkCacheEntry)
		c.curBytes += size - int64(len(entry.data))
		entry.data = data
		entry.createdAt = time.Now()
		c.lru.MoveToFront(elem)
	} else {
		// Evict oldest entries until within limit
		for c.curBytes+size > c.maxBytes && c.lru.Len() > 0 {
			c.removeElement(c.lru.Back())
		}
		entry := &chunkCacheEntry{key: key, data: data, createdAt: time.Now()}
		elem := c.lru.PushFront(entry)
		c.items[key] = elem
		c.curBytes += size
	}
}

func (c *playbackChunkCache) removeElement(elem *list.Element) {
	c.lru.Remove(elem)
	entry := elem.Value.(*chunkCacheEntry)
	delete(c.items, entry.key)
	c.curBytes -= int64(len(entry.data))
}

func (c *playbackChunkCache) Stats() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"count":    len(c.items),
		"bytes":    c.curBytes,
		"maxBytes": c.maxBytes,
	}
}
