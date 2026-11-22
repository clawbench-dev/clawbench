package ai

import (
	"testing"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// advertiseTerminalCapability — CodeBuddy background-task compatibility
// ---------------------------------------------------------------------------

func TestAdvertiseTerminalCapability_CodeBuddyHidden(t *testing.T) {
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseTerminalForTest()

	defer model.SetBackendRegistryForTest([]model.BackendSpec{
		{Backend: "codebuddy", HideTerminalCapability: true},
	})()
	agent := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseTerminalCapability(agent); got {
		t.Error("advertiseTerminalCapability(codebuddy) = true, want false (Terminal capability must be hidden for CodeBuddy so its TaskOutput registry stays consistent)")
	}
}

func TestAdvertiseTerminalCapability_OtherBackendsAdvertised(t *testing.T) {
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseTerminalForTest()

	for _, backend := range []string{"claude", "opencode", "kimi", "codex", "qoder"} {
		agent := &model.Agent{ID: backend + "-agent", Backend: backend}
		if got := advertiseTerminalCapability(agent); !got {
			t.Errorf("advertiseTerminalCapability(%q) = false, want true (Terminal capability should stay advertised)", backend)
		}
	}
}

func TestAdvertiseTerminalCapability_NilAgentAdvertised(t *testing.T) {
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseTerminalForTest()

	if got := advertiseTerminalCapability(nil); !got {
		t.Error("advertiseTerminalCapability(nil) = false, want true (default should advertise)")
	}
}

func TestAdvertiseTerminalCapability_TestOverrideWins(t *testing.T) {
	// The test override must take precedence so integration tests can force
	// either behavior for regression checks.
	SetAdvertiseTerminalForTest(true)
	defer ResetAdvertiseTerminalForTest()

	codebuddy := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseTerminalCapability(codebuddy); !got {
		t.Error("test override true should force Terminal advertised even for CodeBuddy")
	}
}

// ---------------------------------------------------------------------------
// advertiseReadTextFileCapability — CodeBuddy image-Read compatibility
// ---------------------------------------------------------------------------

func TestAdvertiseReadTextFileCapability_CodeBuddyHidden(t *testing.T) {
	ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseReadTextFileForTest()

	defer model.SetBackendRegistryForTest([]model.BackendSpec{
		{Backend: "codebuddy", HideReadTextFileCapability: true},
	})()
	agent := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseReadTextFileCapability(agent); got {
		t.Error("advertiseReadTextFileCapability(codebuddy) = true, want false (CodeBuddy must be forced onto its native ReadTool so image Reads emit an image block instead of proxied mojibake text)")
	}
}

func TestAdvertiseReadTextFileCapability_OtherBackendsAdvertised(t *testing.T) {
	ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseReadTextFileForTest()

	for _, backend := range []string{"claude", "opencode", "kimi", "codex", "qoder"} {
		agent := &model.Agent{ID: backend + "-agent", Backend: backend}
		if got := advertiseReadTextFileCapability(agent); !got {
			t.Errorf("advertiseReadTextFileCapability(%q) = false, want true (only CodeBuddy swaps its Read tool on this capability)", backend)
		}
	}
}

func TestAdvertiseReadTextFileCapability_NilAgentAdvertised(t *testing.T) {
	ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseReadTextFileForTest()

	if got := advertiseReadTextFileCapability(nil); !got {
		t.Error("advertiseReadTextFileCapability(nil) = false, want true (default should advertise)")
	}
}

func TestAdvertiseReadTextFileCapability_TestOverrideWins(t *testing.T) {
	SetAdvertiseReadTextFileForTest(true)
	defer ResetAdvertiseReadTextFileForTest()

	codebuddy := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseReadTextFileCapability(codebuddy); !got {
		t.Error("test override true should force fs.readTextFile advertised even for CodeBuddy")
	}
}

func TestFileSystemCapabilities_ReadHiddenWriteKept(t *testing.T) {
	// Regression guard: the fix must hide ONLY fs.readTextFile. CodeBuddy gates
	// Write/Edit/MultiEdit on fs.writeTextFile, and their callEdit path reads
	// the file through the client directly (not via isSupportRead), so hiding
	// the read capability must not also hide writes — otherwise edits would
	// stop flowing through ClawBench's fs handlers.
	ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseReadTextFileForTest()
	defer model.SetBackendRegistryForTest([]model.BackendSpec{
		{Backend: "codebuddy", HideReadTextFileCapability: true},
	})()

	codebuddy := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	got := fileSystemCapabilities(codebuddy)
	if got.ReadTextFile {
		t.Error("fs.readTextFile must be hidden for CodeBuddy (native ReadTool must handle image Reads)")
	}
	if !got.WriteTextFile {
		t.Error("fs.writeTextFile must stay advertised so Write/Edit keep using ClawBench's fs handlers")
	}

	// Other backends keep both.
	claude := fileSystemCapabilities(&model.Agent{ID: "claude-agent", Backend: "claude"})
	if !claude.ReadTextFile || !claude.WriteTextFile {
		t.Errorf("other backends must keep both fs capabilities, got %+v", claude)
	}
}

func TestBuildInitializeRequest_CapabilitiesAreWired(t *testing.T) {
	// This is the wiring guard the per-helper tests cannot provide: the
	// capability decisions only matter if they actually reach the Initialize
	// request that spawnLocked sends. Without asserting on the built request,
	// reverting the call site to a hard-coded literal would leave every other
	// unit test green (the only production-path coverage is build-tagged
	// integration, which CI never runs).
	ResetAdvertiseReadTextFileForTest()
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseTerminalForTest()

	defer model.SetBackendRegistryForTest([]model.BackendSpec{
		{Backend: "codebuddy", HideTerminalCapability: true, HideReadTextFileCapability: true},
	})()

	codebuddy := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	req := buildInitializeRequest(codebuddy)

	if req.ClientCapabilities.Fs.ReadTextFile {
		t.Error("CodeBuddy Initialize request must NOT advertise fs.readTextFile (image Reads would be proxied as text)")
	}
	if !req.ClientCapabilities.Fs.WriteTextFile {
		t.Error("CodeBuddy Initialize request must still advertise fs.writeTextFile")
	}
	if req.ClientCapabilities.Terminal {
		t.Error("CodeBuddy Initialize request must NOT advertise Terminal (background-task registry incompatibility)")
	}
	if req.ClientInfo == nil || req.ClientInfo.Name != "clawbench" {
		t.Errorf("Initialize request must identify the client, got %+v", req.ClientInfo)
	}
	// Form elicitation is backend-independent: OpenCode's question tool refuses
	// to send the form at all unless the client advertises it, and answers the
	// model with "the question couldn't be shown" (#633). Advertising it while
	// no handler exists would be worse, so acp_elicitation.go implements the
	// other half.
	if req.ClientCapabilities.Elicitation == nil || req.ClientCapabilities.Elicitation.Form == nil {
		t.Error("Initialize request must advertise form elicitation (OpenCode question tool)")
	}
	// Plan operations are backend-independent too: schema 1.32 sends
	// plan_update/plan_removed only to clients that advertise them, and
	// acp_events.go now renders both.
	if req.ClientCapabilities.Plan == nil {
		t.Error("Initialize request must advertise plan operations (plan_update/plan_removed)")
	}
	if req.ClientCapabilities.Session == nil ||
		req.ClientCapabilities.Session.ConfigOptions == nil ||
		req.ClientCapabilities.Session.ConfigOptions.Boolean == nil {
		t.Error("Initialize request must advertise boolean config options")
	}

	// A non-CodeBuddy agent must get the full capability set — proves the
	// request is built per-agent rather than from a constant.
	other := buildInitializeRequest(&model.Agent{ID: "claude-agent", Backend: "claude"})
	if !other.ClientCapabilities.Fs.ReadTextFile || !other.ClientCapabilities.Terminal {
		t.Errorf("non-CodeBuddy agent must advertise fs.readTextFile + Terminal, got %+v", other.ClientCapabilities)
	}
}
