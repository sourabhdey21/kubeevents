package filter

import (
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubeevents/kubeevents/internal/config"
)

// Noisy Normal reasons that flood Telegram if left unfiltered.
// Create/delete lifecycle reasons (Created, Started, Killing, SuccessfulCreate,
// SuccessfulDelete) are intentionally kept so users see resource activity.
var noisyNormalReasons = map[string]struct{}{
	"Pulling":           {},
	"Pulled":            {},
	"Scheduled":         {},
	"ScalingReplicaSet": {},
	"ProbeWarning":      {},
	"LeaderElection":    {},
	"NodeReady":         {},
}

// Filter decides whether an event should be notified and deduplicates repeats.
type Filter struct {
	cfg  *config.Config
	mu   sync.Mutex
	seen map[string]time.Time
}

// New creates a Filter from config.
func New(cfg *config.Config) *Filter {
	return &Filter{
		cfg:  cfg,
		seen: make(map[string]time.Time),
	}
}

// Allow returns true if the event should trigger a Telegram notification.
func (f *Filter) Allow(ev *corev1.Event) bool {
	if ev == nil {
		return false
	}

	// Severity gate
	if f.cfg.MinSeverity == "warning" && ev.Type != corev1.EventTypeWarning {
		return false
	}

	// Explicit event type allow-list
	if len(f.cfg.EventTypes) > 0 && !containsFold(f.cfg.EventTypes, ev.Type) {
		return false
	}

	// Resource kind allow-list
	if len(f.cfg.ResourceKinds) > 0 && !containsFold(f.cfg.ResourceKinds, ev.InvolvedObject.Kind) {
		return false
	}

	// Drop noisy Normal chatter
	if f.cfg.SkipNoisyNormals && ev.Type == corev1.EventTypeNormal {
		if _, ok := noisyNormalReasons[ev.Reason]; ok {
			return false
		}
		// Also drop reasons that start with common noisy prefixes
		for prefix := range map[string]struct{}{
			"NodeHas": {}, "NodeAllocatable": {},
		} {
			if strings.HasPrefix(ev.Reason, prefix) {
				return false
			}
		}
	}

	return f.dedup(ev)
}

func (f *Filter) dedup(ev *corev1.Event) bool {
	if f.cfg.DedupWindow <= 0 {
		return true
	}

	key := dedupKey(ev)
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()

	// Opportunistic cleanup
	if len(f.seen) > 5000 {
		for k, t := range f.seen {
			if now.Sub(t) > f.cfg.DedupWindow {
				delete(f.seen, k)
			}
		}
	}

	if last, ok := f.seen[key]; ok && now.Sub(last) < f.cfg.DedupWindow {
		return false
	}
	f.seen[key] = now
	return true
}

func dedupKey(ev *corev1.Event) string {
	return strings.Join([]string{
		ev.InvolvedObject.Namespace,
		ev.InvolvedObject.Kind,
		ev.InvolvedObject.Name,
		ev.Reason,
		ev.Message,
		ev.Type,
	}, "|")
}

func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}
