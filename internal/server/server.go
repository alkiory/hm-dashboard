// Package server wires HTTP handlers to the snapshot store and the
// embedded web assets. We deliberately stay on net/http — Go 1.22's
// pattern-aware ServeMux is good enough and avoids importing gin/chi/etc.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alkiory/homelab-dashboard/internal/config"
	"github.com/alkiory/homelab-dashboard/internal/snapshot"
)

// AssetFS is the subset of the embedded web/ tree that is served as static
// assets (CSS, JS, htmx, etc.). set in main.go so the server package has no
// knowledge of the embed FS layout.
var AssetFS fs.FS

// Server bundles handlers with their dependencies.
type Server struct {
	cfg      config.Config
	store    *snapshot.Store
	mux      *http.ServeMux
	interval time.Duration
}

// New constructs a Server with all routes registered.
func New(cfg config.Config, store *snapshot.Store) *Server {
	s := &Server{
		cfg:      cfg,
		store:    store,
		interval: cfg.PollingInterval,
	}
	s.routes()
	return s
}

// Handler returns the root http.Handler — useful for httptest.
func (s *Server) Handler() http.Handler { return s.mux }

// routes registers every URL pattern on s.mux. Grouped by card endpoint
// for readability.
func (s *Server) routes() {
	mux := http.NewServeMux()

	// Static assets. The AssetFS variable is supplied by main.go.
	staticRoot, _ := fs.Sub(AssetFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(staticRoot)))

	// Card partials — HTMX swaps the inner HTML of the .card body.
	mux.HandleFunc("GET /api/cards/hardware", s.handleCard(s.renderHardware))
	mux.HandleFunc("GET /api/cards/network", s.handleCard(s.renderNetwork))
	mux.HandleFunc("GET /api/cards/containers", s.handleCard(s.renderContainers))
	mux.HandleFunc("GET /api/cards/services", s.handleCard(s.renderServices))

	// Full dashboard page.
	mux.HandleFunc("GET /{$}", s.handleIndex)

	// Health endpoint — used by Docker healthcheck.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness: returns 200 only after the first snapshot tick has run.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.store.Load().LastUpdated.Load() == 0 {
			http.Error(w, "priming", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready"))
	})

	s.mux = mux
}

// handleCard wraps a card renderer with the common headers and the
// card-template dispatch.
func (s *Server) handleCard(render func(*snapshot.Snapshot) ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := render(s.store.Load())
		if err != nil {
			slog.Warn("card render failed", slog.String("err", err.Error()))
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// handleIndex returns the full page on initial load. The page's card
// placeholders are then refreshed by HTMX via the card partials.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := getTemplate("layout")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Strict-Transport-Security", "max-age=86400")
	data := s.viewModel(s.store.Load())
	if err := tmpl.Execute(w, data); err != nil {
		slog.Warn("index render failed", slog.String("err", err.Error()))
	}
}

// viewModel is the single struct passed to all renders. Templates declare
// fields as needed; missing fields are silently ignored.
func (s *Server) viewModel(snap *snapshot.Snapshot) map[string]any {
	m := map[string]any{
		"Title":          s.cfg.DashboardTitle,
		"IntervalMs":     s.interval.Milliseconds(),
		"LastUpdatedAgo": lastUpdatedAgo(snap),
		"PollingSeconds": int(s.interval.Seconds()),
		"HasError":       snap.HasError.Load(),
		"PollError":      "",
		"Now":            time.Now(),
	}
	if snap.HasError.Load() {
		m["PollError"] = "one or more collectors reported an issue; refresh to retry"
	}
	return m
}

// lastUpdatedAgo is a stable string used in the header. Computed once per
// render so multiple templates can show the same value.
func lastUpdatedAgo(snap *snapshot.Snapshot) string {
	ts := snap.LastUpdated.Load()
	if ts == 0 {
		return "priming…"
	}
	d := time.Since(time.Unix(0, ts))
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return strings.ReplaceAll(d.Round(time.Minute).String(), "0s", "")
	}
	return strings.ReplaceAll(d.Round(time.Minute).String(), "0s", "")
}

// staticHandler adapts a fs.FS-based http.FileServer so requests get
// reasonable cache headers and a clean Content-Type.
func staticHandler(fsys fs.FS) http.Handler {
	h := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		h.ServeHTTP(w, r)
	})
}

// --- lifecycle -------------------------------------------------------------

// Listen starts the HTTP server. Blocks until ctx is cancelled; returns
// nil on a clean shutdown, ctx.Err() otherwise.
func (s *Server) Listen(ctx context.Context) error {
	addr := ":" + strconv.Itoa(s.cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", slog.String("addr", addr))
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}


