package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/speech"
)

// newTTSProvider is the ONLY switch from a configured engine name to a built
// provider — main() calls it too, and so does the hot-reload path.
//
// It used to exist twice. The copy inside main() knew only "edge" and
// "openai"; minimax, alibaba, tencent and volcano fell into its default arm and
// were silently replaced by Edge. The settings panel writes the config and the
// hot-reload path (which used this function) honoured it, so switching worked
// until the next restart — then the server came back on Edge while config.yaml
// still said "minimax". Nothing in the logs disagreed: the startup line printed
// engine=edge, which looks exactly like "the config never got saved".
//
// These tests pin the mapping for every engine, so an engine that is added to
// the config UI but forgotten here fails loudly instead of degrading to Edge.
func TestNewTTSProviderMapsEveryEngine(t *testing.T) {
	cases := []struct {
		engine string
		want   string
	}{
		{"edge", "*speech.EdgeTTSProvider"},
		{"openai", "*speech.OpenAITTSProvider"},
		{"alibaba", "*speech.AlibabaTTSProvider"},
		{"tencent", "*speech.TencentTTSProvider"},
		{"volcano", "*speech.VolcanoTTSProvider"},
		{"minimax", "*speech.MinimaxTTSProvider"},
	}
	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			cfg := model.Config{}
			cfg.TTS.Engine = tc.engine
			p := newTTSProvider(cfg)
			require.NotNil(t, p, "engine %q produced no provider", tc.engine)
			assert.Equal(t, tc.want, typeName(p))
		})
	}
}

// An engine name nobody recognises still has to yield a working provider: the
// server must boot even with a hand-edited config, and Edge needs no
// credentials. Falling back is fine — doing it silently is what this file
// exists to prevent, hence the startup log now prints both the configured
// engine and the built provider type.
func TestNewTTSProviderFallsBackToEdge(t *testing.T) {
	for _, engine := range []string{"", "no-such-engine"} {
		cfg := model.Config{}
		cfg.TTS.Engine = engine
		p := newTTSProvider(cfg)
		require.NotNil(t, p)
		assert.Equal(t, "*speech.EdgeTTSProvider", typeName(p), "engine %q", engine)
	}
}

// TestNewTTSProviderPropagatesConfig guards against the providers being built
// without their configuration. A provider that is the right TYPE but carries no
// API key fails only at synthesis time, which is the "it looks configured but
// says it isn't" experience all over again.
func TestNewTTSProviderPropagatesConfig(t *testing.T) {
	cfg := model.Config{}
	cfg.TTS.Engine = "minimax"
	cfg.TTS.Voice = "zh-CN-XiaoxiaoNeural"
	cfg.TTS.Speed = 1.5
	cfg.TTS.Minimax.APIKey = "mm-key"
	cfg.TTS.Minimax.Model = "speech-02-hd"
	cfg.TTS.Minimax.Voice = "female-shaonv"

	p, ok := newTTSProvider(cfg).(*speech.MinimaxTTSProvider)
	require.True(t, ok, "minimax engine must build a MinimaxTTSProvider")
	assert.Equal(t, "mm-key", p.APIKey)
	assert.Equal(t, "speech-02-hd", p.Model)
	assert.Equal(t, "female-shaonv", p.Voice)
	assert.InDelta(t, 1.5, p.Speed, 1e-9)
}

// typeName renders a provider's concrete type the way the startup log does, so
// a failure here reads the same as the log line being asserted.
func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}
