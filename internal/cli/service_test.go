//go:build linux

package cli

import (
	"strings"
	"testing"
)

// ---------- decideServiceInstall ----------

func TestDecideServiceInstall_Refusals(t *testing.T) {
	cases := []struct {
		name      string
		exe       string
		stable    string
		container bool
		managed   bool
		systemd   bool
		wantOK    bool
		wantIn    string
		wantExe   string
	}{
		{
			name:      "container is refused even with systemd present",
			exe:       "/usr/local/bin/clawbench",
			systemd:   true,
			container: true,
			wantIn:    "容器",
		},
		{
			name:    "desktop-managed process is refused",
			exe:     "/opt/clawbench/clawbench",
			systemd: true,
			managed: true,
			wantIn:  "客户端托管",
		},
		{
			name:   "no systemctl",
			exe:    "/usr/local/bin/clawbench",
			wantIn: "systemctl",
		},
		{
			// The regression this whole check exists for: an npm install's
			// directory carries the version and is replaced on upgrade, so a
			// unit pinned to it dies at the first upgrade.
			name:    "npm versioned path is refused",
			exe:     "/home/u/.local/share/clawbench/npm/lib/node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench",
			systemd: true,
			wantIn:  "带版本号",
		},
		{
			name:    "node_modules anywhere is refused",
			exe:     "/srv/app/node_modules/clawbench/bin/clawbench",
			systemd: true,
			wantIn:  "带版本号",
		},
		{
			name:    "stable binary path is accepted",
			exe:     "/usr/local/bin/clawbench",
			systemd: true,
			wantOK:  true,
			wantExe: "/usr/local/bin/clawbench",
		},
		{
			// The npm layout the unit must be pinned to: the real binary is
			// versioned, but the shim on PATH is not, and npm rewrites the shim
			// on upgrade — so the service follows upgrades instead of breaking.
			name:    "versioned binary with a stable PATH entry pins the entry",
			exe:     "/home/u/.local/share/clawbench/npm/lib/node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench",
			stable:  "/usr/local/bin/clawbench",
			systemd: true,
			wantOK:  true,
			wantExe: "/usr/local/bin/clawbench",
		},
		{
			// A "stable" entry that is itself versioned is no better than the
			// binary path; the upgrade would break it the same way.
			name:    "versioned PATH entry does not count as stable",
			exe:     "/home/u/.local/share/clawbench/npm/lib/node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench",
			stable:  "/srv/node_modules/clawbench/bin/clawbench",
			systemd: true,
			wantIn:  "带版本号",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotExe, out := decideServiceInstall(tc.exe, tc.stable, "/data/.clawbench", tc.systemd, tc.container, tc.managed)
			if out.ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason: %s)", out.ok, tc.wantOK, out.reason)
			}
			if tc.wantOK && gotExe != tc.wantExe {
				t.Errorf("exe = %q, want %q", gotExe, tc.wantExe)
			}
			if tc.wantIn != "" && !strings.Contains(out.reason, tc.wantIn) {
				t.Errorf("reason %q does not mention %q", out.reason, tc.wantIn)
			}
		})
	}
}

func TestDecideServiceInstall_EmptyExe(t *testing.T) {
	_, out := decideServiceInstall("", "", "/data", true, false, false)
	if out.ok {
		t.Fatal("empty executable path must be refused")
	}
}

// ---------- buildUnitFile ----------

// The unit must run the binary directly. If ExecStart ever becomes a shell
// wrapper, the server stops being the unit's MainPID, IsRunningUnderSupervisor
// goes false, and a restart launches a sentinel that fights systemd for the
// data-directory lock.
func TestBuildUnitFile_ExecStartRunsBinaryDirectly(t *testing.T) {
	unit := buildUnitFile("/usr/local/bin/clawbench", "/home/u/.clawbench", "u", "/home/u")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/clawbench --data-dir /home/u/.clawbench") {
		t.Fatalf("ExecStart missing or not direct:\n%s", unit)
	}
	for _, bad := range []string{"bash -c", "sh -c", "node ", "/bin/sh"} {
		if strings.Contains(unit, bad) {
			t.Errorf("ExecStart must not go through %q:\n%s", bad, unit)
		}
	}
	if !strings.Contains(unit, "Type=simple") {
		t.Error("Type=simple is required for MainPID to be the server itself")
	}
}

// HOME must be explicit in both scopes: systemd does not infer it once User=
// is set, and the AI CLIs read their config from it.
func TestBuildUnitFile_AlwaysSetsHome(t *testing.T) {
	systemUnit := buildUnitFile("/usr/local/bin/clawbench", "/data", "alice", "/home/alice")
	if !strings.Contains(systemUnit, "Environment=HOME=/home/alice") {
		t.Errorf("system unit missing Environment=HOME:\n%s", systemUnit)
	}
	if !strings.Contains(systemUnit, "User=alice") {
		t.Errorf("system unit missing User=:\n%s", systemUnit)
	}
	if !strings.Contains(systemUnit, "WantedBy=multi-user.target") {
		t.Errorf("system unit needs multi-user.target:\n%s", systemUnit)
	}

	userUnit := buildUnitFile("/usr/local/bin/clawbench", "/data", "", "/home/bob")
	if !strings.Contains(userUnit, "Environment=HOME=/home/bob") {
		t.Errorf("user unit missing Environment=HOME:\n%s", userUnit)
	}
	if strings.Contains(userUnit, "User=") {
		t.Errorf("user unit must not set User= (the manager already runs as that user):\n%s", userUnit)
	}
	if !strings.Contains(userUnit, "WantedBy=default.target") {
		t.Errorf("user unit needs default.target:\n%s", userUnit)
	}
}

func TestBuildUnitFile_QuotesPathsWithSpaces(t *testing.T) {
	unit := buildUnitFile("/home/a b/clawbench", "/home/a b/.clawbench", "u", "/home/a b")
	if !strings.Contains(unit, `ExecStart="/home/a b/clawbench" --data-dir "/home/a b/.clawbench"`) {
		t.Fatalf("paths with spaces must be quoted:\n%s", unit)
	}
}

func TestSystemdQuote_LeavesPlainPathsAlone(t *testing.T) {
	if got := systemdQuote("/usr/local/bin/clawbench"); got != "/usr/local/bin/clawbench" {
		t.Errorf("plain path was modified: %q", got)
	}
}

// ---------- isVersionedInstallPath ----------

func TestIsVersionedInstallPath(t *testing.T) {
	yes := []string{
		"/home/u/.local/share/clawbench/npm/lib/node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench",
		"/usr/lib/node_modules/clawbench/bin/clawbench",
	}
	for _, p := range yes {
		if !isVersionedInstallPath(p) {
			t.Errorf("%q should be treated as a versioned install path", p)
		}
	}
	no := []string{
		"/usr/local/bin/clawbench",
		"/home/u/clawbench",
		"/opt/clawbench/clawbench",
	}
	for _, p := range no {
		if isVersionedInstallPath(p) {
			t.Errorf("%q should NOT be treated as a versioned install path", p)
		}
	}
}

// ---------- dispatch ----------

func TestRunServiceCommand_UnknownAction(t *testing.T) {
	if code := RunServiceCommand([]string{"frobnicate"}); code != 2 {
		t.Errorf("unknown action exit code = %d, want 2", code)
	}
}
