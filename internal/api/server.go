package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeevents/kubeevents/internal/store"
	"github.com/kubeevents/kubeevents/web"
)

// Server serves the web UI and JSON/SSE APIs.
type Server struct {
	store       *store.Store
	clusterName string
	namespace   string
	mux         *http.ServeMux
}

// New creates an API/UI server.
func New(st *store.Store, clusterName, watchNamespace string) *Server {
	s := &Server{
		store:       st,
		clusterName: clusterName,
		namespace:   watchNamespace,
		mux:         http.NewServeMux(),
	}
	s.routes()
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	s.mux.HandleFunc("/api/meta", s.handleMeta)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	s.mux.HandleFunc("/api/stream", s.handleStream)

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		klog.ErrorS(err, "failed to load embedded UI; UI routes disabled")
		return
	}
	fileServer := http.FileServer(http.FS(staticFS))
	s.mux.Handle("/", spaFallback(fileServer, staticFS))
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	ns := s.namespace
	if ns == "" {
		ns = "all"
	}
	writeJSON(w, map[string]any{
		"clusterName":    s.clusterName,
		"watchNamespace": ns,
		"version":        "0.2.0",
		"now":            time.Now().UTC(),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	total, warnings, normals := s.store.Stats()
	writeJSON(w, map[string]any{
		"total":    total,
		"warnings": warnings,
		"normals":  normals,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := store.Query{
		Type:      r.URL.Query().Get("type"),
		Namespace: r.URL.Query().Get("namespace"),
		Kind:      r.URL.Query().Get("kind"),
		Search:    r.URL.Query().Get("q"),
		Limit:     200,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	events := s.store.List(q)
	writeJSON(w, map[string]any{
		"count":  len(events),
		"events": events,
	})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.store.Subscribe()
	defer s.store.Unsubscribe(ch)

	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: k8s\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func spaFallback(fileServer http.Handler, staticFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(staticFS, path); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
