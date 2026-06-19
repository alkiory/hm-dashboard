# Home Lab Dashboard — Review & Validation Changelog

**Project:** `/home/alkiory-dev/projects/homeLabDashboard/`
**Status:** Scaffold complete; awaiting in-environment verification (Go runtime + Docker socket not available in the sandbox where this was authored, so no live `go build`/`docker compose up` was run).
**Author of record:** AI scaffold; user validation required for production sign-off.

---

## 1. Executive summary

A self-hosted, single-binary homelab dashboard with:

| Aspect                       | Implementation                                          |
| ---------------------------- | ------------------------------------------------------- |
| Backend                      | Go 1.26.4 (`go.mod`), `gopsutil/v3`, official `docker/docker` client, `yaml.v3` |
| Frontend                     | HTMX 2 (CDN), ~50 lines vanilla JS, ~4 KB handcrafted CSS |
| Persistence                  | Stateless; optional `dashboard-data` volume reserved for future SQLite |
| Resource footprint (target)  | < 1 % CPU at idle, < 50 MB RSS                          |
| Docker image (target)        | ~22 MB compressed (multi-stage Alpine)                  |
| Polling                      | Single background goroutine, lock-free HTTP path        |
| Templating                   | `html/template` parsed once, cached under a mutex       |
| Security                     | tecnativa socket-proxy sidecar (read-only Docker API), `/proc` + `/sys` mounted `:ro` |
| Reverse proxy                | Nginx Proxy Manager (TLS upstream, plain HTTP 8080 down) |

**Total source: 2,048 lines of Go across 16 files**, plus 5 HTML templates, 1 CSS, 1 vanilla JS.

---

## 2. File inventory

```
./.dockerignore
./.env.example
./Dockerfile
./README.md
./docker-compose.yml
./go.mod
./main.go                              77 lines
./internal/collector/
├── docker.go           Container list + one-shot stats
├── docker_env.go       env-var indirection (test seam)
├── format.go           FmtBytes / FmtRate / FmtUptime
├── netstate.go         bandwidth rate cache (lock-protected)
├── network.go          interfaces, IPv4/IPv6, public IP
├── syscall_unix.go     /sys reads
├── system.go           CPU, mem, disk, thermal, uptime
└── types.go            shared snapshot types
./internal/config/config.go        env-var parsing
./internal/discovery/
├── scanner.go         walks COMPOSE_SCAN_PATH, parses YAML
└── types.go
./internal/poller/poller.go         goroutine ticker; 3 concurrent collectors
./internal/server/
├── server.go          HTTP mux + lifecycle
└── templates.go       173 lines — template cache + card renderers
./internal/snapshot/snapshot.go     atomic.Pointer[Snapshot]
./web/
├── static/css/app.css
├── static/js/app.js
└── templates/
├── layout.html
└── partials/
├── containers.html
├── hardware.html
├── network.html
└── services.html
```

30 files. No binary assets. Single dependency manifest (`go.mod`).

---

## 3. What was implemented (this session)

The complete 10-step deliverable list from the original request:

1. **Tech stack rationale** — Go + HTMX argued in `README.md` §1 (frameworks vs homelab needs).
2. **Architecture diagram** — ASCII system-context in `README.md` §2 (NPM → Dashboard → socket-proxy → Docker).
3. **Folder structure** — documented in `README.md` §3 and confirmed in §2 above.
4. **Optimised multi-stage Dockerfile** — Build on `golang:1.26.4`, runtime on `alpine:3.20` as non-root uid 1000, `HEALTHCHECK` via curl against `/healthz`, `ldflags "-s -w -X main.version=…"`, optional UPX compression.
5. **Production-ready docker-compose.yml** — tecnativa socket-proxy sidecar with `POST=0 DELETE=0 EXEC=0 EVENTS=0 IMAGES=0 VOLUMES=0 NETWORKS=0 CONTAINERS=1`; `max-size:10m max-file:3` log rotation; `/proc` + `/sys` mounted `:ro`; named bridge network `hld-internal`; `dashboard-data` volume reserved.
6. **Go backend (Docker SDK)** — `internal/collector/docker.go` uses `github.com/docker/docker/client`, ContainerList + ContainerStats (one-shot, `stream=false`); falls back without restart if daemon unreachable.
7. **HTML templates + HTMX** — Five partial templates total (1 layout + 4 cards) using `{{define "layout"}}` for the page wrapper. HTMX triggers `"load delay:100ms, every Xs"` per card body, `innerHTML` swap, `.pulse` indicator element.
8. **Auto-discovery logic** — `internal/discovery/scanner.go` walks configured directories to depth 3, recognises `compose.yml`, `compose.yaml`, `docker-compose.yml`, `docker-compose.yaml`; parses YAML safely (multi-doc support); filters labels to the `dashboard.*` / `homelab.*` namespace; explicitly skips symlinks; honours `dashboard.description|home.url|home.title` for service metadata.
9. **Ngnix Proxy Manager deployment instructions** — `README.md` §6 documents the `Proxy Hosts → Add Proxy Host` flow with HTTPS terminaison and HSTS.
10. **Complete project scaffold** — 30 files as enumerated in §2.

---

## 4. Constraints compliance

| Constraint                                                | Implementation                                                    | Status |
| --------------------------------------------------------- | ----------------------------------------------------------------- | ------ |
| No heavy frameworks (React, Next, Angular, Vue, NestJS)   | Stdlib `net/http` + `html/template` + HTMX; single 14 KB asset    | ✅      |
| Minimise external dependencies                           | 4 direct deps: gopsutil, docker, docker/go-connections, yaml.v3  | ✅      |
| No development-only configurations                         | All routes, paths and middleware are prod-shaped; no dev flags    | ✅      |
| Architecture: Go + HTMX                                   | Yes                                                              | ✅      |
| < 50 MB RAM                                               | atomic.Pointer state, parsed-once templates, one poller — design supports it | ⚠ Pending runtime measurement |
| 10–30 s polling default                                   | `POLLING_INTERVAL=15s`                                            | ✅      |
| Docker socket via `/var/run/docker.sock`                  | Via tecnativa proxy at `tcp://docker-proxy:2375`                  | ✅      |
| Documented secure socket mapping                          | README §7 — three patterns (proxy / direct+ro / no-Docker)       | ✅      |
| Modern, clean, responsive UI                              | CSS custom properties + grid auto-fit; mobile breakpoint at 720 px | ✅      |
| Dark mode toggle                                          | data-theme attribute + localStorage + prefers-color-scheme fallback | ✅      |
| SQLite for any local config (stateless preferred)         | Stateless; volume reserved                                         | ✅      |
| Multi-stage Production Dockerfile                          | Yes (`golang:1.26.4` → `alpine:3.20`)                         | ✅      |
| Configuration via env vars                                | `PORT`, `POLLING_INTERVAL`, `COMPOSE_SCAN_PATH`, `LOG_LEVEL`, `ENABLE_PUBLIC_IP`, `DASHBOARD_TITLE`, `DOCKER_HOST` | ✅ |
| Native Docker healthcheck in compose                      | `HEALTHCHECK` curl `/healthz` (30 s interval, 3 retries, 15 s start) | ✅      |
| Log rotation at Docker level                              | `max-size: 10m`, `max-file: 3` json-file driver                  | ✅      |
| Run behind NPM, internal HTTP only                        | Internal port 8080; no TLS in app; relies on NPM                  | ✅      |

---

## 5. Validation & smoke test plan

This plan walks every requirement end-to-end. Items marked **[auto]** can be executed in any environment with `bash` + `docker`; **[run]** requires the dashboard running on a target host.

### 5.1 Build-time validation

#### **[auto]** Step 1 — Verify the build compiles cleanly

```bash
cd /home/alkiory-dev/projects/homeLabDashboard
go env -w GOFLAGS=-mod=mod
go mod tidy
go vet ./...
go build -o /tmp/dashboard .
```

**Expected:**
- `go mod tidy` resolves a clean `go.sum` (current `go.mod` declares `+incompatible` for `github.com/docker/docker`; this is normal for the v25 client).
- `go vet ./...` emits **no output**.
- `go build` produces `/tmp/dashboard` (≈ 12 MB stripped binary on linux/amd64).

#### **[auto]** Step 2 — Verify no `atomic` struct-copy UB exists

```bash
grep -RIn '\*= \*' internal/ | grep -i snapshot
```

**Expected:** no matches in `internal/poller/`. The poller in `internal/poller/poller.go` constructs a fresh `&snapshot.Snapshot{...}` per tick; no `*next = *snap` copy of the carrier.

#### **[auto]** Step 3 — Verify the embed compilation

```bash
go build -tags netgo,osusergo -ldflags '-s -w' -o /tmp/dashboard .
```

**Expected:** compiles (the `netgo,osusergo` tags confirm pure-Go networking).

### 5.2 Runtime validation

#### **[run]** Step 4 — Bring the stack up

```bash
docker compose up -d --build
docker compose ps
```

**Expected:**
- `dashboard` and `hld-docker-proxy` containers in `running` state.
- `docker compose logs -f dashboard` shows `slog.Info("dashboard up", ...)` with `port=8080`.
- No SIGSEGV / panics.

#### **[run]** Step 5 — Verify liveness & readiness

```bash
curl -fsS http://localhost:8080/healthz   # → ok
curl -fsS http://localhost:8080/readyz    # (after first poll) → ready
```

**Expected:** both endpoints return 200 within 30 s of startup. `/readyz` returns `503 priming` for the first poll cycle.

#### **[run]** Step 6 — Verify the four cards render and refresh

Open http://localhost:8080/ in a browser. After 15 s:

| Card           | Acceptable first paint          | Acceptable polled value (15 s)                |
| -------------- | ------------------------------- | --------------------------------------------- |
| Hardware       | CPU %, mem %, disk %, uptime    | Same numbers, no NaN, fonts unchanged         |
| Network        | "IPv4 up/down" dot              | Per-interface RX/TX rate rows present         |
| Containers     | Running/stopped counts          | List with name, image, state, restart count   |
| Services       | "No services discovered"        | Cards for each compose service with URL       |

**Expected:** each card body shows the `<span class="pulse">` indicator briefly during the fetch, then swaps to fresh content. The header "priming…" stamp turns into a relative timestamp.

#### **[run]** Step 7 — Verify the dark/light toggle

Click the `◐` button in the top right. **Expected:** background flips between dark and light; selection persists across reload (localStorage key `hld.theme`); system-default falls back to dark.

#### **[run]** Step 8 — Verify service discovery

Create `/srv/test/docker-compose.yml`:

```yaml
services:
  demo:
    image: nginx:alpine
    ports: ["8088:80"]
    labels:
      dashboard.description: "Demo web server"
      dashboard.url: "http://demo.local"
```

Restart: `docker compose restart dashboard`. **Expected:** a new card appears in the Services card within one polling interval (15 s). Click → opens `http://demo.local`.

### 5.3 Security validation

#### **[run]** Step 9 — Verify the socket-proxy deny-list is effective

```bash
docker exec -it home-lab-dashboard sh
# try a forbidden op
```

Or from the dashboard container:

```bash
apk add curl
curl -s -o /dev/null -w "%{http_code}\n" \
     -X POST http://docker-proxy:2375/containers/prune
curl -s -o /dev/null -w "%{http_code}\n" \
     -X DELETE http://docker-proxy:2375/containers/<id>
curl -s -o /dev/null -w "%{http_code}\n" \
     http://docker-proxy:2375/events
curl -s -o /dev/null -w "%{http_code}\n" \
     -X POST http://docker-proxy:2375/exec
```

**Expected:** all return errors from the proxy (forbidden). Listing containers should succeed:

```bash
curl -s http://docker-proxy:2375/containers/json | head -c 200
```

#### **[run]** Step 10 — Verify the app container cannot mutate Docker state

From inside the dashboard container:

```bash
apk add docker-cli 2>/dev/null  # (with socket mounted, NOT the default config)
```

In the **default** (pattern A) compose, the dashboard container does not have the socket mounted — confirms via:

```bash
ls /var/run/docker.sock         # → no such file
mount | grep docker              # → empty
```

#### **[auto]** Step 11 — Confirm read-only mounts in compose

```bash
grep -E '(:ro|/proc|/sys)' docker-compose.yml
```

**Expected:** every `/proc` and `/sys` mount declares `:ro`; the `dashboard-data:` volume is the only RW mount for the app container.

### 5.4 NPM validation

#### **[run]** Step 12 — Configure NPM proxy host

In NPM UI: **Hosts → Proxy Hosts → Add Proxy Host**:

- **Domain**: `home.example.com`
- **Scheme**: `http`
- **Forward Hostname/IP**: `<docker-host-ip>`
- **Forward Port**: `8080`
- **Websockets**: off
- **SSL**: "Request a new SSL Certificate", enable **Force SSL**, **HTTP/2**, **HSTS**

**Expected:**

```bash
curl -fsSI https://home.example.com/healthz | head -1   # → HTTP/2 200
curl -fsSI http://home.example.com/                     # → 301 to https
```

The dashboard only sees traffic at `8080`; it does not handle TLS.

### 5.5 Footprint validation

#### **[run]** Step 13 — Measure the resource budget

```bash
docker stats --no-stream home-lab-dashboard
#                         CPU %   MEM USAGE / LIMIT
#   home-lab-dashboard    < 0.5 % ~12 MB / 256 MB
```

After 5 minutes of idle: the MEM reading should sit near **12 MB**; under 20 simultaneously-updated tabs (rare), still below **30 MB**.

```bash
docker image inspect ghcr.io/alkiory/homelab-dashboard:latest \
  --format '{{.Size}}'
# → ~22 MB compressed
```

#### **[run]** Step 14 — Confirm log rotation

```bash
ls -lh $(docker inspect --format '{{.LogPath}}' home-lab-dashboard)
docker inspect --format '{{.HostConfig.LogConfig}}' home-lab-dashboard
# → {json-file {max-file 3 max-size 10m labels [service com.docker.compose.project]}}
```

### 5.6 Failure-mode validation

#### **[run]** Step 15 — Outage resilience

```bash
docker stop hld-docker-proxy    # simulate Docker-API outage
docker exec home-lab-dashboard wget -q -O- http://127.0.0.1:8080/healthz   # → ok
```

**Expected:** `/healthz` returns `ok` (HTTP server is alive); the Containers card shows "Docker is unavailable" but the rest of the dashboard still updates every 15 s.

#### **[run]** Step 16 — Container restart storm

Trigger 5 restarts on a few services. Confirm:

- Containers card shows non-zero `↻ N` for affected services
- No log flood (slog level is `info` by default; switch to `debug` for diagnosis)

---

## 6. Acceptance criteria

The project is **ACCEPTABLE** when all of the following are true:

| #   | Criterion                                                                                                | Source              |
| --- | -------------------------------------------------------------------------------------------------------- | ------------------- |
| A1  | `go vet ./... && go build ./...` exits 0                                                                 | §5.1 Steps 1 + 3    |
| A2  | `grep -RIn '\*= \*' internal/poller/` finds no Snapshot struct copy                                       | §5.1 Step 2         |
| A3  | `curl /healthz` returns `200 ok` within 30 s of startup                                                  | §5.2 Step 5         |
| A4  | All four cards render visible content (no "priming…" stuck)                                              | §5.2 Step 6         |
| A5  | Card bodies auto-refresh every 15 s without full-page reload                                              | §5.2 Step 6         |
| A6  | A new compose file in `COMPOSE_SCAN_PATH` results in a Services card entry within one polling interval  | §5.2 Step 8         |
| A7  | `POST /containers/prune` and `DELETE /containers/...` from inside the dashboard container are blocked    | §5.3 Step 9         |
| A8  | `/proc` and `/sys` mounts are `:ro` in the running container                                             | §5.3 Step 11        |
| A9  | Hitting `home.example.com` over HTTPS returns 200 with `HTTP/2` + HSTS headers                            | §5.4 Step 12        |
| A10 | Idle RAM < 50 MB; idle CPU < 1 %; image size < 30 MB                                                      | §5.5 Step 13        |
| A11 | A Docker-API outage does NOT take down `/healthz` or the other cards                                     | §5.6 Step 15        |

If any one of A1–A11 fails, the project is **NOT** ready for production sign-off. Document the failing criterion and which step revealed it.

---

## 7. Known limitations & follow-ups

| Limitation                                          | Severity   | Follow-up                                                                                  |
| --------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------ |
| No automated test suite                            | Medium     | Add `internal/collector/format_test.go`, `internal/discovery/scanner_test.go` (table-driven with goldens) |
| Author session had no Go toolchain; build unverified | High     | Run §5.1 Steps 1–3 in the target environment before merge                                  |
| HTMX is fetched from CDN by default                | Low        | README §9 already covers air-gap vendoring; ship with `web/static/js/htmx.min.js` once |
| Compose label-namespace filter is opinionated      | Low        | If the user wants `traefik.*` labels surfaced, extend `applyLabel` in `internal/discovery/scanner.go` |
| Public IP lookup fetches external APIs (cached)    | Low        | Already gated by `ENABLE_PUBLIC_IP=0` for fully air-gapped users                          |
| `composeFile.Build` is parsed but ignored           | Cosmetic   | Already correct behaviour; kept in struct for future "buildkit-canary" feature           |
| No retry/backoff for transient Docker errors       | Low        | Single token `"priming"` overlay works; add jittered backoff if docker-flapping is common |
| README references Alpine.js (user spec mentioned)  | Cosmetic   | Decision: HTMX + 50 lines vanilla JS is smaller. README could note this explicitly        |
| Only IPv4 connectivity check                       | Cosmetic   | `net.ConnectionsWithContext` could be called with both kinds if IPv6 monitoring is wanted |
| No /metrics scraping for Prometheus                | Future     | Add `metrics.PromHTTPHandler()` once owners opt-in                                          |

---

## 8. Sign-off checklist

When the operator has walked through §5 in their environment and confirmed §6 acceptance criteria, fill in:

```
Validated by:  ____________________
Date:         ____________________
Host:         Pi 4 (4 GB)  /  i3-N305  /  other: _________
Docker:       _________   (25.0+ recommended)
Go toolchain: not required for runtime, required only for rebuild

Step 1  go vet / build               [  ] PASS    [  ] FAIL — note: _______
Step 5  /healthz / /readyz            [  ] PASS    [  ] FAIL — note: _______
Step 6  cards render & refresh        [  ] PASS    [  ] FAIL — note: _______
Step 7  theme toggle persists         [  ] PASS    [  ] FAIL — note: _______
Step 8  service discovery             [  ] PASS    [  ] FAIL — note: _______
Step 9  socket-proxy deny-list        [  ] PASS    [  ] FAIL — note: _______
Step 10 /var/run/docker.sock mounting [  ] PASS    [  ] FAIL — note: _______
Step 11 /proc /sys :ro                [  ] PASS    [  ] FAIL — note: _______
Step 12 NPM HTTPS proxy               [  ] PASS    [  ] FAIL — note: _______
Step 13 footprint                     [  ] RSS ___MB  CPU ___%  Image ___MB
Step 14 log rotation correct          [  ] PASS    [  ] FAIL — note: _______
Step 15 outage resilience             [  ] PASS    [  ] FAIL — note: _______

Decision:
[  ] ACCEPTED for production
[  ] ACCEPTED with caveats (list below)
[  ] REJECTED — blocking issues:
```

---

## 9. Changelog (this scaffold)

```
2025-06-19  Initial scaffold
            + 16 Go files (2,048 LoC)
            + 5 HTML templates (layout + 4 card partials)
            + 1 CSS (≈ 4 KB)
            + 1 vanilla JS theme toggler (≈ 1 KB)
            + 1 multi-stage Dockerfile (alpine, nonroot, 22 MB target)
            + 1 docker-compose.yml (socket-proxy, log rotation, healthcheck)
            + README.md (12 sections incl. ASCII diagram + NPM walkthrough)
            + .env.example + .dockerignore

2025-06-19  Post-review fixes
            * Exported collector.CollectHardware / CollectContainers
            * collector.CollectNetwork → (Network, error) with return out, nil
            * main.go: added `var version = "dev"` for linker -X flag
            * Removed stray itoa replica; use strconv.Itoa
            * Removed dead fmtBytes/fmtFloat/fmtInt/LogHardware
            * Symlink skipping in scanner
            * Label sub-namespace filter: only dashboard.*/homelab.*
            * Layout: single hx-trigger per card; HTMX script explicit path; <span class="pulse">
            * HasPrefix helper added to funcMap; services.html uses it for URL guard
            * Removed empty integrity="" and crossorigin="anonymous"
            * Scan depth fixed, loop detection via `seen` map and symlink filter
```
