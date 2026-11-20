package server

import (
	"os"
	"testing"

	"github.com/alkiory/homelab-dashboard/internal/collector"
	"github.com/alkiory/homelab-dashboard/internal/config"
	"github.com/alkiory/homelab-dashboard/internal/discovery"
	"github.com/alkiory/homelab-dashboard/internal/snapshot"
)

func TestTemplatesRender(t *testing.T) {
	TemplatesFS = os.DirFS("../..")

	store := snapshot.New()
	snap := &snapshot.Snapshot{
		Hardware: collector.Hardware{
			Hostname:      "test-box",
			OS:            "linux",
			UptimeSec:     12345,
			CPUCores:      8,
			CPUPercent:    42.5,
			MemoryTotal:   16 * 1024 * 1024 * 1024,
			MemoryUsed:    8 * 1024 * 1024 * 1024,
			MemoryPercent: 50.0,
			SwapTotal:     4 * 1024 * 1024 * 1024,
			SwapUsed:      1 * 1024 * 1024 * 1024,
			HasTemp:       true,
			TemperatureC:  48.5,
			Disks: []collector.Disk{
				{Path: "/", FSType: "ext4", Total: 500 * 1024 * 1024 * 1024, Used: 200 * 1024 * 1024 * 1024, Percent: 40.0},
			},
		},
		Network: collector.Network{
			ConnectedIPv4: true,
			PublicIP:      "1.2.3.4",
			HasPublicIP:   true,
			Interfaces: []collector.Interface{
				{Name: "eth0", IPv4: "192.168.1.50", OperState: "up", RXBytesRate: 1024, TXBytesRate: 2048},
			},
		},
		Container: collector.Container{
			Available: true,
			Running:   1,
			Total:     1,
			Items: []collector.ContainerSummary{
				{
					ID:         "abc1234",
					FullID:     "abc1234567890",
					Name:       "test-container",
					Image:      "nginx:latest",
					State:      "running",
					Health:     "healthy",
					CPUPercent: 1.2,
					CanRestart: true,
					CanStop:    true,
				},
			},
		},
		Services: discovery.Result{
			Services: []discovery.Service{
				{Project: "web", Name: "app", URL: "http://localhost:8080", Ports: []string{"8080"}},
			},
		},
	}
	store.Swap(snap)

	srv := New(config.Config{
		Port:           3001,
		DashboardTitle: "Test Lab",
	}, store)

	t.Run("renderHardware", func(t *testing.T) {
		body, err := srv.renderHardware(snap)
		if err != nil {
			t.Fatalf("renderHardware failed: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("renderHardware returned empty body")
		}
	})

	t.Run("renderNetwork", func(t *testing.T) {
		body, err := srv.renderNetwork(snap)
		if err != nil {
			t.Fatalf("renderNetwork failed: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("renderNetwork returned empty body")
		}
	})

	t.Run("renderContainers", func(t *testing.T) {
		body, err := srv.renderContainers(snap)
		if err != nil {
			t.Fatalf("renderContainers failed: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("renderContainers returned empty body")
		}
	})

	t.Run("renderServices", func(t *testing.T) {
		body, err := srv.renderServices(snap)
		if err != nil {
			t.Fatalf("renderServices failed: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("renderServices returned empty body")
		}
	})

	t.Run("renderLayout", func(t *testing.T) {
		tmpl, err := getTemplate("layout")
		if err != nil {
			t.Fatalf("getTemplate(layout) failed: %v", err)
		}
		buf := &bytesBuffer{}
		if err := tmpl.Execute(buf, srv.viewModel(snap)); err != nil {
			t.Fatalf("layout execution failed: %v", err)
		}
		if len(buf.Bytes()) == 0 {
			t.Fatal("layout returned empty body")
		}
	})
}
