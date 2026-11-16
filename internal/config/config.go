// Package config loads runtime configuration from environment variables.
// All settings have safe defaults so the binary boots in any environment.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the runtime configuration for the dashboard.
type Config struct {
	Port             int           // Internal HTTP port (NPM terminates TLS upstream)
	PollingInterval  time.Duration // How often the background poller refreshes data
	ComposeScanPaths []string      // Directories to scan for compose files
	DashboardTitle   string        // Title shown in the header
	EnablePublicIP   bool          // Whether to query an external IP echo service
	LogLevel         slog.Level    // Slog log level
}

// FromEnv reads configuration from environment variables and returns a
// fully-populated Config. Missing values fall back to sensible defaults.
// Invalid numeric or duration values log a warning and use the default.
func FromEnv() Config {
	c := Config{
		Port:            3001,
		PollingInterval: 15 * time.Second,
		DashboardTitle:  "Home Lab Dashboard",
		EnablePublicIP:  true,
		LogLevel:        slog.LevelInfo,
	}

	c.Port = intEnv("PORT", c.Port)
	c.PollingInterval = durationEnv("POLLING_INTERVAL", c.PollingInterval)
	c.DashboardTitle = strEnv("DASHBOARD_TITLE", c.DashboardTitle)
	c.EnablePublicIP = boolEnv("ENABLE_PUBLIC_IP", c.EnablePublicIP)
	c.LogLevel = parseLogLevel(strEnv("LOG_LEVEL", "info"))

	if raw := os.Getenv("COMPOSE_SCAN_PATH"); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			// Strip trailing slashes; empty entries ignored.
			p = strings.TrimRight(p, "/")
			if p != "" {
				c.ComposeScanPaths = append(c.ComposeScanPaths, p)
			}
		}
	}

	return c
}

func intEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env, using default",
			slog.String("key", key), slog.String("value", v),
			slog.Int("default", def))
		return def
	}
	return n
}

func durationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	// Accept seconds (30) or duration string ("15s", "1m", "30s").
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration env, using default",
			slog.String("key", key), slog.String("value", v),
			slog.Duration("default", def))
		return def
	}
	return d
}

func boolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	// Accept truthy/falsy spelled forms in addition to "1"/"0".
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		slog.Warn("invalid boolean env, using default",
			slog.String("key", key), slog.String("value", v),
			slog.Bool("default", def))
		return def
	}
}

func strEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
