package collector

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"
)

// CollectNetwork returns the latest network view: per-interface live
// counters, delta-derived bandwidth, public IP (cached), and a global
// IPv4 connectivity flag. Called by the poller on every tick — never on
// the request path.
//
// The error return is currently nil-on-success; preserved so the poller
// and future refactors can propagate driver-level errors without changing
// the call signature.
func CollectNetwork(ctx context.Context, st *NetState, enablePublicIP bool) (Network, error) {
	var out Network

	// IPv4 connectivity: one cheap TCP ESTABLISHED scan.
	if conn, err := netConnections(ctx, "tcp4"); err == nil {
		out.ConnectedIPv4 = hasEstablished(conn)
	}

	rs := st.sample(ctx)

	// We also need the absolute byte counters for the cards; one call to
	// gopsutil gives us both counters and the per-interface data we need.
	if counters, err := ioCounters(ctx, true); err == nil {
		var rxTotal, txTotal float64
		for _, c := range counters {
			if shouldSkipInterface(c.Name) {
				continue
			}
			row := Interface{
				Name:    c.Name,
				RXBytes: c.BytesRecv,
				TXBytes: c.BytesSent,
			}
			if p, ok := rs.rates[c.Name]; ok {
				row.RXBytesRate = p[0]
				row.TXBytesRate = p[1]
				rxTotal += p[0]
				txTotal += p[1]
			}
			row.OperState = operState(ctx, c.Name)
			row.IPv4, row.IPv6 = interfaceAddresses(c.Name)
			out.Interfaces = append(out.Interfaces, row)
		}
		out.RXBytesPerSec = round1(rxTotal)
		out.TXBytesPerSec = round1(txTotal)
	}

	if enablePublicIP {
		out.PublicIP, out.HasPublicIP = lookupPublicIP(ctx)
	}
	return out, nil
}

// ioCounters is the gopsutil seam. Tests can override it; production is
// the real gnet.IOCountersWithContext. The gopsutil call already respects
// the supplied ctx's deadline, so no separate trampoline is needed.
// gopsutil/v3 v3.24.5 exports the byte-counter struct as IOCountersStat
// (plural), not IOCounterStat.
var ioCounters = func(ctx context.Context, all bool) ([]gnet.IOCountersStat, error) {
	return gnet.IOCountersWithContext(ctx, all)
}

// netConnections is the gopsutil Connections seam.
var netConnections = func(ctx context.Context, kind string) ([]gnet.ConnectionStat, error) {
	return gnet.ConnectionsWithContext(ctx, kind)
}

// --- display helpers -------------------------------------------------------

// shouldSkipInterface returns true for loopback and other virtual interfaces
// that would otherwise dominate the bandwidth totals.
func shouldSkipInterface(name string) bool {
	switch name {
	case "lo":
		return true
	}
	return false
}

// operState returns "up"/"down"/"unknown" from /sys/class/net/<name>/operstate.
func operState(ctx context.Context, name string) string {
	type r struct{ s string }
	ch := make(chan r, 1)
	go func() {
		b, err := readFile("/sys/class/net/" + name + "/operstate")
		if err != nil {
			ch <- r{s: "unknown"}
			return
		}
		s := strings.TrimSpace(b)
		if s == "" {
			s = "unknown"
		}
		ch <- r{s: s}
	}()
	select {
	case <-ctx.Done():
		return "unknown"
	case x := <-ch:
		return x.s
	}
}

func interfaceAddresses(name string) (ipv4, ipv6 string) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.To4() != nil && ipv4 == "" {
			ipv4 = ip.String()
			continue
		}
		if ip.To4() == nil && ipv6 == "" {
			ipv6 = ip.String()
		}
	}
	return ipv4, ipv6
}

func hasEstablished(conns []gnet.ConnectionStat) bool {
	for _, c := range conns {
		if c.Status == "ESTABLISHED" {
			return true
		}
	}
	return false
}

// --- public IP lookup ------------------------------------------------------

var ipCache = struct {
	sync.Mutex
	ip      string
	expires time.Time
}{}

// publicIPServices is an ordered fallback list.
var publicIPServices = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// lookupPublicIP returns the most recently seen external IP and caches it
// for one minute so polling doesn't hammer external services.
func lookupPublicIP(ctx context.Context) (string, bool) {
	ipCache.Lock()
	if ipCache.ip != "" && time.Now().Before(ipCache.expires) {
		out := ipCache.ip
		ipCache.Unlock()
		return out, true
	}
	ipCache.Unlock()

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 3 * time.Second}
	for _, url := range publicIPServices {
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "homelab-dashboard/1.0")
		req.Header.Set("Accept", "text/plain")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if isPlausibleIP(ip) {
			ipCache.Lock()
			ipCache.ip = ip
			ipCache.expires = time.Now().Add(time.Minute)
			ipCache.Unlock()
			return ip, true
		}
	}
	return "", false
}

// isPlausibleIP is split out so tests can inject valid/invalid samples.
// In production it just calls net.ParseIP.
var isPlausibleIP = func(s string) bool {
	return net.ParseIP(s) != nil
}

// fmtRate formats a bandwidth figure as "12.3 KB/s" etc. for the UI.
func fmtRate(bps float64) string {
	const (
		KB = 1024.0
		MB = 1024.0 * 1024.0
		GB = 1024.0 * 1024.0 * 1024.0
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.1f GB/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.1f MB/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.1f KB/s", bps/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}
