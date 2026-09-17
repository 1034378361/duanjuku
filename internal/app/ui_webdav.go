package app

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/webdav"
)

// webdavHandler provides an embedded WebDAV server for Apple TV / iOS Infuse,
// VLC, Kodi, or native OS mounts (Finder / Windows File Explorer).
// It exposes the OutputDir (containing downloaded MP4s and Emby STRM/NFO files)
// secured with HTTP Basic Auth matching admin credentials.
func (a *UIApp) webdavHandler() http.Handler {
	// Ensure output directory exists
	_ = os.MkdirAll(a.cfg.OutputDir, 0o755)

	srv := &webdav.Handler{
		Prefix:     "/webdav",
		FileSystem: webdav.Dir(a.cfg.OutputDir),
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				logDebug("WebDAV request error", "method", r.Method, "path", r.URL.Path, "error", err)
			}
		},
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Allow CORS and basic DAV discovery
		writer.Header().Set("DAV", "1, 2")

		// Strip trailing slash redirect loop if necessary
		if request.URL.Path == "/webdav" {
			http.Redirect(writer, request, "/webdav/", http.StatusMovedPermanently)
			return
		}

		// Authenticate via Basic Auth or session cookie
		user, pass, ok := request.BasicAuth()
		authed := false
		if ok && user == a.cfg.adminUsername && pass == a.cfg.adminPassword {
			authed = true
		} else if ok {
			// Also check against registered local accounts
			manager := a.browserViewers()
			if manager != nil {
				store := manager.accountStore()
				if store != nil {
					store.mu.Lock()
					if acc, exists := store.state.Accounts[user]; exists && verifyAccountPassword(acc, pass) {
						authed = true
					}
					store.mu.Unlock()
				}
			}
		}

		// Allow local loopback for internal tools
		if !authed && (request.RemoteAddr == "127.0.0.1" || strings.HasPrefix(request.RemoteAddr, "127.0.0.1:") || strings.HasPrefix(request.RemoteAddr, "[::1]:")) {
			// On loopback, allow if user header provided or for diagnostic probes
			if request.Header.Get("X-Juku-Internal") == "true" {
				authed = true
			}
		}

		if !authed {
			writer.Header().Set("WWW-Authenticate", `Basic realm="Juku WebDAV"`)
			http.Error(writer, "需要登录短剧库账号访问 WebDAV", http.StatusUnauthorized)
			return
		}

		srv.ServeHTTP(writer, request)
	})
}
