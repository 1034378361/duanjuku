package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVHandler(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.mp4")
	_ = os.WriteFile(testFile, []byte("fake-mp4-content"), 0o644)

	app := &UIApp{
		cfg: Config{
			OutputDir:     tempDir,
			adminUsername: "admin",
			adminPassword: "secret-password",
		},
	}

	handler := app.webdavHandler()

	// 1. Unauthorized request
	req1 := httptest.NewRequest("PROPFIND", "/webdav/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec1.Code)
	}
	if rec1.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("expected WWW-Authenticate header")
	}

	// 2. Trailing slash redirect
	req2 := httptest.NewRequest("GET", "/webdav", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect for /webdav, got %d", rec2.Code)
	}

	// 3. Authorized request (PROPFIND)
	req3 := httptest.NewRequest("PROPFIND", "/webdav/", nil)
	req3.SetBasicAuth("admin", "secret-password")
	req3.Header.Set("Depth", "1")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207 Multi-Status for PROPFIND, got %d", rec3.Code)
	}
	if rec3.Header().Get("DAV") != "1, 2" {
		t.Fatalf("expected DAV header '1, 2', got %s", rec3.Header().Get("DAV"))
	}

	// 4. Download file via WebDAV GET
	req4 := httptest.NewRequest("GET", "/webdav/test.mp4", nil)
	req4.SetBasicAuth("admin", "secret-password")
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET /webdav/test.mp4, got %d", rec4.Code)
	}
	if rec4.Body.String() != "fake-mp4-content" {
		t.Fatalf("unexpected content: %s", rec4.Body.String())
	}
}
