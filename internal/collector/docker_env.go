package collector

import "os"

// lookupEnv is the production seam for reading environment variables.
// This indirection exists so tests can stub it deterministically without
// monkey-patching package globals.
var lookupEnv = func(k string) (string, bool) { return os.LookupEnv(k) }

// env is a tiny convenience wrapper that returns just the value.
var env = func(k string) string {
	if v, ok := lookupEnv(k); ok {
		return v
	}
	return ""
}
