package collector

import (
	"os"
)

// openDir wraps os.Open for paths used by the thermal reader. Centralised
// here so future platforms (e.g. FreeBSD sysctl) can swap implementations
// via the same name without touching collector code.
var openDir = func(p string) (*os.File, error) { return os.Open(p) }

// readAll reads a small file (a thermal zone temp is a few bytes). The
// size cap is a defensive guard against a misconfigured sysfs returning
// megabytes on a hot path.
var readAll = func(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 0, 16)
	tmp := make([]byte, 16)
	for {
		n, err := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > 32 {
				return buf, nil
			}
		}
		if err != nil {
			return buf, err
		}
	}
}
