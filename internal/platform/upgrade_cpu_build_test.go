//go:build linux && amd64

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCheckBinaryCPUCompatibility_RealV3Binary builds a tiny GOAMD64=v3
// executable and runs the check on it against this host's real flags.
//
// This is the end-to-end path the upgrade uses, and the one a purely synthetic
// fixture cannot cover: the GOAMD64 value must come out of an actual build
// record, and LZCNT must be matched against the name the kernel really prints
// ("abm"). On a v3 host the verdict must be compatible — a rejection here means
// the check would block every legitimate upgrade. On a v2-only host it must be
// rejected, naming LZCNT among the gaps.
func TestCheckBinaryCPUCompatibility_RealV3Binary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a Go binary")
	}
	bin := buildTinyBinary(t, "v3bin", "GOAMD64=v3")

	level, err := requiredAMD64Level(bin)
	if err != nil {
		t.Fatalf("requiredAMD64Level: %v", err)
	}
	if level != amd64LevelV3 {
		t.Fatalf("level = %d, want v3 — the build record was not read", level)
	}

	need, compatible := CheckBinaryCPUCompatibility(bin)
	// The verdict must match what this host's real flags say about the same
	// level — never a blanket accept or reject, which is what a wrong LZCNT
	// spelling would produce.
	expectedCompatible := !hostLacksAnyV3Feature(t)
	if compatible != expectedCompatible {
		t.Fatalf("v3 binary verdict = %v (%s), but host flags say compatible = %v", compatible, need, expectedCompatible)
	}
}

// TestCheckBinaryCPUCompatibility_RealV2Binary pins the false-positive guard
// around the v2 cut-off: a plain GOAMD64=v2 build runs on every x86-64 CPU
// (that is the whole point of the v2 level), so it must never be blocked —
// including on hosts whose /proc/cpuinfo lacks some v2 flag someone might
// naively add to the required list.
func TestCheckBinaryCPUCompatibility_RealV2Binary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a Go binary")
	}
	bin := buildTinyBinary(t, "v2bin", "GOAMD64=v2")

	level, err := requiredAMD64Level(bin)
	if err != nil {
		t.Fatalf("requiredAMD64Level: %v", err)
	}
	if level != amd64LevelV2 {
		t.Fatalf("level = %d, want v2 — the build record was not read", level)
	}
	if need, ok := CheckBinaryCPUCompatibility(bin); !ok {
		t.Errorf("a GOAMD64=v2 binary must run on any x86-64 host, got %s", need)
	}
}

// buildTinyBinary compiles a minimal main package with the given build setting
// and returns the binary's path, skipping when no toolchain is available.
func buildTinyBinary(t *testing.T, name, setting string) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command(goBin, "build", "-o", bin, src)
	cmd.Env = buildEnv(setting)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build with %s using this toolchain: %v\n%s", setting, err, out)
	}
	return bin
}

// buildEnv starts from the current environment minus the Go settings the build
// overrides. Appending on top of os.Environ() would leave two GOAMD64/GOFLAGS
// entries and make the effective value depend on parser order.
func buildEnv(settings ...string) []string {
	overridden := make(map[string]bool, len(settings))
	for _, s := range settings {
		key, _, _ := strings.Cut(s, "=")
		overridden[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(settings)+2)
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if !overridden[key] {
			env = append(env, e)
		}
	}
	env = append(env, settings...)
	return append(env, "CGO_ENABLED=0")
}

// hostLacksAnyV3Feature is the independent oracle: it reads /proc/cpuinfo and
// lists the v3 features it does not find *by hand*, without going through
// cpuinfoFlag. Deliberately not the production mapping — if that mapping is
// wrong (the LZCNT spelling being the known trap), this still tells the truth.
func hostLacksAnyV3Feature(t *testing.T) bool {
	t.Helper()
	flags, ok := hostAMD64Features()
	if !ok {
		t.Skip("/proc/cpuinfo has no flags line in this environment")
	}
	has := func(names ...string) bool {
		for _, n := range names {
			if slices.Contains(flags, n) {
				return true
			}
		}
		return false
	}
	return !has("avx") || !has("avx2") || !has("bmi1") || !has("bmi2") || !has("fma") || !has("abm", "lzcnt")
}
