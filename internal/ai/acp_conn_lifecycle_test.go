package ai

import (
	"encoding/json"
	"testing"
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
