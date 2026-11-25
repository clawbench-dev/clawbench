package kiro

import (
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// findPlugin returns the registered Kiro plugin. Registration happens in
// init(), so the test reads the real entry rather than a copy made here.
func findPlugin(t *testing.T) *backends.BackendPlugin {
	t.Helper()
	for _, p := range backends.All() {
		if p.ID == "kiro" {
			return p
		}
	}
	t.Fatal("kiro backend is not registered")
	return nil
}

// TestRegisterPinsSpec pins the launch contract. --agent-engine=v3 is the part
// that matters: the flag overrides a saved acp.agentEngine setting, and a
// silently-v2 server has a different flag and slash-command surface.
func TestRegisterPinsSpec(t *testing.T) {
	p := findPlugin(t)
	if p.Spec.DefaultCmd != "kiro-cli" {
		t.Fatalf("DefaultCmd = %q, want kiro-cli", p.Spec.DefaultCmd)
	}
	if p.Spec.AcpCommand != "kiro-cli acp --agent-engine=v3" {
		t.Fatalf("AcpCommand = %q, want the v3-engine launch", p.Spec.AcpCommand)
	}
	if p.Spec.LoginCmd != "kiro-cli login" {
		t.Fatalf("LoginCmd = %q, want kiro-cli login", p.Spec.LoginCmd)
	}
	if p.Spec.SortOrder != 25 {
		t.Fatalf("SortOrder = %d, want 25", p.Spec.SortOrder)
	}
}

// loadSession is true in the authenticated handshake (measured on 3.5.0), and
// ACPLoadSession REPLACES that report — so a mismatch would either disable a
// working acp-load or advertise one Kiro does not have.
func TestACPLoadSessionMatchesMeasuredHandshake(t *testing.T) {
	if !findPlugin(t).Spec.ACPLoadSession {
		t.Fatal("authenticated initialize reports loadSession=true on 3.5.0")
	}
}

// MCPUnsupported must stay false: the authenticated handshake advertises
// http+sse and session/new accepts mcpServers, so filtering MCP out here would
// silently remove a feature Kiro says it has.
func TestMCPNotMarkedUnsupported(t *testing.T) {
	if findPlugin(t).Spec.MCPUnsupported {
		t.Fatal("Kiro advertises mcpCapabilities http/sse; MCPUnsupported must stay false")
	}
}

// TestProviderEnvIsKeyOnly pins the auth model: KIRO_API_KEY is what flips the
// handshake out of "login required", and there is no endpoint to override.
func TestProviderEnvIsKeyOnly(t *testing.T) {
	p := findPlugin(t)
	if p.ProviderEnv == nil {
		t.Fatal("ProviderEnv is nil")
	}
	if p.ProviderEnv.KeyOnly != "KIRO_API_KEY" {
		t.Fatalf("KeyOnly = %q, want KIRO_API_KEY", p.ProviderEnv.KeyOnly)
	}
	if p.ProviderEnv.Anthropic != nil || p.ProviderEnv.OpenAI != nil || p.ProviderEnv.Gemini != nil {
		t.Fatal("Kiro has no endpoint override; protocol slots must stay empty")
	}
	if !model.BackendIsKeyOnly("kiro") {
		t.Fatal("kiro must be classified key-only")
	}
	// OwnServiceEnv only applies to the "private" protocol; Kiro has no
	// endpoint/edition to hide, so it must stay empty.
	if p.ProviderEnv.OwnServiceEnv != "" {
		t.Fatalf("OwnServiceEnv = %q, want empty (no private-protocol entry)", p.ProviderEnv.OwnServiceEnv)
	}
}

// TestSkillsAreNativeAndLoaded pins the measured skill surface: Kiro scans
// ~/.kiro/skills and <project>/.kiro/skills at session start and puts them in
// the prompt (prompts/skills.rs, `Skills` UserContent variant). Flipping
// AutoLoadsNativeSkills to false would make ClawBench inject a second copy.
func TestSkillsAreNativeAndLoaded(t *testing.T) {
	p := findPlugin(t)
	if !p.Spec.AutoLoadsNativeSkills {
		t.Fatal("Kiro loads its own skills into the prompt; AutoLoadsNativeSkills must be true")
	}
	if len(p.Spec.NativeSkillsDirs) != 1 || p.Spec.NativeSkillsDirs[0] != ".kiro/skills" {
		t.Fatalf("NativeSkillsDirs = %v, want exactly [.kiro/skills]", p.Spec.NativeSkillsDirs)
	}
}

// TestModelSourceIsStatic pins that discovery does not shell out: both the
// signed-out and the rejected-key forms of `chat --list-models` fail (measured
// on 3.5.0), so a CLI probe would report a failure on every discovery.
func TestModelSourceIsStatic(t *testing.T) {
	p := findPlugin(t)
	if p.ModelSource == nil {
		t.Fatal("ModelSource is nil")
	}
	if p.ModelSource.Kind() != model.SourceKindStatic {
		t.Fatalf("ModelSource kind = %v, want static", p.ModelSource.Kind())
	}
	// Discover() itself is gated on `kiro-cli` being on PATH (StaticSource
	// returns nothing when it is not), so the content is asserted on the
	// catalog it serves rather than through PATH-dependent discovery.
	defaults := 0
	for _, m := range KiroCatalog {
		if m.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("exactly one model must be the default, got %d", defaults)
	}
}

// The catalog only carries ids that were asserted; an entry invented from a
// docs display name would fail on the wire with "model not found".
func TestKiroCatalogOnlyHasAssertedIDs(t *testing.T) {
	if len(KiroCatalog) != 1 {
		t.Fatalf("catalog grew to %d entries; add one only after capturing the id "+
			"from a real session, and update this assertion with the evidence", len(KiroCatalog))
	}
	if KiroCatalog[0].ID != "auto" {
		t.Fatalf("only `auto` is asserted (Kiro's documented recommended default); got %q", KiroCatalog[0].ID)
	}
}

// TestNewSessionsStartSupervised pins the startup default that keeps the
// permission flow alive: Kiro's own default is autopilot=on, which means tools
// run without a session/request_permission — ClawBench would never render a
// permission card. Measured on 3.5.0, set_config_option(autopilot=off) is the
// only way to change it, and it resets with every new session.
func TestNewSessionsStartSupervised(t *testing.T) {
	got := backends.LookupACPInitialConfigOptions("kiro")
	if got["autopilot"] != "off" {
		t.Fatalf("kiro must start new sessions with autopilot=off, got %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("only autopilot is pinned, got %v", got)
	}
}
