package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// A provider-bound summary model must produce the LLM summarizer. This is the
// post-upgrade shape of config.yaml ({provider_id, model}, no legacy endpoint),
// so a nil here is what used to abort startup.
func TestNewTTSSummarizer_APIUsesBoundProvider(t *testing.T) {
	cfg := providerBoundConfig(t, "https://gw.example/anthropic")
	cfg.Summarize.TTSBackend = "api"

	assert.IsType(t, &summarize.AnthropicSummarizer{}, newTTSSummarizer(cfg))
}

// "api" with nothing bound must degrade to the simple cleaner instead of
// returning nil: the caller assigns this to the global summarizer, and the
// startup path used to exit the process on nil — a state the settings page
// itself can produce (switching to "api" before a model is bound).
func TestNewTTSSummarizer_APIWithoutModelDegradesToSimple(t *testing.T) {
	model.ReplaceProviders(nil)
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	cfg := model.Config{}
	cfg.Summarize.TTSBackend = "api"

	s := newTTSSummarizer(cfg)
	require.NotNil(t, s, "startup must never receive a nil summarizer")
	assert.IsType(t, &summarize.SimpleSummarizer{}, s)
}

// A deleted provider row reads as "not configured" (the contract
// AISummaryConfig documents), so it must degrade rather than resurrect a stale
// endpoint — and must not abort startup either.
func TestNewTTSSummarizer_MissingProviderRowDegradesToSimple(t *testing.T) {
	model.ReplaceProviders(nil)
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	missing := int64(999)
	cfg := model.Config{}
	cfg.AISummary.ProviderID = &missing
	cfg.AISummary.Model = "m"
	cfg.Summarize.TTSBackend = "api"

	assert.IsType(t, &summarize.SimpleSummarizer{}, newTTSSummarizer(cfg))
}

// An unknown value can only come from a hand-edited config (the PATCH endpoint
// validates the enum); it must fall back, not leave the global summarizer nil.
func TestNewTTSSummarizer_UnknownBackendDegradesToSimple(t *testing.T) {
	cfg := model.Config{}
	cfg.Summarize.TTSBackend = "not-a-backend"

	assert.IsType(t, &summarize.SimpleSummarizer{}, newTTSSummarizer(cfg))
}

// "auto" never needed an LLM to run: without one it routes everything to the
// simple cleaner. It must still be the routing wrapper, not a bare simple.
func TestNewTTSSummarizer_AutoWithoutModelStillRoutes(t *testing.T) {
	model.ReplaceProviders(nil)
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	cfg := model.Config{}
	cfg.Summarize.TTSBackend = "auto"

	assert.IsType(t, &summarize.AutoSummarizer{}, newTTSSummarizer(cfg))
}

// Startup must build the summarizer AFTER the provider registry is in memory.
//
// This is a source guard because the wiring lives inline in main(): building it
// right after InitDB (the old position) made a provider-bound config resolve to
// nothing, since GetProvider only sees rows that LoadProvidersIntoMemory has
// published. The symptom was an upgraded install that could not start at all
// with tts_backend "api" and silently lost its LLM summaries with "auto".
func TestStartupBuildsSummarizerAfterProvidersLoad(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err, "main.go must be readable from the package dir")
	text := string(src)

	providers := strings.Index(text, "service.LoadProvidersIntoMemory()")
	require.NotEqual(t, -1, providers, "startup must load model providers into memory")

	const setSummarizer = "handler.SetSummarizer(newTTSSummarizer(model.ConfigInstance))"
	summarizer := strings.Index(text, setSummarizer)
	require.NotEqual(t, -1, summarizer,
		"startup must build the TTS summarizer through the shared builder (newTTSSummarizer)")

	assert.Greater(t, summarizer, providers,
		"the TTS summarizer resolves ai_summary through the provider registry; building it before LoadProvidersIntoMemory makes every provider-bound config read as unconfigured")
}
