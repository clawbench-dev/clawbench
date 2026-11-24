package cline

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func clinePlugin(t *testing.T) *backends.BackendPlugin {
	t.Helper()
	p := backends.Lookup("cline")
	require.NotNil(t, p, "the cline package must register itself in init()")
	return p
}

// The ACP entry point and the account-login command are what the app spawns and
// what the AUTH_REQUIRED error tells the user to run. Both were measured on
// 3.0.75 (`cline --help`, and session/new answering -32000 before sign-in).
func TestRegistration_ACPSurface(t *testing.T) {
	p := clinePlugin(t)
	assert.Equal(t, "cline", p.Spec.DefaultCmd)
	assert.Equal(t, "cline --acp", p.Spec.AcpCommand)
	assert.Equal(t, "cline", p.Spec.LoginCmd)
	assert.True(t, p.Spec.ACPLoadSession, "initialize reports loadSession:true")
	assert.Equal(t, "npm install -g cline", p.Spec.InstallCmd)
	assert.Equal(t, 22, p.Spec.SortOrder)
	assert.Nil(t, p.ACP, "plain ACP: no tool-name or input-remap tables")
	assert.Nil(t, p.Behavior, "plain ACP: no private wire shapes")
	assert.Nil(t, p.MidTurn)
	assert.Nil(t, p.PluginCommands)
}

// Cline's own skill loader (3.0.75 bundle: bA(), a `skills` tool category with
// use_skill, SKILL.md discovery under .cline/.claude/.agents) means ClawBench
// must not inject a second copy. Flipping this false is the dangerous
// direction, so it is pinned here as well as in native_skills_test.go.
func TestRegistration_LoadsNativeSkills(t *testing.T) {
	p := clinePlugin(t)
	assert.True(t, p.Spec.AutoLoadsNativeSkills)
	assert.Equal(t, []string{".cline/skills", ".agents/skills"}, p.Spec.NativeSkillsDirs)
}

// Thinking levels come from `--thinking none|low|medium|high|xhigh`.
func TestRegistration_ThinkingLevels(t *testing.T) {
	p := clinePlugin(t)
	assert.Equal(t, []string{"none", "low", "medium", "high", "xhigh"}, p.Spec.ThinkingEffortLevels)
}

// A leftover `cline --acp` process after a crash is recognized by the
// command line when the CLAWBENCH_CHILD marker is unreadable.
func TestRegistration_OrphanPattern(t *testing.T) {
	p := clinePlugin(t)
	assert.Equal(t, ai.OrphanCmdlinePattern{Binary: "cline", Arg: "--acp"}, p.OrphanCmdline)
}

// cline only gates sessions on an account. settings/providers.json was measured
// NOT to drive the session, so the classification must stay the deliberate
// "cannot take a provider" one — an entry that accepted a binding would fail
// silently on every turn.
func TestRegistration_CannotTakeProvider(t *testing.T) {
	p := clinePlugin(t)
	require.NotNil(t, p.ProviderEnv)
	require.True(t, reflect.ValueOf(*p.ProviderEnv).IsZero(), "ProviderEnv must be the empty decision")
	assert.True(t, model.HasProviderEnvEntry("cline"))
	assert.False(t, model.ProviderUsesConfigDir("cline"))
	assert.False(t, model.ProviderUsesConfigFile("cline"))
}

// session/list answers -32601, so the drawer depends on the scanner.
func TestRegistration_HasOnDiskSessions(t *testing.T) {
	p := clinePlugin(t)
	require.NotNil(t, p.ListSessions)
	assert.True(t, ai.HasListSessionsFromDisk("cline"))
	require.NotNil(t, p.ModelSource)
	assert.Equal(t, model.SourceKindPlugin, p.ModelSource.Kind())
	assert.True(t, model.HasModelSource("cline"))
}

// catalogPathFor accepts both shapes of a node root, because `npm root -g`
// names node_modules while a resolved bin directory points above it. Getting
// this wrong made discovery report an unreadable path during development.
func TestCatalogPathFor_BothRootShapes(t *testing.T) {
	prefix := filepath.Join("home", "user", ".nvm", "versions", "node", "v24", "lib")
	assert.Equal(t,
		filepath.Join(prefix, "node_modules", "@cline", "llms", "dist", "models.js"),
		catalogPathFor(prefix))
	assert.Equal(t,
		filepath.Join(prefix, "node_modules", "@cline", "llms", "dist", "models.js"),
		catalogPathFor(filepath.Join(prefix, "node_modules")))
}

// The real bundle: a curated provider entry must come back with its display
// name, unrelated `{id,name}` records must not, and the result must be stable.
func TestParseClineModelCatalog_RealBundleExcerpt(t *testing.T) {
	// Excerpts copied from @cline/llms 3.0.75 models.js, including the shapes
	// that must be rejected (a price block, a manifest entry without a window).
	src := `var x=1;"kimi-k3.1-pro":{id:"kimi-k3.1-pro",name:"Kimi K3.1 Pro",contextWindow:262144,` +
		`maxInputTokens:262144,capabilities:["tools"],pricing:{input:0.5,output:2}}}` +
		`,"openai/gpt-4o":{id:"openai/gpt-4o",name:"GPT-4o",contextWindow:128000,capabilities:["images"]}` +
		`,"qwq":{id:"qwq",name:"QwQ",contextWindow:32000}` +
		`;var tools=[{id:"read",name:"Read",description:"reads"}]` +
		`;"plugin":{id:"plugin",name:"Plugin",version:"1"}`

	models := parseClineModelCatalog(src)
	require.Len(t, models, 3)
	assert.Equal(t, model.AgentModel{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro"}, models[0])
	assert.Equal(t, model.AgentModel{ID: "openai/gpt-4o", Name: "GPT-4o"}, models[1])
	assert.Equal(t, model.AgentModel{ID: "qwq", Name: "QwQ"}, models[2])
}

// The key and the id are written identically by the generator; a record where
// they differ is not a model entry.
func TestParseClineModelCatalog_RejectsKeyIDMismatch(t *testing.T) {
	src := `"alias":{id:"real-id",name:"Real",contextWindow:1000}`
	assert.Empty(t, parseClineModelCatalog(src))
}

// Duplicates keep their first occurrence, and a missing name falls back to the
// id, so the picker never shows a blank label.
func TestParseClineModelCatalog_DedupeAndNameFallback(t *testing.T) {
	src := `"m":{id:"m",name:"M",contextWindow:1},` +
		`"m":{id:"m",name:"M again",contextWindow:1},` +
		`"n":{id:"n",contextWindow:2}`
	models := parseClineModelCatalog(src)
	require.Len(t, models, 2)
	assert.Equal(t, "M", models[0].Name)
	assert.Equal(t, model.AgentModel{ID: "n", Name: "n"}, models[1])
}

// The probe reads whatever node root CLAWBENCH_CLINE_ROOT names, and its
// failure is reported as a readable detail instead of an empty list.
func TestDiscoverClineModels_ReadsExplicitRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "node_modules", "@cline", "llms", "dist")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	bundle := `export{Za as X,b2 as MODEL_COLLECTIONS_BY_PROVIDER_ID};` +
		`"probe/model":{id:"probe/model",name:"Probe Model",contextWindow:4096}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "models.js"), []byte(bundle), 0o644))

	only := func(r string) func() []string { return func() []string { return []string{r} } }

	models, detail := discoverClineModelsFrom(only(root))
	assert.Empty(t, detail)
	require.Len(t, models, 1)
	assert.Equal(t, "probe/model", models[0].ID)

	empty := t.TempDir()
	models, detail = discoverClineModelsFrom(only(empty))
	assert.Empty(t, models)
	assert.Contains(t, detail, "not readable")

	// A bundle that exists but no longer holds model entries is reported as
	// such, not as a missing install.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "models.js"), []byte("export{}"), 0o644))
	models, detail = discoverClineModelsFrom(only(root))
	assert.Empty(t, models)
	assert.Contains(t, detail, "no model entries")
}

// The environment seam and the production entry points: CLAWBENCH_CLINE_ROOT is
// tried first, and the PATH lookup parses a real bin directory back to its node
// root (npm installs the launcher under node_modules/.bin).
func TestClinePackageRoots_ExplicitRootFirst(t *testing.T) {
	t.Setenv("CLAWBENCH_CLINE_ROOT", "/opt/cline-root")
	roots := clinePackageRoots()
	require.NotEmpty(t, roots)
	assert.Equal(t, "/opt/cline-root", roots[0])
}

func TestDiscoverClineModels_ReportsWhenNothingIsInstalled(t *testing.T) {
	t.Setenv("CLAWBENCH_CLINE_ROOT", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no cline, no npm
	models, detail := discoverClineModels()
	assert.Empty(t, models)
	assert.NotEmpty(t, detail)
}
