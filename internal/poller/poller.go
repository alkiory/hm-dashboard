// Package poller drives a single background goroutine that refreshes the
// dashboard's Snapshot at a fixed interval. All Docker / gopsutil work
// happens here — never on the request path.
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/alkiory/homelab-dashboard/internal/collector"
	"github.com/alkiory/homelab-dashboard/internal/config"
	"github.com/alkiory/homelab-dashboard/internal/discovery"
	"github.com/alkiory/homelab-dashboard/internal/snapshot"
)

// Poller holds the configuration shared with the rest of the app and the
// discovery scanner (which is more expensive than the live metrics).
type Poller struct {
	cfg     config.Config
	store   *snapshot.Store
	scanner *discovery.Scanner
	netSt   *collector.NetState
	// lastGood holds a copy of the most recent container view so we can
	// recover from a transient Docker outage without greying out the
	// container list.
	mu       sync.Mutex
	lastGood collector.Container
}

// New builds a Poller ready to drive the configured cadence.
func New(cfg config.Config, store *snapshot.Store, scanner *discovery.Scanner) *Poller {
	return &Poller{
		cfg:     cfg,
		store:   store,
		scanner: scanner,
		netSt:   collector.NewNetState(),
	}
}

// Run blocks until ctx is cancelled, ticking every cfg.PollingInterval. The
// first tick fires immediately so the first HTTP request doesn't see a
// blank UI.
func (p *Poller) Run(ctx context.Context) {
	// Prime immediately.
	p.tick(ctx)
	t := time.NewTicker(p.cfg.PollingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// tick performs one collection pass and pushes the resulting snapshot. We
// run the three collectors (hardware/network/docker) concurrently, then
// the scanner sequentially. A single error from one collector is recorded
// into Snapshot.HasError but never aborts the cycle.
func (p *Poller) tick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, p.cfg.PollingInterval)
	defer cancel()

	var (
		wg                          sync.WaitGroup
		hwOut                       collector.Hardware
		netOut                      collector.Network
		contOut                     collector.Container
		hwErr, netErr, contErr      error
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		hwOut, hwErr = collector.CollectHardware(tickCtx)
		if hwErr != nil {
			slog.Warn("hardware collect error", slog.String("err", hwErr.Error()))
		}
	}()
	go func() {
		defer wg.Done()
		netOut, netErr = collector.CollectNetwork(tickCtx, p.netSt, p.cfg.EnablePublicIP)
		if netErr != nil {
			slog.Warn("network collect error", slog.String("err", netErr.Error()))
		}
	}()
	go func() {
		defer wg.Done()
		contOut, contErr = collector.CollectContainers(tickCtx)
		if contErr != nil {
			slog.Warn("container collect error", slog.String("err", contErr.Error()))
		}
	}()
	wg.Wait()

	// Cache the last good container view so a one-off daemon outage doesn't
	// wipe the UI.
	if contErr == nil && contOut.Available {
		p.mu.Lock()
		p.lastGood = contOut
		p.mu.Unlock()
	} else if !contOut.Available {
		p.mu.Lock()
		if len(p.lastGood.Items) > 0 {
			contOut = p.lastGood
			contOut.Available = false // marker so UI can show "stale"
		}
		p.mu.Unlock()
	}

	var svc discovery.Result
	if p.scanner != nil && len(p.cfg.ComposeScanPaths) > 0 {
		svc = p.scanner.Scan(tickCtx)
	}

	// Build a fresh snapshot — we never copy structs that contain
	// atomic.Int64 / atomic.Bool, which would otherwise trip `go vet`.
	next := &snapshot.Snapshot{
		Hardware:  hwOut,
		Network:   netOut,
		Container: contOut,
		Services:  svc,
	}
	next.LastUpdated.Store(time.Now().UnixNano())
	next.HasError.Store(hwErr != nil || netErr != nil || (contErr != nil && !contOut.Available))

	p.store.Swap(next)
}
