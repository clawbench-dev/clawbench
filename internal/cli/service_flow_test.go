//go:build linux

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
)

// These tests exercise the install/uninstall/status flows. Everything that
// would touch the real machine — /etc, systemd, the caller's HOME, the actual
// executable — is routed through the package-level seams in service.go, so the
// tests can run as an ordinary user in CI without a live systemd.

// stubServiceSeams replaces every seam with a safe default and restores them
// afterwards. Individual tests override the ones they care about.
func stubServiceSeams(t *testing.T) {
	t.Helper()
	origEuid, origExec, origHome := geteuid, executablePath, userHomeDir
	origStdin, origContainer := serviceStdin, isContainer
	origRun, origFind := runCommand, findStableEntry
	origDataDir, origManaged := model.DataDir, model.ManagedByEmbedder

	t.Cleanup(func() {
		geteuid, executablePath, userHomeDir = origEuid, origExec, origHome
		serviceStdin, isContainer = origStdin, origContainer
		runCommand, findStableEntry = origRun, origFind
		model.DataDir, model.ManagedByEmbedder = origDataDir, origManaged
	})

	geteuid = func() int { return 1000 }
	executablePath = func() (string, error) { return "/usr/local/bin/clawbench", nil }
	userHomeDir = func() (string, error) { return t.TempDir(), nil }
	serviceStdin = strings.NewReader("")
	isContainer = func() bool { return false }
	runCommand = func(string, ...string) (string, error) { return "", nil }
	findStableEntry = func() string { return "/usr/local/bin/clawbench" }
	model.DataDir = t.TempDir()
	model.ManagedByEmbedder = false
}

// captureStd runs fn with stdout and stderr redirected to a pipe and returns
// everything written. fmt reads os.Stdout/os.Stderr on every call, so swapping
// the package vars is enough.
func captureStd(t *testing.T, fn func()) string {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout, os.Stderr = origOut, origErr
	return <-done
}

// ---------- usage / dispatch ----------

func TestPrintServiceUsage_ListsEveryAction(t *testing.T) {
	out := captureStd(t, printServiceUsage)
	for _, want := range []string{"install", "uninstall", "status", "--binary"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage output missing %q:\n%s", want, out)
		}
	}
}

func TestRunServiceCommand_HelpVariantsExitZero(t *testing.T) {
	stubServiceSeams(t)
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var code int
		captureStd(t, func() { code = RunServiceCommand(args) })
		if code != 0 {
			t.Errorf("RunServiceCommand(%v) = %d, want 0", args, code)
		}
	}
}

// ---------- hasSystemd ----------

func TestHasSystemd(t *testing.T) {
	stubServiceSeams(t)

	var calls [][]string
	runCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	if !hasSystemd(true) {
		t.Error("hasSystemd(system) should be true when systemctl answers")
	}
	if !hasSystemd(false) {
		t.Error("hasSystemd(user) should be true when systemctl answers")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 probes, got %d", len(calls))
	}
	if got := strings.Join(calls[0], " "); got != "systemctl show-environment" {
		t.Errorf("system probe = %q", got)
	}
	if got := strings.Join(calls[1], " "); got != "systemctl --user show-environment" {
		t.Errorf("user probe = %q", got)
	}

	runCommand = func(string, ...string) (string, error) { return "", errors.New("no systemd") }
	if hasSystemd(true) || hasSystemd(false) {
		t.Error("hasSystemd must be false when systemctl fails")
	}
}

// ---------- resolveServiceBinary ----------

func TestResolveServiceBinary(t *testing.T) {
	stubServiceSeams(t)
	exe := "/usr/local/bin/clawbench"

	t.Run("no override pins the PATH entry", func(t *testing.T) {
		gotExe, gotStable, code := resolveServiceBinary(nil)
		if code != 0 || gotExe != exe || gotStable != "/usr/local/bin/clawbench" {
			t.Fatalf("got (%q, %q, %d)", gotExe, gotStable, code)
		}
	})

	t.Run("executable lookup failure", func(t *testing.T) {
		executablePath = func() (string, error) { return "", errors.New("nope") }
		t.Cleanup(func() { executablePath = func() (string, error) { return exe, nil } })
		var code int
		captureStd(t, func() { _, _, code = resolveServiceBinary(nil) })
		if code != 1 {
			t.Errorf("code = %d, want 1", code)
		}
	})

	binDir := t.TempDir()
	exec := filepath.Join(binDir, "clawbench")
	if err := os.WriteFile(exec, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(binDir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("--binary with an executable file", func(t *testing.T) {
		gotExe, gotStable, code := resolveServiceBinary([]string{"--binary", exec})
		if code != 0 || gotExe != exe || gotStable != exec {
			t.Fatalf("got (%q, %q, %d)", gotExe, gotStable, code)
		}
	})

	t.Run("--binary= form and -b form", func(t *testing.T) {
		for _, args := range [][]string{{"--binary=" + exec}, {"-b", exec}} {
			_, gotStable, code := resolveServiceBinary(args)
			if code != 0 || gotStable != exec {
				t.Errorf("args %v: got (%q, %d)", args, gotStable, code)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
		}{
			{"missing file", []string{"--binary", filepath.Join(binDir, "absent")}},
			{"not executable", []string{"--binary", plain}},
			{"npm versioned path", []string{"--binary", "/srv/node_modules/clawbench/bin/clawbench"}},
			{"unresolvable path", []string{"--binary", filepath.Join(binDir, "a", "b")}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var code int
				captureStd(t, func() { _, _, code = resolveServiceBinary(tc.args) })
				if code != 1 {
					t.Errorf("code = %d, want 1", code)
				}
			})
		}
	})
}

// ---------- pickServiceScope ----------

func TestPickServiceScope(t *testing.T) {
	stubServiceSeams(t)

	t.Run("no systemd at all", func(t *testing.T) {
		runCommand = func(string, ...string) (string, error) { return "", errors.New("down") }
		var code int
		out := captureStd(t, func() { _, code = pickServiceScope(false) })
		if code != 1 || !strings.Contains(out, "systemd") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})

	t.Run("only the user scope answers", func(t *testing.T) {
		runCommand = func(_ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "--user" {
				return "", nil
			}
			return "", errors.New("no system manager")
		}
		system, code := pickServiceScope(false)
		if code != 0 || system {
			t.Errorf("got (%v, %d), want (false, 0)", system, code)
		}
	})

	t.Run("only the system scope answers", func(t *testing.T) {
		runCommand = func(_ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "--user" {
				return "", errors.New("no user manager")
			}
			return "", nil
		}
		system, code := pickServiceScope(false)
		if code != 0 || !system {
			t.Errorf("got (%v, %d), want (true, 0)", system, code)
		}
	})

	t.Run("root never gets asked", func(t *testing.T) {
		// Both managers answer, but a root shell has no user manager of its
		// own to install into, so the prompt is skipped and system wins.
		runCommand = func(string, ...string) (string, error) { return "", nil }
		var system bool
		var code int
		captureStd(t, func() { system, code = pickServiceScope(true) })
		if code != 0 || !system {
			t.Errorf("got (%v, %d), want (true, 0)", system, code)
		}
	})

	t.Run("both answer and the user chooses", func(t *testing.T) {
		runCommand = func(string, ...string) (string, error) { return "", nil }
		for stdin, want := range map[string]bool{"y\n": true, "n\n": false} {
			serviceStdin = strings.NewReader(stdin)
			var system bool
			captureStd(t, func() { system, _ = pickServiceScope(false) })
			if system != want {
				t.Errorf("stdin %q: system = %v, want %v", stdin, system, want)
			}
		}
	})
}

// ---------- resolveServiceIdentity ----------

func TestResolveServiceIdentity(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }

	t.Run("unreadable HOME", func(t *testing.T) {
		userHomeDir = func() (string, error) { return "", errors.New("no home") }
		t.Cleanup(func() { userHomeDir = func() (string, error) { return home, nil } })
		var code int
		captureStd(t, func() { _, _, code = resolveServiceIdentity(true) })
		if code != 1 {
			t.Errorf("code = %d, want 1", code)
		}
	})

	t.Run("user scope has no User=", func(t *testing.T) {
		gotHome, gotUser, code := resolveServiceIdentity(false)
		if code != 0 || gotHome != home || gotUser != "" {
			t.Errorf("got (%q, %q, %d)", gotHome, gotUser, code)
		}
	})

	t.Run("system scope resolves SUDO_USER's home", func(t *testing.T) {
		t.Setenv("SUDO_USER", "alice")
		runCommand = func(_ string, args ...string) (string, error) {
			if len(args) == 2 && args[0] == "passwd" && args[1] == "alice" {
				return "alice:x:1000:1000::/home/alice:/bin/bash", nil
			}
			return "", errors.New("unexpected")
		}
		gotHome, gotUser, code := resolveServiceIdentity(true)
		if code != 0 || gotUser != "alice" || gotHome != "/home/alice" {
			t.Errorf("got (%q, %q, %d)", gotHome, gotUser, code)
		}
	})

	t.Run("system scope falls back to USER", func(t *testing.T) {
		t.Setenv("SUDO_USER", "")
		t.Setenv("USER", "bob")
		runCommand = func(string, ...string) (string, error) { return "", errors.New("no getent") }
		gotHome, gotUser, code := resolveServiceIdentity(true)
		if code != 0 || gotUser != "bob" || gotHome != home {
			t.Errorf("got (%q, %q, %d)", gotHome, gotUser, code)
		}
	})

	t.Run("system scope defaults to root", func(t *testing.T) {
		t.Setenv("SUDO_USER", "")
		t.Setenv("USER", "")
		_, gotUser, code := resolveServiceIdentity(true)
		if code != 0 || gotUser != "root" {
			t.Errorf("user = %q, code = %d", gotUser, code)
		}
	})
}

// ---------- install ----------

func TestRunServiceInstall_WritesUnitAndEnables(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	serviceStdin = strings.NewReader("y\n")

	var commands []string
	runCommand = func(name string, args ...string) (string, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, joined)
		// Only the user manager answers, so the install goes to the user scope
		// instead of prompting and possibly writing under /etc.
		if joined == "systemctl show-environment" {
			return "", errors.New("no system manager")
		}
		return "", nil
	}

	var code int
	out := captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 0 {
		t.Fatalf("code = %d, want 0\n%s", code, out)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", serviceUnitName)
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("unit file not written: %v", err)
	}
	if !strings.Contains(string(unit), "ExecStart=/usr/local/bin/clawbench") {
		t.Errorf("unit does not pin the stable entry:\n%s", unit)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{"systemctl --user daemon-reload", "systemctl --user enable --now " + serviceUnitName} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if !strings.Contains(out, "已注册") {
		t.Errorf("success message missing:\n%s", out)
	}
}

func TestRunServiceInstall_Cancelled(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	serviceStdin = strings.NewReader("n\n")
	runCommand = func(_ string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "show-environment" {
			return "", errors.New("no system manager")
		}
		return "", nil
	}

	var code int
	captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", serviceUnitName)); !os.IsNotExist(err) {
		t.Error("a cancelled install must not write the unit file")
	}
}

func TestRunServiceInstall_RefusedInContainer(t *testing.T) {
	stubServiceSeams(t)
	isContainer = func() bool { return true }
	var code int
	out := captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 1 || !strings.Contains(out, "容器") {
		t.Errorf("code = %d, out = %q", code, out)
	}
}

func TestRunServiceInstall_RefusedWhenDesktopManaged(t *testing.T) {
	stubServiceSeams(t)
	model.ManagedByEmbedder = true
	var code int
	out := captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 1 || !strings.Contains(out, "客户端托管") {
		t.Errorf("code = %d, out = %q", code, out)
	}
}

func TestRunServiceInstall_NoSystemd(t *testing.T) {
	stubServiceSeams(t)
	runCommand = func(string, ...string) (string, error) { return "", errors.New("down") }
	serviceStdin = strings.NewReader("y\n")
	var code int
	captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunServiceInstall_WriteFailure(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	// A regular file where the .config directory should be makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(home, ".config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	userHomeDir = func() (string, error) { return home, nil }
	serviceStdin = strings.NewReader("y\n")
	runCommand = func(_ string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "show-environment" {
			return "", errors.New("no system manager")
		}
		return "", nil
	}

	var code int
	out := captureStd(t, func() { code = runServiceInstall(nil) })
	if code != 1 || !strings.Contains(out, "写服务文件失败") {
		t.Errorf("code = %d, out = %q", code, out)
	}
}

// ---------- enableServiceUnit ----------

func TestEnableServiceUnit(t *testing.T) {
	stubServiceSeams(t)

	t.Run("user scope uses --user", func(t *testing.T) {
		var calls []string
		runCommand = func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return "", nil
		}
		if code := enableServiceUnit(false); code != 0 {
			t.Fatalf("code = %d", code)
		}
		want := []string{
			"systemctl --user daemon-reload",
			"systemctl --user enable --now " + serviceUnitName,
		}
		if strings.Join(calls, "\n") != strings.Join(want, "\n") {
			t.Errorf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("daemon-reload failure propagates", func(t *testing.T) {
		runCommand = func(string, ...string) (string, error) { return "", errors.New("boom") }
		var code int
		captureStd(t, func() { code = enableServiceUnit(false) })
		if code != 1 {
			t.Errorf("code = %d, want 1", code)
		}
	})

	t.Run("system scope without root hands the command over", func(t *testing.T) {
		geteuid = func() int { return 1000 }
		called := false
		runCommand = func(string, ...string) (string, error) { called = true; return "", nil }
		var code int
		out := captureStd(t, func() { code = enableServiceUnit(true) })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if called {
			t.Error("must not run systemctl without root")
		}
		if !strings.Contains(out, "sudo systemctl daemon-reload") {
			t.Errorf("hand-off command missing:\n%s", out)
		}
	})

	t.Run("system scope as root runs systemctl", func(t *testing.T) {
		geteuid = func() int { return 0 }
		var calls []string
		runCommand = func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return "", nil
		}
		if code := enableServiceUnit(true); code != 0 {
			t.Fatalf("code = %d", code)
		}
		joined := strings.Join(calls, "\n")
		if strings.Contains(joined, "--user") {
			t.Errorf("system scope must not pass --user:\n%s", joined)
		}
		if !strings.Contains(joined, "systemctl enable --now "+serviceUnitName) {
			t.Errorf("enable missing:\n%s", joined)
		}
	})
}

// ---------- uninstall ----------

func TestRunServiceUninstall(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	geteuid = func() int { return 1000 }

	t.Run("nothing installed", func(t *testing.T) {
		var code int
		out := captureStd(t, func() { code = runServiceUninstall() })
		if code != 0 || !strings.Contains(out, "没有找到服务文件") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})

	t.Run("removes the unit", func(t *testing.T) {
		unitPath := filepath.Join(home, ".config", "systemd", "user", serviceUnitName)
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var calls []string
		runCommand = func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return "", nil
		}

		var code int
		captureStd(t, func() { code = runServiceUninstall() })
		if code != 0 {
			t.Fatalf("code = %d", code)
		}
		if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
			t.Error("unit file should be removed")
		}
		joined := strings.Join(calls, "\n")
		if !strings.Contains(joined, "systemctl --user disable --now "+serviceUnitName) {
			t.Errorf("disable missing:\n%s", joined)
		}
	})

	t.Run("remove failure is reported", func(t *testing.T) {
		unitPath := filepath.Join(home, ".config", "systemd", "user", serviceUnitName)
		if err := os.MkdirAll(filepath.Join(unitPath, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(unitPath) })
		var code int
		out := captureStd(t, func() { code = runServiceUninstall() })
		if code != 1 || !strings.Contains(out, "失败") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})
}

// ---------- status ----------

func TestRunServiceStatus(t *testing.T) {
	stubServiceSeams(t)
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }

	t.Run("not registered", func(t *testing.T) {
		var code int
		out := captureStd(t, func() { code = runServiceStatus() })
		if code != 0 || !strings.Contains(out, "当前没有注册") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})

	t.Run("registered and active", func(t *testing.T) {
		unitPath := filepath.Join(home, ".config", "systemd", "user", serviceUnitName)
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runCommand = func(_ string, args ...string) (string, error) {
			last := args[len(args)-2]
			if last == "is-active" {
				return "active", nil
			}
			if last == "MainPID" {
				return "MainPID=4242", nil
			}
			return "", nil
		}
		var code int
		out := captureStd(t, func() { code = runServiceStatus() })
		if code != 0 {
			t.Fatalf("code = %d", code)
		}
		for _, want := range []string{"已注册", "active", "MainPID=4242"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("query failures are explained", func(t *testing.T) {
		runCommand = func(string, ...string) (string, error) { return "", errors.New("no perms") }
		var code int
		out := captureStd(t, func() { code = runServiceStatus() })
		if code != 0 || !strings.Contains(out, "查询失败") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})

	t.Run("empty answer is unknown", func(t *testing.T) {
		runCommand = func(string, ...string) (string, error) { return "", nil }
		var code int
		out := captureStd(t, func() { code = runServiceStatus() })
		if code != 0 || !strings.Contains(out, "未知") {
			t.Errorf("code = %d, out = %q", code, out)
		}
	})
}

// ---------- small helpers ----------

func TestRunSystemctl(t *testing.T) {
	stubServiceSeams(t)

	var got []string
	runCommand = func(name string, args ...string) (string, error) {
		got = append([]string{name}, args...)
		return "", nil
	}
	if code := runSystemctl([]string{"--user"}, "status", "x"); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if strings.Join(got, " ") != "systemctl --user status x" {
		t.Errorf("invocation = %v", got)
	}

	runCommand = func(string, ...string) (string, error) { return "detail", errors.New("failed") }
	var code int
	out := captureStd(t, func() { code = runSystemctl(nil, "start") })
	if code != 1 || !strings.Contains(out, "detail") {
		t.Errorf("code = %d, out = %q", code, out)
	}
}

func TestWriteUnitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "clawbench.service")
	if err := writeUnitFile(path, "hello"); err != nil {
		t.Fatalf("writeUnitFile: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("read back %q, %v", b, err)
	}

	// A file where the directory should be makes MkdirAll fail.
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeUnitFile(filepath.Join(blocked, "clawbench.service"), "x"); err == nil {
		t.Error("expected an error when the parent path is a file")
	}
}

func TestPromptBool(t *testing.T) {
	stubServiceSeams(t)
	cases := map[string]bool{
		"y\n":    true,
		"yes\n":  true,
		"是\n":    true,
		"Y\n":    true,
		"n\n":    false,
		"no\n":   false,
		"否\n":    false,
		"what\n": true, // unknown input keeps the default
		"\n":     true,
		"":       true, // EOF keeps the default
	}
	for stdin, want := range cases {
		serviceStdin = strings.NewReader(stdin)
		var got bool
		captureStd(t, func() { got = promptBool("q", true) })
		if got != want {
			t.Errorf("promptBool with %q = %v, want %v", stdin, got, want)
		}
	}

	// Default false must be honored for empty/unknown input.
	for _, stdin := range []string{"", "\n", "huh\n"} {
		serviceStdin = strings.NewReader(stdin)
		var got bool
		captureStd(t, func() { got = promptBool("q", false) })
		if got {
			t.Errorf("default false not honored for %q", stdin)
		}
	}
}

func TestLookupHome(t *testing.T) {
	stubServiceSeams(t)

	runCommand = func(string, ...string) (string, error) {
		return "alice:x:1000:1000::/home/alice:/bin/bash", nil
	}
	if got := lookupHome("alice"); got != "/home/alice" {
		t.Errorf("lookupHome = %q", got)
	}

	runCommand = func(string, ...string) (string, error) { return "alice:x", nil }
	if got := lookupHome("alice"); got != "" {
		t.Errorf("short passwd line should yield empty, got %q", got)
	}

	runCommand = func(string, ...string) (string, error) { return "", errors.New("missing") }
	if got := lookupHome("nobody"); got != "" {
		t.Errorf("error should yield empty, got %q", got)
	}
}

func TestUserOrFallback(t *testing.T) {
	t.Setenv("SUDO_USER", "alice")
	if got := userOrFallback(); got != "alice" {
		t.Errorf("SUDO_USER should win, got %q", got)
	}
	t.Setenv("SUDO_USER", "")
	t.Setenv("USER", "bob")
	if got := userOrFallback(); got != "bob" {
		t.Errorf("USER should be used, got %q", got)
	}
	t.Setenv("USER", "")
	if got := userOrFallback(); got != "$(id -un)" {
		t.Errorf("fallback = %q", got)
	}
}
