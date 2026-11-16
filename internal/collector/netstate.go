package collector

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/net"
)

// netState holds previous IOCounters per interface so we can compute
// per-second rates between polling ticks. Kept private; NetState is the
// public seam.
type netState struct {
	mu       sync.Mutex
	last     map[string]net.IOCountersStat
	lastTime time.Time
}

// NetState is the read+write side of the bandwidth-rate cache that the
// poller carries across requests. Splitting the public/private seam keeps
// the API tidy and gives us a single place to add per-test stubs.
type NetState struct {
	s netState
}

// NewNetState returns an empty NetState ready for the first sample.
func NewNetState() *NetState {
	return &NetState{}
}

// rateSample is the per-tick output of NetState.sample(). rates maps
// interface name to (rx,tx) bytes/sec.
type rateSample struct {
	rates   map[string][2]float64
	hadData bool
}

// sample reads current IOCounters, compares against the previous tick, and
// returns rateSample. The first call has no baseline so rates are zero —
// by design, since one sample cannot know the elapsed time.
func (n *NetState) sample(ctx context.Context) rateSample {
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return rateSample{}
	}

	n.s.mu.Lock()
	prev := n.s.last
	prevT := n.s.lastTime
	now := time.Now()
	n.s.last = make(map[string]net.IOCountersStat, len(counters))
	for _, c := range counters {
		n.s.last[c.Name] = c
	}
	elapsed := now.Sub(prevT).Seconds()
	n.s.lastTime = now
	n.s.mu.Unlock()

	out := rateSample{
		rates:   make(map[string][2]float64, len(counters)),
		hadData: prev != nil && elapsed > 0,
	}
	for _, c := range counters {
		if !out.hadData {
			out.rates[c.Name] = [2]float64{0, 0}
			continue
		}
		rx, tx := float64(c.BytesRecv), float64(c.BytesSent)
		if p, ok := prev[c.Name]; ok {
			drx := float64(int64(c.BytesRecv) - int64(p.BytesRecv))
			dtx := float64(int64(c.BytesSent) - int64(p.BytesSent))
			if drx < 0 {
				drx = 0
			}
			if dtx < 0 {
				dtx = 0
			}
			rx = drx / elapsed
			tx = dtx / elapsed
		}
		out.rates[c.Name] = [2]float64{round1(rx), round1(tx)}
	}
	return out
}
