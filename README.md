# Home Lab Dashboard![Home Lab Dashboard](image.png)

<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Docker Image Size](https://img.shields.io/badge/docker%20image-~22%20MB-2496ED?logo=docker&logoColor=white)](Dockerfile)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

A self-hosted, single-binary dashboard for a homelab host. It runs entirely in the background, swaps an in-memory atomic snapshot every polling tick, and
serves server-rendered HTML — no SPA, no database, no third-party browser
dependency.

| Card | What it shows | Source |
| --- | --- | --- |
| **Hardware** | CPU, memory, swap, disks, optional temperature, hostname, OS, uptime | `gopsutil/v3`, `/proc`, `/sys/class/thermal/*` |
| **Network** | Interfaces, link state, address info, bandwidth rates, public IP | `gopsutil/net`, `/sys/class/net`, optional HTTPS IP echo services |
| **Containers** | State, health, CPU/memory usage, action buttons (start / restart / stop) | Docker Engine API via `DOCKER_HOST` |
| **Services** | Compose-discovered projects and their metadata labels | YAML scan of `COMPOSE_SCAN_PATH` roots |

## Table of Contents

- [Screenshots](#screenshots)
- [Highlights](#highlights)
- [Tech Stack](#tech-stack)
- [Architecture](#architecture)
- [Quick Start (Docker Compose)](#quick-start-docker-compose)
- [Local Development](#local-development)
- [Configuration](#configuration)
- [HTTP Routes](#http-routes)
- [Service Discovery & Compose Labels](#service-discovery--compose-labels)
- [Docker Access Model](#docker-access-model)
- [Reverse Proxy (Nginx Proxy Manager)](#reverse-proxy-nginx-proxy-manager)
- [Repository Map](#repository-map)
- [Troubleshooting](#troubleshooting)
- [Known Limitations](#known-limitations)

## Screenshots

![Home Lab Dashboard preview](image.png)

The dashboard renders four auto-refreshing cards in a 2×2 grid. Each card
is fetched independently by the browser-side JavaScript and swapped in
place, so a slow collector only stalls its own card — the others keep
updating.

- **Hardware (top-left).** CPU and Memory meters with live percentages,
  core count, total/used memory, swap, hostname, OS, and uptime. A
  temperature reading is shown when `/sys/class/thermal` exposes one.
  Below the meters, every disk that survives the virtual-filesystem
  filter appears with its own usage bar and filesystem type.
- **Network (top-right).** A connectivity badge (IPv4 up/down dot),
  the public IP (when `ENABLE_PUBLIC_IP` is enabled and a provider
  responds), and a single throughput readout (combined RX/TX rate)
  across the top. The table below lists interfaces with name, IPv4
  address, operstate (up/down/unknown) and per-interface RX/TX rate.
- **Containers (bottom-left, wide).** Count chips for running /
  stopped / unhealthy / total sit at the top. One row per container
  shows the truncated name and image, a state badge (`running` /
  `stopped` / other), a health badge when Docker reports one, and
  inline CPU% and memory-used/limit percentages. A "Details"
  disclosure expands to show the full container ID, compose project,
  status text, ports, recorded healthcheck output for unhealthy
  containers, and Start / Restart / Stop action buttons that POST to
  `/api/containers/{id}/{action}`.
- **Services (bottom-right, wide).** Compose-discovered services are
  grouped by project (`svc-group`). Each tile renders a title, an
  optional description (from `dashboard.description` / `homelab.description`),
  a safe link — only `http:` and `https:` URLs become `<a>` tags; other
  schemes are rendered as text to avoid XSS — and the published ports
  surfaced with the 📡 glyph. Scanner warnings (parse errors, missing
  files) collapse into a `<details>` block above the grid.

> Only one PNG is bundled in this repo today — `image.png`. Per-card
> screenshots are welcome as PRs; drop them into `docs/screenshots/` and
> link them from this section.

## Highlights

- **Single Go binary**, ~22 MB image, <50 MB RAM, no persistent state.
- **Lock-free request path**: collectors never run inside HTTP handlers.
  Hardware, network, Docker, and compose scans execute concurrently on a
  background goroutine that swaps a `snapshot.Snapshot` via
  `atomic.Pointer`.
- **Resilient container view**: when the Docker daemon briefly disappears,
  the dashboard keeps showing the last good container list and marks the
  card as stale instead of going blank.
- **Inline container actions**: start, restart, and stop from the UI — the
  cards refresh in place after each action.
- **Health-aware**: `/healthz` returns `ok`, `/readyz` returns `503 priming`
  until the first tick completes.
- **Dark/light theme toggle**, persisted in `localStorage`, respecting
  `prefers-color-scheme`.
- **Graceful degradation**: Docker (and even service discovery) are
  optional. Hardware and network always render.

## Tech Stack

| Layer | Choice |
| --- | --- |
| Language | Go 1.25 |
| HTTP | Standard library `net/http` with method-aware routes |
| Templates | `html/template`, embedded at compile time |
| Metrics | [`gopsutil/v3`](https://github.com/shirou/gopsutil) |
| Container data | [`docker/docker`](https://pkg.go.dev/github.com/docker/docker/client) SDK |
| YAML | `gopkg.in/yaml.v3` |
| Logging | `log/slog` |
| Frontend | Handcrafted CSS, vanilla JS `fetch()` polling (no framework) |
| Runtime image | Alpine 3.20, nonroot user, multi-stage `Dockerfile` |

## Architecture

```
Browser
   │   HTTPS (terminated upstream, usually NPM)
   ▼
Home Lab Dashboard (single binary)
   │
   ├── background poller goroutine  ─ every POLLING_INTERVAL
   │     ├── hardware collector   (gopsutil, /proc, /sys)
   │     ├── network collector    (gopsutil, /sys/class/net, optional public IP)
   │     ├── container collector  (Docker SDK via DOCKER_HOST)
   │     └── compose scanner      (walks COMPOSE_SCAN_PATH roots to depth 3)
   │
   ▼
atomic snapshot.Store  ── readers: HTTP handlers, writers: poller
   │
   ▼
HTTP handlers render /            from full layout
HTTP handlers render /api/cards/* from card partials
HTTP handlers accept POST  /api/containers/{id}/{action} for start/restart/stop
```

- The poller is the **only writer** of the snapshot. Handlers only call
  `store.Load()` and render templates — they never touch Docker, `/proc`,
  `/sys`, or external services.
- Each tick deadline equals `POLLING_INTERVAL`. Slow collectors cannot pile
  up; a late tick is dropped.
- The card endpoints return HTML fragments (`text/html; charset=utf-8`) so
  they slot straight into `innerHTML` replacement.

## Quick Start (Docker Compose)

The checked-in `docker-compose.yml` is tuned for a specific host. Before
deploying on yours, review and adjust:

| Placeholder | Value in repo | Replace with |
| --- | --- | --- |
| Published port | `3051:3001` | any host port you can spare |
| External network | `npm_network` | your reverse-proxy network |
| Compose scan mount | `/home/alkiory/projects:/projects:ro` | a directory of compose files |
| Compose scan path | `/projects` | the matching path inside the container |

Then:

```bash
docker compose build
docker compose up -d
docker compose ps
docker compose logs -f --tail 200 home-lab-dashboard
```

Smoke-test from the Docker host:

```bash
curl -fsS http://localhost:3051/healthz   # → ok
curl -fsS http://localhost:3051/readyz    # → ready (503 priming until first tick)
```

## Local Development

The default Go cache path can be unwritable in some sandboxes; fall back to
a writable path if `go test` fails with a cache error.

```bash
GOCACHE=/tmp/hld-go-cache go test ./...
GOCACHE=/tmp/hld-go-cache go build .
```

Run directly:

```bash
PORT=3001 \
POLLING_INTERVAL=15s \
COMPOSE_SCAN_PATH=/path/to/stacks \
ENABLE_PUBLIC_IP=0 \
go run .
```

For live Docker data with the binary running on the host, point the SDK at
a socket or TCP endpoint:

```bash
DOCKER_HOST=unix:///var/run/docker.sock      # direct socket
DOCKER_HOST=tcp://localhost:2375            # TCP (e.g. socket-proxy on host)
```

Docker is optional: without it, the Containers card shows "Docker is
unavailable" while Hardware, Network, and Services keep rendering.

## Configuration

All runtime configuration is environment-driven. Everything has a sensible
default so the binary boots with `docker run IMAGE` and no flags.

| Variable | Default | Description / accepted forms |
| --- | --- | --- |
| `PORT` | `3001` | Internal HTTP port. |
| `POLLING_INTERVAL` | `15s` | Accepts bare seconds (`15`) or Go durations (`30s`, `1m`). The compose file in this repo overrides it to `30s` to avoid `context deadline exceeded` under load. |
| `DASHBOARD_TITLE` | `Home Lab Dashboard` | Header brand and `<title>`. |
| `ENABLE_PUBLIC_IP` | `true` | Set to `0` / `false` / `no` / `off` to skip outbound HTTPS calls. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `COMPOSE_SCAN_PATH` | _unset_ | Comma-separated list of directories to walk for compose files. |
| `DOCKER_HOST` | _unset_ | Docker SDK endpoint. Compose sets it to `tcp://hld-docker-proxy:2375`. |

Invalid numeric / duration / boolean values log a warning and keep the
default — the process keeps running.

## HTTP Routes

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/` | Full dashboard page (renders once, then cards refresh in place). |
| `GET` | `/api/cards/hardware` | Hardware card partial. |
| `GET` | `/api/cards/network` | Network card partial. |
| `GET` | `/api/cards/containers` | Containers card partial. |
| `GET` | `/api/cards/services` | Services card partial. |
| `POST` | `/api/containers/{id}/{action}` | Inline container action (`start`, `restart`, `stop`); returns the refreshed Containers card. |
| `GET` | `/healthz` | Liveness; always `ok` while the process is alive. |
| `GET` | `/readyz` | Readiness; `ready` once the first tick has completed, otherwise `503 priming`. |
| `GET` | `/static/*` | Embedded CSS and JS (`Cache-Control: public, max-age=300`). |

The `GET` card endpoints set `Cache-Control: no-store` so partials always
reflect the live snapshot.

## Service Discovery & Compose Labels

`internal/discovery/scanner.go` walks every `COMPOSE_SCAN_PATH` root to
**depth 3** and recognizes the following file names (case-sensitive):

- `compose.yml` / `compose.yaml`
- `docker-compose.yml` / `docker-compose.yaml`

Symlinks are skipped. For each file, it reads multi-document YAML if
present, then extracts `services[*]`, project name (top-level `name:`,
falling back to the parent directory), image, ports, and a small set of
labels.

Only labels prefixed `dashboard.` or `homelab.` are surfaced; everything
else is ignored to avoid noise.

```yaml
services:
  paperless:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    ports:
      - "8000:8000"
    labels:
      dashboard.description: "Document management"
      dashboard.url: "https://paperless.example.com"
      dashboard.title: "Paperless-ngx"
```

The top-level compose `name:` field, when present, becomes the project
name; otherwise the parent directory name is used so stacks still group
correctly.

## Docker Access Model

The default compose stack uses `tecnativa/docker-socket-proxy` as
`hld-docker-proxy`. The dashboard itself never touches
`/var/run/docker.sock` directly:

```
DOCKER_HOST=tcp://hld-docker-proxy:2375
```

The proxy:

- Mounts the host socket **read-only**.
- Exposes a curated API surface (`CONTAINERS`, `INFO`, `POST`, `STATS`,
  `NETWORKS`).
- Lets the dashboard issue start / restart / stop calls without granting
  full socket access.

If you don't want Docker data at all, drop `DOCKER_HOST` and the proxy
service. Hardware, Network, and Services keep working unchanged.

## Reverse Proxy (Nginx Proxy Manager)

The dashboard serves plain HTTP internally. Terminate TLS at Nginx Proxy
Manager (or any other reverse proxy) and forward to the container.

| NPM field | Value |
| --- | --- |
| Scheme | `http` |
| Forward Hostname/IP | `home-lab-dashboard` on the same Docker network, or the host IP |
| Forward Port | `3001` on the Docker network, or `3051` if forwarding to the published host port |
| Websockets Support | Off |
| Cache Assets | Off |
| SSL | Request a certificate, then enable "Force SSL" |

The supplied `docker-compose.yml` attaches the dashboard to the external
`npm_network`. Create or rename that network to match your proxy
environment.

## Repository Map

```text
main.go                          Program entrypoint, embedded web assets, signal handling.
internal/config/config.go        Environment variable parsing and defaults.
internal/poller/poller.go        Background polling loop and snapshot assembly.
internal/snapshot/snapshot.go    Atomic snapshot store (writers: poller, readers: HTTP).
internal/collector/              Hardware, network, Docker, formatting helpers.
internal/discovery/              Compose-file scanner for service discovery.
internal/server/                 HTTP routes, lifecycle, template cache, card renderers.
web/templates/                   Full-page layout and card partials.
web/static/css/app.css           Handwritten dashboard styles.
web/static/js/app.js             Theme toggle, card polling, container-action submitter.
Dockerfile                       Multi-stage static Go build → Alpine 3.20 runtime image.
docker-compose.yml               Stack using tecnativa/docker-socket-proxy.
.env.example                     Documented environment variables.
knowledge.md                     Deeper support notes for maintainers.
```

### Adding a new card

Following the existing order keeps the codebase consistent:

1. Add fields to the collector or discovery output.
2. Extend `snapshot.Snapshot`.
3. Populate them in `poller.tick()`.
4. Add a template partial under `web/templates/partials/`.
5. Add a renderer in `internal/server/templates.go`.
6. Register the route in `internal/server/server.go`.
7. Wire the partial into `web/templates/layout.html`.

## Troubleshooting

### Cards stuck on "Loading…" or "Unable to load data"

Probe each card individually, then read the logs:

```bash
curl -i http://localhost:3051/api/cards/hardware
curl -i http://localhost:3051/api/cards/network
curl -i http://localhost:3051/api/cards/containers
curl -i http://localhost:3051/api/cards/services

docker compose logs --tail 200 home-lab-dashboard
```

Template render failures surface as HTTP 500 with `card render failed` in
the logs.

### `/readyz` returns `503 priming`

The poller hasn't finished its first tick. If this persists longer than
one `POLLING_INTERVAL`, check for collector hangs or Docker timeouts:

```bash
docker compose logs -f home-lab-dashboard
```

### Containers card shows "Docker is unavailable"

```bash
docker compose ps
docker compose logs --tail 200 hld-docker-proxy
docker compose exec home-lab-dashboard sh -c 'echo "$DOCKER_HOST"'
```

The expected value in the shipped compose file is
`tcp://hld-docker-proxy:2375`. Both services must be on the same Docker
network.

### Services card is empty

Check all four signals at once:

```bash
docker compose exec home-lab-dashboard sh -c 'echo "$COMPOSE_SCAN_PATH"; \
  find /projects -maxdepth 4 \
    \( -name compose.yml -o -name compose.yaml \
       -o -name docker-compose.yml -o -name docker-compose.yaml \)'
```

`COMPOSE_SCAN_PATH` must be set inside the container, the host directory
must be mounted read-only, the files must be within 3 directory levels
of a scan root, and the file names must match the recognized list.

### Public IP is missing

Causes, in order of likelihood:

- `ENABLE_PUBLIC_IP=0` / `false`.
- Outbound HTTPS is blocked by the host firewall.
- All external IP echo services are temporarily unavailable.

This only affects display; the rest of the Network card keeps rendering.
Disable public-IP lookups entirely with `ENABLE_PUBLIC_IP=0`.

### Hardware metrics look like container metrics

The container reads host-like data only through these mounts:

```yaml
volumes:
  - /proc:/proc:ro
  - /sys:/sys:ro
```

If those are missing, the numbers reflect the container's view of
`/proc`, not the host. Disk visibility depends on the container's mount
namespace — add explicit read-only mounts for any host path that should
appear in the dashboard.

### Container start / restart / stop never returns

The action is dispatched via `POST /api/containers/{id}/{action}` with a
20 s timeout on the server side. The card refreshes after the call. If a
particular action hangs forever, the most common cause is the underlying
Docker operation timing out — increase the timeout or check daemon
responsiveness with `docker ps` from the host.

## Known Limitations

- **No automated tests yet.** The current `go test ./...` passes because
  every package reports `[no test files]`. Code seams are in place
  (`internal/config`, `internal/collector/format.go`,
  `internal/collector/docker.go`, `internal/discovery/scanner.go`,
  `internal/server`).
- **Container restart counts** (`↻ N` in the row) are shown when present
  but the Docker SDK v25 `ContainerList` call does not include them. When
  the count is unavailable, the badge is hidden rather than guessing.
- **No persistent storage.** State is in-memory by design; a restart
  blanks `/readyz` until the first tick.
- **Public IP lookup performs outbound HTTPS calls** unless
  `ENABLE_PUBLIC_IP=0`.
- **The compose file contains host-specific paths and Spanish comments**
  for the current deployment environment; clone-tailor before deploying
  elsewhere.
- **Templates use `hx-*` attribute names** for clarity, but no HTMX
  library is shipped — the polling is implemented manually in
  `web/static/js/app.js`.

---

For deeper support guidance and troubleshooting recipes, see
[`knowledge.md`](knowledge.md).
