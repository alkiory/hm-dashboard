# Home Lab Dashboard Support Knowledge

This document is for developers who need to maintain, debug, or extend this project. It describes the current checked-in code, not an idealized target state.

## Project Summary

Home Lab Dashboard is a lightweight Go web service that renders a self-hosted dashboard for homelab hosts. It collects host hardware metrics, network state, Docker container status, and service metadata discovered from Docker Compose files.

The app is intentionally small:

- Backend: Go stdlib `net/http`, `html/template`, `log/slog`, embedded static files.
- Metrics: `gopsutil/v3` for system and network data, Docker SDK for container data.
- Frontend: static HTML templates, handcrafted CSS, and vanilla JavaScript fetch polling.
- Runtime state: in-memory only, stored as atomically swapped snapshots.
- Deployment: Docker image plus `docker-compose.yml`, normally behind Nginx Proxy Manager or another reverse proxy.

## Repository Map

```text
main.go                         Program entrypoint, embedded web assets, signal handling.
internal/config/config.go       Environment variable parsing and defaults.
internal/poller/poller.go       Background polling loop and snapshot assembly.
internal/snapshot/snapshot.go   Atomic snapshot store used by poller and HTTP handlers.
internal/collector/             Hardware, network, Docker, and formatting collectors.
internal/discovery/             Compose-file scanner for service discovery.
internal/server/                HTTP routes, lifecycle, template cache, card renderers.
web/templates/                  Full-page layout and card partials.
web/static/css/app.css          Handwritten dashboard styles.
web/static/js/app.js            Theme toggle and card refresh logic.
Dockerfile                      Multi-stage static Go build and Alpine runtime image.
docker-compose.yml              Local/production compose stack using a Docker socket proxy.
.env.example                    Supported runtime environment variables.
```

## Runtime Architecture

Startup flow:

1. `main.go` calls `config.FromEnv()`.
2. `slog` is configured from `LOG_LEVEL`.
3. The embedded `web/` tree is assigned to `server.AssetFS` and `server.TemplatesFS`.
4. A `snapshot.Store` is created with an empty initial snapshot.
5. A `discovery.Scanner` is created from `COMPOSE_SCAN_PATH`.
6. `poller.Run()` starts in a goroutine and immediately primes the first snapshot.
7. The HTTP server listens on `PORT`.

Request flow:

- HTTP handlers never call Docker, `/proc`, `/sys`, or external IP services directly.
- The poller builds a fresh `snapshot.Snapshot` every interval and swaps it into `snapshot.Store`.
- The server reads the current snapshot and renders either the full layout or a card partial.
- Browser-side `web/static/js/app.js` finds elements with `hx-get` attributes and refreshes each card with `fetch()`. There is no HTMX library shipped even though the template attributes use `hx-*` names.

## Key Runtime Concepts

### Snapshot Store

`internal/snapshot` is the central concurrency boundary. The poller is the only writer. HTTP requests are readers.

Important rule: do not copy `snapshot.Snapshot` values after atomic fields have been used. Build a fresh `&snapshot.Snapshot{...}` and call `Store.Swap(next)`, as `internal/poller/poller.go` currently does.

### Poller

`internal/poller/poller.go` runs one tick immediately, then repeats at `POLLING_INTERVAL`.

Each tick:

- Creates a context with a deadline equal to the polling interval.
- Runs hardware, network, and Docker collectors concurrently.
- Scans compose files after collectors complete.
- Preserves the last good Docker container view when Docker temporarily becomes unavailable.
- Sets `Snapshot.HasError` when any key collector reports an issue.

### Collectors

`internal/collector/system.go` reads CPU, memory, swap, disks, uptime, OS, hostname, and Linux thermal zones. Virtual filesystems are filtered out of disk reporting.

`internal/collector/network.go` reads interface counters, connection status, interface addresses, `/sys/class/net/*/operstate`, and optionally public IP. Public IP lookups use external HTTPS services and are cached for one minute.

`internal/collector/docker.go` talks to Docker through `DOCKER_HOST` when set, otherwise `unix:///var/run/docker.sock`. It lists all containers and fetches one-shot stats for running containers.

`internal/collector/netstate.go` stores previous network counters so bandwidth rates can be computed between ticks. The first tick has zero rates because there is no baseline yet.

### Service Discovery

`internal/discovery/scanner.go` walks each `COMPOSE_SCAN_PATH` root to depth 3. It recognizes:

- `compose.yml`
- `compose.yaml`
- `docker-compose.yml`
- `docker-compose.yaml`

It skips symlinks, parses YAML documents, and extracts service name, image, ports, project name, and selected metadata labels.

Only labels under `dashboard.*` or `homelab.*` are surfaced. Useful labels:

```yaml
labels:
  dashboard.description: "Friendly description"
  dashboard.url: "https://service.example.com"
  dashboard.title: "Fallback title/description"
```

Top-level compose `name:` becomes the project name. If absent, the compose file's parent directory is used.

### Server and Templates

Routes in `internal/server/server.go`:

- `GET /` renders `web/templates/layout.html`.
- `GET /api/cards/hardware` renders the hardware partial.
- `GET /api/cards/network` renders the network partial.
- `GET /api/cards/containers` renders the containers partial.
- `GET /api/cards/services` renders the services partial.
- `GET /healthz` returns `ok` when the process is alive.
- `GET /readyz` returns `503 priming` until the first snapshot has been produced.
- `GET /static/*` serves embedded CSS and JS.

Templates are parsed lazily and cached in `internal/server/templates.go`. Template helper functions are defined in `funcMap()`.

When adding a new card:

1. Add data fields to the collector or discovery output.
2. Include those fields in `snapshot.Snapshot`.
3. Populate them in `poller.tick()`.
4. Add a template partial under `web/templates/partials/`.
5. Add a renderer in `internal/server/templates.go`.
6. Register a route in `internal/server/server.go`.
7. Add a card section to `web/templates/layout.html`.

## Configuration

Supported environment variables:

| Variable | Default | Notes |
| --- | --- | --- |
| `PORT` | `3001` | Internal HTTP port. |
| `POLLING_INTERVAL` | `15s` | Accepts seconds like `15` or durations like `30s`, `1m`. |
| `DASHBOARD_TITLE` | `Home Lab Dashboard` | Header and page title. |
| `ENABLE_PUBLIC_IP` | `true` | Disable to avoid outbound HTTP calls. Accepts `1/0`, `true/false`, `yes/no`, `on/off`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `COMPOSE_SCAN_PATH` | unset | Comma-separated directories to scan for compose files. |
| `DOCKER_HOST` | unset | Used by Docker SDK. Compose sets this to the socket proxy. |

## Local Development

Run tests and compile checks with a writable Go cache if the default home cache is unavailable:

```bash
GOCACHE=/tmp/hld-go-cache go test ./...
GOCACHE=/tmp/hld-go-cache go build .
```

Run locally:

```bash
PORT=3001 \
POLLING_INTERVAL=15s \
COMPOSE_SCAN_PATH=/path/to/stacks \
ENABLE_PUBLIC_IP=0 \
go run .
```

If running outside Docker and you want container data, Docker must be reachable via either:

```bash
DOCKER_HOST=unix:///var/run/docker.sock
```

or a TCP socket/proxy:

```bash
DOCKER_HOST=tcp://localhost:2375
```

The dashboard still runs without Docker access, but the Containers card will show Docker as unavailable.

## Docker Deployment

The checked-in `docker-compose.yml` uses:

- `tecnativa/docker-socket-proxy` as `hld-docker-proxy`.
- Dashboard port `3001` inside the container, published as host port `3051`.
- `DOCKER_HOST=tcp://hld-docker-proxy:2375`.
- `/proc:/proc:ro` and `/sys:/sys:ro` so host metrics can be read from inside the container.
- `/home/alkiory/projects:/projects:ro` and `COMPOSE_SCAN_PATH=/projects` for compose discovery.
- External Docker network `npm_network`.

Before deploying on a different host, review these host-specific values:

- Published port `3051:3001`.
- External network name `npm_network`.
- Compose scan mount `/home/alkiory/projects:/projects:ro`.
- `COMPOSE_SCAN_PATH=/projects`.

Common commands:

```bash
docker compose build
docker compose up -d
docker compose logs -f --tail 200 home-lab-dashboard
curl -fsS http://localhost:3051/healthz
curl -fsS http://localhost:3051/readyz
```

## Verification Status

Current validation from this workspace:

```text
GOCACHE=/tmp/hld-go-cache go test ./...
```

Result: all packages pass, but every package reports `[no test files]`.

The first attempt without `GOCACHE` failed because the sandboxed environment could not write to `/home/alkiory-dev/.cache/go-build`. That is an environment issue, not a project compile failure.

## Testing Gaps

There are no checked-in tests. Good first tests would cover:

- `internal/config`: env parsing for invalid ints, durations, booleans, and path splitting.
- `internal/collector/format.go`: byte, rate, and uptime formatting.
- `internal/collector/docker.go`: `healthFromStatus`, `imageShort`, `formatPorts`, CPU percent calculations.
- `internal/collector/system.go`: `skipFS`, `parseInt`, thermal-zone selection with stubbed file readers.
- `internal/discovery/scanner.go`: compose parsing, label handling, port normalization, symlink skipping, scan-depth behavior.
- `internal/server`: route registration and status codes with `httptest`.

Several package globals already exist as seams for tests, including Docker env lookup, gopsutil wrappers, public IP validation, and thermal file readers.

## Support Playbook

### Dashboard is up but cards say "Loading..." or "Unable to load data"

Check card endpoints directly:

```bash
curl -i http://localhost:3051/api/cards/hardware
curl -i http://localhost:3051/api/cards/network
curl -i http://localhost:3051/api/cards/containers
curl -i http://localhost:3051/api/cards/services
```

Then check logs:

```bash
docker compose logs --tail 200 home-lab-dashboard
```

Template render errors usually appear as HTTP 500 and `card render failed`.

### `/readyz` returns `503 priming`

The poller has not completed the first tick. If this lasts longer than one polling interval, check logs for collector hangs or Docker timeouts.

```bash
docker compose logs -f home-lab-dashboard
```

### Containers card says Docker is unavailable

Confirm the app has a valid Docker endpoint:

```bash
docker compose ps
docker compose logs --tail 200 hld-docker-proxy
docker compose exec home-lab-dashboard sh -c 'echo "$DOCKER_HOST"'
```

The configured compose expects:

```text
DOCKER_HOST=tcp://hld-docker-proxy:2375
```

Also confirm both services are on the same Docker network.

### Services card is empty

Check:

- `COMPOSE_SCAN_PATH` is set inside the dashboard container.
- The host path is mounted read-only into the container.
- Compose files are within 3 directory levels of a scan root.
- File names match the recognized compose file names.
- Files are not symlinks.

Example:

```bash
docker compose exec home-lab-dashboard sh -c 'echo "$COMPOSE_SCAN_PATH"; find /projects -maxdepth 4 -name compose.yml -o -name compose.yaml -o -name docker-compose.yml -o -name docker-compose.yaml'
```

### Public IP is missing

`ENABLE_PUBLIC_IP` may be disabled, outbound HTTPS may be blocked, or all external IP providers may be unavailable. This only affects display; it should not break the Network card.

To avoid outbound calls entirely:

```text
ENABLE_PUBLIC_IP=0
```

### Hardware metrics look like container metrics

When running in Docker, the app reads from the mounted `/proc` and `/sys`. Confirm these mounts exist and are read-only:

```yaml
volumes:
  - /proc:/proc:ro
  - /sys:/sys:ro
```

Disk visibility depends on what the container can see from its mount namespace. Additional host mount points may need explicit read-only mounts if they should appear in the dashboard.

## Coding Guidelines

- Keep the HTTP request path read-only and fast. Collectors belong in the poller, not in handlers.
- Preserve the single-binary model. Static files should stay under `web/` and be embedded by `main.go`.
- Prefer stdlib and existing dependencies over adding frontend or backend frameworks.
- Keep template helpers small and deterministic.
- Treat Docker as optional. The dashboard should continue rendering hardware, network, and services when Docker is unavailable.
- Be careful with public URLs from compose labels. Templates currently render only `http:` and `https:` URLs as links.
- Do not add persistent storage unless there is a concrete requirement. The current app is stateless.
- If adding new shared state, prefer immutable snapshot fields over mutable global data.

## Known Rough Edges

- There are no automated tests.
- `review.md` and parts of `README.md` contain stale historical notes that do not exactly match the current code and compose file.
- Some comments mention HTMX, but the app currently uses custom vanilla JS polling and does not include the HTMX script.
- Docker restart counts are always zero because `ContainerList` does not include restart count in Docker SDK v25; an inspect call would be needed.
- Public IP lookup performs outbound HTTP requests unless `ENABLE_PUBLIC_IP` is disabled.
- The compose file contains host-specific Spanish comments and paths for the current environment.
