package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// Cluster identity shown in messages
	ClusterName string

	// Namespace to watch; empty or "*" means all namespaces
	Namespace string

	// Comma-separated event types: Normal, Warning (empty = both)
	EventTypes []string

	// Comma-separated involved object kinds to include (empty = all)
	// e.g. Pod,Deployment,ReplicaSet,Service,Node,Job,CronJob,StatefulSet,DaemonSet
	ResourceKinds []string

	// Skip Normal events that are noisy (e.g. Pulling, Pulled, Scheduled)
	SkipNoisyNormals bool

	// Minimum severity: "all" or "warning" (warning = Warning events only)
	MinSeverity string

	// Deduplicate identical events within this window
	DedupWindow time.Duration

	// Max Telegram messages per minute (rate limit)
	MaxMessagesPerMinute int

	// Include a startup ping to Telegram
	SendStartupMessage bool

	// HTTP listen address for health probes
	HealthAddr string

	// Resync period for the informer
	ResyncPeriod time.Duration
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		TelegramBotToken:     strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChatID:       strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		ClusterName:          envOr("CLUSTER_NAME", "kubernetes"),
		Namespace:            strings.TrimSpace(os.Getenv("WATCH_NAMESPACE")),
		EventTypes:           splitCSV(os.Getenv("EVENT_TYPES")),
		ResourceKinds:        splitCSV(os.Getenv("RESOURCE_KINDS")),
		SkipNoisyNormals:     envBool("SKIP_NOISY_NORMALS", true),
		MinSeverity:          strings.ToLower(envOr("MIN_SEVERITY", "all")),
		DedupWindow:          envDuration("DEDUP_WINDOW", 2*time.Minute),
		MaxMessagesPerMinute: envInt("MAX_MESSAGES_PER_MINUTE", 30),
		SendStartupMessage:   envBool("SEND_STARTUP_MESSAGE", true),
		HealthAddr:           envOr("HEALTH_ADDR", ":8080"),
		ResyncPeriod:         envDuration("RESYNC_PERIOD", 0),
	}

	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.TelegramChatID == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID is required")
	}
	if cfg.MinSeverity != "all" && cfg.MinSeverity != "warning" {
		return nil, fmt.Errorf("MIN_SEVERITY must be 'all' or 'warning', got %q", cfg.MinSeverity)
	}
	if cfg.Namespace == "*" {
		cfg.Namespace = ""
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
