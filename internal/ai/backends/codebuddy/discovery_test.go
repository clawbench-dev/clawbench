package codebuddy

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// productJSON builds a product-config JSON blob with the given model IDs.
// The first ID becomes the implicit default unless one is marked isDefault.
func productJSON(t *testing.T, ids ...string) []byte {
	t.Helper()
	type m struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault,omitempty"`
	}
	models := make([]m, 0, len(ids))
	for _, id := range ids {
		models = append(models, m{ID: id, Name: "Name-" + id})
	}
	data, err := json.Marshal(map[string]any{"models": models})
	require.NoError(t, err)
	return data
}

// productJSONWithTags builds a product-config blob carrying tags, so tests can
// exercise tag-based filtering (e.g. image-generation models).
func productJSONWithTags(t *testing.T, entries ...codebuddyProductModel) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"models": entries})
	require.NoError(t, err)
	return data
}

func TestParseCodebuddyModels_RealOutput(t *testing.T) {
	output := `Codebuddy Code - AI-powered coding assistant

Usage: codebuddy [options] [prompt]

Options:
  --model <model>  Model to use. Currently supported: (glm-4-plus, glm-4-flash, deepseek-v3, deepseek-r1)
  --json           Output in JSON format

Examples:
  codebuddy "fix the bug"
`

	models := ParseCodebuddyModels(output)
	require.Len(t, models, 4)
	assert.Equal(t, "glm-4-plus", models[0].ID)
	assert.Equal(t, "glm-4-plus", models[0].Name)
	assert.True(t, models[0].Default, "first model should be default")
	assert.Equal(t, "deepseek-r1", models[3].ID)
	assert.False(t, models[3].Default)
}

func TestParseCodebuddyModels_EmptyOutput(t *testing.T) {
	models := ParseCodebuddyModels("")
	assert.Nil(t, models)
}

func TestParseCodebuddyModels_NoSupportedSection(t *testing.T) {
	output := "Some random help text without model list"
	models := ParseCodebuddyModels(output)
	assert.Nil(t, models)
}

func TestParseCodebuddyModels_SingleModel(t *testing.T) {
	output := "Currently supported: (glm-4-plus)"
	models := ParseCodebuddyModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "glm-4-plus", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestCodebuddyModelRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Currently supported: (glm-4-plus, glm-4-flash)", true},
		{"Currently supported: ()", false}, // empty parens — regex requires at least one char inside
		{"No match here", false},
		{"Currently supported: (model-a)", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, codebuddyModelRe.MatchString(tt.input))
		})
	}
}

func TestParseCodebuddyModels_SpacesInModelList(t *testing.T) {
	output := "Currently supported: ( model-a , model-b , model-c )"
	models := ParseCodebuddyModels(output)
	require.Len(t, models, 3)
	assert.Equal(t, "model-a", models[0].ID)
	assert.Equal(t, "model-c", models[2].ID)
}

func TestParseCodebuddyModels_NameEqualsID(t *testing.T) {
	output := "Currently supported: (glm-4-plus)"
	models := ParseCodebuddyModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, models[0].ID, models[0].Name, "name should equal ID for parsed models")
}

// --- parseProductModels ---

func TestParseProductModels_FiltersDefaultAndImages(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "default", Name: "Default"},
		codebuddyProductModel{ID: "hunyuan-image-v3.0-art", Name: "Hunyuan Image V3", Tags: []string{"text-to-image"}},
		codebuddyProductModel{ID: "glm-4.7", Name: "GLM-4.7"},
		codebuddyProductModel{ID: "deepseek-v4-pro", Name: "Deepseek-V4-Pro"},
	)
	models := parseProductModels(data)
	require.Len(t, models, 2, "only 'default' and image models must be filtered out")
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.True(t, models[0].Default, "first surviving model becomes default")
	assert.Equal(t, "deepseek-v4-pro", models[1].ID)
	assert.False(t, models[1].Default)
}

// TestParseProductModels_KeepsAuto guards issue #539: "auto" is CodeBuddy's real
// default (isDefault:true) and must survive, unlike the non-selectable
// "default" fallback marker.
func TestParseProductModels_KeepsAuto(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "auto", Name: "Auto", IsDefault: true, Tags: []string{"craft"}},
		codebuddyProductModel{ID: "deepseek-v4-flash", Name: "Deepseek-V4-Flash"},
	)
	models := parseProductModels(data)
	require.Len(t, models, 2, "auto must be kept")
	assert.Equal(t, "auto", models[0].ID)
	assert.True(t, models[0].Default, "the isDefault:true entry is the default")
	assert.False(t, models[1].Default)
}

// TestParseProductModels_FiltersGenerativeMediaByTag guards the second half of
// issue #539: the filter must key off tags, not a hardcoded ID, so a renamed
// image model (hunyuan-image-v3.0 → ...-art, hunyuan-image-alpha) AND video
// models (kling-video-v3) are all excluded.
func TestParseProductModels_FiltersGenerativeMediaByTag(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "hunyuan-image-v3.0-art", Name: "Hunyuan Image V3", Tags: []string{"text-to-image"}},
		codebuddyProductModel{ID: "hunyuan-image-alpha-edit", Name: "Hunyuan Image Alpha Edit", Tags: []string{"image-to-image"}},
		codebuddyProductModel{ID: "kling-video-v3", Name: "Kling-Video-V3", Tags: []string{"text-to-video", "image-to-video"}},
		codebuddyProductModel{ID: "kimi-k2.6", Name: "Kimi-K2.6"},
	)
	models := parseProductModels(data)
	require.Len(t, models, 1, "image and video models must be filtered by tag")
	assert.Equal(t, "kimi-k2.6", models[0].ID)
}

// TestParseProductModels_SingleDefault guards issue #539's default-mismatch: at
// most one entry may carry Default, and the isDefault flag wins over position.
func TestParseProductModels_SingleDefault(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "deepseek-v3-2-volc", Name: "DeepSeek-V3.2"},
		codebuddyProductModel{ID: "auto", Name: "Auto", IsDefault: true},
		codebuddyProductModel{ID: "glm-5.1", Name: "GLM-5.1"},
	)
	models := parseProductModels(data)
	require.Len(t, models, 3)

	var defaults []string
	for _, m := range models {
		if m.Default {
			defaults = append(defaults, m.ID)
		}
	}
	assert.Equal(t, []string{"auto"}, defaults,
		"exactly one default, honoring isDefault rather than the first row")
}

// TestParseProductModels_DedupesDuplicateIDs verifies the single-default
// invariant holds even when the config lists the same model twice (the
// downstream merge dedups by ID, but this function should not emit two rows).
func TestParseProductModels_DedupesDuplicateIDs(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "auto", Name: "Auto", IsDefault: true},
		codebuddyProductModel{ID: "auto", Name: "Auto", IsDefault: true},
		codebuddyProductModel{ID: "glm-5.1", Name: "GLM-5.1"},
	)
	models := parseProductModels(data)
	require.Len(t, models, 2, "duplicate IDs must be collapsed")
	assert.Equal(t, []string{"auto", "glm-5.1"}, idsOf(models))
}

func TestParseProductModels_FallsBackNameToID(t *testing.T) {
	data := []byte(`{"models":[{"id":"glm-4.7"}]}`)
	models := parseProductModels(data)
	require.Len(t, models, 1)
	assert.Equal(t, "glm-4.7", models[0].Name)
}

func TestParseProductModels_InvalidJSON(t *testing.T) {
	assert.Nil(t, parseProductModels([]byte("not json")))
}

func TestParseProductModels_NoModels(t *testing.T) {
	assert.Nil(t, parseProductModels([]byte(`{"models":[]}`)))
	assert.Nil(t, parseProductModels([]byte(`{"models":[{"id":"default"}]}`)))
}

// --- discoverCodebuddyModelsFrom: layout probing ---

// The runtime cache is the ONLY local source. These tests pin that: a bundled
// product JSON or an ACC_PRODUCT_CONFIG* environment override present next to a
// REAL cache must be ignored entirely, not merely ranked lower.

func TestDiscoverFrom_NPMLayoutRuntimeCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realPath := filepath.Join(root, "node_modules", "@tencent-ai", "codebuddy-code", "bin", "codebuddy")
	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"),
		productJSON(t, "glm-4.7", "deepseek-v4-pro"))

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	require.NotEmpty(t, models, "detail: %s", detail)
	assert.Equal(t, []string{"glm-4.7", "deepseek-v4-pro"}, idsOf(models))
	assert.Empty(t, detail)
}

func TestDiscoverFrom_NativeLayoutRuntimeCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	// Native install: binary under versions/<ver>/, no product JSON on disk.
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"), productJSON(t, "glm-4.7", "kimi-k2.6"))

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	require.NotEmpty(t, models, "native layout must use the runtime cache")
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.Empty(t, detail)
}

// TestDiscoverFrom_ProductFileIsNeverConsulted is the regression test for the
// removal of the bundled product.*.json fallback: those files are factory
// defaults whose model list can be disjoint from the account's deployment
// (issue #539), so an empty cache must report NOTHING rather than their models.
func TestDiscoverFrom_ProductFileIsNeverConsulted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	pkgDir := filepath.Join(root, "node_modules", "@tencent-ai", "codebuddy-code")
	writeProductFixture(t, pkgDir, "product.cloudhosted.json",
		productJSON(t, "deepseek-v3-2-volc", "glm-4.7", "kimi-k2.5"))
	realPath := filepath.Join(pkgDir, "bin", "codebuddy")

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	assert.Empty(t, models, "a factory-default product file must never become the model list")
	assert.Contains(t, detail, "no CodeBuddy model list found")
	assert.Contains(t, detail, "local_storage")
}

// TestDiscoverFrom_EnvOverrideIsNeverConsulted pins the removal of the
// ACC_PRODUCT_CONFIG* source: the variables are read from the SERVER process
// env, so honoring them made discovery depend on an unrelated deployment's
// configuration.
func TestDiscoverFrom_EnvOverrideIsNeverConsulted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	cfg := filepath.Join(root, "myproduct.json")
	require.NoError(t, os.WriteFile(cfg, productJSON(t, "env-model"), 0o644))
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", cfg)
	t.Setenv("ACC_PRODUCT_CONFIG_V3", string(productJSON(t, "inline-model")))
	realPath := filepath.Join(root, "pkg", "bin", "codebuddy")

	models, _ := discoverCodebuddyModelsFrom(realPath, home)
	assert.Empty(t, models, "the env override is not a source any more")
}

func TestDiscoverFrom_CacheWinsOverProductFile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	pkgDir := filepath.Join(root, "node_modules", "@tencent-ai", "codebuddy-code")
	writeProductFixture(t, pkgDir, "product.cloudhosted.json",
		productJSON(t, "deepseek-v3-2-volc", "glm-4.7", "kimi-k2.5"))
	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"),
		productJSON(t, "auto", "deepseek-v4-flash", "glm-5.1"))
	realPath := filepath.Join(pkgDir, "bin", "codebuddy")

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	require.NotEmpty(t, models, "detail: %s", detail)
	assert.Equal(t, []string{"auto", "deepseek-v4-flash", "glm-5.1"}, idsOf(models))
}

func TestDiscoverFrom_NoSourcesReportsDetail(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "empty-home")
	require.NoError(t, os.MkdirAll(home, 0o755))

	models, detail := discoverCodebuddyModelsFrom("", home)
	assert.Empty(t, models)
	assert.NotEmpty(t, detail, "a failure must explain what was attempted")
	assert.Contains(t, detail, "codebuddy CLI not found on PATH")
}

// TestDiscoverFrom_EmptyCacheDetailNamesTheCache keeps the P1 diagnostic: when
// the CLI is installed but has never run against this account, the reason must
// point at the cache directory the user can populate by signing in.
func TestDiscoverFrom_EmptyCacheDetailNamesTheCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	assert.Empty(t, models)
	assert.Contains(t, detail, "no CodeBuddy model list found")
	assert.Contains(t, detail, filepath.Join(home, ".codebuddy", "local_storage"),
		"the detail must name the directory that is empty")
}

// TestDiscoverFrom_PrefersBaseCacheOverPlugin is the regression test for the
// ranking defect: the cache directory holds both the base config and plugin
// overlays (which carry extra internal completion models). Selection must be
// deterministic and prefer the base, newest entry rather than depending on
// hash-based filename order.
func TestDiscoverFrom_PrefersBaseCacheOverPlugin(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	dir := filepath.Join(home, ".codebuddy", "local_storage")

	// A plugin overlay that sorts FIRST by filename and is newer, but must lose.
	writeCacheEntryNamed(t, dir, "entry_0000_plugin.info",
		cacheProductJSON(t, "plugin", "2026-09-11T00:00:00.000Z", "glm-5.1", "codewise-completions"))
	// The base config, older and sorting later, but authoritative.
	writeCacheEntryNamed(t, dir, "entry_zzzz_base.info",
		cacheProductJSON(t, "", "2026-08-01T00:00:00.000Z", "deepseek-v4-flash", "glm-5.1"))

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	require.NotEmpty(t, models, "detail: %s", detail)
	assert.Equal(t, "deepseek-v4-flash", models[0].ID, "base config must win over a plugin overlay")
	assert.NotContains(t, idsOf(models), "codewise-completions",
		"plugin-only internal models must not leak into the list")
}

// TestDiscoverFrom_PrefersNewestBaseCache verifies that among base entries the
// most recent date wins, independent of filename order.
func TestDiscoverFrom_PrefersNewestBaseCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	dir := filepath.Join(home, ".codebuddy", "local_storage")
	writeCacheEntryNamed(t, dir, "entry_0000_old.info",
		cacheProductJSON(t, "", "2026-01-01T00:00:00.000Z", "old-model"))
	writeCacheEntryNamed(t, dir, "entry_zzzz_new.info",
		cacheProductJSON(t, "", "2026-09-11T00:00:00.000Z", "new-model"))

	models, detail := discoverCodebuddyModelsFrom(realPath, home)
	require.NotEmpty(t, models, "detail: %s", detail)
	assert.Equal(t, "new-model", models[0].ID, "newest base entry must win")
}

// TestDiscoverFrom_CacheGateRequiresCLI verifies the documented safety property:
// a valid cache is not used when no codebuddy CLI is on PATH.
func TestDiscoverFrom_CacheGateRequiresCLI(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	writeCacheEntryNamed(t, dir, "entry_base.info",
		cacheProductJSON(t, "", "2026-09-11T00:00:00.000Z", "cached-model"))

	models, detail := discoverCodebuddyModelsFrom("", home)
	assert.Empty(t, models, "cache must not be used without a CLI")
	assert.Contains(t, detail, "codebuddy CLI not found on PATH")
}

// --- decodeCacheEntry ---

// --- decodeCacheEntry ---

func TestDecodeCacheEntry_RejectsOversizedPlainJSON(t *testing.T) {
	// A plain-JSON entry larger than the cap must be rejected rather than
	// loaded, so a corrupt file cannot force an unbounded allocation.
	big := append([]byte(`{"models":[`), bytes.Repeat([]byte("x"), maxCacheEntryBytes+16)...)
	big = append(big, ']', '}')
	_, ok := decodeCacheEntry(big)
	assert.False(t, ok, "oversized plain JSON must be rejected")
}

func TestDecodeCacheEntry_RejectsOversizedGzipPayload(t *testing.T) {
	// A gzip payload whose decompressed output exceeds the cap must be
	// rejected, not silently truncated.
	inner := bytes.Repeat([]byte("a"), maxCacheEntryBytes+16)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(inner)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	_, ok := decodeCacheEntry([]byte(`"` + encoded + `"`))
	assert.False(t, ok, "decompressed output over the cap must be rejected")
}

func TestModelsFromRuntimeCache_SkipsOversizedEntry(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Oversized entry sorts first by filename but must be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry_a_big.info"),
		bytes.Repeat([]byte("z"), maxCacheEntryBytes+1), 0o644))
	writeCacheEntryNamed(t, dir, "entry_z_good.info", cacheProductJSON(t, "", "2026-09-11T00:00:00.000Z", "good-model"))

	models := modelsFromRuntimeCache(home)
	require.NotEmpty(t, models, "the oversized entry must not block the good one")
	assert.Equal(t, "good-model", models[0].ID)
}

func TestDecodeCacheEntry_Base64Gzip(t *testing.T) {
	inner := productJSON(t, "cached-model")

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(inner)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	quoted := []byte(`"` + encoded + `"`)

	out, ok := decodeCacheEntry(quoted)
	require.True(t, ok)
	assert.JSONEq(t, string(inner), string(out))
}

func TestDecodeCacheEntry_PlainJSON(t *testing.T) {
	inner := productJSON(t, "plain-model")
	out, ok := decodeCacheEntry(inner)
	require.True(t, ok)
	assert.JSONEq(t, string(inner), string(out))
}

func TestDecodeCacheEntry_UnrelatedEntry(t *testing.T) {
	_, ok := decodeCacheEntry([]byte(`"cloudhosted"`))
	assert.False(t, ok, "a non-JSON, non-base64 cache value must be rejected")
}

func TestModelsFromRuntimeCache_SkipsUnrelatedEntries(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Unrelated entries the CLI also writes.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry_a.info"), []byte(`"cloudhosted"`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry_b.info"), []byte(`{"productFeatures":{"a":true}}`), 0o644))
	writeCacheEntry(t, dir, productJSON(t, "real-model"))

	models := modelsFromRuntimeCache(home)
	require.NotEmpty(t, models)
	assert.Equal(t, "real-model", models[0].ID)
}

func TestModelsFromRuntimeCache_MissingDir(t *testing.T) {
	assert.Nil(t, modelsFromRuntimeCache(t.TempDir()))
}

func TestModelsFromRuntimeCache_EmptyHome(t *testing.T) {
	assert.Nil(t, modelsFromRuntimeCache(""))
}

func TestModelsFromRuntimeCache_SkipsSubdirsAndNonInfoFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested.info"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("irrelevant"), 0o644))

	assert.Nil(t, modelsFromRuntimeCache(home),
		"directories and non-.info files must be ignored")
}

func TestModelsFromRuntimeCache_NoUsableEntry(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// Valid JSON but no usable models.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry_a.info"), []byte(`{"models":[]}`), 0o644))

	assert.Nil(t, modelsFromRuntimeCache(home))
}

// --- detail reporting through the ModelSource entry point ---

func TestCodebuddySource_ReportsDetailOnFailure(t *testing.T) {
	origResolve := codebuddyResolveCLIPath
	origHome := codebuddyUserHomeDir
	t.Cleanup(func() {
		codebuddyResolveCLIPath = origResolve
		codebuddyUserHomeDir = origHome
	})

	codebuddyResolveCLIPath = func(string) string { return "" }
	codebuddyUserHomeDir = func() (string, error) { return t.TempDir(), nil }

	models, detail := discoverCodebuddyModels()
	assert.Empty(t, models)
	assert.NotEmpty(t, detail, "the failure reason must be returned to the caller")
}

func TestCodebuddySource_NoDetailOnSuccess(t *testing.T) {
	origResolve := codebuddyResolveCLIPath
	origHome := codebuddyUserHomeDir
	t.Cleanup(func() {
		codebuddyResolveCLIPath = origResolve
		codebuddyUserHomeDir = origHome
	})

	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "pkg", "bin")
	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"), productJSON(t, "ok-model"))

	codebuddyResolveCLIPath = func(string) string { return filepath.Join(binDir, "codebuddy") }
	codebuddyUserHomeDir = func() (string, error) { return home, nil }

	models, detail := discoverCodebuddyModels()
	require.NotEmpty(t, models)
	assert.Empty(t, detail, "a successful discovery has no failure to report")
}

func TestCodebuddySource_ConcurrentCallsDoNotShareDetail(t *testing.T) {
	// The old design stored the last failure in package state, so a concurrent
	// refresh could overwrite the detail a manual refresh was about to read.
	// With the detail returned per call, a failure and a success interleaved
	// must not contaminate each other.
	origResolve := codebuddyResolveCLIPath
	origHome := codebuddyUserHomeDir
	t.Cleanup(func() {
		codebuddyResolveCLIPath = origResolve
		codebuddyUserHomeDir = origHome
	})

	root := t.TempDir()
	home := filepath.Join(root, "home")
	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"), productJSON(t, "ok-model"))
	okBin := filepath.Join(root, "pkg", "bin", "codebuddy")

	const rounds = 20
	results := make(chan [2]string, rounds*2)
	for range rounds {
		go func() {
			codebuddyResolveCLIPath = func(string) string { return okBin }
			codebuddyUserHomeDir = func() (string, error) { return home, nil }
			models, detail := discoverCodebuddyModels()
			results <- [2]string{fmt.Sprintf("%d", len(models)), detail}
		}()
		go func() {
			codebuddyResolveCLIPath = func(string) string { return "" }
			codebuddyUserHomeDir = func() (string, error) { return t.TempDir(), nil }
			models, detail := discoverCodebuddyModels()
			results <- [2]string{fmt.Sprintf("%d", len(models)), detail}
		}()
	}
	for range rounds * 2 {
		r := <-results
		if r[0] == "0" {
			assert.NotEmpty(t, r[1], "an empty result must carry its own reason")
		} else {
			assert.Empty(t, r[1], "a successful result must not inherit another call's failure detail")
		}
	}
}

// writeProductFixture drops a product JSON on disk. It exists ONLY so tests can
// assert such a file is IGNORED — discovery no longer reads them.
func writeProductFixture(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
}

// writeCacheEntry writes a product JSON as a base64+gzip .info entry, matching
// the format CodeBuddy uses for its local_storage cache.
func writeCacheEntry(t *testing.T, dir string, inner []byte) {
	t.Helper()
	writeCacheEntryNamed(t, dir, "entry_product.info", inner)
}

// writeCacheEntryNamed writes a cache entry under an explicit filename, so tests
// can control os.ReadDir ordering.
func writeCacheEntryNamed(t *testing.T, dir, name string, inner []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(inner)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(`"`+encoded+`"`), 0o644))
}

// cacheProductJSON builds a product config carrying the metadata fields the
// cache ranking reads (pluginName, date) alongside a models list.
func cacheProductJSON(t *testing.T, plugin, date string, ids ...string) []byte {
	t.Helper()
	type m struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	models := make([]m, 0, len(ids))
	for _, id := range ids {
		models = append(models, m{ID: id, Name: "Name-" + id})
	}
	obj := map[string]any{
		"productName":    "CodeBuddy",
		"deploymentType": "Cloud-Hosted",
		"date":           date,
		"models":         models,
	}
	if plugin != "" {
		obj["pluginName"] = plugin
	}
	data, err := json.Marshal(obj)
	require.NoError(t, err)
	return data
}

// idsOf extracts model IDs for assertions.
func idsOf(models []model.AgentModel) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

// TestParseProductModels_CarriesCredits guards issue #657: the picker shows the
// same multiplier the CodeBuddy CLI does. The value is the product config's own
// `credits` string, verbatim — the auto router has none, and that must stay
// empty rather than becoming "0".
func TestParseProductModels_CarriesCredits(t *testing.T) {
	data := productJSONWithTags(t,
		codebuddyProductModel{ID: "auto", Name: "Auto", IsDefault: true},
		codebuddyProductModel{ID: "deepseek-v4-flash", Name: "Deepseek-V4-Flash", Credits: " x0.06 credits "},
		codebuddyProductModel{ID: "glm-5.3", Name: "GLM-5.3", Credits: "x1.00 credits"},
	)

	models := parseProductModels(data)
	require.Len(t, models, 3)
	assert.Empty(t, models[0].Credits, "the auto router publishes no multiplier")
	assert.Equal(t, "x0.06 credits", models[1].Credits, "surrounding whitespace is trimmed, the text is kept")
	assert.Equal(t, "x1.00 credits", models[2].Credits)
}
