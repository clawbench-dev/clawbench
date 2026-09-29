package deepseek

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDeepSeekModels_RealOutput(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
* deepseek-v4-pro (deepseek)
  deepseek-ai/deepseek-v4-pro (nvidia-nim)
  deepseek-ai/deepseek-v4-flash (nvidia-nim)
  gpt-4.1 (openai)
  gpt-4.1-mini (openai)
  deepseek/deepseek-v4-pro (openrouter)
  deepseek/deepseek-v4-flash (openrouter)
  deepseek-coder:1.3b (ollama)
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 2, "should only include deepseek provider models, not third-party")

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].Name)
	assert.False(t, models[0].Default, "flash is not the default")
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].Name)
	assert.True(t, models[1].Default, "pro is the default (marked with *)")
}

func TestParseDeepSeekModels_EmptyOutput(t *testing.T) {
	models := parseDeepSeekModels("no models here")
	assert.Nil(t, models)
}

func TestParseDeepSeekModels_NoDefaultMarker(t *testing.T) {
	output := `  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first model should be default as fallback")
	assert.False(t, models[1].Default)
}

func TestParseDeepSeekModels_DefaultFromHeader(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.False(t, models[0].Default)
	assert.True(t, models[1].Default, "should match default from header")
}

func TestParseDeepSeekModels_ProviderPrefixInIDAndName(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
* deepseek-v4-pro (deepseek)
  deepseek-v4-flash (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)

	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].Name)
	assert.True(t, models[0].Default)

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].Name)
}

func TestParseDeepSeekModels_ThirdPartyProviderFiltered(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-pro (deepseek)
  deepseek-v4-pro (nvidia-nim)
  gpt-4.1 (openai)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
}

// TestParseDeepSeekModels_CurrentFormatZhHans covers CodeWhale 0.10.0, whose
// output is localized (zh-Hans by default), scoped to a single provider, and
// no longer tags each row with its provider. The leading "*" still marks the
// default; banner and footer lines must not be mistaken for models.
func TestParseDeepSeekModels_CurrentFormatZhHans(t *testing.T) {
	output := `deepseek 的模型（默认：deepseek-v4-pro）
source=provider_models	status={"state":"unknown"}	fetched_at=unknown	observed_at=unknown
内置或已配置的模型；尚未验证可用性。
  deepseek-flash
  deepseek-v4-flash
  deepseek-v4-flash-vision-exp
* deepseek-v4-pro
已缓存的目录；运行 ` + "`codewhale models --update`" + ` 可更新已配置的提供商。
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 4, "banner/footer lines must not be parsed as models")

	assert.Equal(t, "deepseek/deepseek-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash-vision-exp", models[2].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[3].ID)

	assert.False(t, models[0].Default)
	assert.True(t, models[3].Default, "pro carries the * marker")
}

// TestParseDeepSeekModels_CurrentFormatEnUS covers the same current format with
// the English header template ("{provider} models (default: {model})"), which
// the binary emits when the UI locale is English.
func TestParseDeepSeekModels_CurrentFormatEnUS(t *testing.T) {
	output := `deepseek models (default: deepseek-v4-pro)
Bundled or configured models; availability not verified.
  deepseek-flash
  deepseek-v4-flash
* deepseek-v4-pro
Cached catalog; run ` + "`codewhale models --update`" + ` to refresh configured providers.
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 3)
	assert.Equal(t, "deepseek/deepseek-flash", models[0].ID)
	assert.True(t, models[2].Default, "default comes from the header when no * is present")
}

// TestParseDeepSeekModels_CurrentFormatNoStar covers a scoped listing with no
// explicit marker: the header names the default, so the first entry must not
// silently win.
func TestParseDeepSeekModels_CurrentFormatNoStar(t *testing.T) {
	output := `deepseek 的模型（默认：deepseek-v4-pro）
  deepseek-v4-flash
  deepseek-v4-pro
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.False(t, models[0].Default, "first entry must not be the fallback default")
	assert.True(t, models[1].Default, "default comes from the localized header")
}

// TestParseDeepSeekModels_LegacyFormatStillWorks guards the pre-0.10.0 output,
// where every configured provider is listed and rows carry a "(provider)" tag.
func TestParseDeepSeekModels_LegacyFormatStillWorks(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
* deepseek-v4-pro (deepseek)
  deepseek-ai/deepseek-v4-pro (nvidia-nim)
  gpt-4.1 (openai)
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 2, "third-party providers must be filtered out")
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].ID)
	assert.True(t, models[1].Default)
}

// TestParseDeepSeekModels_BannerLinesRejected pins the specific non-model lines
// the current CLI interleaves, which a looser pattern would turn into models.
func TestParseDeepSeekModels_BannerLinesRejected(t *testing.T) {
	for _, line := range []string{
		`source=provider_models	status={"state":"unknown"}	fetched_at=unknown	observed_at=unknown`,
		"内置或已配置的模型；尚未验证可用性。",
		"已缓存的目录；运行 `codewhale models --update` 可更新已配置的提供商。",
		"Cached catalog; run `codewhale models --update` to refresh configured providers.",
	} {
		assert.Empty(t, parseDeepSeekModels(line), "must not parse a model from %q", line)
	}
}

// TestParseDeepSeekModels_ScopedToOtherProviderIsRejected guards the fallback
// path: when --provider deepseek is unavailable (older CLI), the unscoped
// `codewhale models` prints whichever provider is ACTIVE. Current-format rows
// are untagged, so only the header can reveal the mismatch — parsing them as
// deepseek/* would silently attach another provider's models to this backend.
func TestParseDeepSeekModels_ScopedToOtherProviderIsRejected(t *testing.T) {
	for _, output := range []string{
		`openai 的模型（默认：gpt-5.6）
  gpt-5.5
* gpt-5.6
`,
		`openai models (default: gpt-5.6)
  gpt-5.5
* gpt-5.6
`,
	} {
		assert.Empty(t, parseDeepSeekModels(output),
			"a listing scoped to another provider must yield no deepseek models")
	}
}

// TestParseDeepSeekModels_ScopedToDeepseekKeepsRows is the positive counterpart:
// the header names deepseek, so the untagged rows are ours.
func TestParseDeepSeekModels_ScopedToDeepseekKeepsRows(t *testing.T) {
	output := `deepseek 的模型（默认：deepseek-v4-pro）
  deepseek-v4-flash
* deepseek-v4-pro
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].ID)
	assert.True(t, models[1].Default)
}

func TestDeepSeekSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "deepseek", Backend: "deepseek", DefaultCmd: "codewhale"}
	assert.True(t, model.CanDiscoverModels(spec), "deepseek should support model discovery")

	src, ok := model.LookupModelSource("deepseek")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindCLI, src.Kind())
}
