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

	t.Run("piper", func(t *testing.T) {
		a := &PiperProvider{ModelPath: "/m/a.onnx", NoiseScale: 0.667, LengthScale: 1.0, SentenceSilence: 0.2}
		b := &PiperProvider{ModelPath: "/m/b.onnx", NoiseScale: 0.667, LengthScale: 1.0, SentenceSilence: 0.2}
		c := &PiperProvider{ModelPath: "/m/a.onnx", NoiseScale: 0.5, LengthScale: 1.0, SentenceSilence: 0.2}
		d := &PiperProvider{ModelPath: "/m/a.onnx", NoiseScale: 0.667, LengthScale: 2.0, SentenceSilence: 0.2}
		e := &PiperProvider{ModelPath: "/m/a.onnx", NoiseScale: 0.667, LengthScale: 1.0, SentenceSilence: 0.5}

		if a.CacheIdentity() == b.CacheIdentity() {
			t.Error("model path change must change the identity")
		}
		if a.CacheIdentity() == c.CacheIdentity() {
			t.Error("noise_scale change must change the identity")
		}
		if a.CacheIdentity() == d.CacheIdentity() {
			t.Error("length_scale change must change the identity")
		}
		if a.CacheIdentity() == e.CacheIdentity() {
			t.Error("sentence_silence change must change the identity")
		}
		if !strings.HasPrefix(a.CacheIdentity(), "piper|") {
			t.Errorf("identity %q should be namespaced by engine", a.CacheIdentity())
		}
	})

	t.Run("kokoro", func(t *testing.T) {
		a := &KokoroProvider{ModelPath: "/m/k.onnx", Voice: "zf_001", Lang: "cmn", Speed: 1.0}
		b := &KokoroProvider{ModelPath: "/m/k.onnx", Voice: "zm_010", Lang: "cmn", Speed: 1.0}
		c := &KokoroProvider{ModelPath: "/m/k.onnx", Voice: "zf_001", Lang: "en-us", Speed: 1.0}
		d := &KokoroProvider{ModelPath: "/m/k.onnx", Voice: "zf_001", Lang: "cmn", Speed: 1.5}
		e := &KokoroProvider{ModelPath: "/m/other.onnx", Voice: "zf_001", Lang: "cmn", Speed: 1.0}

		if a.CacheIdentity() == b.CacheIdentity() {
			t.Error("voice change must change the identity")
		}
		if a.CacheIdentity() == c.CacheIdentity() {
			t.Error("lang change must change the identity")
		}
		if a.CacheIdentity() == d.CacheIdentity() {
			t.Error("speed change must change the identity")
		}
		if a.CacheIdentity() == e.CacheIdentity() {
			t.Error("model path change must change the identity")
		}
		if !strings.HasPrefix(a.CacheIdentity(), "kokoro|") {
			t.Errorf("identity %q should be namespaced by engine", a.CacheIdentity())
		}
	})

	t.Run("moss-nano", func(t *testing.T) {
		a := &MossNanoProvider{ModelDir: "/m", Backend: "onnx", Voice: "Junhao"}
		b := &MossNanoProvider{ModelDir: "/m", Backend: "pytorch", Voice: "Junhao"}
		c := &MossNanoProvider{ModelDir: "/m", Backend: "onnx", Voice: "Zhiming"}
		d := &MossNanoProvider{ModelDir: "/other", Backend: "onnx", Voice: "Junhao"}

		if a.CacheIdentity() == b.CacheIdentity() {
			t.Error("backend change must change the identity")
		}
		if a.CacheIdentity() == c.CacheIdentity() {
			t.Error("voice change must change the identity")
		}
		if a.CacheIdentity() == d.CacheIdentity() {
			t.Error("model dir change must change the identity")
		}
		if !strings.HasPrefix(a.CacheIdentity(), "moss-nano|") {
			t.Errorf("identity %q should be namespaced by engine", a.CacheIdentity())
		}
	})
}

// Different engines must never collide, even with identical field values.
func TestCacheIdentity_EnginesDoNotCollide(t *testing.T) {
	identities := []string{
		(&EdgeTTSProvider{Voice: "v", Rate: "r"}).CacheIdentity(),
		(&PiperProvider{ModelPath: "m", NoiseScale: 1, LengthScale: 1, SentenceSilence: 1}).CacheIdentity(),
		(&KokoroProvider{ModelPath: "m", Voice: "v", Lang: "l", Speed: 1}).CacheIdentity(),
		(&MossNanoProvider{ModelDir: "m", Backend: "b", Voice: "v"}).CacheIdentity(),
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
		"edge":      NewEdgeTTSProvider(),
		"piper":     NewPiperProvider(),
		"kokoro":    NewKokoroProvider(),
		"moss-nano": NewMossNanoProvider(),
		"openai":    NewOpenAITTSProvider(),
	}
	for name, p := range providers {
		if _, ok := p.(CacheIdentityProvider); !ok {
			t.Errorf("%s provider must implement CacheIdentityProvider", name)
		}
	}
}
