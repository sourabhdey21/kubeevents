package watcher

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeevents/kubeevents/internal/config"
	"github.com/kubeevents/kubeevents/internal/filter"
	"github.com/kubeevents/kubeevents/internal/notifier"
	"github.com/kubeevents/kubeevents/internal/store"
)

// EventWatcher watches the Kubernetes Events API and notifies on changes.
type EventWatcher struct {
	client   kubernetes.Interface
	cfg      *config.Config
	filter   *filter.Filter
	notifier *notifier.Telegram
	store    *store.Store
	started  time.Time
}

// New creates an EventWatcher.
func New(client kubernetes.Interface, cfg *config.Config, n *notifier.Telegram, st *store.Store) *EventWatcher {
	return &EventWatcher{
		client:   client,
		cfg:      cfg,
		filter:   filter.New(cfg),
		notifier: n,
		store:    st,
		started:  time.Now(),
	}
}

// Run starts the informer and blocks until context cancellation.
func (w *EventWatcher) Run(ctx context.Context) error {
	w.seedRecent(ctx)

	namespace := w.cfg.Namespace
	opts := []informers.SharedInformerOption{}
	if namespace != "" {
		opts = append(opts, informers.WithNamespace(namespace))
	}

	factory := informers.NewSharedInformerFactoryWithOptions(w.client, w.cfg.ResyncPeriod, opts...)
	informer := factory.Core().V1().Events().Informer()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ev, ok := obj.(*corev1.Event)
			if !ok {
				return
			}
			w.handle(ev, "ADDED")
		},
		UpdateFunc: func(_, newObj interface{}) {
			ev, ok := newObj.(*corev1.Event)
			if !ok {
				return
			}
			w.handle(ev, "UPDATED")
		},
	})
	if err != nil {
		return fmt.Errorf("register event handler: %w", err)
	}

	factory.Start(ctx.Done())
	klog.InfoS("waiting for event informer cache sync", "namespace", emptyAsAll(namespace))
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return fmt.Errorf("timed out waiting for event informer cache sync")
	}
	klog.InfoS("event informer synced; watching for new events", "uiEvents", w.store.Len())

	<-ctx.Done()
	return ctx.Err()
}

func (w *EventWatcher) seedRecent(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	opts := metav1.ListOptions{Limit: 100}
	var list *corev1.EventList
	var err error
	if w.cfg.Namespace == "" {
		list, err = w.client.CoreV1().Events("").List(listCtx, opts)
	} else {
		list, err = w.client.CoreV1().Events(w.cfg.Namespace).List(listCtx, opts)
	}
	if err != nil {
		klog.ErrorS(err, "failed to seed recent events for UI")
		return
	}

	// Sort-ish by appending older first; List order is not guaranteed, so sort by time.
	items := list.Items
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if eventTime(&items[j]).Before(eventTime(&items[i])) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	for i := range items {
		ev := &items[i]
		w.store.Add(ev, "SEED", false)
	}
	klog.InfoS("seeded UI event store", "count", len(items))
}

func (w *EventWatcher) handle(ev *corev1.Event, action string) {
	// Ignore historical events that existed before we started (initial list).
	if eventTime(ev).Before(w.started.Add(-30 * time.Second)) {
		return
	}

	// Always capture for the web UI.
	notified := false
	if w.filter.Allow(ev) {
		notified = true
		klog.InfoS("notifying",
			"type", ev.Type,
			"reason", ev.Reason,
			"kind", ev.InvolvedObject.Kind,
			"name", ev.InvolvedObject.Name,
			"namespace", ev.InvolvedObject.Namespace,
		)

		sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := w.notifier.SendEvent(sendCtx, ev); err != nil {
			klog.ErrorS(err, "failed to send telegram notification",
				"reason", ev.Reason,
				"object", fmt.Sprintf("%s/%s", ev.InvolvedObject.Kind, ev.InvolvedObject.Name),
			)
			notified = false
		}
		cancel()
	} else {
		klog.V(4).InfoS("filtered telegram event (kept for UI)",
			"action", action,
			"type", ev.Type,
			"reason", ev.Reason,
			"kind", ev.InvolvedObject.Kind,
			"name", ev.InvolvedObject.Name,
			"namespace", ev.InvolvedObject.Namespace,
		)
	}

	w.store.Add(ev, action, notified)
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
	if !ev.CreationTimestamp.IsZero() {
		return ev.CreationTimestamp.Time
	}
	return time.Time{}
}

func emptyAsAll(ns string) string {
	if ns == "" {
		return "all"
	}
	return ns
}
