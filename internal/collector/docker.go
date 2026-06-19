package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// dockerClient wraps the moby client so the rest of the package deals only
// in our own types. We never expose *client.Client to other code; that keeps
// import-surface small and makes stubbing for tests trivial.
type dockerClient struct {
	cli *client.Client
}

// newDockerClient prefers the Unix socket at DOCKER_HOST if set, otherwise
// falls back to the default /var/run/docker.sock. Any failure is surfaced
// in the snapshot, not propagated, so the rest of the dashboard keeps
// working without Docker access.
func newDockerClient() (*dockerClient, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}
	if host := env("DOCKER_HOST"); host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.WithHost("unix:///var/run/docker.sock"))
	}
	// docker/docker v25.0.6 exposes the variadic Opt-taking constructor as
	// NewClientWithOpts; earlier names (NewClient) returned in a later
	// release require non-variadic options.
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &dockerClient{cli: cli}, nil
}

func (d *dockerClient) close() error {
	if d.cli == nil {
		return nil
	}
	return d.cli.Close()
}

// CollectContainers lists containers and reads one round of stats for each
// running container via ContainerStats with stream=false (one-shot). The
// daemon returns a snapshot per container instead of a stream, which is
// dramatically cheaper for a dashboard workload.
//
// If the Docker daemon is unreachable, Container.Available is set to false
// and an error is returned to the caller. The poller logs it and continues
// with the rest of the snapshot (hardware/network still update normally).
// Exported so the poller package can call it.
func CollectContainers(ctx context.Context) (Container, error) {
	var out Container
	out.Available = false

	d, err := newDockerClient()
	if err != nil {
		return out, fmt.Errorf("docker client: %w", err)
	}
	defer d.close()

	cs, err := d.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return out, fmt.Errorf("docker list: %w", err)
	}

	out.Available = true
	out.Total = len(cs)
	// First pass: cheap metadata. Stats are gathered in a second pass so
	// even if stats calls hang we still show state/image/service info.
	items := make([]ContainerSummary, 0, len(cs))
	for _, c := range cs {
		name := ""
		// c.Names is []string in docker v25.0.6; the first entry starts
		// with "/" which the template does not want. Trim defensively.
		if len(c.Names) > 0 {
			name = trimPrefix(c.Names[0], "/")
		}
		s := ContainerSummary{
			ID:      shortID(c.ID),
			Name:    name,
			Image:   imageShort(c.Image),
			State:   strings.ToLower(c.State),
			Status:  c.Status,
			Health:  healthFromStatus(c.Status, c.State),
			Project: labelValue(c.Labels, "com.docker.compose.project"),
			Ports:   formatPorts(c.Ports),
			// RestartCount is not on the list response in v25.0.6
			// (only on inspect). Left at zero until we add an inspect
			// round-trip, see TODO in follow-ups.
		}
		items = append(items, s)
		switch s.State {
		case "running":
			out.Running++
		case "exited", "dead":
			out.Stopped++
		}
		if s.Health == "unhealthy" {
			out.Unhealthy++
		}
	}

	// Second pass: per-container stats. v25.0.6 ContainerStats returns a
	// types.ContainerStats{Body io.ReadCloser, OSType} — the per-cpu /
	// memory numbers are streamed as JSON inside Body and need to be
	// decoded into container.StatsResponse. Issued as one round-trip per
	// container because v25 has no batch stats API. We wrap the decode
	// + metric writes in an inline closure so defer fires per iteration
	// instead of accumulating N open Bodies until CollectContainers
	// returns (defer-in-loop pitfall).
	for i := range items {
		if items[i].State != "running" {
			continue
		}
		raw, err := d.cli.ContainerStats(ctx, items[i].ID, false)
		if err != nil {
			slog.Warn("docker stats fetch failed",
				slog.String("container", items[i].ID),
				slog.String("err", err.Error()))
			continue
		}
		if err := func() error {
			defer raw.Body.Close()
			var sr types.Stats
			if err := json.NewDecoder(raw.Body).Decode(&sr); err != nil {
				return err
			}
			items[i].CPUPercent = round1(cpuPercentFromStats(sr))
			items[i].MemoryUsed = sr.MemoryStats.Usage
			if sr.MemoryStats.Limit > 0 {
				items[i].MemoryLimit = sr.MemoryStats.Limit
			}
			return nil
		}(); err != nil {
			slog.Warn("docker stats decode failed",
				slog.String("container", items[i].ID),
				slog.String("err", err.Error()))
		}
	}

	out.Items = items
	return out, nil
}

// --- helpers ---------------------------------------------------------------

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func trimPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

// imageShort trims image:tag for display; honours registry ports so
// "registry.local:5000/x" survives.
func imageShort(image string) string {
	sep := strings.LastIndex(image, ":")
	if sep < 0 {
		return image
	}
	if strings.Contains(image[sep:], "/") {
		// The ":" is part of a registry host (port) — keep it.
		return image
	}
	return image[:sep]
}

func labelValue(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}

// healthFromStatus extracts a normalised health string from the Docker
// "Status" field which reads "Up 2 hours (healthy)" or "Exited (0) 3 hours
// ago". For non-running containers we always report "none".
func healthFromStatus(status, state string) string {
	if state != "running" {
		return "none"
	}
	open := strings.Index(status, "(")
	close := strings.Index(status, ")")
	if open > -1 && close > open {
		switch strings.ToLower(status[open+1 : close]) {
		case "healthy":
			return "healthy"
		case "unhealthy":
			return "unhealthy"
		case "starting":
			return "starting"
		}
	}
	return "none"
}

// formatPorts produces a compact "0.0.0.0:80->80/tcp" string per port,
// concatenated by space. Returns "" when no ports are published.
func formatPorts(ports []types.Port) string {
	var b strings.Builder
	for i, p := range ports {
		if i > 0 {
			b.WriteByte(' ')
		}
		ip := p.IP
		if ip == "" {
			ip = "0.0.0.0"
		}
		fmt.Fprintf(&b, "%s:%d->%d/%s", ip, p.PublicPort, p.PrivatePort, p.Type)
	}
	return b.String()
}


// cpuPercentFromStats computes a normalised % CPU using the daemon-supplied
// pre-totals (PreCPUUsage). These are populated by Docker when stream=false.
// Fallback path returns 0 if the daemon hasn't delivered a baseline yet.
func cpuPercentFromStats(st types.Stats) float64 {
	totalDelta := float64(st.CPUStats.CPUUsage.TotalUsage - st.PreCPUStats.CPUUsage.TotalUsage)
	// docker/docker v25.0.6 names the system-wide CPU counter `SystemUsage`
	// (json: system_cpu_usage); older / newer code looked like SystemCPUUsage.
	systemDelta := float64(st.CPUStats.SystemUsage - st.PreCPUStats.SystemUsage)
	if systemDelta <= 0 || totalDelta < 0 {
		return 0
	}
	numCPU := float64(st.CPUStats.OnlineCPUs)
	if numCPU <= 0 {
		numCPU = 1
	}
	return (totalDelta / systemDelta) * numCPU * 100.0
}
