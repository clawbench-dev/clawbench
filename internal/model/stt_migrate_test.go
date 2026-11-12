package model

import "testing"

// TestMigrateLegacySTT covers the one-way move from the flat `stt.*` layout to
// the per-engine sections. The whole point is that no configuration is lost and
// that each engine only receives what it actually owned.
func TestMigrateLegacySTT(t *testing.T) {
	raw := map[string]any{
		"stt": map[string]any{
			"provider":          "doubao",
			"base_url":          "http://whisper.local/v1",
			"api_key":           "k-active",
			"model":             "large-v3",
			"resource_id":       "volc.bigasr.auc",
			"language":          "en",
			"alibaba_base_url":  "https://dashscope-intl.aliyuncs.com",
			"alibaba_model":     "paraformer-realtime-8k-v2",
			"enable_itn":        false,
			"enable_punc":       true,
			"enable_ddc":        true,
			"output_zh_variant": "tw",
			"end_window_size":   float64(1200),
			"streaming":         true,
			"chunk_ms":          float64(600),
			"final_recheck":     true,
		},
	}
	cfg := Config{}
	presence := map[string]bool{}
	MigrateLegacySTT(raw, &cfg, presence)

	// vLLM kept its own four fields.
	if cfg.STT.VLLM.BaseURL != "http://whisper.local/v1" {
		t.Errorf("VLLM.BaseURL = %q", cfg.STT.VLLM.BaseURL)
	}
	if cfg.STT.VLLM.Model != "large-v3" {
		t.Errorf("VLLM.Model = %q", cfg.STT.VLLM.Model)
	}
	if cfg.STT.VLLM.ChunkMs != 600 {
		t.Errorf("VLLM.ChunkMs = %d, want 600", cfg.STT.VLLM.ChunkMs)
	}
	if !cfg.STT.VLLM.FinalRecheck {
		t.Error("VLLM.FinalRecheck = false, want true")
	}

	// Doubao kept the resource id and the zh variant.
	if cfg.STT.Doubao.ResourceID != "volc.bigasr.auc" {
		t.Errorf("Doubao.ResourceID = %q", cfg.STT.Doubao.ResourceID)
	}
	if cfg.STT.Doubao.OutputZHVariant != "tw" {
		t.Errorf("Doubao.OutputZHVariant = %q", cfg.STT.Doubao.OutputZHVariant)
	}

	// Alibaba kept its own endpoint and model.
	if cfg.STT.Alibaba.BaseURL != "https://dashscope-intl.aliyuncs.com" {
		t.Errorf("Alibaba.BaseURL = %q", cfg.STT.Alibaba.BaseURL)
	}
	if cfg.STT.Alibaba.Model != "paraformer-realtime-8k-v2" {
		t.Errorf("Alibaba.Model = %q", cfg.STT.Alibaba.Model)
	}

	// The ambiguous key went to the engine that was selected, not to all three.
	if cfg.STT.Doubao.APIKey != "k-active" {
		t.Errorf("Doubao.APIKey = %q, want the active engine to inherit it", cfg.STT.Doubao.APIKey)
	}
	if cfg.STT.VLLM.APIKey != "" || cfg.STT.Alibaba.APIKey != "" {
		t.Errorf("inactive engines gained a key: vllm=%q alibaba=%q", cfg.STT.VLLM.APIKey, cfg.STT.Alibaba.APIKey)
	}
	if !cfg.STT.Doubao.Streaming {
		t.Error("Doubao.Streaming = false, want the active engine to inherit it")
	}
	if cfg.STT.Alibaba.Streaming {
		t.Error("inactive engines gained streaming")
	}

	// Language reached the two engines that read it, and not Doubao (which
	// auto-detects).
	if cfg.STT.VLLM.Language != "en" {
		t.Errorf("VLLM.Language = %q, want en", cfg.STT.VLLM.Language)
	}
	if cfg.STT.Alibaba.Language != "en" {
		t.Errorf("Alibaba.Language = %q, want en", cfg.STT.Alibaba.Language)
	}

	// Recognition options reached both streaming engines, including the
	// explicit false.
	if cfg.STT.Doubao.EnableITN || cfg.STT.Alibaba.EnableITN {
		t.Error("explicit enable_itn=false did not survive")
	}
	if !cfg.STT.Doubao.EnablePunc || !cfg.STT.Alibaba.EnablePunc {
		t.Error("enable_punc=true did not survive")
	}
	if !cfg.STT.Doubao.EnableDDC || !cfg.STT.Alibaba.EnableDDC {
		t.Error("enable_ddc=true did not survive")
	}
	if cfg.STT.Doubao.EndWindowSize != 1200 || cfg.STT.Alibaba.EndWindowSize != 1200 {
		t.Errorf("end_window_size = %d/%d, want 1200",
			cfg.STT.Doubao.EndWindowSize, cfg.STT.Alibaba.EndWindowSize)
	}

	// The presence map must record the per-engine paths, otherwise ApplyDefaults
	// would switch the ITN default back on and lose the explicit false.
	for _, key := range []string{
		"stt.doubao.enable_itn", "stt.alibaba.enable_itn",
		"stt.doubao.enable_punc", "stt.alibaba.enable_punc",
		"stt.vllm.base_url", "stt.doubao.resource_id", "stt.alibaba.model",
	} {
		if !presence[key] {
			t.Errorf("presence[%q] = false, want true", key)
		}
	}
}

// TestMigrateLegacySTTExplicitFalseSurvivesDefaults is the end-to-end guard: a
// migrated explicit false must still be false after defaults run.
func TestMigrateLegacySTTExplicitFalseSurvivesDefaults(t *testing.T) {
	setupTestBinDir(t)

	raw := map[string]any{
		"stt": map[string]any{
			"provider":    "doubao",
			"enable_itn":  false,
			"enable_punc": false,
		},
	}
	cfg := Config{}
	presence := ParsePresenceMap(raw)
	MigrateLegacySTT(raw, &cfg, presence)
	ApplyDefaults(&cfg, presence)

	if cfg.STT.Doubao.EnableITN || cfg.STT.Doubao.EnablePunc {
		t.Errorf("Doubao ITN/punc = %v/%v, want both false after defaults",
			cfg.STT.Doubao.EnableITN, cfg.STT.Doubao.EnablePunc)
	}
	if cfg.STT.Alibaba.EnableITN || cfg.STT.Alibaba.EnablePunc {
		t.Errorf("Alibaba ITN/punc = %v/%v, want both false after defaults",
			cfg.STT.Alibaba.EnableITN, cfg.STT.Alibaba.EnablePunc)
	}
}

// TestMigrateLegacySTTNoLegacyKeysIsNoOp guards the common case: a config that
// already uses the per-engine layout must not be touched.
func TestMigrateLegacySTTNoLegacyKeysIsNoOp(t *testing.T) {
	raw := map[string]any{
		"stt": map[string]any{
			"provider": "alibaba",
			"alibaba":  map[string]any{"api_key": "sk-dash"},
		},
	}
	cfg := Config{}
	cfg.STT.VLLM.APIKey = "keep-me"
	presence := map[string]bool{}
	MigrateLegacySTT(raw, &cfg, presence)

	if cfg.STT.VLLM.APIKey != "keep-me" {
		t.Errorf("VLLM.APIKey = %q, want keep-me", cfg.STT.VLLM.APIKey)
	}
	if len(presence) != 0 {
		t.Errorf("presence = %v, want empty", presence)
	}
}

// TestMigrateLegacySTTWithoutSection covers a config with no stt section at all.
func TestMigrateLegacySTTWithoutSection(t *testing.T) {
	cfg := Config{}
	presence := map[string]bool{}
	MigrateLegacySTT(map[string]any{"port": float64(8080)}, &cfg, presence)
	if len(presence) != 0 {
		t.Errorf("presence = %v, want empty", presence)
	}
}

// TestDropLegacySTTKeys covers the write-path cleanup: the old keys must not
// survive in config.yaml, where a hand-edit of one would look effective.
func TestDropLegacySTTKeys(t *testing.T) {
	raw := map[string]any{
		"stt": map[string]any{
			"provider":          "vllm",
			"api_key":           "old",
			"base_url":          "http://old/v1",
			"streaming":         true,
			"output_zh_variant": "tw",
			"shortcut_key":      "F9",
			"vllm":              map[string]any{"api_key": "new"},
		},
	}
	DropLegacySTTKeys(raw)

	section := raw["stt"].(map[string]any)
	for _, key := range legacySTTKeys {
		if _, ok := section[key]; ok {
			t.Errorf("legacy key %q survived", key)
		}
	}
	if section["provider"] != "vllm" {
		t.Errorf("provider = %v, want kept", section["provider"])
	}
	if section["shortcut_key"] != "F9" {
		t.Errorf("shortcut_key = %v, want kept", section["shortcut_key"])
	}
	if _, ok := section["vllm"]; !ok {
		t.Error("the per-engine section was dropped")
	}
}

// TestMigrateLegacySTT_StreamingForVLLMIsDropped pins the removal of the vLLM
// streaming switch: a legacy flat `streaming` aimed at that engine is dropped
// rather than stored on an engine whose dictation path never reads it.
func TestMigrateLegacySTT_StreamingForVLLMIsDropped(t *testing.T) {
	raw := map[string]any{
		"stt": map[string]any{
			"provider":  "vllm",
			"streaming": true,
		},
	}
	cfg := Config{}
	presence := map[string]bool{}
	MigrateLegacySTT(raw, &cfg, presence)

	if cfg.STT.ActiveStreaming() {
		t.Error("ActiveStreaming = true, want false (the vLLM engine has no switch)")
	}
	for _, key := range []string{"stt.vllm.streaming", "stt.doubao.streaming", "stt.alibaba.streaming"} {
		if presence[key] {
			t.Errorf("presence[%q] = true, want the legacy value dropped", key)
		}
	}
}

// TestActiveAccessors covers the shared helpers that read "the selected
// engine's" language and streaming flag.
func TestActiveAccessors(t *testing.T) {
	cfg := STTConfig{
		Provider: "doubao",
		VLLM:     STTEngineConfig{Language: "en"},
		Doubao:   STTDoubaoConfig{Streaming: false},
		Alibaba:  STTAlibabaConfig{Language: "ja", Streaming: true},
	}
	if got := cfg.ActiveLanguage(); got != "" {
		t.Errorf("doubao ActiveLanguage = %q, want empty (auto-detect)", got)
	}
	if cfg.ActiveStreaming() {
		t.Error("doubao ActiveStreaming = true, want false")
	}

	cfg.Provider = "alibaba"
	if got := cfg.ActiveLanguage(); got != "ja" {
		t.Errorf("alibaba ActiveLanguage = %q, want ja", got)
	}
	if !cfg.ActiveStreaming() {
		t.Error("alibaba ActiveStreaming = false, want true")
	}

	cfg.Tencent = STTTencentConfig{Streaming: true}
	cfg.Provider = "tencent"
	if got := cfg.ActiveLanguage(); got != "" {
		t.Errorf("tencent ActiveLanguage = %q, want empty (the model name carries the language)", got)
	}
	if !cfg.ActiveStreaming() {
		t.Error("tencent ActiveStreaming = false, want true")
	}

	cfg.Provider = "vllm"
	if got := cfg.ActiveLanguage(); got != "en" {
		t.Errorf("vllm ActiveLanguage = %q, want en", got)
	}
	// The OpenAI-compatible engine cannot decode live PCM, so it has no
	// streaming switch and never reports streaming.
	if cfg.ActiveStreaming() {
		t.Error("vllm ActiveStreaming = true, want false")
	}

	// An unset provider behaves like the default engine.
	cfg.Provider = ""
	if got := cfg.ActiveLanguage(); got != "en" {
		t.Errorf("default ActiveLanguage = %q, want en", got)
	}
}
