package discovery

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxScanDepth caps recursive descent from every COMPOSE_SCAN_PATH root.
// Home labs rarely nest stacks more than 2 levels deep; 3 keeps the scan
// fast while still catching the "compose-stacks/<name>/docker-compose.yml"
// convention.
const MaxScanDepth = 3

// composeFileNames is the canonical set of filenames we recognise.
// (Compose V2 ships these alongside `docker-compose.yml`.)
var composeFileNames = map[string]bool{
	"compose.yml":     true,
	"compose.yaml":    true,
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
}

// Scanner walks configured roots and surfaces services found in compose
// files. State changes (file modifications) re-trigger discovery on the next
// tick, but we keep the work bounded to avoid runaway CPU.
type Scanner struct {
	roots []string
}

// New returns a Scanner that will walk the given roots. Roots that do not
// exist are silently skipped; callers should validate paths up-front if
// they want a hard error.
func New(roots []string) *Scanner {
	return &Scanner{roots: roots}
}

// Scan performs one full discovery pass and returns a structured Result.
// Errors are collected per-file and returned in Result.Errors rather than
// aborting the scan, so a single malformed file can't hide the rest.
func (s *Scanner) Scan(ctx context.Context) Result {
	var res Result
	now := time.Now()

	if len(s.roots) == 0 {
		return res
	}

	seen := map[string]bool{}

	for _, root := range s.roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Don't recurse into directories we can't read (e.g. perms).
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// WalkDir uses lstat, so symlinked directories aren't followed,
			// but a *file* symlink at the compose name could redirect reads
			// anywhere. Skip those explicitly.
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if depthFromRoot(root, path) > MaxScanDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if !composeFileNames[d.Name()] {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			services, parseErr := parseComposeFile(path, now)
			if parseErr != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, parseErr))
				return nil
			}
			res.Services = append(res.Services, services...)
			return nil
		})
	}

	// Stable sort keeps the UI identical between refreshes.
	sort.SliceStable(res.Services, func(i, j int) bool {
		if res.Services[i].Project != res.Services[j].Project {
			return res.Services[i].Project < res.Services[j].Project
		}
		return res.Services[i].Name < res.Services[j].Name
	})
	return res
}

// depthFromRoot returns how many directories `path` sits below `root`.
// Both paths are expected to be cleaned and absolute.
func depthFromRoot(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return MaxScanDepth + 1
	}
	if rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// composeFile is the minimal shape we read from disk. Docker Compose
// allows merging `services` from a top-level list across multiple yaml
// documents (the legacy "extends" mechanism); we handle that here.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Name     string                    `yaml:"name"`
}

// composeService is the slice of the compose schema we care about.
// All fields are optional; missing ones render as empty in the UI.
type composeService struct {
	Image       string             `yaml:"image"`
	Build       any                `yaml:"build"` // ignored; we want runtime image
	Ports       []any              `yaml:"ports"`
	Labels      map[string]string  `yaml:"labels"`
	ContainerLabels map[string]string `yaml:"container_labels"`
	Environment any                `yaml:"environment"` // ignored
	HealthCheck any                `yaml:"healthcheck"` // ignored
}

// parseComposeFile decodes a docker-compose file (single or multi-doc) and
// returns one Service per entry. We do not actually validate the schema —
// the YAML library reports syntax errors and missing files surface through
// the returned error.
func parseComposeFile(path string, now time.Time) ([]Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var services []Service

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc composeFile
		if err := dec.Decode(&doc); err != nil {
			if err.Error() == "EOF" {
				break
			}
			// One bad doc shouldn't sink the rest of the file: just stop.
			slog.Warn("compose yaml decode error",
				slog.String("path", path),
				slog.String("err", err.Error()),
			)
			break
		}
		project := doc.Name
		if project == "" {
			project = projectFromPath(path)
		}
		for name, svc := range doc.Services {
			s := Service{
				Project:     project,
				Name:        name,
				Image:       svc.Image,
				ComposeFile: path,
				Discovered:  now,
				Ports:       normalisePorts(svc.Ports),
			}
			// Merge labels: docker compose puts container_labels at the
			// container level. We treat both equally for metadata scanning.
			for k, v := range svc.Labels {
				s.Labels, s.Description, s.URL = applyLabel(s.Labels, s.Description, s.URL, k, v)
			}
			for k, v := range svc.ContainerLabels {
				s.Labels, s.Description, s.URL = applyLabel(s.Labels, s.Description, s.URL, k, v)
			}
			// Fall back to image as description if there isn't a label.
			if s.Description == "" {
				s.Description = svc.Image
			}
			services = append(services, s)
		}
	}
	return services, nil
}

// applyLabel updates metadata based on a single compose label. Only labels
// in the "dashboard.*" or "homelab.*" namespaces surface to the UI; the
// rest are ignored to keep responses small and focused.
func applyLabel(labels []string, desc, url, k, v string) ([]string, string, string) {
	lk := strings.ToLower(k)
	known := strings.HasPrefix(lk, "dashboard.") || strings.HasPrefix(lk, "homelab.")
	if !known {
		return labels, desc, url
	}
	labels = append(labels, k+"="+v)
	switch {
	case strings.HasSuffix(lk, "description"):
		if desc == "" {
			desc = v
		}
	case strings.HasSuffix(lk, "url"):
		if url == "" {
			url = v
		}
	case strings.HasSuffix(lk, "title"):
		if desc == "" {
			desc = v
		}
	}
	return labels, desc, url
}

// normalisePorts flattens the multiple compose port representations into a
// uniform list of strings. Compose accepts both "8080:80" and the long
// form {target:80,published:8080,protocol:tcp}. We render both as the
// short "host:container/proto" string.
func normalisePorts(in []any) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		switch v := p.(type) {
		case string:
			out = append(out, shortPort(v))
		case map[string]any:
			out = append(out, renderLongPort(v))
		case int:
			out = append(out, fmt.Sprintf("%d/tcp", v))
		default:
			continue
		}
	}
	return out
}

func shortPort(s string) string {
	// "8080:80/tcp" -> "8080:80/tcp"; "8080" -> "8080/tcp".
	if strings.Contains(s, "/") {
		return s
	}
	return s + "/tcp"
}

func renderLongPort(m map[string]any) string {
	port := asInt(m["target"])
	pub := asInt(m["published"])
	proto, _ := m["protocol"].(string)
	if proto == "" {
		proto = "tcp"
	}
	switch {
	case pub > 0 && port > 0:
		return fmt.Sprintf("%d:%d/%s", pub, port, proto)
	case port > 0:
		return fmt.Sprintf("%d/%s", port, proto)
	}
	return ""
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// projectFromPath derives a project name from the immediate parent of the
// compose file when the file itself doesn't declare a top-level `name:`.
func projectFromPath(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if base == "." || base == "/" {
		return ""
	}
	return base
}
