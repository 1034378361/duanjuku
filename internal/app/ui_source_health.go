package app

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// sourceHealthEntry holds the most recent health-check result for a single source.
type sourceHealthEntry struct {
	Name      string        `json:"name"`
	Source    string        `json:"source"`
	Status    string        `json:"status"`  // "ok" | "degraded" | "down" | "unknown"
	HTTPCode  int           `json:"httpCode,omitempty"`
	Latency   int64         `json:"latencyMs,omitempty"`
	Error     string        `json:"error,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	CheckedAt time.Time     `json:"checkedAt"`
}

// sourceHealthCache caches the latest probe results and refreshes them in the background.
type sourceHealthCache struct {
	mu       sync.RWMutex
	entries  []sourceHealthEntry
	checkedAt time.Time
}

var globalSourceHealth = &sourceHealthCache{}

// GET /api/ui/source-health — returns cached health state for all sources.
// A background refresh is triggered if the cache is stale (> 60s).
func (a *UIApp) handleSourceHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", "GET")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	globalSourceHealth.mu.RLock()
	stale := time.Since(globalSourceHealth.checkedAt) > 60*time.Second
	entries := append([]sourceHealthEntry{}, globalSourceHealth.entries...)
	checkedAt := globalSourceHealth.checkedAt
	globalSourceHealth.mu.RUnlock()

	// Kick off a background refresh if stale, without blocking this request.
	if stale {
		go a.refreshSourceHealth(context.Background())
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"entries":   entries,
		"checkedAt": checkedAt,
		"stale":     stale,
	})
}

// POST /api/ui/source-health/refresh — forces an immediate synchronous health probe.
func (a *UIApp) handleSourceHealthRefresh(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	a.refreshSourceHealth(ctx)

	globalSourceHealth.mu.RLock()
	entries := append([]sourceHealthEntry{}, globalSourceHealth.entries...)
	checkedAt := globalSourceHealth.checkedAt
	globalSourceHealth.mu.RUnlock()

	writeJSON(writer, http.StatusOK, map[string]any{
		"entries":   entries,
		"checkedAt": checkedAt,
		"stale":     false,
	})
}

// refreshSourceHealth probes all source endpoints and updates the cache.
func (a *UIApp) refreshSourceHealth(ctx context.Context) {
	type probeTarget struct {
		name     string
		source   string
		endpoint string
	}
	targets := []probeTarget{
		{"红果短剧", sourceHongguo, a.downloader.providerBaseURL(sourceHongguo) + "/"},
		{"黄果 AI", sourceHuangguoAI, a.downloader.providerBaseURL(sourceHuangguoAI) + "/"},
		{"黄果 Video", sourceHuangguoVideo, a.downloader.providerBaseURL(sourceHuangguoVideo) + "/videos"},
		{"黄豆短剧", sourceHuangdou, a.downloader.providerBaseURL(sourceHuangdou) + "/home"},
	}

	type result struct {
		index int
		entry sourceHealthEntry
	}
	results := make(chan result, len(targets))

	for i, t := range targets {
		i, t := i, t
		go func() {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := time.Now()
			entry := sourceHealthEntry{
				Name:      t.name,
				Source:    t.source,
				CheckedAt: start,
			}
			statusCode, err := a.checkNetworkResource(probeCtx, http.MethodGet, t.endpoint, t.endpoint, false)
			entry.Latency = time.Since(start).Milliseconds()
			entry.HTTPCode = statusCode
			if err != nil {
				entry.Status = "down"
				entry.Error = a.redactError(err)
				if statusCode >= 200 && statusCode < 300 {
					// reachable but content error
					entry.Status = "degraded"
				}
			} else {
				entry.Status = "ok"
			}
			results <- result{index: i, entry: entry}
		}()
	}

	entries := make([]sourceHealthEntry, len(targets))
	for range targets {
		r := <-results
		entries[r.index] = r.entry
	}

	globalSourceHealth.mu.Lock()
	globalSourceHealth.entries = entries
	globalSourceHealth.checkedAt = time.Now()
	globalSourceHealth.mu.Unlock()

	logInfo("站源健康检查完成", "sources", len(entries))
}
