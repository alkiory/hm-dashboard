package collector

import "fmt"

// FmtBytes renders a byte count in IEC units (KiB, MiB, GiB, TiB).
// Returns "0 B" for the zero value and rounds to one decimal place for
// values under 100. Unit tests cover edge cases (gigabyte boundary,
// zero, very large).
func FmtBytes(n uint64) string {
	const (
		KiB = 1 << 10
		MiB = 1 << 20
		GiB = 1 << 30
		TiB = 1 << 40
	)
	switch {
	case n >= TiB:
		return fmt.Sprintf("%.1f TiB", float64(n)/TiB)
	case n >= GiB:
		return fmt.Sprintf("%.1f GiB", float64(n)/GiB)
	case n >= MiB:
		return fmt.Sprintf("%.1f MiB", float64(n)/MiB)
	case n >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(n)/KiB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// FmtRate formats a bytes-per-second value into a friendly string with
// adaptive units. We deliberately clamp to one decimal for sub-MB values
// to match dashboard norms ("12.3 KB/s" rather than "12 KB/s").
func FmtRate(bps float64) string {
	const (
		KB = 1024.0
		MB = 1024.0 * 1024.0
		GB = 1024.0 * 1024.0 * 1024.0
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.1f GB/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.1f MB/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.1f KB/s", bps/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// FmtUptime formats a system uptime (seconds) as "3d 4h 17m" or shorter.
// Composition is right-aligned: zero uptime returns "—".
func FmtUptime(secs uint64) string {
	if secs == 0 {
		return "—"
	}
	const (
		minSec  = 60
		hourSec = 3600
		daySec  = 86400
	)
	days := secs / daySec
	secs %= daySec
	hours := secs / hourSec
	secs %= hourSec
	mins := secs / minSec

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
