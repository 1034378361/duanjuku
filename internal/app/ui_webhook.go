package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// sendWebhook dispatches an asynchronous notification to the configured Webhook URL.
// It auto-detects Feishu/Lark, WeCom (企业微信), DingTalk, Bark, or defaults to a standard JSON payload.
func sendWebhook(webhookURL, title, content string) {
	if strings.TrimSpace(webhookURL) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		var payload any
		u := strings.ToLower(webhookURL)
		switch {
		case strings.Contains(u, "feishu.cn") || strings.Contains(u, "larksuite.com"):
			// Feishu / Lark bot format
			payload = map[string]any{
				"msg_type": "text",
				"content": map[string]string{
					"text": fmt.Sprintf("【%s】\n%s", title, content),
				},
			}
		case strings.Contains(u, "qyapi.weixin.qq.com"):
			// WeCom (企业微信) bot format
			payload = map[string]any{
				"msg_type": "text",
				"text": map[string]string{
					"content": fmt.Sprintf("【%s】\n%s", title, content),
				},
			}
		case strings.Contains(u, "oapi.dingtalk.com"):
			// DingTalk bot format
			payload = map[string]any{
				"msgtype": "text",
				"text": map[string]string{
					"content": fmt.Sprintf("【%s】\n%s", title, content),
				},
			}
		default:
			// Generic JSON webhook (compatible with Bark, Gotify, ntfy, etc.)
			payload = map[string]any{
				"title":     title,
				"body":      content,
				"message":   content,
				"timestamp": time.Now().Unix(),
			}
		}

		body, err := json.Marshal(payload)
		if err != nil {
			logWarn("Webhook 序列化失败", "error", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
		if err != nil {
			logWarn("Webhook 请求构建失败", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Juku-Webhook/1.0")

		client := &http.Client{Timeout: 8 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			logWarn("Webhook 推送失败", "url", webhookURL, "error", err)
			return
		}
		defer resp.Body.Close()
		logInfo("Webhook 推送完成", "status", resp.StatusCode)
	}()
}

// notifyDramaUpdate checks if any followed drama received new episodes and notifies.
func (app *UIApp) notifyDramaUpdate(drama Drama, newEpisodes, totalEpisodes int) {
	webhookURL := app.cfg.WebhookURL
	if webhookURL == "" {
		return
	}
	title := "短剧更新提醒"
	message := fmt.Sprintf("您追的短剧《%s》已更新 %d 集（现共 %d 集），可在短剧库直接播放！",
		drama.DisplayTitle(), newEpisodes, totalEpisodes)
	sendWebhook(webhookURL, title, message)
}

// POST /api/ui/webhook/test — sends a test notification
func (app *UIApp) handleWebhookTest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		input.URL = app.cfg.WebhookURL
	}
	targetURL := strings.TrimSpace(input.URL)
	if targetURL == "" {
		targetURL = app.cfg.WebhookURL
	}
	if targetURL == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "未配置 Webhook URL，请通过 JUKU_WEBHOOK_URL 或参数提供"})
		return
	}
	sendWebhook(targetURL, "短剧库通知测试", "恭喜！短剧库 Webhook 通知推送功能配置成功。")
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "message": "测试消息已发送"})
}
