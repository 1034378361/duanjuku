package app

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// handlePlaylist serves standard M3U / IPTV playlists for Apple TV (Infuse, APTV, VidHub),
// VLC, IINA, Kodi, and Android TV players.
// It supports single-drama playlists (?id=...), user followed dramas (default),
// and full-library playlists (?scope=all).
func (app *UIApp) handlePlaylist(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	user, pass, hasBasic := request.BasicAuth()
	authed := false
	owner := ""
	if hasBasic && user == app.cfg.adminUsername && pass == app.cfg.adminPassword {
		authed = true
		owner = user
	} else if hasBasic {
		manager := app.browserViewers()
		if manager != nil {
			store := manager.accountStore()
			if store != nil {
				store.mu.Lock()
				if acc, exists := store.state.Accounts[user]; exists && verifyAccountPassword(acc, pass) {
					authed = true
					owner = acc.ID
				}
				store.mu.Unlock()
			}
		}
	} else {
		manager := app.browserViewers()
		if manager != nil {
			account, _, _, _ := manager.requestAccount(request)
			if account.ID != "" {
				authed = true
				owner = account.ID
			} else {
				store := manager.accountStore()
				if store != nil {
					requireLogin, _ := store.policy()
					if !requireLogin {
						authed = true
					}
				}
			}
		}
	}

	if !authed {
		writer.Header().Set("WWW-Authenticate", `Basic realm="Juku Playlist"`)
		http.Error(writer, "需要登录短剧库账号获取播放列表", http.StatusUnauthorized)
		return
	}

	dramaID := request.URL.Query().Get("id")
	scope := request.URL.Query().Get("scope")

	key, err := app.embySigningKey(true)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法获取播放列表密钥"})
		return
	}

	base := request.URL.Query().Get("baseUrl")
	if base == "" {
		scheme := "http"
		if request.TLS != nil || request.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + request.Host
	}
	base = strings.TrimRight(base, "/")

	var targetDramas []Drama
	app.mu.Lock()
	if dramaID != "" {
		for _, d := range app.dramas {
			if d.ID == dramaID {
				targetDramas = append(targetDramas, d)
				break
			}
		}
	} else if scope == "all" {
		targetDramas = append(targetDramas, app.dramas...)
	} else {
		manager := app.browserViewers()
		if manager != nil {
			v := manager.acquire(viewerID(owner))
			if v != nil {
				fStore := v.followingStore()
				if fStore != nil {
					entries, _ := fStore.list()
					ids := make(map[string]bool, len(entries))
					for _, e := range entries {
						if e.Saved || !e.Completed {
							ids[e.DramaID] = true
						}
					}
					for _, d := range app.dramas {
						if ids[d.ID] {
							targetDramas = append(targetDramas, d)
						}
					}
				}
				v.release()
			}
		}
	}
	app.mu.Unlock()

	if len(targetDramas) == 0 && dramaID != "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "剧库中未找到指定短剧"})
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()

	var sb strings.Builder
	sb.WriteString("#EXTM3U x-tvg-url=\"\"\n")

	for _, drama := range targetDramas {
		if app.downloader == nil {
			continue
		}
		title, chapters, err := app.downloader.GetDramaChapters(ctx, drama.ID)
		if err != nil || len(chapters) == 0 {
			continue
		}
		displayTitle := firstNonEmpty(title, drama.DisplayTitle())
		cover := embyCoverURL(drama, base, key, owner)

		records, err := embyChapterRecords(drama.ID, chapters, nil)
		if err != nil {
			continue
		}

		for _, ch := range records {
			query := url.Values{
				"id":      {drama.ID},
				"chapter": {ch.ID},
				"key":     {embyToken(key, drama.ID, ch.ID, owner)},
			}
			if owner != "" {
				query.Set("account", owner)
			}
			streamURL := base + "/api/emby/stream.m3u8?" + query.Encode()

			epName := fmt.Sprintf("%s - 第%02d集 %s", displayTitle, ch.Number, ch.Title)
			sb.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"%s\" tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n",
				drama.ID, epName, cover, displayTitle, epName))
			sb.WriteString(streamURL + "\n")
		}
	}

	filename := "juku-playlist.m3u"
	if len(targetDramas) == 1 {
		filename = safeFilename(targetDramas[0].DisplayTitle()) + ".m3u"
	}

	content := sb.String()
	writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(content))
}
