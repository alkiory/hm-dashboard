# AGENTS.md

Persistent context instructions for agents (Codex) working in this repository.

## Before touching code

`knowledge.md` is the source of truth for the **current** state of the code.

`README.md` contains obsolete or aspirational sections (see "Notes you must not assume are true" below). If there is a conflict between them, `knowledge.md` or the code itself wins — never assume a feature exists simply because it is documented in the README without verifying it in the code.

## Project Summary

Home Lab Dashboard is a self-hosted Go web service that renders a homelab dashboard: host hardware metrics, network status, Docker container status, and services discovered from Docker Compose files.

The design is intentionally minimalist: a single binary, no heavy frameworks, no persistent state, and no SPA.

## Stack

* Backend: Go standard library (`net/http`, `html/template`, `log/slog`), with static assets embedded into the binary.
* Metrics: `gopsutil/v3` (system and network) and Docker SDK (containers).
* Frontend: static HTML templates, handcrafted CSS, and vanilla JavaScript polling via `fetch()`. **HTMX is not actually installed**, even though templates use `hx-*` attributes — these are only naming conventions; polling is implemented manually in `web/static/js/app.js`.
* Runtime state: in-memory only, using atomically swapped snapshots (`atomic.Pointer[Snapshot]`).
* Deployment: Docker image + `docker-compose.yml`, usually behind Nginx Proxy Manager or another reverse proxy.

## Repository Map

```text
main.go                         Entrypoint, embedded web assets, signal handling.
internal/config/config.go       Environment variable parsing and defaults.
internal/poller/poller.go       Background polling loop and snapshot assembly.
internal/snapshot/snapshot.go   Atomic snapshot store (poller writes, server reads).
internal/collector/             Hardware, network, Docker collectors and formatters.
internal/discovery/             Compose file scanner for service discovery.
internal/server/                HTTP routes, lifecycle, template cache, card renderers.
web/templates/                  Full layout and card partial templates.
web/static/css/app.css          Handwritten dashboard CSS.
web/static/js/app.js            Theme toggle and card refresh logic.
Dockerfile                      Static multi-stage build + Alpine runtime image.
docker-compose.yml              Local/production compose stack using Docker socket proxy.
.env.example                    Supported environment variables.
```

## Architecture — Concurrency Rules (Important)

* `internal/snapshot` is the project's only concurrency boundary. The **poller is the only writer**; HTTP handlers are **read-only consumers**.
* Hard rule: never copy `snapshot.Snapshot` values after its atomic fields have been used. Always build a new `&snapshot.Snapshot{...}` and call `Store.Swap(next)`, as done in `internal/poller/poller.go`.
* HTTP handlers **must never** call Docker, `/proc`, `/sys`, or external IP services directly. All such logic belongs in the poller.
* Each poller tick:

  * Creates a context with a deadline equal to `POLLING_INTERVAL`.
  * Runs hardware, network, and Docker collectors in parallel.
  * Scans Compose files afterward.
  * Preserves the last valid container view if Docker becomes temporarily unavailable.

## Flow for Adding a New Card (Follow This Order)

1. Add data fields to the collector or discovery output.
2. Include those fields in `snapshot.Snapshot`.
3. Populate them in `poller.tick()`.
4. Add a template partial under `web/templates/partials/`.
5. Add a renderer in `internal/server/templates.go`.
6. Register the route in `internal/server/server.go`.
7. Add the card section in `web/templates/layout.html`.

## Coding Conventions

* Keep the HTTP request path read-only and fast. Collectors belong in the poller, never in handlers.
* Preserve the single-binary model: static files remain under `web/` and are embedded from `main.go`.
* Prefer the standard library and existing dependencies before adding new frontend or backend frameworks.
* Template helpers should be small and deterministic.
* Docker is always optional: the dashboard must continue rendering hardware, network, and services even when Docker is unavailable.
* Be careful with public URLs coming from Compose labels. Templates should only render `http:` and `https:` URLs as links.
* **Do not add persistent storage unless there is a concrete and explicit user requirement.** The application is stateless by design. (The README mentions SQLite as an option — do not implement it unless explicitly requested; see discrepancy notes below.)
* If new shared state is added, prefer immutable snapshot fields over mutable global state.
* Run `gofmt` before any change. Use `golangci-lint run` if available in the environment.
* Handle errors explicitly (avoid `panic` except in `main`).

## Configuration (Environment Variables)

| Variable            | Default              | Notes                                                                                                                                                                              |
| ------------------- | -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PORT`              | `3001`               | Internal HTTP port.                                                                                                                                                                |
| `POLLING_INTERVAL`  | `15s`                | Accepts seconds (`15`) or duration strings (`30s`, `1m`). **Production compose may override this default** — do not assume `15s` without checking the actual `docker-compose.yml`. |
| `DASHBOARD_TITLE`   | `Home Lab Dashboard` | Header title.                                                                                                                                                                      |
| `ENABLE_PUBLIC_IP`  | `true`               | Disabling it prevents outbound HTTP requests. Accepts `1/0`, `true/false`, `yes/no`, `on/off`.                                                                                     |
| `LOG_LEVEL`         | `info`               | `debug`, `info`, `warn`, `error`.                                                                                                                                                  |
| `COMPOSE_SCAN_PATH` | unset                | Comma-separated directories to scan for Compose files.                                                                                                                             |
| `DOCKER_HOST`       | unset                | Used by the Docker SDK. The compose file points it to the socket proxy.                                                                                                            |

## Useful Commands

```bash
# Build and tests (use an alternate GOCACHE if the default cache is not writable)
GOCACHE=/tmp/hld-go-cache go test ./...
GOCACHE=/tmp/hld-go-cache go build .

# Run locally
PORT=3001 \
POLLING_INTERVAL=15s \
COMPOSE_SCAN_PATH=/path/to/stacks \
ENABLE_PUBLIC_IP=0 \
go run .

# Docker
docker compose build
docker compose up -d
docker compose logs -f --tail 200 home-lab-dashboard
curl -fsS http://localhost:3051/healthz
curl -fsS http://localhost:3051/readyz
```

Relevant endpoints for quick debugging:

```bash
curl -i http://localhost:3051/api/cards/hardware
curl -i http://localhost:3051/api/cards/network
curl -i http://localhost:3051/api/cards/containers
curl -i http://localhost:3051/api/cards/services
```

## Testing Status

There are currently no automated tests in the repository (confirmed: every package reports `[no test files]`).

Areas already structured for testing if tests are requested:

* `internal/config`: environment variable parsing (invalid ints, durations, booleans, path splitting).
* `internal/collector/format.go`: byte, rate, and uptime formatters.
* `internal/collector/docker.go`: `healthFromStatus`, `imageShort`, `formatPorts`, CPU percentage calculations.
* `internal/collector/system.go`: `skipFS`, `parseInt`, thermal zone selection with stubbed file readers.
* `internal/discovery/scanner.go`: Compose parsing, label handling, port normalization, symlink skipping, scan depth.
* `internal/server`: route registration and HTTP status codes with `httptest`.

The project already exposes package-level globals as testing seams: Docker environment lookup, gopsutil wrappers, public IP validation, and thermal file readers. Reuse these seams instead of introducing new mocks.

## Known Rough Edges (Do Not "Fix" Unless Requested)

* Docker restart count is always zero: `ContainerList` does not expose it in Docker SDK v25. Supporting it would require an additional `inspect` call. **The README advertises restart counts as a visible feature — they are not currently functional.**
* The production Compose file contains comments and host-specific paths (`/home/alkiory/projects`, network `npm_network`) — do not generalize them unless explicitly asked to port to another host.
* Public IP lookup performs outbound HTTP requests unless `ENABLE_PUBLIC_IP=0`.

## Notes You Must NOT Assume Are True Just Because They Appear in README.md

The README contains outdated or aspirational sections relative to the current codebase. Verify these items in the code before acting on them:

* **Optional SQLite ("favorites"/"pinned services")**: the README suggests it, but the coding guidelines explicitly prohibit adding persistent storage without a concrete requirement. Do not implement it on your own initiative.
* **Restart counts in the containers card**: advertised in the README, confirmed as non-functional in `knowledge.md`.
* **`/srv:/srv:ro` mount in the architecture diagram**: the actual compose file uses a different host-specific path. Check `docker-compose.yml` before assuming paths.
* **`docker-compose.direct.yml`**: mentioned in the README repository structure. Confirm it actually exists before referencing it.
* **`POLLING_INTERVAL` default value**: the README and `.env.example` may not match the value configured in the production `docker-compose.yml`.

## Agent Task Scope

* If the task concerns a specific card or collector, modify only the files relevant to that flow (see "Flow for Adding a New Card"). Do not touch other cards unless explicitly requested.
* Do not modify `docker-compose.yml` (host-specific paths, networks, ports) unless explicitly requested — these are real production values.
* Do not add frontend frameworks or replace the vanilla JavaScript polling with real HTMX or similar libraries unless explicitly requested.
* After any Go change, run:

```bash
GOCACHE=/tmp/hld-go-cache go build .
```

and, when applicable:

```bash
GOCACHE=/tmp/hld-go-cache go test ./...
```

before considering the task complete.
