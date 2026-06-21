package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"depsilo/internal/db"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func dispatch(ctx context.Context, cfg db.WebhookConfig, event Event) error {
	payload, err := formatPayload(cfg.Platform, event)
	if err != nil {
		return fmt.Errorf("format %s payload: %w", cfg.Platform, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func formatPayload(platform string, event Event) ([]byte, error) {
	switch platform {
	case "slack":
		return formatSlack(event)
	case "dingtalk":
		return formatDingTalk(event)
	case "wecom":
		return formatWeCom(event)
	case "feishu":
		return formatFeishu(event)
	default:
		return formatGeneric(event)
	}
}

// Slack: incoming webhook attachment format.
func formatSlack(event Event) ([]byte, error) {
	color := severityColor(event.Severity)
	attachment := map[string]interface{}{
		"color":  color,
		"title":  event.Title,
		"text":   event.Message,
		"footer": "Depsilo",
		"ts":     event.Timestamp.Unix(),
	}
	if event.Detail != "" {
		attachment["fields"] = []map[string]interface{}{
			{"value": event.Detail, "short": false},
		}
	}
	return json.Marshal(map[string]interface{}{
		"attachments": []map[string]interface{}{attachment},
	})
}

// DingTalk: markdown message for bot webhooks.
func formatDingTalk(event Event) ([]byte, error) {
	text := fmt.Sprintf("## %s\n\n%s", event.Title, event.Message)
	if event.Detail != "" {
		text += fmt.Sprintf("\n\n> %s", event.Detail)
	}
	text += fmt.Sprintf("\n\n---\n*Depsilo · %s*", event.Timestamp.Format("2006-01-02 15:04:05"))
	return json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": event.Title,
			"text":  text,
		},
	})
}

// WeCom: markdown message for group bot webhooks.
func formatWeCom(event Event) ([]byte, error) {
	content := fmt.Sprintf("**%s**\n%s", event.Title, event.Message)
	if event.Detail != "" {
		content += fmt.Sprintf("\n> %s", event.Detail)
	}
	content += fmt.Sprintf("\n\n<font color=\"comment\">Depsilo · %s</font>",
		event.Timestamp.Format("2006-01-02 15:04:05"))
	return json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	})
}

// Feishu: interactive card format.
func formatFeishu(event Event) ([]byte, error) {
	feishuColor := severityFeishuColor(event.Severity)
	return json.Marshal(map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title":    map[string]string{"tag": "plain_text", "content": event.Title},
				"template": feishuColor,
			},
			"elements": []map[string]interface{}{
				{
					"tag":  "div",
					"text": map[string]string{"tag": "lark_md", "content": event.Message},
				},
			},
		},
	})
}

// Generic: plain JSON with all fields.
func formatGeneric(event Event) ([]byte, error) {
	return json.Marshal(event)
}

func severityColor(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	case "info":
		return "#439FE0"
	default:
		return "good"
	}
}

func severityFeishuColor(severity string) string {
	switch severity {
	case "critical":
		return "red"
	case "warning":
		return "yellow"
	case "info":
		return "blue"
	default:
		return "green"
	}
}
