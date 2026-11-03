package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// providerBoundConfig returns a config shaped exactly like the one the settings
// page writes: a provider reference plus a model, no endpoint block.
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
// ai_summary endpoint block is empty.
//
// Regression guard: the registry is loaded AFTER InitDB, so any summarizer
// built during the early part of startup resolved every provider reference to
// "not configured" — with summarize.tts_backend "api" the process exited during
// startup, with "auto" voice summaries silently degraded to the simple cleaner
// while recommendation / auto-title / /btw / live notes (which read
// SummaryConfigured) kept working.
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
// documents this) rather than silently resurrecting a stale endpoint.
func TestBuildAISummarizer_MissingProviderRowIsUnconfigured(t *testing.T) {
	model.ReplaceProviders(nil)
	missing := int64(999)
	cfg := model.Config{}
	cfg.AISummary.ProviderID = &missing
	cfg.AISummary.Model = "m"

	assert.Nil(t, buildAISummarizer(cfg))
}

// An ai_summary.api endpoint block (configs written before the provider
// binding existed, or hand-edited files) is a supported fallback while no
// provider is bound — no migration rewrites it into a providers row.
func TestBuildAISummarizer_UsesEndpointFallbackWithoutProvider(t *testing.T) {
	model.ReplaceProviders(nil)
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	cfg := model.Config{}
	cfg.AISummary.API = model.APIConfig{BaseURL: "https://legacy.example/v1", Key: "sk-legacy"}
	cfg.AISummary.Model = "legacy-model"
	cfg.AISummary.Format = "openai"

	s := buildAISummarizer(cfg)
	require.NotNil(t, s, "an explicit endpoint block is a configured summary model")
	assert.IsType(t, &summarize.OpenAISummarizer{}, s)
}
