package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alkiory/homelab-dashboard/internal/collector"
	"github.com/alkiory/homelab-dashboard/internal/discovery"
	"github.com/alkiory/homelab-dashboard/internal/snapshot"
)

type serviceGroup struct {
	Name     string
	Services []discovery.Service
}

// TemplatesFS holds the embedded web/ tree. Set by main.go so this
// package has no embed.FS dependency.
var TemplatesFS fs.FS

// templateCache holds one parsed *template.Template per card name.
// Parsed lazily on first request and reused for the program lifetime;
// parsing on every request is the single biggest avoidable CPU cost in
// a Go HTML server.
var (
	templateCache   = map[string]*template.Template{}
	templateCacheMu sync.Mutex
)

// getTemplate returns the parsed template for the named card or page.
// Names match the filenames under web/templates without the extension.
func getTemplate(name string) (*template.Template, error) {
	templateCacheMu.Lock()
	defer templateCacheMu.Unlock()
	if t, ok := templateCache[name]; ok {
		return t, nil
	}
	if TemplatesFS == nil {
		return nil, fmt.Errorf("templates not embedded")
	}
	pattern := "web/templates/" + name + ".html"
	src, err := fs.ReadFile(TemplatesFS, pattern)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	t, err := template.New(name).Funcs(funcMap()).Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	templateCache[name] = t
	return t, nil
}

// funcMap exposes helpers to all templates. Keep this minimal — every
// {{ }} invocation pays a map lookup.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"pct": func(v float64) string {
			if v <= 0 {
				return "0%"
			}
			return fmt.Sprintf("%.1f%%", v)
		},
		"mib":    func(n uint64) string { return collector.FmtBytes(n) },
		"rate":   func(bps float64) string { return collector.FmtRate(bps) },
		"uptime": func(secs uint64) string { return collector.FmtUptime(secs) },
		"ago": func(unixNano int64) string {
			if unixNano == 0 {
				return "priming…"
			}
			d := time.Since(time.Unix(0, unixNano))
			if d < time.Minute {
				return "just now"
			}
			return d.Round(time.Second).String()
		},
		"isHealthy":   func(s string) bool { return s == "healthy" },
		"isUnhealthy": func(s string) bool { return s == "unhealthy" },
		"isRunning":   func(s string) bool { return strings.EqualFold(s, "running") },
		"isStopped":   func(s string) bool { return strings.EqualFold(s, "exited") || strings.EqualFold(s, "dead") },
		"hasPrefix":   strings.HasPrefix,
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"hostname": func(h string) string {
			if h == "" {
				return "host"
			}
			return h
		},
		"safeURL": func(s string) template.URL {
			return template.URL(s)
		},
	}
}

// --- card renderers -------------------------------------------------------

func (s *Server) renderHardware(snap *snapshot.Snapshot) ([]byte, error) {
	t, err := getTemplate("partials/hardware")
	if err != nil {
		return nil, err
	}
	return renderCard(t, map[string]any{
		"Hardware":    snap.Hardware,
		"LastUpdated": snap.LastUpdated.Load(),
	})
}

func (s *Server) renderNetwork(snap *snapshot.Snapshot) ([]byte, error) {
	t, err := getTemplate("partials/network")
	if err != nil {
		return nil, err
	}
	return renderCard(t, map[string]any{
		"Network":     snap.Network,
		"LastUpdated": snap.LastUpdated.Load(),
	})
}

func (s *Server) renderContainers(snap *snapshot.Snapshot) ([]byte, error) {
	t, err := getTemplate("partials/containers")
	if err != nil {
		return nil, err
	}
	s.actionMu.Lock()
	action := s.action
	s.action = containerActionStatus{}
	s.actionMu.Unlock()
	return renderCard(t, map[string]any{
		"Container":   snap.Container,
		"Action":      action,
		"LastUpdated": snap.LastUpdated.Load(),
	})
}

func (s *Server) renderServices(snap *snapshot.Snapshot) ([]byte, error) {
	t, err := getTemplate("partials/services")
	if err != nil {
		return nil, err
	}
	return renderCard(t, map[string]any{
		"Groups":      groupServices(snap.Services.Services),
		"Errors":      snap.Services.Errors,
		"LastUpdated": snap.LastUpdated.Load(),
	})
}

func groupServices(services []discovery.Service) []serviceGroup {
	if len(services) == 0 {
		return nil
	}
	grouped := make(map[string][]discovery.Service)
	for _, svc := range services {
		key := svc.Project
		if key == "" {
			key = "Ungrouped"
		}
		grouped[key] = append(grouped[key], svc)
	}

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	groups := make([]serviceGroup, 0, len(names))
	for _, name := range names {
		group := grouped[name]
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].Name < group[j].Name
		})
		groups = append(groups, serviceGroup{Name: name, Services: group})
	}
	return groups
}

func renderCard(t *template.Template, data map[string]any) ([]byte, error) {
	buf := &bytesBuffer{}
	if err := t.Execute(buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bytesBuffer is a tiny growable buffer we reuse across requests.
// Pooling is unnecessary because each render is one allocation and is
// released the moment the response is written.
type bytesBuffer struct {
	b []byte
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return b.b }

// Ensure discovery + collector types are reachable from this file — if a
// future refactor removes one, the compiler flags it here.
var (
	_ discovery.Service
	_ collector.ContainerSummary
)
