package codebuddy

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// writeProductFile writes a product JSON into dir under the given filename.
func writeProductFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
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

func TestParseProductModels_FiltersAndDefaults(t *testing.T) {
	data := productJSON(t, "default", "auto", "hunyuan-image-v3.0", "glm-4.7", "deepseek-v4-pro")
	models := parseProductModels(data)
	require.Len(t, models, 2, "default/auto/hunyuan-image must be filtered out")
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.True(t, models[0].Default, "first surviving model becomes default")
	assert.Equal(t, "deepseek-v4-pro", models[1].ID)
	assert.False(t, models[1].Default)
}

func TestParseProductModels_HonorsIsDefault(t *testing.T) {
	data := []byte(`{"models":[{"id":"a","name":"A"},{"id":"b","name":"B","isDefault":true}]}`)
	models := parseProductModels(data)
	require.Len(t, models, 2)
	assert.True(t, models[1].Default, "explicit isDefault is honored")
	assert.True(t, models[0].Default, "first model is forced default regardless")
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

func TestDiscoverFrom_NPMLayout(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "node_modules", "@tencent-ai", "codebuddy-code")
	binDir := filepath.Join(pkgDir, "bin")
	writeProductFile(t, pkgDir, "product.cloudhosted.json", productJSON(t, "glm-4.7", "deepseek-v4-pro"))
	realPath := filepath.Join(binDir, "codebuddy")

	models, detail := discoverCodebuddyModelsFrom(realPath, t.TempDir(), func(string) string { return "" })
	require.NotEmpty(t, models, "npm layout must resolve via Dir(Dir(realPath))")
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.Empty(t, detail)
}

func TestDiscoverFrom_NativeLayoutRuntimeCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	// Native install: binary under versions/<ver>/, no product JSON on disk.
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	// Runtime cache is the only source that works for this layout.
	writeCacheEntry(t, filepath.Join(home, ".codebuddy", "local_storage"), productJSON(t, "glm-4.7", "kimi-k2.6"))

	models, detail := discoverCodebuddyModelsFrom(realPath, home, func(string) string { return "" })
	require.NotEmpty(t, models, "native layout must fall back to the runtime cache")
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.Empty(t, detail)
}

func TestDiscoverFrom_ProductFileAlongsideBinary(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "somewhere", "bin")
	writeProductFile(t, binDir, "product.internal.json", productJSON(t, "internal-model"))
	realPath := filepath.Join(binDir, "codebuddy")

	models, _ := discoverCodebuddyModelsFrom(realPath, t.TempDir(), func(string) string { return "" })
	require.NotEmpty(t, models, "product file next to the binary must be found")
	assert.Equal(t, "internal-model", models[0].ID)
}

func TestDiscoverFrom_MultipleProductFileNames(t *testing.T) {
	// All four distribution variants must be recognized.
	for _, name := range codebuddyProductFiles {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "pkg", "bin")
			writeProductFile(t, filepath.Join(root, "pkg"), name, productJSON(t, "model-"+name))
			realPath := filepath.Join(binDir, "codebuddy")

			models, _ := discoverCodebuddyModelsFrom(realPath, t.TempDir(), func(string) string { return "" })
			require.NotEmpty(t, models, "variant %s must be parsed", name)
			assert.Equal(t, "model-"+name, models[0].ID)
		})
	}
}

func TestDiscoverFrom_EnvOverrideFile(t *testing.T) {
	root := t.TempDir()
	cfg := writeProductFile(t, filepath.Join(root, "custom"), "myproduct.json", productJSON(t, "env-model"))
	getenv := func(k string) string {
		if k == "ACC_PRODUCT_CONFIG_PATH" {
			return cfg
		}
		return ""
	}

	// The env override is gated on the CLI being present, so pass a real path.
	realPath := filepath.Join(root, "pkg", "bin", "codebuddy")
	models, _ := discoverCodebuddyModelsFrom(realPath, t.TempDir(), getenv)
	require.NotEmpty(t, models, "env file path override must be honored")
	assert.Equal(t, "env-model", models[0].ID)
}

func TestDiscoverFrom_EnvOverrideIgnoredWithoutCLI(t *testing.T) {
	// A stray ACC_PRODUCT_CONFIG* in the server environment must not make
	// discovery report models for a CLI that isn't installed.
	cfg := writeProductFile(t, t.TempDir(), "product.json", productJSON(t, "env-model"))
	getenv := func(k string) string {
		if k == "ACC_PRODUCT_CONFIG_PATH" {
			return cfg
		}
		return ""
	}

	models, _ := discoverCodebuddyModelsFrom("", t.TempDir(), getenv)
	assert.Empty(t, models, "env override must be ignored when the CLI is absent")
}

func TestDiscoverFrom_EnvOverrideInlineJSON(t *testing.T) {
	inline := string(productJSON(t, "inline-model"))
	getenv := func(k string) string {
		if k == "ACC_PRODUCT_CONFIG_V3" {
			return inline
		}
		return ""
	}

	realPath := filepath.Join(t.TempDir(), "pkg", "bin", "codebuddy")
	models, _ := discoverCodebuddyModelsFrom(realPath, t.TempDir(), getenv)
	require.NotEmpty(t, models, "inline JSON env override must be honored")
	assert.Equal(t, "inline-model", models[0].ID)
}

func TestModelsFromEnvOverride_NilGetenv(t *testing.T) {
	assert.Nil(t, modelsFromEnvOverride(nil))
}

func TestModelsFromEnvOverride_InvalidInlineJSONFallsThrough(t *testing.T) {
	// An inline JSON value that does not yield models must not abort the search;
	// a later variable with a valid file path should still be consulted.
	valid := string(productJSON(t, "later-model"))
	cfg := writeProductFile(t, t.TempDir(), "product.json", []byte(valid))
	getenv := func(k string) string {
		switch k {
		case "ACC_PRODUCT_CONFIG_PATH":
			return `{"models":[]}` // valid JSON, no usable models
		case "ACC_PRODUCT_CONFIG_V3":
			return cfg
		}
		return ""
	}

	models := modelsFromEnvOverride(getenv)
	require.NotEmpty(t, models, "a later env var must still be tried")
	assert.Equal(t, "later-model", models[0].ID)
}

func TestParseProductModels_FirstModelExplicitlyDefault(t *testing.T) {
	// When the first surviving model already carries isDefault, the explicit
	// flag is preserved rather than being reassigned.
	data := []byte(`{"models":[{"id":"a","name":"A","isDefault":true},{"id":"b","name":"B"}]}`)
	models := parseProductModels(data)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default)
	assert.False(t, models[1].Default)
}

func TestParseProductModels_LeadingFilteredEntryStillDefaults(t *testing.T) {
	// "default"/"auto" are filtered before the first surviving model, so the
	// implicit-default rule must land on the first model that survives.
	data := []byte(`{"models":[{"id":"default","name":"Auto"},{"id":"auto"},{"id":"glm-4.7"}]}`)
	models := parseProductModels(data)
	require.Len(t, models, 1)
	assert.Equal(t, "glm-4.7", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestDiscoverFrom_NoSourcesReportsDetail(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "empty-home")
	require.NoError(t, os.MkdirAll(home, 0o755))

	models, detail := discoverCodebuddyModelsFrom("", home, func(string) string { return "" })
	assert.Empty(t, models)
	assert.NotEmpty(t, detail, "a failure must explain what was attempted")
	assert.Contains(t, detail, "no CodeBuddy model list found")
	assert.Contains(t, detail, "codebuddy CLI not found on PATH")
}

// TestDiscoverFrom_NativeLayoutDetailMentionsCache guards the P1 diagnostic: for
// a native install the product config is compiled into the binary, so the
// failure reason must not tell the user to go find a file that never exists —
// it has to report the runtime-cache outcome too.
func TestDiscoverFrom_NativeLayoutDetailMentionsCache(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realPath := filepath.Join(home, ".local", "share", "codebuddy", "versions", "2.150.0", "codebuddy")
	require.NoError(t, os.MkdirAll(filepath.Dir(realPath), 0o755))

	models, detail := discoverCodebuddyModelsFrom(realPath, home, func(string) string { return "" })
	assert.Empty(t, models)
	assert.Contains(t, detail, "runtime cache: no usable entry",
		"detail must report the cache outcome for native installs")
	assert.NotContains(t, detail, "no product config found",
		"must not claim no config was found when the config is embedded in the binary")
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

	models, detail := discoverCodebuddyModelsFrom(realPath, home, func(string) string { return "" })
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

	models, detail := discoverCodebuddyModelsFrom(realPath, home, func(string) string { return "" })
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

	models, detail := discoverCodebuddyModelsFrom("", home, func(string) string { return "" })
	assert.Empty(t, models, "cache must not be used without a CLI")
	assert.Contains(t, detail, "runtime cache: skipped (CLI not found)")
}

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

func TestDiscoverFrom_UnparseableProductFileListedInDetail(t *testing.T) {
	// A product file that exists but yields no models must be reported in the
	// detail, so the user can see the file was found but unusable.
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	writeProductFile(t, pkgDir, "product.cloudhosted.json", []byte(`{"models":[]}`))
	realPath := filepath.Join(pkgDir, "bin", "codebuddy")

	models, detail := discoverCodebuddyModelsFrom(realPath, t.TempDir(), func(string) string { return "" })
	assert.Empty(t, models)
	assert.Contains(t, detail, "product.cloudhosted.json",
		"a found-but-unusable product file must appear in the detail")
}

func TestDiscoverFrom_DetailListIsCapped(t *testing.T) {
	// Many probe paths must not produce an unbounded detail string: the listed
	// paths are truncated to a fixed count, so adding more versions does not
	// keep growing it.
	build := func(versionCount int) string {
		home := filepath.Join(t.TempDir(), "home")
		versionsDir := filepath.Join(home, ".local", "share", "codebuddy", "versions")
		for i := range versionCount {
			dir := filepath.Join(versionsDir, fmt.Sprintf("2.0.%d", i))
			writeProductFile(t, dir, "product.cloudhosted.json", []byte(`{"models":[]}`))
		}
		_, detail := discoverCodebuddyModelsFrom("", home, func(string) string { return "" })
		return detail
	}

	countListed := func(detail string) int {
		return strings.Count(detail, "product.cloudhosted.json")
	}

	small := build(8)
	large := build(30)

	assert.Contains(t, small, "…", "the listed paths must be truncated")
	assert.Equal(t, 6, countListed(small), "at most 6 paths are listed")
	assert.Equal(t, 6, countListed(large), "30 versions must not list more paths than 8")
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

// --- detail reporting via the registered seam ---

func TestCodebuddyDiscoveryDetail_AfterFailure(t *testing.T) {
	origResolve := codebuddyResolveCLIPath
	origHome := codebuddyUserHomeDir
	origGetenv := codebuddyGetenv
	t.Cleanup(func() {
		codebuddyResolveCLIPath = origResolve
		codebuddyUserHomeDir = origHome
		codebuddyGetenv = origGetenv
	})

	codebuddyResolveCLIPath = func(string) string { return "" }
	codebuddyUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	codebuddyGetenv = func(string) string { return "" }

	models := DiscoverCodebuddyModels()
	assert.Empty(t, models)
	assert.NotEmpty(t, CodebuddyDiscoveryDetail(), "failure detail must be recorded")
}

func TestCodebuddyDiscoveryDetail_ClearedOnSuccess(t *testing.T) {
	origResolve := codebuddyResolveCLIPath
	origHome := codebuddyUserHomeDir
	origGetenv := codebuddyGetenv
	t.Cleanup(func() {
		codebuddyResolveCLIPath = origResolve
		codebuddyUserHomeDir = origHome
		codebuddyGetenv = origGetenv
	})

	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "pkg", "bin")
	writeProductFile(t, filepath.Join(root, "pkg"), "product.cloudhosted.json", productJSON(t, "ok-model"))

	codebuddyResolveCLIPath = func(string) string { return filepath.Join(binDir, "codebuddy") }
	codebuddyUserHomeDir = func() (string, error) { return home, nil }
	codebuddyGetenv = func(string) string { return "" }

	models := DiscoverCodebuddyModels()
	require.NotEmpty(t, models)
	assert.Empty(t, CodebuddyDiscoveryDetail(), "detail must be cleared on success")
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
