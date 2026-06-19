// Home Lab Dashboard — single-binary entry point.
//
// Boot sequence (no work happens before this):
//  1. Parse env-derived config.
//  2. Configure slog with the chosen level.
//  3. Embed FS exposes web/ templates + assets. Both FS subtrees are wired
//     into the server package so the rest of the codebase has no embed dep.
//  4. Build the snapshot store and the discovery scanner.
//  5. Start the poller goroutine (it primes the first tick immediately).
//  6. Bind HTTP and block on Listen() until ctx is cancelled by a signal.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alkiory/homelab-dashboard/internal/config"
	"github.com/alkiory/homelab-dashboard/internal/discovery"
	"github.com/alkiory/homelab-dashboard/internal/poller"
	"github.com/alkiory/homelab-dashboard/internal/server"
	"github.com/alkiory/homelab-dashboard/internal/snapshot"
)

//go:embed web
var webFS embed.FS

// version is set at link time via -ldflags "-X main.version=…". When unset
// it falls back to "dev" so the binary still builds locally without flags.
var version = "dev"

func main() {
	cfg := config.FromEnv()

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})
	slog.SetDefault(slog.New(handler))

	// Verify the embed at startup — a missing layout.html would otherwise
	// surface only on the first request, which is harder to debug.
	if _, err := fs.ReadFile(webFS, "web/templates/layout.html"); err != nil {
		slog.Error("embedded templates missing", slog.String("err", err.Error()))
		os.Exit(1)
	}

	// Two aliases into the same embed: handler package accepts both names
	// rather than splitting responsibilities here.
	server.AssetFS = webFS
	server.TemplatesFS = webFS

	rootCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store := snapshot.New()
	scanner := discovery.New(cfg.ComposeScanPaths)

	p := poller.New(cfg, store, scanner)
	go p.Run(rootCtx)

	srv := server.New(cfg, store)
	slog.Info("dashboard up",
		slog.String("version", version),
		slog.Int("port", cfg.Port),
		slog.Duration("pollingInterval", cfg.PollingInterval),
		slog.Int("composeRoots", len(cfg.ComposeScanPaths)),
	)
	if err := srv.Listen(rootCtx); err != nil && err != context.Canceled {
		slog.Error("server exited", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
