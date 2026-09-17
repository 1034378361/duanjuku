package app

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// handlePlaybackResume handles GET /api/ui/playback/resume?dramaId=...
// It provides a high-performance, single-drama resume lookup for cross-device playback,
// eliminating the need to download the full 500-entry history list.
func (app *UIApp) handlePlaybackResume(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	dramaID := strings.TrimSpace(request.URL.Query().Get("dramaId"))
	if dramaID == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "缺少 dramaId 参数"})
		return
	}
	id, source, valid := playbackHistoryIdentity(dramaID)
	if !valid || !dramaAllowed(request.Context(), id, source) {
		writeJSON(writer, http.StatusOK, map[string]any{"found": false})
		return
	}

	entry, found := viewer.playbackHistory().get(id)
	if !found {
		writeJSON(writer, http.StatusOK, map[string]any{"found": false})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"found": true,
		"entry": entry,
	})
}

// playbackSyncPayload defines the input structure for direct progress synchronization
// without requiring an active browser-managed Web playback session.
type playbackSyncPayload struct {
	DramaID   string  `json:"dramaId"`
	Title     string  `json:"title"`
	Episode   string  `json:"episode"`
	Index     int     `json:"index"`
	Total     int     `json:"total"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Completed bool    `json:"completed"`
}

// handlePlaybackSync handles POST /api/ui/playback/sync
// It allows cross-device clients (Apple TV, mobile apps, third-party webhooks)
// to record playback progress directly into the viewer's account store.
func (app *UIApp) handlePlaybackSync(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	var payload playbackSyncPayload
	decoder := json.NewDecoder(io.LimitReader(request.Body, 8192))
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
		return
	}
	id, source, valid := playbackHistoryIdentity(payload.DramaID)
	if !valid {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的剧集 ID"})
		return
	}
	if !dramaAllowed(request.Context(), id, source) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "无权访问此剧集"})
		return
	}
	if !validPlaybackHistoryTime(payload.Position) || !validPlaybackHistoryTime(payload.Duration) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放时间无效"})
		return
	}

	index := payload.Index
	if index < 1 {
		index = 1
	}
	total := payload.Total
	if total < index {
		total = index
	}

	now := time.Now()
	entry := playbackHistoryEntry{
		DramaID:       id,
		Source:        source,
		Title:         payload.Title,
		Episode:       payload.Episode,
		Index:         index,
		Total:         total,
		Position:      math.Round(payload.Position*1000) / 1000,
		Duration:      payload.Duration,
		Completed:     payload.Completed,
		Mode:          "sync",
		WatchedAt:     now,
		sessionOpened: now,
	}

	store := viewer.playbackHistory()
	saved, err := store.record(entry, now)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": publicError(err).Error()})
		return
	}
	if saved {
		_ = store.flush()
	}
	logInfo("观看进度已跨设备同步", "drama", id, "index", index, "pos", entry.Position)
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "entry": entry})
}
