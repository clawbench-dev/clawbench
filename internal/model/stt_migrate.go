package model

// Per-engine STT configuration: shared accessors and the migration from the
// old flat layout.
//
// Credentials and engine-specific knobs used to be flat `stt.*` fields shared by
// every engine. That meant switching the engine overwrote the previous one's
// key, endpoint and options: point the recogniser at Doubao once and the
// self-hosted Whisper endpoint is gone when you switch back. They now live in
// `stt.vllm`, `stt.doubao` and `stt.alibaba`.

// ActiveLanguage is the language code the selected engine should be told.
//
// Doubao auto-detects and has no language setting, and Tencent encodes the
// language in the engine model name, so both return "".
func (c STTConfig) ActiveLanguage() string {
	switch c.Provider {
	case "doubao", "tencent":
		return ""
	case "alibaba":
		return c.Alibaba.Language
	default:
		return c.VLLM.Language
	}
}

// ActiveStreaming reports whether the selected engine uses live streaming.
//
// The vLLM/OpenAI-compatible engine has no switch: it cannot decode live PCM,
// so its dictation path always uploads one finished recording.
func (c STTConfig) ActiveStreaming() bool {
	switch c.Provider {
	case "doubao":
		return c.Doubao.Streaming
	case "alibaba":
		return c.Alibaba.Streaming
	case "tencent":
		return c.Tencent.Streaming
	default:
		return false
	}
}

// legacySTTKeys are the pre-per-engine flat `stt.*` keys.
var legacySTTKeys = []string{
	"base_url", "api_key", "model", "resource_id", "language",
	"alibaba_base_url", "alibaba_model",
	"enable_itn", "enable_punc", "enable_ddc", "output_zh_variant",
	"end_window_size", "streaming", "chunk_ms", "final_recheck",
}

// MigrateLegacySTT moves the pre-per-engine flat `stt.*` keys into the engine
// sections, in place on cfg.
//
// It reads the raw map rather than cfg because the typed struct no longer has
// the old fields — yaml.Unmarshal drops them silently. presence is updated with
// the new paths so ApplyDefaults keeps an explicit `false` (ITN and punctuation
// default to ON, and the presence map is the only way to tell "the user turned
// it off" from "the key was never written").
//
// The ambiguous keys go to the engine that was selected: a shared `api_key`
// belonged to whatever engine `provider` pointed at, so handing it to all three
// would invent credentials the user never configured for the other two.
func MigrateLegacySTT(raw map[string]any, cfg *Config, presence map[string]bool) {
	section, ok := raw["stt"].(map[string]any)
	if !ok {
		return
	}
	legacy := newLegacySTTValues(section)
	if !legacy.any() {
		return
	}

	migrateLegacyVLLM(legacy, &cfg.STT.VLLM, presence)
	migrateLegacyDoubao(legacy, &cfg.STT.Doubao, presence)
	migrateLegacyAlibaba(legacy, &cfg.STT.Alibaba, presence)
	migrateLegacyActiveEngine(legacy, cfg, section, presence)
	migrateLegacyRecognitionOptions(legacy, &cfg.STT, presence)
}

// migrateLegacyVLLM moves the fields that always belonged to the
// OpenAI-compatible engine.
func migrateLegacyVLLM(v legacySTTValues, cfg *STTEngineConfig, presence map[string]bool) {
	if v.hasBaseURL {
		cfg.BaseURL = v.baseURL
		markPresence(presence, "stt.vllm.base_url")
	}
	if v.hasModel {
		cfg.Model = v.model
		markPresence(presence, "stt.vllm.model")
	}
	if v.hasChunkMs {
		cfg.ChunkMs = v.chunkMs
		markPresence(presence, "stt.vllm.chunk_ms")
	}
	if v.hasFinalRecheck {
		cfg.FinalRecheck = v.finalRecheck
		markPresence(presence, "stt.vllm.final_recheck")
	}
}

// migrateLegacyDoubao moves the fields that always belonged to Doubao.
func migrateLegacyDoubao(v legacySTTValues, cfg *STTDoubaoConfig, presence map[string]bool) {
	if v.hasResourceID {
		cfg.ResourceID = v.resourceID
		markPresence(presence, "stt.doubao.resource_id")
	}
	if v.hasZhVariant {
		cfg.OutputZHVariant = v.zhVariant
		markPresence(presence, "stt.doubao.output_zh_variant")
	}
}

// migrateLegacyAlibaba moves the fields that always belonged to Alibaba Cloud.
func migrateLegacyAlibaba(v legacySTTValues, cfg *STTAlibabaConfig, presence map[string]bool) {
	if v.hasAlibabaBaseURL {
		cfg.BaseURL = v.alibabaBaseURL
		markPresence(presence, "stt.alibaba.base_url")
	}
	if v.hasAlibabaModel {
		cfg.Model = v.alibabaModel
		markPresence(presence, "stt.alibaba.model")
	}
}

// migrateLegacyActiveEngine hands the keys that could belong to any engine
// (api_key, streaming, language) to the one that was selected. cfg already
// carries the provider (yaml.Unmarshal still reads stt.provider), but the raw
// map is consulted as well so the function is self-contained for callers that
// only have the file contents.
func migrateLegacyActiveEngine(v legacySTTValues, cfg *Config, section map[string]any, presence map[string]bool) {
	active := cfg.STT.Provider
	if active == "" {
		if p, ok := section["provider"].(string); ok {
			active = p
		}
	}
	if active == "" {
		active = "vllm"
	}

	if v.hasAPIKey && v.apiKey != "" {
		switch active {
		case "doubao":
			cfg.STT.Doubao.APIKey = v.apiKey
			markPresence(presence, "stt.doubao.api_key")
		case "alibaba":
			cfg.STT.Alibaba.APIKey = v.apiKey
			markPresence(presence, "stt.alibaba.api_key")
		default:
			cfg.STT.VLLM.APIKey = v.apiKey
			markPresence(presence, "stt.vllm.api_key")
		}
	}
	if v.hasStreaming {
		switch active {
		case "doubao":
			cfg.STT.Doubao.Streaming = v.streaming
			markPresence(presence, "stt.doubao.streaming")
		case "alibaba":
			cfg.STT.Alibaba.Streaming = v.streaming
			markPresence(presence, "stt.alibaba.streaming")
		}
		// The vLLM engine has no streaming switch (it cannot decode live PCM),
		// so a legacy value aimed at it is dropped rather than stored.
	}
	// Language: vLLM used it per request and Alibaba as a hint; Doubao ignored
	// it. Copying it to both is faithful — neither engine gains a value it was
	// not already being given.
	if v.hasLanguage && v.language != "" {
		cfg.STT.VLLM.Language = v.language
		markPresence(presence, "stt.vllm.language")
		cfg.STT.Alibaba.Language = v.language
		markPresence(presence, "stt.alibaba.language")
	}
}

// migrateLegacyRecognitionOptions moves the four options both streaming engines
// expose, recording the per-engine presence so an explicit false survives.
func migrateLegacyRecognitionOptions(v legacySTTValues, cfg *STTConfig, presence map[string]bool) {
	if v.hasITN {
		cfg.Doubao.EnableITN = v.enableITN
		cfg.Alibaba.EnableITN = v.enableITN
		markPresence(presence, "stt.doubao.enable_itn")
		markPresence(presence, "stt.alibaba.enable_itn")
	}
	if v.hasPunc {
		cfg.Doubao.EnablePunc = v.enablePunc
		cfg.Alibaba.EnablePunc = v.enablePunc
		markPresence(presence, "stt.doubao.enable_punc")
		markPresence(presence, "stt.alibaba.enable_punc")
	}
	if v.hasDDC {
		cfg.Doubao.EnableDDC = v.enableDDC
		cfg.Alibaba.EnableDDC = v.enableDDC
		markPresence(presence, "stt.doubao.enable_ddc")
		markPresence(presence, "stt.alibaba.enable_ddc")
	}
	if v.hasEndWindow {
		cfg.Doubao.EndWindowSize = v.endWindow
		cfg.Alibaba.EndWindowSize = v.endWindow
		markPresence(presence, "stt.doubao.end_window_size")
		markPresence(presence, "stt.alibaba.end_window_size")
	}
}

// legacySTTValues is the old flat `stt` section, decoded by hand with a
// "was it present" flag per key. The flags matter: an explicit `false` or `0`
// must survive the move, and a missing key must not be invented.
type legacySTTValues struct {
	baseURL, model, apiKey, resourceID, language    string
	alibabaBaseURL, alibabaModel, zhVariant         string
	chunkMs, endWindow                              int
	streaming, finalRecheck, enableITN              bool
	enablePunc, enableDDC                           bool
	hasBaseURL, hasModel, hasAPIKey, hasResourceID  bool
	hasLanguage, hasAlibabaBaseURL, hasAlibabaModel bool
	hasZhVariant, hasChunkMs, hasEndWindow          bool
	hasStreaming, hasFinalRecheck                   bool
	hasITN, hasPunc, hasDDC                         bool
}

func newLegacySTTValues(section map[string]any) legacySTTValues {
	var v legacySTTValues
	str := func(key string) (string, bool) {
		s, ok := section[key].(string)
		return s, ok
	}
	num := func(key string) (int, bool) {
		n, ok := section[key].(float64)
		return int(n), ok
	}
	boolean := func(key string) (bool, bool) {
		b, ok := section[key].(bool)
		return b, ok
	}

	v.baseURL, v.hasBaseURL = str("base_url")
	v.model, v.hasModel = str("model")
	v.apiKey, v.hasAPIKey = str("api_key")
	v.resourceID, v.hasResourceID = str("resource_id")
	v.language, v.hasLanguage = str("language")
	v.alibabaBaseURL, v.hasAlibabaBaseURL = str("alibaba_base_url")
	v.alibabaModel, v.hasAlibabaModel = str("alibaba_model")
	v.zhVariant, v.hasZhVariant = str("output_zh_variant")
	v.chunkMs, v.hasChunkMs = num("chunk_ms")
	v.endWindow, v.hasEndWindow = num("end_window_size")
	v.streaming, v.hasStreaming = boolean("streaming")
	v.finalRecheck, v.hasFinalRecheck = boolean("final_recheck")
	v.enableITN, v.hasITN = boolean("enable_itn")
	v.enablePunc, v.hasPunc = boolean("enable_punc")
	v.enableDDC, v.hasDDC = boolean("enable_ddc")
	return v
}

func (v legacySTTValues) any() bool {
	return v.hasBaseURL || v.hasModel || v.hasAPIKey || v.hasResourceID ||
		v.hasLanguage || v.hasAlibabaBaseURL || v.hasAlibabaModel ||
		v.hasZhVariant || v.hasChunkMs || v.hasEndWindow ||
		v.hasStreaming || v.hasFinalRecheck || v.hasITN || v.hasPunc || v.hasDDC
}

// markPresence records a per-engine path as explicitly configured so
// ApplyDefaults leaves the migrated value alone (in particular an explicit
// false on a switch that defaults to ON).
func markPresence(presence map[string]bool, key string) {
	if presence == nil {
		return
	}
	presence[key] = true
}

// DropLegacySTTKeys removes the pre-per-engine flat keys from the generic
// config map, in place.
//
// The write path merges a patch into the existing file, so without this the old
// keys would survive forever and a hand-edit of one would look effective while
// being ignored — the same trap the legacy chat.user_nickname cleanup exists
// for.
func DropLegacySTTKeys(raw map[string]any) {
	section, ok := raw["stt"].(map[string]any)
	if !ok {
		return
	}
	for _, key := range legacySTTKeys {
		delete(section, key)
	}
}
