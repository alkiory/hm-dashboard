// Package collector aggregates hardware, network, and container metrics
// into a single Snapshot. The server reads from a cached snapshot, so the
// request path never touches Docker or /proc directly.
package collector

import "time"

// Snapshot is the immutable view served to the UI for one polling cycle.
type Snapshot struct {
	// LastUpdated is when this snapshot was produced by the poller.
	LastUpdated time.Time `json:"lastUpdated"`
	// PollError is the most recent error encountered while gathering data.
	// When non-empty, the UI shows a warning banner but still renders.
	PollError string `json:"pollError,omitempty"`

	Hardware  Hardware  `json:"hardware"`
	Network   Network   `json:"network"`
	Container Container `json:"container"`
}

// Hardware represents the host's CPU, memory, disk, and thermal state.
type Hardware struct {
	CPUPercent    float64 `json:"cpuPercent"`
	CPUCores      int     `json:"cpuCores"`
	MemoryUsed    uint64  `json:"memoryUsed"`
	MemoryTotal   uint64  `json:"memoryTotal"`
	MemoryPercent float64 `json:"memoryPercent"`
	SwapUsed      uint64  `json:"swapUsed"`
	SwapTotal     uint64  `json:"swapTotal"`
	Disks         []Disk  `json:"disks"`
	TemperatureC  float64 `json:"temperatureC"`
	HasTemp       bool    `json:"hasTemp"`
	UptimeSec     uint64  `json:"uptimeSec"`
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
}

// Disk is one mounted filesystem the dashboard reports on.
type Disk struct {
	Path        string  `json:"path"`
	Used        uint64  `json:"used"`
	Total       uint64  `json:"total"`
	Percent     float64 `json:"percent"`
	FSType      string  `json:"fsType"`
}

// Network captures per-interface live counters plus a connection summary.
type Network struct {
	Interfaces    []Interface `json:"interfaces"`
	PublicIP      string      `json:"publicIP,omitempty"`
	HasPublicIP   bool        `json:"hasPublicIP"`
	ConnectedIPv4 bool        `json:"connectedIpv4"`
	RXBytesPerSec float64     `json:"rxBytesPerSec"`
	TXBytesPerSec float64     `json:"txBytesPerSec"`
}

// Interface is one network interface with delta-derived bandwidth.
type Interface struct {
	Name        string `json:"name"`
	IPv4        string `json:"ipv4,omitempty"`
	IPv6        string `json:"ipv6,omitempty"`
	RXBytes     uint64 `json:"rxBytes"`
	TXBytes     uint64 `json:"txBytes"`
	RXBytesRate float64 `json:"rxBytesRate"`
	TXBytesRate float64 `json:"txBytesRate"`
	OperState   string `json:"operState"`
}

// Container is the Docker view of the host's container fleet.
type Container struct {
	Available bool        `json:"available"`
	Total     int         `json:"total"`
	Running   int         `json:"running"`
	Stopped   int         `json:"stopped"`
	Unhealthy int         `json:"unhealthy"`
	Items     []ContainerSummary `json:"items"`
}

// ContainerSummary is a compact per-container record for the table/card grid.
type ContainerSummary struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	State         string  `json:"state"`         // running | exited | paused | restarting | dead
	Status        string  `json:"status"`        // human friendly, e.g. "Up 4 hours"
	Health        string  `json:"health"`        // healthy | unhealthy | starting | none
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsed    uint64  `json:"memoryUsed"`
	MemoryLimit   uint64  `json:"memoryLimit"`
	RestartCount  int     `json:"restartCount"`
	Project       string  `json:"project"`       // compose project name if any
	Ports         string  `json:"ports"`
}
