# Home Lab Dashboard

A self-hosted, lightweight dashboard for a homelab host. It is a single Go
binary with embedded HTML/CSS/JS assets, background polling, and a lock-free
request path.

| What it shows | How it is collected |
| --- | --- |
| CPU, memory, swap, disks, temperature, uptime | `gopsutil/v3`, `/proc`, `/sys/class/thermal/*` |
| Network interfaces, bandwidth, connectivity, public IP | `gopsutil/net`, `/sys/class/net`, optional HTTPS IP echo services |
| Docker container state, health, CPU, memory | Docker Engine API through `DOCKER_HOST` |
| Services discovered from Compose files | YAML scan of `COMPOSE_SCAN_PATH` roots |

The frontend is intentionally plain: server-rendered templates, handcrafted
CSS, and a small vanilla JavaScript file that fetches card partials on a timer.
There is no SPA framework and no required external browser dependency.

## Architecture

```text
Browser
  |
  | HTTP(S), usually through Nginx Proxy Manager
  v
Home Lab Dashboard
  |
  | background poller, every POLLING_INTERVAL
  +--> hardware collector: gopsutil, /proc, /sys
  +--> network collector: gopsutil, interface state, optional public IP
  +--> Docker collector: Docker SDK, DOCKER_HOST
  +--> service discovery: compose-file scan
  |
  v
atomic snapshot.Store
  |
  v
HTTP handlers render / and /api/cards/* from the latest snapshot
```

HTTP requests do not call Docker, scan files, or read host metrics directly.
The poller builds a fresh snapshot and swaps it into `snapshot.Store`; handlers
only load that snapshot and render templates.

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
docker-compose.yml              Compose stack using a Docker socket proxy.
.env.example                    Supported runtime environment variables.
knowledge.md                    Maintainer support notes and troubleshooting detail.
```

## Configuration

All runtime configuration comes from environment variables.

| Variable | Default | Notes |
| --- | --- | --- |
| `PORT` | `3001` | Internal HTTP port. |
| `POLLING_INTERVAL` | `15s` | Accepts seconds like `15` or durations like `30s`, `1m`. |
| `DASHBOARD_TITLE` | `Home Lab Dashboard` | Header and page title. |
| `ENABLE_PUBLIC_IP` | `true` | Disable to avoid outbound HTTP calls. Accepts `1/0`, `true/false`, `yes/no`, `on/off`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `COMPOSE_SCAN_PATH` | unset | Comma-separated directories to scan for compose files. |
| `DOCKER_HOST` | unset | Docker SDK endpoint. Compose sets this to the socket proxy. |

## Quick Start With Docker Compose

The checked-in compose file is host-specific in a few places. Before deploying,
review:

- Published port: `3051:3001`
- External network: `npm_network`
- Compose scan mount: `/home/alkiory/projects:/projects:ro`
- Compose scan path: `COMPOSE_SCAN_PATH=/projects`

Start the stack:

```bash
docker compose build
docker compose up -d
docker compose ps
docker compose logs -f --tail 200 home-lab-dashboard
```

Verify from the Docker host:

```bash
curl -fsS http://localhost:3051/healthz
curl -fsS http://localhost:3051/readyz
```

Expected responses:

```text
ok
ready
```

`/readyz` returns `503 priming` until the first polling tick completes.

## Local Development

Use a writable Go cache when the default home cache is unavailable:

```bash
GOCACHE=/tmp/hld-go-cache go test ./...
GOCACHE=/tmp/hld-go-cache go build .
```

Run the app directly:

```bash
PORT=3001 \
POLLING_INTERVAL=15s \
COMPOSE_SCAN_PATH=/path/to/stacks \
ENABLE_PUBLIC_IP=0 \
go run .
```

If you want Docker data while running locally, expose Docker through one of:

```bash
DOCKER_HOST=unix:///var/run/docker.sock
DOCKER_HOST=tcp://localhost:2375
```

The dashboard still runs without Docker access. The Containers card will show
Docker as unavailable while the other cards continue to render.

## Docker Access Model

The default compose stack uses `tecnativa/docker-socket-proxy` as
`hld-docker-proxy`. The dashboard connects with:

```text
DOCKER_HOST=tcp://hld-docker-proxy:2375
```

This keeps the Docker socket out of the dashboard container. The proxy has the
host socket mounted read-only and exposes only the API areas enabled in
`docker-compose.yml`.

If you do not want Docker access at all, remove `DOCKER_HOST` and the socket
proxy dependency. Hardware, network, and service discovery still work.

## Service Discovery

`internal/discovery/scanner.go` walks every `COMPOSE_SCAN_PATH` root to depth
3 and recognizes:

- `compose.yml`
- `compose.yaml`
- `docker-compose.yml`
- `docker-compose.yaml`

It skips symlinks and extracts service name, image, ports, project name, and
selected metadata labels. Only labels under `dashboard.*` or `homelab.*` are
surfaced.

Example:

```yaml
services:
  demo:
    image: nginx:alpine
    ports:
      - "8088:80"
    labels:
      dashboard.description: "Demo web server"
      dashboard.url: "https://demo.example.com"
```

Top-level compose `name:` becomes the project name. If absent, the compose
file's parent directory is used.

## HTTP Routes

| Route | Purpose |
| --- | --- |
| `GET /` | Full dashboard page. |
| `GET /api/cards/hardware` | Hardware card partial. |
| `GET /api/cards/network` | Network card partial. |
| `GET /api/cards/containers` | Containers card partial. |
| `GET /api/cards/services` | Services card partial. |
| `GET /healthz` | Liveness check, returns `ok`. |
| `GET /readyz` | Readiness check, returns `ready` after the first snapshot. |
| `GET /static/*` | Embedded CSS and JavaScript. |

## Nginx Proxy Manager

The dashboard serves plain HTTP internally. Terminate TLS at Nginx Proxy
Manager or another reverse proxy.

Suggested NPM values:

| NPM field | Value |
| --- | --- |
| Scheme | `http` |
| Forward Hostname/IP | `home-lab-dashboard` when on the same Docker network, otherwise the Docker host IP |
| Forward Port | `3001` internally, or `3051` if forwarding to the published host port |
| Websockets Support | Off |
| Cache Assets | Off |
| SSL | Request a certificate, then enable Force SSL |

The included compose file attaches the dashboard to the external
`npm_network`. Create or rename that network to match your proxy environment.

## Troubleshooting

### Cards stay on "Loading..." or show "Unable to load data"

Check the card endpoints directly:

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

### Containers card says Docker is unavailable

Confirm the app has a Docker endpoint and the proxy is running:

```bash
docker compose ps
docker compose logs --tail 200 hld-docker-proxy
docker compose exec home-lab-dashboard sh -c 'echo "$DOCKER_HOST"'
```

The expected value in the checked-in compose file is:

```text
DOCKER_HOST=tcp://hld-docker-proxy:2375
```

### Services card is empty

Check that `COMPOSE_SCAN_PATH` is set, the host directory is mounted into the
container, compose files are within 3 directory levels of a scan root, and the
file names match the recognized compose names.

```bash
docker compose exec home-lab-dashboard sh -c 'echo "$COMPOSE_SCAN_PATH"; find /projects -maxdepth 4 -name compose.yml -o -name compose.yaml -o -name docker-compose.yml -o -name docker-compose.yaml'
```

### Public IP is missing

`ENABLE_PUBLIC_IP` may be disabled, outbound HTTPS may be blocked, or all
external IP providers may be unavailable. Disable public IP lookup with:

```text
ENABLE_PUBLIC_IP=0
```

### Hardware metrics look incomplete in Docker

The app reads host-like data through these mounts:

```yaml
volumes:
  - /proc:/proc:ro
  - /sys:/sys:ro
```

Disk visibility depends on the container mount namespace. Mount additional
host paths read-only if they should appear in the dashboard.

## Validation Status

Current workspace validation:

```bash
GOCACHE=/tmp/hld-go-cache go test ./...
```

Result: all packages pass, but the repository currently has no test files.

## Maintainer Notes

See `knowledge.md` for deeper support guidance, known rough edges, and testing
recommendations.

Known current rough edges:

- There are no automated tests.
- Some historical notes in older docs may not match the current compose file.
- Template attributes use `hx-*` names, but the app uses custom vanilla JS
  polling and does not ship HTMX.
- Docker restart counts are always zero because `ContainerList` does not
  include restart count in Docker SDK v25.
- Public IP lookup performs outbound HTTPS requests unless disabled.
- The compose file contains host-specific paths and comments for the current
  deployment environment.
