package ai

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"
)

// openCodePermissionEnv injects an OPENCODE_PERMISSION value into opencode ACP
// processes. See the workaround comment in acp_conn_lifecycle.go for the full
// bug context (subagent permission asks silently dropped by opencode's ACP layer).

func TestOpenCodePermissionEnv_ForOpenCode(t *testing.T) {
	got := openCodePermissionEnv("opencode")
	wantPrefix := "OPENCODE_PERMISSION="
	if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("openCodePermissionEnv(\"opencode\") = %q, want prefix %q", got, wantPrefix)
	}
	raw := got[len(wantPrefix):]
	if raw == "" {
		t.Fatal("OPENCODE_PERMISSION value must not be empty")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("OPENCODE_PERMISSION is not valid JSON: %v\nvalue: %s", err, raw)
	}
}

func TestOpenCodePermissionEnv_OtherBackends(t *testing.T) {
	for _, cmd := range []string{"claude", "codebuddy", "codex", "qoder"} {
		if got := openCodePermissionEnv(cmd); got != "" {
			t.Errorf("openCodePermissionEnv(%q) = %q, want empty (must not affect other backends)", cmd, got)
		}
	}
}

func TestOpenCodePermissionEnv_PreservesModeRestrictions(t *testing.T) {
	// The override must ONLY auto-allow the permissions that default to "ask"
	// and hang subagents (external_directory, .env reads, doom_loop). It must
	// NOT use {"*":"allow"}, which would override per-agent permission rules
	// (e.g. plan mode's edit deny) and break mode enforcement.
	got := openCodePermissionEnv("opencode")
	raw := got[len("OPENCODE_PERMISSION="):]

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("OPENCODE_PERMISSION is not valid JSON: %v", err)
	}

	wantKeys := map[string]bool{"external_directory": true, "read": true, "doom_loop": true}
	for key := range parsed {
		if _, ok := wantKeys[key]; !ok {
			t.Errorf("unexpected permission key %q in override (expected only external_directory/read/doom_loop)", key)
		}
		delete(wantKeys, key)
	}
	for key := range wantKeys {
		t.Errorf("missing permission key %q in override", key)
	}

	// "read" must whitelist .env files, not blanket-allow.
	var read map[string]string
	if err := json.Unmarshal(parsed["read"], &read); err != nil {
		t.Fatalf(`"read" is not a valid JSON object: %v`, err)
	}
	if read["*.env"] != "allow" || read["*.env.*"] != "allow" {
		t.Errorf(`"read" must allow *.env / *.env.* , got %v`, read)
	}
}

// ---------------------------------------------------------------------------
// advertiseTerminalCapability — CodeBuddy background-task compatibility
// ---------------------------------------------------------------------------

func TestAdvertiseTerminalCapability_CodeBuddyHidden(t *testing.T) {
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseTerminalForTest()

	agent := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseTerminalCapability(agent); got {
		t.Error("advertiseTerminalCapability(codebuddy) = true, want false (Terminal capability must be hidden for CodeBuddy so its TaskOutput registry stays consistent)")
	}
}

func TestAdvertiseTerminalCapability_OtherBackendsAdvertised(t *testing.T) {
	ResetAdvertiseTerminalForTest()
	defer ResetAdvertiseTerminalForTest()

	for _, backend := range []string{"claude", "opencode", "kimi", "codex", "qoder", "copilot"} {
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

	agent := &model.Agent{ID: "cb-agent", Backend: "codebuddy"}
	if got := advertiseReadTextFileCapability(agent); got {
		t.Error("advertiseReadTextFileCapability(codebuddy) = true, want false (CodeBuddy must be forced onto its native ReadTool so image Reads emit an image block instead of proxied mojibake text)")
	}
}

func TestAdvertiseReadTextFileCapability_OtherBackendsAdvertised(t *testing.T) {
	ResetAdvertiseReadTextFileForTest()
	defer ResetAdvertiseReadTextFileForTest()

	for _, backend := range []string{"claude", "opencode", "kimi", "codex", "qoder", "copilot"} {
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

	// A non-CodeBuddy agent must get the full capability set — proves the
	// request is built per-agent rather than from a constant.
	other := buildInitializeRequest(&model.Agent{ID: "claude-agent", Backend: "claude"})
	if !other.ClientCapabilities.Fs.ReadTextFile || !other.ClientCapabilities.Terminal {
		t.Errorf("non-CodeBuddy agent must advertise fs.readTextFile + Terminal, got %+v", other.ClientCapabilities)
	}
}
