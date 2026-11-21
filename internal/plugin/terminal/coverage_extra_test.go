package terminal

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/terminal"
)

// Recording a command from the terminal is what makes the history panel useful,
// and the sink is installed by SetTerminalManager (a second creation site would
// otherwise forget it). This is the whole reason the sink exists: the service
// package cannot be imported from the terminal one, so the injection point is
// the only place the wiring can be checked.
//
// The recorder is unexported and has no test hook, and a real submission needs a
// foreground shell on a PTY (already covered by the terminal package's own
// tests). What is pinned here is the wiring: a manager created the way main.go
// creates it — through SetTerminalManager — must come out with a sink, or every
// command a user types silently never reaches the history panel.
func TestSetTerminalManager_InstallsTheCommandRecorder(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		if cur := GetTerminalManager(); cur != nil && cur != origMgr {
			cur.Close()
		}
		SetTerminalManager(origMgr)
	})

	// A broken database is not needed: the wiring is the assertion, and a real
	// row would only re-test service.RecordTerminalCommand.
	manager := terminal.NewManager(model.TerminalConfig{
		Enabled: true, IdleTimeout: "1m", BufferLines: 100, MaxLineBytes: 65536, MaxBufferMB: 4,
	}, 0)
	SetTerminalManager(manager)
	if !manager.HasCommandRecorder() {
		t.Fatal("SetTerminalManager must install the command recorder; otherwise /api/terminal/history stays empty")
	}

	// Clearing the manager must not panic and leaves the global unset.
	SetTerminalManager(nil)
	if GetTerminalManager() != nil {
		t.Fatal("SetTerminalManager(nil) must clear the global manager")
	}
}

// A nil agent or an empty project has nothing to probe: the probe needs a cwd
// for session/new, and a nil agent is the "unknown id" case of the caller.
