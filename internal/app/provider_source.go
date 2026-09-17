package app

import (
	"strings"
)

const (
	sourceHuangguoAI    = "huangguoai"
	sourceHuangguoVideo = "huangguo-video"
	sourceHuangdou      = "huangdou"
	sourceHongguo       = "hongguo"
)

type providerEpisode struct {
	Key   string
	Title string
	URL   string
	Index int
	HLS   string
}

func providerDramaID(source, sourceID string) string {
	return source + ":" + strings.TrimSpace(sourceID)
}

func providerChapterID(source, sourceID, chapterKey string) string {
	return source + ":" + strings.TrimSpace(sourceID) + ":" + strings.TrimSpace(chapterKey)
}

func splitProviderDramaID(id string) (source, sourceID string, ok bool) {
	source, sourceID, ok = strings.Cut(strings.TrimSpace(id), ":")
	source = canonicalProviderSource(source)
	if !ok || strings.TrimSpace(sourceID) == "" || !isHuangguoProviderSource(source) {
		return "", "", false
	}
	return source, strings.TrimSpace(sourceID), true
}

func isHuangguoProviderSource(source string) bool {
	switch canonicalProviderSource(source) {
	case sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou, sourceHongguo:
		return true
	default:
		return false
	}
}

func canonicalProviderSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "huangguo", "huangguoai", "huangguoai.com":
		return sourceHuangguoAI
	case "huangguo-video", "huangguo.video":
		return sourceHuangguoVideo
	case "huangdou", "tideember.cc", "xqjurgek.top":
		return sourceHuangdou
	case "hongguo", "hongguoduanju.com":
		return sourceHongguo
	default:
		return strings.TrimSpace(source)
	}
}
