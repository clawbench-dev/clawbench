package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// TestNewOpenAITTSProvider_CopiesEveryConfigField pins the construction
// contract of the ONLY production provider factory.
//
// It exists because `tts.openai.stream_mode` shipped without being copied here:
// startup and hot reload both call this function, so the setting was accepted
// by the config loader, echoed by the settings API, and then silently ignored
// for every request. A field-by-field assertion fails on the omission, whereas
// a "provider is non-nil" test passes.
func TestNewOpenAITTSProvider_CopiesEveryConfigField(t *testing.T) {
	cfg := model.Config{}
	cfg.TTS.Speed = 1.25
	cfg.TTS.OpenAI = model.OpenAITTSConfig{
		BaseURL:    "https://tts.example/v1",
		APIKey:     "sk-test",
		Model:      "gpt-4o-mini-tts",
		Voice:      "alloy",
		StreamMode: "sse",
	}

	p := newOpenAITTSProvider(cfg)

	assert.Equal(t, cfg.TTS.OpenAI.BaseURL, p.BaseURL)
	assert.Equal(t, cfg.TTS.OpenAI.APIKey, p.APIKey)
	assert.Equal(t, cfg.TTS.OpenAI.Model, p.Model)
	assert.Equal(t, cfg.TTS.OpenAI.Voice, p.Voice)
	assert.Equal(t, cfg.TTS.Speed, p.Speed)
	assert.Equal(t, cfg.TTS.OpenAI.StreamMode, p.StreamMode,
		"stream_mode must reach the provider; it is the only escape hatch for servers whose model name hides the stream format")
}

// TestNewOpenAITTSProvider_SpeedZeroStaysUnset pins the existing behavior the
// fix must not disturb: a zero speed means "omit the parameter", not "send 0".
func TestNewOpenAITTSProvider_SpeedZeroStaysUnset(t *testing.T) {
	cfg := model.Config{}
	cfg.TTS.Speed = 0

	p := newOpenAITTSProvider(cfg)

	assert.Zero(t, p.Speed)
}

// TestNewOpenAITTSProvider_StreamModeAffectsCacheIdentity proves the copied
// value is actually observable to the rest of the system: the on-disk audio
// cache keys on it, so two modes cannot serve each other's files.
func TestNewOpenAITTSProvider_StreamModeAffectsCacheIdentity(t *testing.T) {
	base := model.Config{}
	base.TTS.OpenAI.BaseURL = "https://tts.example/v1"
	base.TTS.OpenAI.Model = "m"
	base.TTS.OpenAI.Voice = "v"

	auto := base
	auto.TTS.OpenAI.StreamMode = "auto"
	sse := base
	sse.TTS.OpenAI.StreamMode = "sse"

	assert.NotEqual(t,
		newOpenAITTSProvider(auto).CacheIdentity(),
		newOpenAITTSProvider(sse).CacheIdentity(),
		"stream mode changes the request shape, so it must not share cache entries")
}
