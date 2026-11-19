package platform

import (
	"slices"
	"testing"
)

// TestMissingAMD64Features_LZCNTIsReportedAsAbm pins the mapping that makes the
// v3 check work at all: /proc/cpuinfo calls LZCNT "abm", so a v3-capable host
// (v2 flags + avx/avx2/bmi/fma/abm, no literal "lzcnt") must come back clean.
// Treating "lzcnt" as the literal flag name would reject every real v3 CPU.
func TestMissingAMD64Features_LZCNTIsReportedAsAbm(t *testing.T) {
	v3Host := append(append([]string{}, v2OnlyFlags...),
		"avx", "avx2", "bmi1", "bmi2", "fma", "abm")
	if slices.Contains(v3Host, "lzcnt") {
		t.Fatal("fixture must not contain a literal lzcnt flag")
	}
	if got := missingAMD64Features(amd64LevelV3, v3Host); len(got) != 0 {
		t.Errorf("a v3 host calling LZCNT abm must be accepted, missing = %v", got)
	}

	// ...and a host without ABM really is missing LZCNT.
	got := missingAMD64Features(amd64LevelV3, v2OnlyFlags)
	if !slices.Contains(got, "lzcnt") {
		t.Errorf("a CPU without abm must report lzcnt missing, got %v", got)
	}
}

// TestMissingAMD64Features_AcceptsLZCNTSpelling covers /proc/cpuinfo variants
// that do print "lzcnt" directly (some kernels/patches), so neither spelling
// blocks a capable CPU.
func TestMissingAMD64Features_AcceptsLZCNTSpelling(t *testing.T) {
	host := append(append([]string{}, v2OnlyFlags...),
		"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt")
	if got := missingAMD64Features(amd64LevelV3, host); len(got) != 0 {
		t.Errorf("a host reporting literal lzcnt must also be accepted, missing = %v", got)
	}
}

// TestMissingAMD64Features_V3RequiresAbmLikeFeatures guards the whole v3 list
// against silent over- or under-requirement.
func TestMissingAMD64Features_V3RequiresAbmLikeFeatures(t *testing.T) {
	host := append(append([]string{}, v2OnlyFlags...), "avx", "avx2", "bmi1", "bmi2", "fma", "abm")
	for _, drop := range []string{"avx", "avx2", "bmi1", "bmi2", "fma", "abm"} {
		reduced := slices.DeleteFunc(slices.Clone(host), func(f string) bool { return f == drop })
		if got := missingAMD64Features(amd64LevelV3, reduced); len(got) == 0 {
			t.Errorf("dropping %q must make the v3 requirement unmet", drop)
		}
	}
}
