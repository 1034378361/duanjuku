package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaylistHandlerUnauthorized(t *testing.T) {
	app := &UIApp{
		cfg: Config{
			dataDir:       t.TempDir(),
			adminUsername: "admin",
			adminPassword: "secret-password",
		},
	}
	req := httptest.NewRequest("GET", "/api/ui/playlist.m3u", nil)
	rec := httptest.NewRecorder()
	app.handlePlaylist(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusOK {
		t.Fatalf("expected 401 or 200, got %d", rec.Code)
	}
}

func TestPlaylistHandlerWithBasicAuth(t *testing.T) {
	tempDir := t.TempDir()
	app := &UIApp{
		cfg: Config{
			dataDir:       tempDir,
			adminUsername: "admin",
			adminPassword: "secret-password",
		},
		dramas: []Drama{
			{ID: "hongguo:7000000000000000001", Title: "测试短剧", TotalEpisode: 2},
		},
	}
	req := httptest.NewRequest("GET", "/api/ui/playlist.m3u?scope=all", nil)
	req.SetBasicAuth("admin", "secret-password")
	rec := httptest.NewRecorder()
	app.handlePlaylist(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), "#EXTM3U") {
		t.Fatalf("expected #EXTM3U playlist header, got: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl; charset=utf-8" {
		t.Fatalf("expected mpegurl content type, got: %s", rec.Header().Get("Content-Type"))
	}
}

func TestPlaylistHandlerWithChapters(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("must use cached fixture")
		return nil, nil
	})
	id := "hongguo:7000000000000000001"
	chapters := []Chapter{
		{ID: id + ":1", Source: sourceHongguo, Title: "第1集"},
		{ID: id + ":2", Source: sourceHongguo, Title: "第2集"},
	}
	d.hongguoClient().details["7000000000000000001"] = hongguoDetailEntry{
		Drama:     Drama{ID: id, Title: "霸道总裁爱上我"},
		Chapters:  chapters,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	app := &UIApp{
		downloader: d,
		cfg: Config{
			dataDir:       t.TempDir(),
			adminUsername: "admin",
			adminPassword: "secret-password",
		},
		dramas: []Drama{
			{ID: id, Source: sourceHongguo, Title: "霸道总裁爱上我", TotalEpisode: 2},
		},
	}

	req := httptest.NewRequest("GET", "/api/ui/playlist.m3u?id="+id, nil)
	req.SetBasicAuth("admin", "secret-password")
	rec := httptest.NewRecorder()
	app.handlePlaylist(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#EXTINF:-1") {
		t.Fatalf("expected #EXTINF in body, got: %s", body)
	}
	if !strings.Contains(body, "group-title=\"霸道总裁爱上我\"") {
		t.Fatalf("expected group-title in body, got: %s", body)
	}
	if !strings.Contains(body, "chapter=hongguo%3A7000000000000000001%3A1") {
		t.Fatalf("expected stream url for episode 1, got: %s", body)
	}
}
