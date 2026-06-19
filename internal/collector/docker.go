package collector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
	cli, err := client.NewClient(opts...)
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
		s := ContainerSummary{
			ID:           shortID(c.ID),
			Name:         trimPrefix(c.Names, "/"),
			Image:        imageShort(c.Image),
			State:        strings.ToLower(c.State),
			Status:       c.Status,
			Health:       healthFromStatus(c.Status, c.State),
			RestartCount: c.RestartCount,
			Project:      labelValue(c.Labels, "com.docker.compose.project"),
			Ports:        formatPorts(c.Ports),
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

	// Second pass: stats for running containers only.
	runIDs := make([]string, 0, out.Running)
	for _, it := range items {
		if it.State == "running" {
			runIDs = append(runIDs, it.ID)
		}
	}
	if len(runIDs) == 0 {
		out.Items = items
		return out, nil
	}

	// ContainerStats with all IDs in one call. stream=false means one-shot.
	stats, err := d.cli.ContainerStats(ctx, runIDs, false)
	if err != nil {
		// Log and return what we have – stats errors must not grey out the cards.
		slog.Warn("docker stats fetch failed", slog.String("err", err.Error()))
		out.Items = items
		return out, nil
	}

	// Index by short id; Container entries from ContainerList return the
	// same short id once truncated.
	byID := make(map[string]container.StatsResponse, len(stats))
	for _, st := range stats {
		byID[shortID(st.ID)] = st
	}

	for i := range items {
		if items[i].State != "running" {
			continue
		}
		st, ok := byID[items[i].ID]
		if !ok {
			continue
		}
		items[i].CPUPercent = round1(cpuPercentFromStats(st))
		items[i].MemoryUsed = st.MemoryStats.Usage
		if st.MemoryStats.Limit > 0 {
			items[i].MemoryLimit = st.MemoryStats.Limit
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
func formatPorts(ports []container.Port) string {
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
func cpuPercentFromStats(st container.StatsResponse) float64 {
	totalDelta := float64(st.CPUStats.CPUUsage.TotalUsage - st.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(st.CPUStats.SystemCPUUsage - st.PreCPUStats.SystemCPUUsage)
	if systemDelta <= 0 || totalDelta < 0 {
		return 0
	}
	numCPU := float64(st.CPUStats.OnlineCPUs)
	if numCPU <= 0 {
		numCPU = 1
	}
	return (totalDelta / systemDelta) * numCPU * 100.0
}
