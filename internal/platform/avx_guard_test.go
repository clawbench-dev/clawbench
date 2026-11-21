package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release pipeline guards against #719 with the scan below (see "Verify
// cgo code has no AVX" in .github/workflows/release.yml). It was validated by
// hand against real binaries: a gcc that assumes the host is AVX capable
// emits `vmovq`/`vmovdqu` in runtime/cgo's x_cgo_getstackbound, and that
// binary exits with SIGILL on every x86-64 CPU without AVX even though its Go
// build info still says GOAMD64=v1.
//
// The workflow cannot import Go constants, so the values are duplicated here
// and TestAVXGuardScript_MirrorsReleaseWorkflow keeps the copies honest.

const (
	// canaryFunctionName is where #719 crashed: a C function that runs during
	// startup, before any Go CPU feature detection could route around it.
	canaryFunctionName = "x_cgo_getstackbound"
	// vexOrEvexPrefix matches the raw instruction bytes that only appear when
	// the compiler targets AVX: VEX uses C4/C5, EVEX uses 62.
	vexOrEvexPrefix = `(?m)^ +[0-9a-f]+:\s+(c4|c5|62) `
)

func TestAVXGuardScript_MirrorsReleaseWorkflow(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(raw)
	for _, want := range []string{
		"Verify cgo code has no AVX",
		"grep -E '^ +[0-9a-f]+:\\s+(c4|c5|62) '",
		"<" + canaryFunctionName + ">:",
		`CGO_CFLAGS: "-march=x86-64"`,
		`CGO_CFLAGS: "-march=armv8-a"`,
		"strip --strip-all clawbench-linux",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release.yml no longer contains %q; update the AVX guard and this test together (#719)", want)
		}
	}

	// The guard finds the cgo C code by symbol name, so the linux build must
	// keep the symbol table: `-s` makes every release die on "canary not
	// found". That regression is what these tests exist to prevent, so pin the
	// exact flags rather than just checking the guard still exists.
	var linuxBuild string
	for _, line := range strings.Split(workflow, "\n") {
		if strings.Contains(line, "-o clawbench-linux ./cmd/server") {
			linuxBuild = line
		}
	}
	if linuxBuild == "" {
		t.Fatalf("linux build command not found in release.yml")
	}
	if !strings.Contains(linuxBuild, `-ldflags="-w -X`) {
		t.Fatalf("linux build must use -ldflags=\"-w -X ...\" so the AVX canary keeps its symbol: %s", strings.TrimSpace(linuxBuild))
	}
}

// functionDisassembly returns the objdump body of name in binPath, stopping at
// the blank line objdump uses to separate functions (the rest of the block is
// int3 padding).
func functionDisassembly(t *testing.T, objdump, binPath, name string) string {
	t.Helper()
	out, err := exec.Command(objdump, "-d", binPath).Output()
	if err != nil {
		t.Fatalf("objdump: %v", err)
	}
	header := "<" + name + ">:"
	lines := strings.Split(string(out), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasSuffix(line, header) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s not found in %s", name, binPath)
	}
	var body []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}

// TestAVXGuardScript_DistinguishesCodegen pins the guard on real compiler
// output: runtime/cgo (pulled in by any cgo-using program) compiled once with
// the pinned baseline and once with -march=x86-64-v3. The guard must clear the
// first and reject the second. Skips without a C toolchain or objdump.
func TestAVXGuardScript_DistinguishesCodegen(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not in PATH")
	}
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("objdump not available")
	}
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not available")
	}

	dir := t.TempDir()
	writeTempFile(t, filepath.Join(dir, "go.mod"), "module avxcanary\n\ngo 1.21\n")
	// Importing C is what pulls runtime/cgo's C files, and with them the host
	// compiler, into the binary.
	writeTempFile(t, filepath.Join(dir, "main.go"),
		"package main\n\n/*\n#include <pthread.h>\nstatic void cgostub(void) { }\n*/\nimport \"C\"\n\nfunc main() { C.cgostub() }\n")

	// Build with the same -ldflags the release uses (-w, no -s). Building
	// without it would leave the symbol table in place even if the workflow
	// started stripping, and the test would pass on a binary the guard cannot
	// read.
	build := func(name, cflags string) string {
		t.Helper()
		out := filepath.Join(dir, name)
		cmd := exec.Command(goBin, "build", "-ldflags=-w", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=1",
			"CGO_CFLAGS="+cflags,
			"CGO_LDFLAGS="+cflags,
			"GOFLAGS=-mod=mod",
		)
		if combined, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build (%s): %v\n%s", cflags, err, combined)
		}
		return out
	}

	avxMarker := regexp.MustCompile(vexOrEvexPrefix)
	baseline := build("baseline", "-O2 -march=x86-64")
	widened := build("widened", "-O2 -march=x86-64-v3")

	// The control arm must actually reproduce the bug, otherwise a matching
	// assertion proves nothing about the guard.
	if !avxMarker.MatchString(functionDisassembly(t, objdump, widened, canaryFunctionName)) {
		t.Fatalf("expected AVX in %s with -march=x86-64-v3", canaryFunctionName)
	}
	if avxMarker.MatchString(functionDisassembly(t, objdump, baseline, canaryFunctionName)) {
		t.Fatalf("baseline build unexpectedly contains AVX")
	}

	// Run the workflow's script verbatim against both binaries, so a broken
	// quotation or control flow in the step is caught here too.
	if rc, out := runGuardScript(t, baseline); rc != 0 {
		t.Fatalf("guard rejected the pinned baseline build (rc=%d):\n%s", rc, out)
	}
	if rc, out := runGuardScript(t, widened); rc == 0 {
		t.Fatalf("guard accepted AVX in cgo code (rc=0):\n%s", out)
	}
}

// TestAVXGuardScript_FailsClosed covers the branch the happy-path test cannot:
// a binary where the canary function cannot be disassembled must be rejected,
// not silently passed. #719 shipped precisely because a check that could not
// see the problem reported success.
func TestAVXGuardScript_FailsClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not available")
	}
	dir := t.TempDir()
	writeTempFile(t, filepath.Join(dir, "go.mod"), "module avxcanary\n\ngo 1.21\n")
	writeTempFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "nobin"), ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, combined)
	}
	if rc, out := runGuardScript(t, filepath.Join(dir, "nobin")); rc == 0 {
		t.Fatalf("guard passed a binary without cgo C code (rc=0):\n%s", out)
	}
}

// runGuardScript extracts the "Verify cgo code has no AVX" run block from the
// release workflow, points it at binPath and executes it with bash. Running
// the real text, rather than a re-implementation, is what lets these tests
// catch a broken quotation or control flow in the workflow.
func runGuardScript(t *testing.T, binPath string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	script := extractStepRun(t, string(raw), "Verify cgo code has no AVX")
	script = strings.ReplaceAll(script, "clawbench-linux", binPath)
	cmd := exec.Command("bash", "-c", script)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("run guard script: %v\n%s", err, out)
	return 0, ""
}

// extractStepRun returns the `run: |` body of the step called stepName.
func extractStepRun(t *testing.T, workflow, stepName string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: "+stepName {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("step %q not found in release.yml", stepName)
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "run: |" {
			continue
		}
		// The block runs until the indentation drops below the "run:" key.
		var body []string
		for _, line := range lines[i+1:] {
			if strings.TrimSpace(line) == "" {
				body = append(body, "")
				continue
			}
			if indent(line) <= indent(lines[i]) {
				break
			}
			body = append(body, strings.TrimLeft(line, " "))
		}
		return strings.Join(body, "\n")
	}
	t.Fatalf("step %q has no run block", stepName)
	return ""
}

func indent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func writeTempFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
