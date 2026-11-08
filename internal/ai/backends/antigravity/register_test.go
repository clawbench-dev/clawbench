package antigravity

import (
	"strings"
	"testing"

	"clawbench/internal/ai/backends"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAntigravityBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("antigravity")
	require.NotNil(t, plugin, "antigravity should be registered in the backends registry")

	assert.Equal(t, "antigravity", plugin.Spec.ID)
	assert.Equal(t, "antigravity", plugin.Spec.Backend)
	assert.Equal(t, "Antigravity", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.True(t, plugin.Spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, 14, plugin.Spec.SortOrder)
}

// The default ACP command must be a subcommand of the CLI we detect on PATH,
// not a separately-fetched bridge.
//
// This is the regression guard for issue #641: the default used to be
// `npx -y agy-acp@latest`, a third-party Node adapter that never launches agy
// for the prompt turn (the conversation hangs) and leaks until its JS heap OOMs
// after ~15 minutes (surfacing as "AI backend exited abnormally"). Because
// `syncAgentInfrastructure` rewrites `acp_command` from this spec on every
// startup/rescan, every user got that adapter whether they wanted it or not —
// so the spec value IS the product default, and pinning it to the installed
// CLI's own `acp` subcommand is what keeps the backend usable.
//
// The assertion is structural (same executable, trailing subcommand) rather
// than a bare string compare so a future rewrite cannot quietly reintroduce a
// fetched adapter under a different package name.
func TestAntigravityBackendPlugin_AcpCommandIsNativeSubcommand(t *testing.T) {
	plugin := backends.Lookup("antigravity")
	require.NotNil(t, plugin)

	fields := strings.Fields(plugin.Spec.AcpCommand)
	require.NotEmpty(t, fields, "antigravity must declare an ACP command")
	assert.Equal(t, plugin.Spec.DefaultCmd, fields[0],
		"the ACP command must run the CLI detected on PATH (%q), not a fetched adapter: got %q",
		plugin.Spec.DefaultCmd, plugin.Spec.AcpCommand)
	assert.Equal(t, []string{"acp"}, fields[1:],
		"agy exposes ACP as the `acp` subcommand: got %q", plugin.Spec.AcpCommand)
	assert.NotContains(t, plugin.Spec.AcpCommand, "npx",
		"a fetched adapter reintroduces the #641 hang/OOM default")
}

// LoginCmd drives the AUTH_REQUIRED message and the "copy the command" button.
// `agy acp` answers session/new with -32000 Authentication required until the
// CLI has been signed in. `--login` is agy's own ACP terminal-auth flag (it
// signs in with Google and exits), so it is the command an editor-only user
// needs; plain `agy` would open the interactive TUI and wait.
func TestAntigravityBackendPlugin_LoginCmd(t *testing.T) {
	plugin := backends.Lookup("antigravity")
	require.NotNil(t, plugin)

	assert.Equal(t, "agy acp --login", plugin.Spec.LoginCmd)
	assert.Contains(t, plugin.Spec.LoginCmd, plugin.Spec.DefaultCmd,
		"the login command must run the CLI we detect on PATH")
}
