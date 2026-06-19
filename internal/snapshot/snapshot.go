// Package snapshot provides the shared, atomically-swapped state used by
// the poller and the HTTP handlers. One writer (the poller) produces a new
// Snapshot every tick; many readers (HTTP handlers) load it without locks.
package snapshot

import (
	"log/slog"
	"sync/atomic"

	"github.com/alkiory/homelab-dashboard/internal/collector"
	"github.com/alkiory/homelab-dashboard/internal/discovery"
)

// Snapshot bundles the most recent poll-cycle view of the dashboard. It
// is replaced atomically on every tick; readers receive a consistent view.
type Snapshot struct {
	LastUpdated atomic.Int64 // unix nanos; separate so atomic.Int64 can be used
	HasError    atomic.Bool

	Hardware  collector.Hardware
	Network   collector.Network
	Container collector.Container
	Services  discovery.Result
}

// Store holds the latest Snapshot and exposes lock-free load/store.
type Store struct {
	v atomic.Pointer[Snapshot]
}

// New returns an empty Store with a fresh Snapshot already in place so the
// first HTTP request has data to render.
func New() *Store {
	s := &Store{}
	s.v.Store(&Snapshot{})
	return s
}

// Load returns the current snapshot. Always non-nil after New().
func (s *Store) Load() *Snapshot {
	return s.v.Load()
}

// Swap replaces the current snapshot atomically. The previous value will
// be garbage collected once readers are done with it.
func (s *Store) Swap(next *Snapshot) {
	if next == nil {
		return
	}
	s.v.Store(next)
	slog.Debug("snapshot swapped",
		slog.Int64("lastUpdated", next.LastUpdated.Load()),
		slog.Bool("hasError", next.HasError.Load()),
		slog.Int("services", len(next.Services.Services)),
	)
}
