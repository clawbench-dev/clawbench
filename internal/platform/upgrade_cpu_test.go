package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The reporter's CPU (Intel Celeron J4125): x86-64-v2 flags only, no
// AVX/BMI/FMA/LZCNT.
var v2OnlyFlags = []string{"sse3", "sse4_1", "sse4_2", "popcnt", "ssse3", "movbe", "xsave", "rdrand", "aes", "sha_ni"}

// --- amd64LevelFromRecord ---

func TestAMD64LevelFromRecord(t *testing.T) {
	cases := map[string]int{
		"v1":       amd64LevelV1,
		"v2":       amd64LevelV2,
		"v2-lzcnt": amd64LevelLZCNT,
		"v3":       amd64LevelV3,
		"v4":       amd64LevelV4,
	}
	for rec, want := range cases {
		got, ok := amd64LevelFromRecord(rec)
		if !ok || got != want {
			t.Errorf("amd64LevelFromRecord(%q) = (%d, %v), want (%d, true)", rec, got, ok, want)
		}
	}
	for _, rec := range []string{"", "v5", "x86-64-v3", "v2-lzcnt-extra"} {
		if _, ok := amd64LevelFromRecord(rec); ok {
			t.Errorf("amd64LevelFromRecord(%q) should not be recognized", rec)
		}
	}
}

// --- missingAMD64Features ---

// TestMissingAMD64Features_V3OnV2OnlyCPU pins what the reporter would see: the
// v3 binary needs the whole v3 feature set; the CPU has none of it. LZCNT is
// named explicitly because that is the flag Go's v3 runtime spins on.
func TestMissingAMD64Features_V3OnV2OnlyCPU(t *testing.T) {
	got := missingAMD64Features(amd64LevelV3, v2OnlyFlags)
	want := []string{"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt"}
	if !slices.Equal(got, want) {
		t.Errorf("v3 on a v2-only CPU = %v, want %v", got, want)
	}
}

func TestMissingAMD64Features_V3OnV3CPU(t *testing.T) {
	host := append(append([]string{}, v2OnlyFlags...),
		"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt")
	if got := missingAMD64Features(amd64LevelV3, host); len(got) != 0 {
		t.Errorf("v3 on a v3 CPU should have no gaps, got %v", got)
	}
}

// TestMissingAMD64Features_V2BuildDoesNotRequireLZCNT is the precision that
// matters for false positives: a plain GOAMD64=v2 build runs fine on this CPU,
// so it must be reported as compatible.
func TestMissingAMD64Features_V2BuildDoesNotRequireLZCNT(t *testing.T) {
	if got := missingAMD64Features(amd64LevelV2, v2OnlyFlags); len(got) != 0 {
		t.Errorf("a v2 build must run on a v2 CPU, got missing %v", got)
	}
	// ...while v2-lzcnt does require it.
	got := missingAMD64Features(amd64LevelLZCNT, v2OnlyFlags)
	if !slices.Contains(got, "lzcnt") {
		t.Errorf("v2-lzcnt must require lzcnt, got %v", got)
	}
	if !slices.Contains(got, "avx2") {
		t.Errorf("v2-lzcnt builds still list avx2 in their feature set, got %v", got)
	}
}

func TestMissingAMD64Features_V4ImpliesV3(t *testing.T) {
	host := append(append([]string{}, v2OnlyFlags...),
		"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt")
	if got := missingAMD64Features(amd64LevelV4, host); !slices.Contains(got, "avx512f") {
		t.Errorf("v4 on a v3 CPU must report the AVX-512 gap, got %v", got)
	}
}

// --- CheckBinaryCPUCompatibility ---

// TestCheckBinaryCPUCompatibility_NonGoBinary verifies an unreadable binary is
// never treated as incompatible — an unknown build must not block an upgrade.
func TestCheckBinaryCPUCompatibility_NonGoBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clawbench")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if need, ok := CheckBinaryCPUCompatibility(path); !ok {
		t.Errorf("a non-Go file must not be reported as CPU-incompatible, got %s", need)
	}
}

// TestCheckBinaryCPUCompatibility_MissingFile covers a binary that vanished
// before the check ran.
func TestCheckBinaryCPUCompatibility_MissingFile(t *testing.T) {
	if need, ok := CheckBinaryCPUCompatibility(filepath.Join(t.TempDir(), "nope")); !ok {
		t.Errorf("a missing file must not be reported as CPU-incompatible, got %s", need)
	}
}

// TestCheckBinaryCPUCompatibility_ThisBinary reads this test executable's own
// build info. Its GOAMD64 level is whatever this machine can build for, so the
// verdict must be compatible — this is the end-to-end path the upgrade uses.
func TestCheckBinaryCPUCompatibility_ThisBinary(t *testing.T) {
	// The CPU baseline is only consulted for linux/amd64 upgrades. On other
	// hosts (the macOS arm64 runners in particular) the test binary has no
	// GOAMD64 setting at all, so there is nothing to read.
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("CPU baseline applies only to linux/amd64 upgrades")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	level, err := requiredAMD64Level(exe)
	if err != nil {
		t.Fatalf("requiredAMD64Level(%s): %v", exe, err)
	}
	if level < amd64LevelV1 {
		t.Errorf("unexpected level %d", level)
	}
	if need, ok := CheckBinaryCPUCompatibility(exe); !ok {
		t.Errorf("this test binary must be compatible with its own host, got %s", need)
	}
}

func TestAMD64LevelName(t *testing.T) {
	cases := map[int]string{
		amd64LevelV1:    "x86-64",
		amd64LevelV2:    "x86-64-v2",
		amd64LevelLZCNT: "x86-64-v2-lzcnt",
		amd64LevelV3:    "x86-64-v3",
		amd64LevelV4:    "x86-64-v4",
	}
	for level, want := range cases {
		if got := amd64LevelName(level); got != want {
			t.Errorf("amd64LevelName(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestCPUMinISAString(t *testing.T) {
	m := CPUMinISA{Level: "x86-64-v3", Missing: []string{"avx2", "lzcnt"}}
	want := "x86-64-v3（CPU 缺少 avx2、lzcnt）"
	if got := m.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := (CPUMinISA{Level: "x86-64-v3"}).String(); got != "x86-64-v3" {
		t.Errorf("String() without gaps = %q", got)
	}
}
