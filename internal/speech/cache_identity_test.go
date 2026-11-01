package speech

import (
	"strings"
	"testing"
)

// Each provider that affects the audio through configuration must report a
// cache identity, and that identity must distinguish configurations that
// produce different audio. Without these, changing a voice would keep serving
// audio cached under the previous setting.
func TestCacheIdentity_ChangesWithAudioAffectingConfig(t *testing.T) {
	t.Run("edge", func(t *testing.T) {
		a := &EdgeTTSProvider{Voice: "zh-CN-XiaoxiaoNeural", Rate: "+0%"}
		b := &EdgeTTSProvider{Voice: "zh-CN-YunxiNeural", Rate: "+0%"}
		c := &EdgeTTSProvider{Voice: "zh-CN-XiaoxiaoNeural", Rate: "+50%"}

		if a.CacheIdentity() == b.CacheIdentity() {
			t.Error("voice change must change the identity")
		}
		if a.CacheIdentity() == c.CacheIdentity() {
			t.Error("rate change must change the identity")
		}
		if !strings.HasPrefix(a.CacheIdentity(), "edge|") {
			t.Errorf("identity %q should be namespaced by engine", a.CacheIdentity())
		}
	})
}

// Different engines must never collide, even with identical field values.
func TestCacheIdentity_EnginesDoNotCollide(t *testing.T) {
	identities := []string{
		(&EdgeTTSProvider{Voice: "v", Rate: "r"}).CacheIdentity(),
		(&OpenAITTSProvider{BaseURL: "u", Model: "m", Voice: "v"}).CacheIdentity(),
	}

	seen := map[string]bool{}
	for _, id := range identities {
		if id == "" {
			t.Fatal("identity must not be empty")
		}
		if seen[id] {
			t.Fatalf("duplicate cache identity across engines: %q", id)
		}
		seen[id] = true
	}
}

// The compile-time assertions are the real guard; this fails loudly if an
// interface method is renamed or a provider is refactored off the interface.
func TestCacheIdentity_AllProvidersImplement(t *testing.T) {
	providers := map[string]SpeechProvider{
		"edge":   NewEdgeTTSProvider(),
		"openai": NewOpenAITTSProvider(),
	}
	for name, p := range providers {
		if _, ok := p.(CacheIdentityProvider); !ok {
			t.Errorf("%s provider must implement CacheIdentityProvider", name)
		}
	}
}
