package watcher

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeevents/kubeevents/internal/config"
	"github.com/kubeevents/kubeevents/internal/filter"
	"github.com/kubeevents/kubeevents/internal/notifier"
)

// EventWatcher watches the Kubernetes Events API and notifies on changes.
type EventWatcher struct {
	client   kubernetes.Interface
	cfg      *config.Config
	filter   *filter.Filter
	notifier *notifier.Telegram
	started  time.Time
}

// New creates an EventWatcher.
func New(client kubernetes.Interface, cfg *config.Config, n *notifier.Telegram) *EventWatcher {
	return &EventWatcher{
		client:   client,
		cfg:      cfg,
		filter:   filter.New(cfg),
		notifier: n,
		started:  time.Now(),
	}
}

// Run starts the informer and blocks until context cancellation.
func (w *EventWatcher) Run(ctx context.Context) error {
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
			w.handle(ctx, ev, "ADDED")
		},
		UpdateFunc: func(_, newObj interface{}) {
			ev, ok := newObj.(*corev1.Event)
			if !ok {
				return
			}
			w.handle(ctx, ev, "UPDATED")
		},
		// Deletes of Event objects are not usually interesting for notifications.
	})
	if err != nil {
		return fmt.Errorf("register event handler: %w", err)
	}

	factory.Start(ctx.Done())
	klog.InfoS("waiting for event informer cache sync", "namespace", emptyAsAll(namespace))
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return fmt.Errorf("timed out waiting for event informer cache sync")
	}
	klog.InfoS("event informer synced; watching for new events")

	<-ctx.Done()
	return ctx.Err()
}

func (w *EventWatcher) handle(ctx context.Context, ev *corev1.Event, action string) {
	// Ignore historical events that existed before we started (initial list).
	if eventTime(ev).Before(w.started.Add(-30 * time.Second)) {
		return
	}

	if !w.filter.Allow(ev) {
		klog.V(4).InfoS("filtered event",
			"action", action,
			"type", ev.Type,
			"reason", ev.Reason,
			"kind", ev.InvolvedObject.Kind,
			"name", ev.InvolvedObject.Name,
			"namespace", ev.InvolvedObject.Namespace,
		)
		return
	}

	klog.InfoS("notifying",
		"type", ev.Type,
		"reason", ev.Reason,
		"kind", ev.InvolvedObject.Kind,
		"name", ev.InvolvedObject.Name,
		"namespace", ev.InvolvedObject.Namespace,
	)

	sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := w.notifier.SendEvent(sendCtx, ev); err != nil {
		klog.ErrorS(err, "failed to send telegram notification",
			"reason", ev.Reason,
			"object", fmt.Sprintf("%s/%s", ev.InvolvedObject.Kind, ev.InvolvedObject.Name),
		)
	}
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

// ListRecent is a helper used only for debugging / health hints.
func ListRecent(ctx context.Context, client kubernetes.Interface, namespace string, limit int64) ([]corev1.Event, error) {
	opts := metav1.ListOptions{
		Limit:         limit,
		FieldSelector: fields.Everything().String(),
	}
	var list *corev1.EventList
	var err error
	if namespace == "" {
		list, err = client.CoreV1().Events("").List(ctx, opts)
	} else {
		list, err = client.CoreV1().Events(namespace).List(ctx, opts)
	}
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
