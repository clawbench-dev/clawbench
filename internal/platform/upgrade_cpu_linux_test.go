//go:build linux && amd64

package platform

import (
	"slices"
	"testing"
)

// TestHostAMD64Features_RealProcCPUInfo reads the live /proc/cpuinfo. It only
// asserts the parse shape — the feature set is whatever this machine has.
func TestHostAMD64Features_RealProcCPUInfo(t *testing.T) {
	flags, ok := hostAMD64Features()
	if !ok {
		t.Skip("/proc/cpuinfo has no flags line in this environment")
	}
	if len(flags) == 0 || !slices.Contains(flags, "fpu") {
		t.Errorf("parsed flags look wrong: %v", flags)
	}
	// The kernel spells LZCNT as "abm"; if it listed "lzcnt" instead the
	// mapping in cpuinfoFlag would look for the wrong word.
	if slices.Contains(flags, "lzcnt") && !slices.Contains(flags, "abm") {
		t.Log("this CPU reports lzcnt instead of abm; check cpuinfoFlag")
	}
}
