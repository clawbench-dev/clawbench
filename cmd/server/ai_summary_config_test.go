package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// providerBoundConfig returns a config shaped exactly like the one the
// ai_summary migration leaves on disk: a provider reference plus a model, and
// no legacy ai_summary.api endpoint.
func providerBoundConfig(t *testing.T, baseURL string) model.Config {
	t.Helper()
	pid := int64(7)
	model.ReplaceProviders([]*model.Provider{{
		ID:       pid,
		Name:     "gw",
		Protocol: model.ProviderProtocolAnthropic,
		BaseURL:  baseURL,
		APIKey:   "sk-test",
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	cfg := model.Config{}
	cfg.AISummary.ProviderID = &pid
	cfg.AISummary.Model = "MiniMax-M2.7"
	return cfg
}

// A provider-bound summary model must build an LLM summarizer even though the
// legacy ai_summary.api block is empty.
//
// Regression guard for the provider migration: MigrateAISummaryProvider
// rewrites config.yaml to {provider_id, model} and clears ai_summary.api, but
// buildAISummarizer kept gating on ai_summary.api.base_url. On the NEXT start
// that turned the shared summarizer into nil — with summarize.tts_backend
// "api" the process exited during startup, with "auto" voice summaries
// silently degraded to the simple cleaner while recommendation / auto-title /
// /btw / live notes (which read SummaryConfigured) kept working.
func TestBuildAISummarizer_UsesProviderBinding(t *testing.T) {
	cfg := providerBoundConfig(t, "https://gw.example/anthropic")

	s := buildAISummarizer(cfg)
	require.NotNil(t, s, "a bound provider is a configured summary model")

	as, ok := s.(*summarize.AnthropicSummarizer)
	require.True(t, ok, "an anthropic provider must build the anthropic summarizer")
	assert.Equal(t, "https://gw.example/anthropic/v1/messages", as.BaseURL,
		"the provider's base URL must be completed to the messages endpoint")
	assert.Equal(t, "MiniMax-M2.7", as.Model)
}

// Nothing configured at all still means "no LLM": the simple TTS cleaner is the
// documented degradation, so buildAISummarizer must not invent a backend.
func TestBuildAISummarizer_UnconfiguredReturnsNil(t *testing.T) {
	model.ReplaceProviders(nil)

	assert.Nil(t, buildAISummarizer(model.Config{}))
}

// A deleted provider row must read as "not configured" (model.AISummaryConfig
// documents this) rather than silently resurrecting the pre-provider endpoint.
func TestBuildAISummarizer_MissingProviderRowIsUnconfigured(t *testing.T) {
	model.ReplaceProviders(nil)
	missing := int64(999)
	cfg := model.Config{}
	cfg.AISummary.ProviderID = &missing
	cfg.AISummary.Model = "m"

	assert.Nil(t, buildAISummarizer(cfg))
}

// The legacy summarize.tts_api block is a migration safety net for configs that
// have not been rewritten yet. Once a provider is bound it must not be applied:
// re-filling ai_summary.api from it made the startup log report the old
// endpoint as the active one and kept a stale endpoint alive behind the
// provider binding.
func TestApplyLegacySummaryFallback_SkipsWhenProviderBound(t *testing.T) {
	cfg := providerBoundConfig(t, "https://gw.example/anthropic")

	applyLegacySummaryFallback(&cfg, "https://legacy.example/v1", "sk-legacy", "legacy-model", "openai")

	assert.Empty(t, cfg.AISummary.API.BaseURL)
	assert.Empty(t, cfg.AISummary.API.Key)
	assert.Empty(t, cfg.AISummary.Format)
	assert.Equal(t, "MiniMax-M2.7", cfg.AISummary.Model)
}

// Before the migration runs (no provider yet) the fallback is what keeps an
// upgraded install working for the first start.
func TestApplyLegacySummaryFallback_AppliesWithoutProvider(t *testing.T) {
	model.ReplaceProviders(nil)

	cfg := model.Config{}
	applyLegacySummaryFallback(&cfg, "https://legacy.example/v1", "sk-legacy", "legacy-model", "openai")

	assert.Equal(t, "https://legacy.example/v1", cfg.AISummary.API.BaseURL)
	assert.Equal(t, "sk-legacy", cfg.AISummary.API.Key)
	assert.Equal(t, "openai", cfg.AISummary.Format)
	assert.Equal(t, "legacy-model", cfg.AISummary.Model)
	assert.True(t, cfg.AISummary.SummaryConfigured())
}

// An explicit ai_summary.api block wins over the older summarize.tts_api one.
func TestApplyLegacySummaryFallback_DoesNotOverwriteExplicitConfig(t *testing.T) {
	model.ReplaceProviders(nil)

	cfg := model.Config{}
	cfg.AISummary.API = model.APIConfig{BaseURL: "https://explicit.example/v1", Key: "sk-explicit"}
	cfg.AISummary.Model = "explicit-model"
	cfg.AISummary.Format = "anthropic"

	applyLegacySummaryFallback(&cfg, "https://legacy.example/v1", "sk-legacy", "legacy-model", "openai")

	assert.Equal(t, "https://explicit.example/v1", cfg.AISummary.API.BaseURL)
	assert.Equal(t, "sk-explicit", cfg.AISummary.API.Key)
	assert.Equal(t, "explicit-model", cfg.AISummary.Model)
	assert.Equal(t, "anthropic", cfg.AISummary.Format)
}
