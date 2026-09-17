package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type embyWebhookPayload struct {
	Event                 string `json:"Event"`
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
	Item                  struct {
		Name         string `json:"Name"`
		SeriesName   string `json:"SeriesName"`
		IndexNumber  int    `json:"IndexNumber"`
		RunTimeTicks int64  `json:"RunTimeTicks"`
		Path         string `json:"Path"`
	} `json:"Item"`
	User struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	} `json:"User"`
}

// handleEmbyWebhook receives real-time playback events from Emby Server
// (e.g. running on Apple TV, Android TV, or Web) and synchronizes the
// watch progress back to duanjuku's account history.
func (app *UIApp) handleEmbyWebhook(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var payload embyWebhookPayload
	contentType := request.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if err := request.ParseMultipartForm(4 << 20); err == nil {
			data := request.FormValue("data")
			if data != "" {
				_ = json.Unmarshal([]byte(data), &payload)
			}
		}
	} else {
		body, _ := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		_ = json.Unmarshal(body, &payload)
	}

	event := strings.ToLower(payload.Event)
	if event == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "empty event"})
		return
	}

	// Only process playback progress, pause, or stop events
	if !strings.HasPrefix(event, "playback.") {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "not playback event"})
		return
	}

	var dramaID string
	var chapterID string
	var accountName string

	// 1. Try reading the STRM file if Path is provided
	if payload.Item.Path != "" {
		if strmBytes, err := os.ReadFile(payload.Item.Path); err == nil {
			line := strings.TrimSpace(string(strmBytes))
			if parsed, err := url.Parse(line); err == nil {
				dramaID = parsed.Query().Get("id")
				chapterID = parsed.Query().Get("chapter")
				accountName = parsed.Query().Get("account")
			}
		}
	}

	// 2. Fallback: match by SeriesName
	if dramaID == "" && payload.Item.SeriesName != "" {
		app.mu.Lock()
		for _, drama := range app.dramas {
			if drama.DisplayTitle() == payload.Item.SeriesName {
				dramaID = drama.ID
				break
			}
		}
		app.mu.Unlock()
	}

	if dramaID == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "drama not found"})
		return
	}

	// Determine owner account
	if accountName == "" {
		accountName = payload.User.Name
	}

	// Find the accountRecord
	manager := app.browserViewers()
	if manager == nil {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "no viewer manager"})
		return
	}

	store := manager.accountStore()
	store.mu.Lock()
	account, found := store.state.Accounts[accountName]
	if !found {
		// Fallback to first admin/owner account if user name doesn't match directly
		for _, acc := range store.state.Accounts {
			if acc.Admin {
				account = acc
				found = true
				break
			}
		}
	}
	store.mu.Unlock()

	if !found {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "account not found"})
		return
	}

	positionSec := float64(payload.PlaybackPositionTicks) / 10000000.0
	durationSec := float64(payload.Item.RunTimeTicks) / 10000000.0
	if durationSec <= 0 && positionSec > 0 {
		durationSec = positionSec + 1
	}

	index := payload.Item.IndexNumber
	if index < 1 {
		index = 1
	}

	episodeStr := fmt.Sprintf("第%d集", index)
	if payload.Item.Name != "" && !strings.Contains(payload.Item.Name, "S01E") {
		episodeStr = payload.Item.Name
	}

	completed := event == "playback.stop" && durationSec > 0 && positionSec >= (durationSec-5)

	id, source, valid := playbackHistoryIdentity(dramaID)
	if !valid {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "ignored": "invalid drama ID"})
		return
	}

	title := payload.Item.SeriesName
	if title == "" {
		title = dramaID
	}

	entry := playbackHistoryEntry{
		DramaID:       id,
		Source:        source,
		Title:         title,
		ChapterID:     chapterID,
		Episode:       episodeStr,
		Index:         index,
		Total:         index,
		Position:      positionSec,
		Duration:      durationSec,
		Completed:     completed,
		Mode:          "emby",
		WatchedAt:     time.Now(),
		sessionOpened: time.Now(),
	}

	viewer := manager.acquire(account.viewerID())
	defer viewer.release()

	historyStore := viewer.playbackHistory()
	if saved, err := historyStore.record(entry, time.Now()); err == nil && saved {
		_ = historyStore.flush()
		logInfo("Emby 观看进度已同步", "account", accountName, "drama", title, "ep", index, "pos", int(positionSec), "event", event)
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"ok":      true,
		"drama":   title,
		"episode": index,
		"event":   event,
	})
}
