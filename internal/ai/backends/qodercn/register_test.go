package qodercn

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestQoderCNBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin, "qodercn should be registered in backends registry")
	assert.Equal(t, "qodercn", plugin.ID)
	assert.Equal(t, "qodercn", plugin.Spec.Backend,
		"Backend is what ACP event dispatch keys on; it must match the plugin id")
}

func TestQoderCNBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.Equal(t, "Qoder CN", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.Equal(t, "qoderclicn", plugin.Spec.DefaultCmd)
	assert.Equal(t, "qodercn", plugin.Spec.AltCmd,
		"the CN package ships both bins; detection must accept either")
	assert.Equal(t, "qoderclicn --acp", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, "npm install -g @qodercn-ai/qoderclicn", plugin.Spec.InstallCmd)
	assert.Equal(t, 20, plugin.Spec.SortOrder)
}

// LoginCmd must name the CN binary. The two distributions keep separate
// credentials, so the international command would sign the user into the wrong
// product — and the CN binary pointed at the international config root deletes
// that credential outright.
func TestQoderCNBackendPlugin_LoginCmdIsCNDistinct(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.Equal(t, "qoderclicn login", plugin.Spec.LoginCmd)

	intl := backends.Lookup("qoder")
	require.NotNil(t, intl)
	assert.NotEqual(t, intl.Spec.LoginCmd, plugin.Spec.LoginCmd,
		"the two Qoder builds must never share a login command")
	assert.Equal(t, "qodercli login", intl.Spec.LoginCmd)
}

// AcpCommand must spawn a binary detection also checks, or a user who passes
// detection would fail to spawn.
func TestQoderCNBackendPlugin_AcpCommandMatchesDetection(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	bins := []string{plugin.Spec.DefaultCmd, plugin.Spec.AltCmd}
	assert.Contains(t, bins, "qoderclicn",
		"AcpCommand starts with %q, which must be one of the detected bins", plugin.Spec.AcpCommand)
}

// The two builds are separate products: nothing about the CN spec may collide
// with the international one (id, binary, config-derived paths).
func TestQoderCNBackendPlugin_DoesNotCollideWithInternational(t *testing.T) {
	cn := backends.Lookup("qodercn")
	intl := backends.Lookup("qoder")
	require.NotNil(t, cn)
	require.NotNil(t, intl)

	assert.NotEqual(t, cn.ID, intl.ID)
	assert.NotEqual(t, cn.Spec.Backend, intl.Spec.Backend)
	assert.NotEqual(t, cn.Spec.DefaultCmd, intl.Spec.DefaultCmd)
	assert.NotEqual(t, cn.Spec.AcpCommand, intl.Spec.AcpCommand)
	assert.NotEqual(t, cn.Spec.InstallCmd, intl.Spec.InstallCmd)
	assert.NotEqual(t, cn.Spec.SortOrder, intl.Spec.SortOrder,
		"identical SortOrder makes the picker order depend on map iteration")
}

// The CN build's user skills root follows its brand (".qoder-cn/skills") while
// the project root stays ".qoder" — verified in the shipped bundle:
// `saa={user:".qoder-cn",project:".qoder"}`. Getting the user dir wrong makes
// ClawBench inject a second copy of every skill the CN build already loaded.
func TestQoderCNBackendPlugin_DeclaresCNSkillsDirs(t *testing.T) {
	plugin := backends.Lookup("qodercn")
	require.NotNil(t, plugin)

	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
	assert.Equal(t, []string{".qoder-cn/skills", ".agents/skills"}, plugin.Spec.NativeSkillsDirs)
}

// --- CLI backend ---

func TestQoderCNBackend_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qodercn")
	require.NotNil(t, entry, "qodercn CLI factory must be registered")
	backend := entry.NewBackend()
	require.NotNil(t, backend)
	assert.Equal(t, "qodercn", backend.Name())
}

func TestQoderCNBackend_IsCLIBackendWithCNCmd(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qodercn")
	clib, ok := entry.NewBackend().(*ai.CLIBackend)
	require.True(t, ok, "expected *CLIBackend")

	assert.Equal(t, "qoderclicn", clib.Cmd)
	assert.NotNil(t, clib.NewParserFn())
	assert.NotNil(t, clib.PreStartFn, "prompt must go through stdin (--print with a piped stdout)")
}

// The CLI surface is identical between the builds (probe L0 compares all 59
// flags), so the CN backend must produce the same argv as the international
// one. If this drifts, one build silently loses a flag.
func TestQoderCNBackend_ArgsMatchInternational(t *testing.T) {
	cnEntry := ai.LookupBackendFactoryForTest("qodercn")
	intlEntry := ai.LookupBackendFactoryForTest("qoder")
	require.NotNil(t, cnEntry)
	require.NotNil(t, intlEntry)

	req := ai.ChatRequest{
		Prompt:       "p",
		SessionID:    "sess-1",
		WorkDir:      "/tmp/proj",
		Model:        "m",
		SystemPrompt: "sys",
	}
	cnArgs := cnEntry.NewBackend().(*ai.CLIBackend).BuildArgsFn(req)
	intlArgs := intlEntry.NewBackend().(*ai.CLIBackend).BuildArgsFn(req)
	assert.Equal(t, intlArgs, cnArgs, "the two builds share one argv builder")
}

// --- Model discovery ---

// discoveryConfigRoot points the CN discovery at a temp dir by overriding HOME
// (the root is "~/.qoder-cn").
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeCNTexts(t *testing.T, home, payload string) {
	t.Helper()
	dir := filepath.Join(home, ".qoder-cn", ".auth")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dynamic-texts.json"), []byte(payload), 0o600))
}

// The CN catalog must be read from ~/.qoder-cn, NOT ~/.qoder. Both files are
// written here with different model sets so the test distinguishes them.
func TestQoderCNDiscovery_ReadsCNRootOnly(t *testing.T) {
	home := withTempHome(t)

	writeCNTexts(t, home, `{"schemaVersion":3,"locales":{"en":{
		"modelSelector.item.cnmodel":"CN Model"
	}}}`)

	intlDir := filepath.Join(home, ".qoder", ".auth")
	require.NoError(t, os.MkdirAll(intlDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(intlDir, "dynamic-texts.json"),
		[]byte(`{"schemaVersion":3,"locales":{"en":{"modelSelector.item.intlmodel":"Intl Model"}}}`), 0o600))

	src, ok := model.LookupModelSource("qodercn")
	require.True(t, ok, "qodercn must register a model source")
	require.Equal(t, model.SourceKindPlugin, src.Kind())

	models, detail := src.Discover()
	assert.Empty(t, detail)
	require.Len(t, models, 1)
	assert.Equal(t, "cnmodel", models[0].ID, "must read the CN catalog, not the international one")
	assert.Equal(t, "CN Model", models[0].Name)
}

func TestQoderCNDiscovery_MissingFileNamesCNPath(t *testing.T) {
	home := withTempHome(t)

	src, ok := model.LookupModelSource("qodercn")
	require.True(t, ok)
	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Contains(t, detail, ".qoder-cn", "the reason must name the CN config root")
	assert.Contains(t, detail, "not found")
	assert.Contains(t, detail, "qodercn", "the reason must name the CN CLI so the user runs the right login")
	_ = home
}

// A schemaVersion 3 file (locales.<lang>) must parse — the pre-fix parser only
// read the legacy top-level "texts" key and reported "contains no texts"
// against every real install.
func TestQoderCNDiscovery_ParsesCurrentLocaleSchema(t *testing.T) {
	home := withTempHome(t)
	writeCNTexts(t, home, `{
		"schemaVersion": 3,
		"version": "1.0.320",
		"namespace": "qoder-ide",
		"locales": {
			"en": {
				"modelSelector.item.qmodel": "Qwen3.7-Plus",
				"modelSelector.item.qmodel.description": "desc",
				"modelSelector.item.auto": "Auto",
				"modelSelector.item.experts-auto": "Auto",
				"modelSelector.item.qmodel_preview": "Preview",
				"modelSelector.item.kmodel.functionSwitch.highspeed.description": "6x speed"
			},
			"zh-CN": { "modelSelector.item.qmodel": "通义千问" }
		}
	}`)

	src, _ := model.LookupModelSource("qodercn")
	models, detail := src.Discover()
	assert.Empty(t, detail)
	require.Len(t, models, 1, "tier aliases, experts entries, previews and metadata must be filtered out")
	assert.Equal(t, "qmodel", models[0].ID)
	assert.Equal(t, "Qwen3.7-Plus", models[0].Name, "English locale wins when present")
}
