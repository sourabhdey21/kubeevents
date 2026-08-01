package store

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Event is a UI-friendly snapshot of a Kubernetes event.
type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	Count     int32     `json:"count"`
	Timestamp time.Time `json:"timestamp"`
	Notified  bool      `json:"notified"`
	Action    string    `json:"action"`
}

// Store is a thread-safe ring buffer of recent events.
type Store struct {
	mu       sync.RWMutex
	events   []Event
	capacity int
	seq      uint64
	subs     map[chan Event]struct{}
}

// New creates a Store that retains up to capacity events.
func New(capacity int) *Store {
	if capacity <= 0 {
		capacity = 500
	}
	return &Store{
		events:   make([]Event, 0, capacity),
		capacity: capacity,
		subs:     make(map[chan Event]struct{}),
	}
}

// Add inserts or updates an event and fans it out to live subscribers.
func (s *Store) Add(ev *corev1.Event, action string, notified bool) Event {
	rec := FromK8s(ev, action, notified)

	s.mu.Lock()
	s.seq++
	if rec.ID == "" {
		rec.ID = time.Now().UTC().Format("20060102150405") + "-" + itoa(s.seq)
	}
	updated := false
	for i := range s.events {
		if s.events[i].ID == rec.ID {
			s.events[i] = rec
			updated = true
			break
		}
	}
	if !updated {
		s.events = append(s.events, rec)
		if len(s.events) > s.capacity {
			s.events = s.events[len(s.events)-s.capacity:]
		}
	}
	subs := make([]chan Event, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- rec:
		default:
			// Drop if subscriber is slow.
		}
	}
	return rec
}

// List returns events newest-first, optionally filtered.
func (s *Store) List(filter Query) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Event, 0, len(s.events))
	for i := len(s.events) - 1; i >= 0; i-- {
		ev := s.events[i]
		if !filter.Match(ev) {
			continue
		}
		out = append(out, ev)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out
}

// Stats returns simple counters for the UI header.
func (s *Store) Stats() (total, warnings, normals int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total = len(s.events)
	for _, ev := range s.events {
		switch ev.Type {
		case corev1.EventTypeWarning:
			warnings++
		default:
			normals++
		}
	}
	return total, warnings, normals
}

// Len returns the number of stored events.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// Subscribe receives live events. Caller must Unsubscribe.
func (s *Store) Subscribe() chan Event {
	ch := make(chan Event, 32)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a live subscriber.
func (s *Store) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
	close(ch)
}

// Query filters stored events.
type Query struct {
	Type      string
	Namespace string
	Kind      string
	Search    string
	Limit     int
}

// Match reports whether an event passes the query.
func (q Query) Match(ev Event) bool {
	if q.Type != "" && !equalFold(q.Type, ev.Type) {
		return false
	}
	if q.Namespace != "" && !equalFold(q.Namespace, ev.Namespace) {
		return false
	}
	if q.Kind != "" && !equalFold(q.Kind, ev.Kind) {
		return false
	}
	if q.Search != "" {
		needle := toLower(q.Search)
		hay := toLower(ev.Reason + " " + ev.Message + " " + ev.Name + " " + ev.Namespace + " " + ev.Kind)
		if !contains(hay, needle) {
			return false
		}
	}
	return true
}

// FromK8s converts a core Event into a stored record.
func FromK8s(ev *corev1.Event, action string, notified bool) Event {
	ns := ev.InvolvedObject.Namespace
	if ns == "" {
		ns = "—"
	}
	src := ev.Source.Component
	if src == "" && ev.ReportingController != "" {
		src = ev.ReportingController
	}
	ts := eventTime(ev)
	id := string(ev.UID)
	if id == "" {
		id = ev.Namespace + "/" + ev.Name
	}
	return Event{
		ID:        id,
		Type:      ev.Type,
		Reason:    ev.Reason,
		Message:   ev.Message,
		Namespace: ns,
		Kind:      ev.InvolvedObject.Kind,
		Name:      ev.InvolvedObject.Name,
		Source:    src,
		Count:     ev.Count,
		Timestamp: ts,
		Notified:  notified,
		Action:    action,
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
	return time.Now().UTC()
}

func equalFold(a, b string) bool { return toLower(a) == toLower(b) }

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(hay, needle string) bool {
	if needle == "" {
		return true
	}
	n := len(needle)
	if n > len(hay) {
		return false
	}
	for i := 0; i+n <= len(hay); i++ {
		if hay[i:i+n] == needle {
			return true
		}
	}
	return false
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
