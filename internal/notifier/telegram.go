package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kubeevents/kubeevents/internal/config"
)

const telegramAPI = "https://api.telegram.org"

// Telegram sends formatted Kubernetes event notifications.
type Telegram struct {
	token              string
	chatID             string
	clusterName        string
	client             *http.Client
	maxPerMinute       int
	mu                 sync.Mutex
	sentTimestamps     []time.Time
}

// NewTelegram creates a Telegram notifier.
func NewTelegram(cfg *config.Config) *Telegram {
	return &Telegram{
		token:        cfg.TelegramBotToken,
		chatID:       cfg.TelegramChatID,
		clusterName:  cfg.ClusterName,
		client:       &http.Client{Timeout: 15 * time.Second},
		maxPerMinute: cfg.MaxMessagesPerMinute,
	}
}

// SendEvent formats and delivers a Kubernetes event.
func (t *Telegram) SendEvent(ctx context.Context, ev *corev1.Event) error {
	if !t.allowSend() {
		klog.V(2).InfoS("rate limit reached, dropping notification",
			"reason", ev.Reason, "object", ev.InvolvedObject.Name)
		return nil
	}
	return t.sendHTML(ctx, formatEvent(t.clusterName, ev))
}

// SendStartup announces that the watcher is online.
func (t *Telegram) SendStartup(ctx context.Context) error {
	msg := fmt.Sprintf(
		"🔔 <b>KubeEvents</b> is online\n"+
			"🏷 Cluster: <code>%s</code>\n"+
			"📡 Watching Kubernetes events and forwarding them here.",
		html.EscapeString(t.clusterName),
	)
	return t.sendHTML(ctx, msg)
}

func (t *Telegram) allowSend() bool {
	if t.maxPerMinute <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	t.mu.Lock()
	defer t.mu.Unlock()

	kept := t.sentTimestamps[:0]
	for _, ts := range t.sentTimestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	t.sentTimestamps = kept

	if len(t.sentTimestamps) >= t.maxPerMinute {
		return false
	}
	t.sentTimestamps = append(t.sentTimestamps, now)
	return true
}

func (t *Telegram) sendHTML(ctx context.Context, text string) error {
	payload := map[string]any{
		"chat_id":                  t.chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPI, t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}
	return nil
}

func formatEvent(cluster string, ev *corev1.Event) string {
	emoji := "ℹ️"
	switch ev.Type {
	case corev1.EventTypeWarning:
		emoji = "⚠️"
	case corev1.EventTypeNormal:
		emoji = "✅"
	}

	obj := ev.InvolvedObject
	ns := obj.Namespace
	if ns == "" {
		ns = "—"
	}

	when := eventTime(ev).UTC().Format(time.RFC3339)

	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b> — <code>%s</code>\n", emoji, html.EscapeString(ev.Reason), html.EscapeString(ev.Type))
	fmt.Fprintf(&b, "🏷 Cluster: <code>%s</code>\n", html.EscapeString(cluster))
	fmt.Fprintf(&b, "📦 %s/<b>%s</b>\n", html.EscapeString(obj.Kind), html.EscapeString(obj.Name))
	fmt.Fprintf(&b, "🗂 Namespace: <code>%s</code>\n", html.EscapeString(ns))
	if ev.Source.Component != "" {
		fmt.Fprintf(&b, "🔧 Source: <code>%s</code>\n", html.EscapeString(ev.Source.Component))
	}
	fmt.Fprintf(&b, "🕒 %s\n", html.EscapeString(when))
	if count := ev.Count; count > 1 {
		fmt.Fprintf(&b, "🔁 Count: <code>%d</code>\n", count)
	}
	fmt.Fprintf(&b, "\n%s", html.EscapeString(ev.Message))
	return b.String()
}

func eventTime(ev *corev1.Event) time.Time {
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}
	if !ev.FirstTimestamp.IsZero() {
		return ev.FirstTimestamp.Time
	}
	return time.Now()
}
