package copilot

import (
	"strings"
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("copilot")
	require.NotNil(t, plugin, "copilot should be registered in the backends registry")
	assert.Equal(t, "copilot", plugin.ID)
}

// The launch shape and sort order are project decisions (see the naming table),
// so they are pinned rather than left to the zero value going unnoticed.
func TestCopilotBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("copilot")
	require.NotNil(t, plugin)
	assert.Equal(t, "copilot", plugin.Spec.ID)
	assert.Equal(t, "copilot", plugin.Spec.Backend)
	assert.Equal(t, "copilot", plugin.Spec.DefaultCmd)
	assert.Equal(t, "GitHub Copilot", plugin.Spec.Name)
	assert.Equal(t, "copilot --acp", plugin.Spec.AcpCommand)
	assert.Equal(t, 23, plugin.Spec.SortOrder)
	// Measured: initialize advertises loadSession:true and sessionCapabilities
	// {close, list}.
	assert.True(t, plugin.Spec.ACPLoadSession)
	// The CLI gates every session on an account, so -32000 has to be rendered as
	// an instruction rather than a JSON-RPC blob.
	assert.Equal(t, "copilot login", plugin.Spec.LoginCmd)
	assert.Equal(t, "npm install -g @github/copilot", plugin.Spec.InstallCmd)
}

// An MCP server injected through session/new was measured to be attached and
// CALLED in the first turn, so no "unsupported" bit may be set — setting one
// would hide a working capability from every user.
func TestCopilotBackendPlugin_MCPSupported(t *testing.T) {
	plugin := backends.Lookup("copilot")
	require.NotNil(t, plugin)
	assert.False(t, plugin.Spec.MCPUnsupported)
	assert.False(t, plugin.Spec.MCPRejectsSSE)
	assert.False(t, plugin.Spec.MCPAttachesAsync)
}

// Copilot advertises no `thought_level` config option, so a static list would
// render a control whose value never reaches the agent.
func TestCopilotBackendPlugin_NoStaticThinkingLevels(t *testing.T) {
	plugin := backends.Lookup("copilot")
	require.NotNil(t, plugin)
	assert.Nil(t, plugin.Spec.ThinkingEffortLevels)
}

// Verified: the ACP session advertised skills from <HOME>/.copilot/skills and
// <HOME>/.agents/skills, and from the project's .github/skills and
// .claude/skills, without any help from ClawBench.
func TestCopilotBackendPlugin_LoadsNativeSkills(t *testing.T) {
	plugin := backends.Lookup("copilot")
	require.NotNil(t, plugin)
	assert.Equal(t, []string{".copilot/skills", ".agents/skills"}, plugin.Spec.NativeSkillsDirs)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
}

// A BYOK binding is what this backend registers, entirely through environment
// variables: endpoint, credential, wire family and the startup model.
func TestCopilotBackendPlugin_ProviderEnvIsBYOK(t *testing.T) {
	assert.True(t, model.HasProviderEnvEntry("copilot"), "the coverage test requires a provider-env entry")
	require.True(t, model.ProviderEnvSupported("copilot", model.ProviderProtocolOpenAI))
	require.True(t, model.ProviderEnvSupported("copilot", model.ProviderProtocolAnthropic))
	require.True(t, model.ProviderEnvSupported("copilot", model.ProviderProtocolGemini))
	assert.False(t, model.ProviderEnvSupported("copilot", model.ProviderProtocolPrivate),
		"a Copilot account token cannot be passed in; only subscription login reaches GitHub's own service")

	env, err := model.BuildProviderEnv("copilot", &model.Provider{
		Name:     "p",
		Protocol: model.ProviderProtocolOpenAI,
		BaseURL:  "https://example.invalid/v1",
		APIKey:   "sk-test",
	}, "")
	require.NoError(t, err)
	assert.Contains(t, env, "COPILOT_PROVIDER_BASE_URL=https://example.invalid/v1")
	assert.Contains(t, env, "COPILOT_PROVIDER_API_KEY=sk-test")
	// The wire family rides on ProtocolEnv.Extra, one value per protocol row:
	// the CLI refuses an unknown COPILOT_PROVIDER_TYPE at startup.
	assert.Contains(t, env, ProviderTypeEnv+"=openai")
	// BYOK refuses to start a session without an explicit model, and
	// settings.json cannot supply it (measured), so the model is a startup
	// variable.
	for _, e := range env {
		assert.NotContains(t, e, "COPILOT_PROVIDER_MODEL_ID",
			"no model may be invented when the provider declares none")
	}
}

// The model variable is what makes a BYOK session start at all: the session's
// candidate set is fixed at session/new, so no later switch can introduce one.
func TestCopilotBackendPlugin_ModelEnvCarriesTheProvidersModel(t *testing.T) {
	p := &model.Provider{
		Name:         "ClawBench",
		Protocol:     model.ProviderProtocolAnthropic,
		BaseURL:      "https://example.invalid",
		APIKey:       "sk-test",
		Models:       []string{"claude-sonnet-4", "claude-haiku-4"},
		DefaultModel: "claude-sonnet-4",
	}
	env, err := model.BuildProviderEnv("copilot", p, "")
	require.NoError(t, err)
	assert.Contains(t, env, "COPILOT_PROVIDER_MODEL_ID=claude-sonnet-4")

	// An agent pinned to a model the provider serves takes priority: two agents
	// on one provider can run two models.
	env, err = model.BuildProviderEnv("copilot", p, "claude-haiku-4")
	require.NoError(t, err)
	assert.Contains(t, env, "COPILOT_PROVIDER_MODEL_ID=claude-haiku-4")

	// A provider with no model at all appends nothing; the CLI then fails its
	// own startup check with a message that names the missing model, which is
	// better than naming one the endpoint never served.
	env, err = model.BuildProviderEnv("copilot", &model.Provider{
		Name: "empty", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://e.invalid", APIKey: "k",
	}, "")
	require.NoError(t, err)
	for _, e := range env {
		assert.NotContains(t, e, "COPILOT_PROVIDER_MODEL_ID")
	}
}

// The provider binding is environment-only. The config directory was removed
// once measurements showed settings.json carries neither the family nor a
// working model, so its presence here would be a second, fake code path.
func TestCopilotBackendPlugin_BindsThroughTheEnvironmentOnly(t *testing.T) {
	assert.False(t, model.ProviderUsesConfigDir("copilot"))
	assert.False(t, model.ProviderNeedsConfigWrite("copilot"))
}

// Each protocol row must carry ITS OWN family: a single shared value would send
// an Anthropic endpoint OpenAI-shaped requests.
func TestCopilotBackendPlugin_EachProtocolCarriesItsOwnWireFamily(t *testing.T) {
	provider := func(protocol string) *model.Provider {
		return &model.Provider{
			Name:         "p",
			Protocol:     protocol,
			BaseURL:      "https://example.invalid/v1",
			APIKey:       "sk-test",
			Models:       []string{"m"},
			DefaultModel: "m",
		}
	}
	envFor := func(protocol string) []string {
		env, err := model.BuildProviderEnv("copilot", provider(protocol), "")
		require.NoError(t, err)
		return env
	}
	assert.Contains(t, envFor(model.ProviderProtocolOpenAI), ProviderTypeEnv+"=openai")
	assert.Contains(t, envFor(model.ProviderProtocolAnthropic), ProviderTypeEnv+"=anthropic")
	assert.Contains(t, envFor(model.ProviderProtocolGemini), ProviderTypeEnv+"=google")
	// Extra's values must agree with ProviderType: one of them describes the
	// variable, the other fills it.
	for _, protocol := range []string{model.ProviderProtocolOpenAI, model.ProviderProtocolAnthropic, model.ProviderProtocolGemini} {
		assert.Contains(t, envFor(protocol), ProviderTypeEnv+"="+ProviderType(protocol))
	}
}

func TestProviderType_SpellsTheWireFamiliesTheCLIAccepts(t *testing.T) {
	assert.Equal(t, "COPILOT_PROVIDER_TYPE", ProviderTypeEnv)
	// The CLI's own error names the accepted set verbatim: "Must be one of:
	// openai, azure, anthropic, google". The spellings below are that set, one
	// per ClawBench protocol.
	assert.Equal(t, "openai", ProviderType(model.ProviderProtocolOpenAI))
	assert.Equal(t, "anthropic", ProviderType(model.ProviderProtocolAnthropic))
	assert.Equal(t, "google", ProviderType(model.ProviderProtocolGemini))
	assert.Empty(t, ProviderType(model.ProviderProtocolPrivate))
	assert.Empty(t, ProviderType("bogus"))
}

// Before the first session there is no model list, and the probe must say so
// instead of inventing one.
func TestDiscoverCopilotModels_ReportsNothingBeforeASession(t *testing.T) {
	models, reason := discoverCopilotModels()
	assert.Empty(t, models)
	assert.True(t, strings.Contains(reason, "ACP"), "the reason should name where the list comes from, got %q", reason)
}

// The title lookup is prefix-based with "<key>:" or an exact match. These are
// the exact titles measured on the wire; the map is keyed by the prefix the
// shared lookup matches, so a title is checked through the same rule it uses.
func TestCopilotTitlePrefixes_ResolveThroughTheRegistry(t *testing.T) {
	prefixes := backends.LookupACPTitlePrefixes("copilot")
	require.NotEmpty(t, prefixes)
	// resolve mirrors internal/ai's lookupByTitlePrefix: longest key first,
	// match on equality, "<key>:" or "<key> ".
	resolve := func(title string) string {
		best := ""
		for k := range prefixes {
			if (title == k || strings.HasPrefix(title, k+":") || strings.HasPrefix(title, k+" ")) && len(k) > len(best) {
				best = k
			}
		}
		if best == "" {
			return ""
		}
		return prefixes[best]
	}
	for title, want := range map[string]string{
		"Running: echo hi":            "Bash",
		"Searching for 'needle'":      "Grep",
		"Finding files matching *.go": "Glob",
		"Reading shell output":        "Read",
		"write_agent":                 "Agent",
		"view":                        "Read",
		"create":                      "Write",
		"edit":                        "Edit",
		"semantic_code_search":        "Grep",
	} {
		assert.Equal(t, want, resolve(title), "title %q", title)
	}
}
