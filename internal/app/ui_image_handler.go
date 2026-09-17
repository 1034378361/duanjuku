package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *UIApp) handleImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	imgPath := strings.TrimSpace(r.URL.Query().Get("url"))
	if imgPath == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	remoteURL, ok := buildImageURL(imgPath)
	if !ok {
		http.Error(w, "invalid image url", http.StatusBadRequest)
		return
	}
	source, allowed := a.imageSource(r.Context(), remoteURL)
	if !allowed {
		writeSourceDenied(w)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), coverSourceKey{}, source), 90*time.Second)
	defer cancel()
	buf, err := a.loadCoverImage(ctx, remoteURL, decodeImageBytes)
	if err != nil {
		if r.Context().Err() == nil {
			remote, _ := url.Parse(remoteURL)
			a.downloader.recordDiagnostic(diagnosticEvent{Event: "cover.failed", Host: remote.Hostname(), Message: a.redactError(err)})
		}
		http.Error(w, a.redactError(err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", imageContentType(buf))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if sourceScopeRestricted(r.Context()) {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}

func buildImageURL(imgPath string) (string, bool) {
	imgPath = strings.TrimSpace(imgPath)
	if imgPath == "" {
		return "", false
	}
	if u, err := url.Parse(imgPath); err == nil && u.IsAbs() {
		if !validImageURL(u) {
			return "", false
		}
		return u.String(), true
	}
	imgPath = strings.TrimLeft(imgPath, "/")
	if imgPath == "" || strings.Contains(imgPath, "..") || strings.Contains(imgPath, "://") {
		return "", false
	}
	if strings.HasPrefix(imgPath, "upload_01/") || strings.HasPrefix(imgPath, "upload/") {
		return "https://pic.zdmhyg.cn/" + imgPath, true
	}
	return "https://zzzznnn.lkkwip.cn/" + imgPath, true
}

func isHuangguoImageCDNHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "pic.tuafjz.cn" || strings.HasSuffix(host, ".zdmhyg.cn")
}

func sourceCoverReferer(source, remoteURL string) string {
	switch source {
	case sourceHongguo:
		return hongguoBaseURL + "/"
	case sourceHuangguoAI:
		return "https://huangguoai.com/"
	case sourceHuangguoVideo:
		return "https://huangguo.video/"
	case sourceHuangdou:
		if remote, err := url.Parse(remoteURL); err == nil {
			host := strings.ToLower(remote.Hostname())
			if host == "tideember.cc" || host == "xqjurgek.top" {
				return remote.Scheme + "://" + remote.Host + "/home"
			}
		}
		return huangdouBaseURL + "/home"
	}
	return imageReferer(remoteURL)
}

func imageReferer(remoteURL string) string {
	if u, err := url.Parse(remoteURL); err == nil {
		if isHuangguoImageCDNHost(u.Hostname()) {
			return "https://huangguoai.com/"
		}
		switch strings.ToLower(u.Hostname()) {
		case "huangguoai.com", "www.huangguoai.com":
			return "https://huangguoai.com/"
		case "huangguo.video", "cdn.huangguo.video":
			return "https://huangguo.video/"
		case "tideember.cc", "xqjurgek.top":
			return u.Scheme + "://" + u.Host + "/home"
		case "d3rorc0p4i1kyz.cloudfront.net":
			return huangdouBaseURL + "/home"
		}
		if isHongguoImageHost(u.Hostname()) {
			return hongguoBaseURL + "/"
		}
	}
	return "https://d2pypzndaqisk.cloudfront.net/"
}

func decodeImageBytes(buf []byte, remoteURL string) []byte {
	if isKnownImage(buf) {
		return buf
	}
	if decoded := decryptHuangguoImage(buf); isKnownImage(decoded) || isHEICImage(decoded) {
		return decoded
	}
	copyBuf := append([]byte(nil), buf...)
	decryptImageHeader(copyBuf)
	if isKnownImage(copyBuf) {
		return copyBuf
	}
	return buf
}

func decryptHuangguoImage(buf []byte) []byte {
	if len(buf) == 0 || len(buf)%aes.BlockSize != 0 {
		return nil
	}
	block, err := aes.NewCipher([]byte("f5d965df75336270"))
	if err != nil {
		return nil
	}
	out := make([]byte, len(buf))
	cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(out, buf)
	if len(out) > 0 {
		pad := int(out[len(out)-1])
		if pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
			valid := true
			for _, b := range out[len(out)-pad:] {
				if int(b) != pad {
					valid = false
					break
				}
			}
			if valid {
				out = out[:len(out)-pad]
			}
		}
	}
	return out
}

func decryptImageHeader(buf []byte) {
	key := []byte("2019ysapp7527")
	limit := len(buf)
	if limit > 100 {
		limit = 100
	}
	for i := 0; i < limit; i++ {
		buf[i] ^= key[i%len(key)]
	}
}

func imageContentType(buf []byte) string {
	if len(buf) >= 12 && buf[0] == 'R' && buf[1] == 'I' && buf[2] == 'F' && buf[3] == 'F' && buf[8] == 'W' && buf[9] == 'E' && buf[10] == 'B' && buf[11] == 'P' {
		return "image/webp"
	}
	if len(buf) >= 8 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4e && buf[3] == 0x47 {
		return "image/png"
	}
	if len(buf) >= 3 && buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46 {
		return "image/gif"
	}
	if len(buf) >= 2 && buf[0] == 0xff && buf[1] == 0xd8 {
		return "image/jpeg"
	}
	return http.DetectContentType(buf)
}

func isKnownImage(buf []byte) bool {
	return imageContentType(buf) == "image/jpeg" || imageContentType(buf) == "image/png" || imageContentType(buf) == "image/gif" || imageContentType(buf) == "image/webp"
}
