//go:build linux && amd64

package platform

import (
	"os"
	"strings"
)

// hostAMD64Features returns the CPU's flag names as /proc/cpuinfo spells them
// ("avx2", "bmi1", "lzcnt", ...). Empty when unreadable, which callers treat as
// "unknown" and therefore as "do not block".
//
// ok is false when no flags line was found, which callers must read as "host
// unknown" rather than "host has no features".
func hostAMD64Features() ([]string, bool) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "flags" {
			continue
		}
		return strings.Fields(value), true
	}
	return nil, false
}
