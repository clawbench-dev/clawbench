//nolint:goconst,govet // JSON response field names are domain strings; shadowed ok/err is standard Go type-assertion idiom
package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/version"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// configMutex protects ConfigInstance from concurrent access.
// PATCH acquires a full lock; GET acquires a read lock to allow concurrent reads.
var configMutex sync.RWMutex

// hotReloadFields is the set of config dot-paths that take effect immediately
// via applyHotReloadGlobals() and do NOT require a server restart.
var hotReloadFields = map[string]bool{
	"chat.initial_messages":       true,
	"chat.page_size":              true,
	"chat.system_prompt_interval": true,
	"session.max_count":           true,
	"recent_projects.max_count":   true,
	"upload.max_size_mb":          true,
	"upload.max_files":            true,
	"tts.max_cache_files":         true,
	"tts.voice":                   true,
	"tts.speed":                   true,
	"default_agent":               true,
	"localhost_auth_exempt":       true,
	// Terminal — reconfigure Manager or toggle enabled
	"terminal.enabled":      true,
	"terminal.idle_timeout": true,
	"terminal.max_sessions": true,
	"terminal.buffer_lines": true,
	// TTS engine + sub-configs — recreate provider
	"tts.engine":                 true,
	"tts.tts_model":              true, // forward-compatible: MiniMax TTS model name (no-op until MiniMax provider is wired)
	"tts.format":                 true, // forward-compatible: audio format override (no-op, inferred from engine)
	"tts.piper.model_path":       true,
	"tts.piper.noise_scale":      true,
	"tts.piper.length_scale":     true,
	"tts.piper.sentence_silence": true,
	"tts.kokoro.model_path":      true,
	"tts.kokoro.voices_path":     true,
	"tts.kokoro.lang":            true,
	"tts.moss_nano.model_dir":    true,
	"tts.moss_nano.backend":      true,
	// Summarize — reconstruct summarizer
	"summarize.backend":      true,
	"summarize.model":        true,
	"summarize.api.base_url": true,
	"summarize.api.key":      true,
	"summarize.api.format":   true,
}

// restartGracePeriod is the delay before shutting down the server after a restart
// request, giving the HTTP response time to reach the client.
const restartGracePeriod = 200 * time.Millisecond

// restartFunc is the function called to trigger a server restart.
// Set by main.go via SetRestartFunc(). Defaults to a no-op for tests.
var restartFunc func()

// SetRestartFunc sets the function called to trigger a server restart.
// main.go calls this to wire up the actual shutdown+sentinel logic.
func SetRestartFunc(f func()) {
	restartFunc = f
}

// reconfigureOnHotReload is called by applyHotReloadGlobals() to apply
// hot-reload changes that require subsystem reconfiguration (TTS engine swap,
// summarize reconstruction, terminal reconfigure).
// Set by main.go via SetReconfigureFunc(). Defaults to a no-op for tests.
var reconfigureOnHotReload func()

// SetReconfigureFunc sets the function called after a hot-reload patch
// to reconfigure subsystems. main.go calls this to wire up the actual logic.
func SetReconfigureFunc(f func()) {
	reconfigureOnHotReload = f
}

// configResponse is the sanitized config returned to clients via GET /api/config.
// It only contains fields safe for frontend display — no passwords, keys, or
// internal paths.
type configResponse struct {
	Version             string               `json:"version"`
	HasPassword         bool                 `json:"has_password"`          // true when a password is configured
	LocalhostAuthExempt bool                 `json:"localhost_auth_exempt"` // true = localhost bypasses auth (default)
	DefaultAgent        string               `json:"default_agent"`
	Chat                configChat           `json:"chat"`
	Session             configSession        `json:"session"`
	RecentProjects      configRecentProjects `json:"recent_projects"`
	Upload              configUpload         `json:"upload"`
	Terminal            configTerminal       `json:"terminal"`
	TTS                 configTTS            `json:"tts"`
	RAG                 configRAG            `json:"rag"`
	PortForward         configPortForward    `json:"port_forward"`
	Summarize           configSummarize      `json:"summarize"`
}

type configChat struct {
	InitialMessages      int `json:"initial_messages"`
	PageSize             int `json:"page_size"`
	SystemPromptInterval int `json:"system_prompt_interval"`
}

type configSession struct {
	MaxCount int `json:"max_count"`
}

type configRecentProjects struct {
	MaxCount int `json:"max_count"`
}

type configUpload struct {
	MaxSizeMB int `json:"max_size_mb"`
	MaxFiles  int `json:"max_files"`
}

type configTerminal struct {
	Enabled     bool   `json:"enabled"`
	IdleTimeout string `json:"idle_timeout"`
	MaxSessions int    `json:"max_sessions"`
	BufferLines int    `json:"buffer_lines"`
}

type configTTS struct {
	Engine        string          `json:"engine"`
	TTSModel      string          `json:"tts_model"`
	Format        string          `json:"format"`
	Speed         float64         `json:"speed"`
	Voice         string          `json:"voice"`
	MaxCacheFiles int             `json:"max_cache_files"`
	Piper         *configPiper    `json:"piper,omitempty"`
	Kokoro        *configKokoro   `json:"kokoro,omitempty"`
	MossNano      *configMossNano `json:"moss_nano,omitempty"`
}

type configPiper struct {
	ModelPath       string  `json:"model_path"`
	NoiseScale      float64 `json:"noise_scale"`
	LengthScale     float64 `json:"length_scale"`
	SentenceSilence float64 `json:"sentence_silence"`
}

type configKokoro struct {
	ModelPath  string `json:"model_path"`
	VoicesPath string `json:"voices_path"`
	Lang       string `json:"lang"`
}

type configMossNano struct {
	ModelDir string `json:"model_dir"`
	Backend  string `json:"backend"`
}

type configAPI struct {
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
	Format  string `json:"format"`
}

type configRAG struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	ChunkSize      int    `json:"chunk_size"`
	SearchLimit    int    `json:"search_limit"`
	SearchPoolSize int    `json:"search_pool_size"`
	RetentionDays  int    `json:"retention_days"`
}

type configPortForward struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

type configSummarize struct {
	Backend string     `json:"backend"`
	Model   string     `json:"model"`
	API     *configAPI `json:"api,omitempty"`
}

// PatchableConfigPaths defines the whitelist of config paths that PATCH /api/config accepts.
// Any path not in this list will be rejected with 400 Bad Request.
var PatchableConfigPaths = map[string]bool{
	"default_agent":               true,
	"chat.initial_messages":       true,
	"chat.page_size":              true,
	"chat.system_prompt_interval": true,
	"session.max_count":           true,
	"recent_projects.max_count":   true,
	"upload.max_size_mb":          true,
	"upload.max_files":            true,
	"terminal.enabled":            true,
	"terminal.idle_timeout":       true,
	"terminal.max_sessions":       true,
	"terminal.buffer_lines":       true,
	"tts.engine":                  true,
	"tts.tts_model":               true,
	"tts.format":                  true,
	"tts.speed":                   true,
	"tts.voice":                   true,
	"tts.max_cache_files":         true,
	"tts.piper.model_path":        true,
	"tts.piper.noise_scale":       true,
	"tts.piper.length_scale":      true,
	"tts.piper.sentence_silence":  true,
	"tts.kokoro.model_path":       true,
	"tts.kokoro.voices_path":      true,
	"tts.kokoro.lang":             true,
	"tts.moss_nano.model_dir":     true,
	"tts.moss_nano.backend":       true,
	"rag.base_url":                true,
	"rag.model":                   true,
	"rag.api_key":                 true,
	"rag.chunk_size":              true,
	"rag.search_limit":            true,
	"rag.search_pool_size":        true,
	"rag.retention_days":          true,
	"port_forward.enabled":        true,
	"port_forward.port":           true,
	"summarize.backend":           true,
	"summarize.model":             true,
	"summarize.api.base_url":      true,
	"summarize.api.key":           true,
	"summarize.api.format":        true,
	"localhost_auth_exempt":       true,
}

// validTTSEngines is the set of valid TTS engine values.
var validTTSEngines = map[string]bool{
	"edge": true, "piper": true, "kokoro": true, "moss-nano": true,
}

// validSummarizeBackends is the set of valid summarization backend values.
var validSummarizeBackends = map[string]bool{
	"simple": true, "api": true,
	"claude": true, "codebuddy": true,
	"opencode": true, "codex": true, "qoder": true,
	"vecli": true, "deepseek": true, "pi": true,
	"mimo": true,
}

// validTTSFormats is the set of valid TTS output format values.
// Empty string is accepted separately (means "use default"), not as a format value.
var validTTSFormats = map[string]bool{
	"mp3": true, "wav": true, "pcm": true,
}

// validAPIFormats is the set of valid API format values.
var validAPIFormats = map[string]bool{
	"openai": true, "anthropic": true,
}

// validMossNanoBackends is the set of valid MOSS-Nano inference backend values.
var validMossNanoBackends = map[string]bool{
	"onnx": true, "pytorch": true,
}

// getBuildVersion returns a human-readable version string from build info.
// Delegates to version.Get() which handles ldflags injection and VCS fallback.
func getBuildVersion() string {
	return version.Get()
}

// maskAPIKey masks an API key for safe display: first 4 + *** + last 3 chars.
// Returns "****" if the key is too short (< 8 chars).
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 8 {
		return "****"
	}
	return key[:4] + "***" + key[len(key)-3:]
}

// ServeConfig handles GET and PATCH /api/config.
func ServeConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveConfigGet(w, r)
	case http.MethodPatch:
		serveConfigPatch(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveConfigGet(w http.ResponseWriter, _ *http.Request) {
	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	resp := configResponse{
		Version:             getBuildVersion(),
		HasPassword:         model.SessionToken != "",
		LocalhostAuthExempt: cfg.LocalhostAuthExempt,
		DefaultAgent:        cfg.DefaultAgent,
		Chat: configChat{
			InitialMessages:      cfg.Chat.InitialMessages,
			PageSize:             cfg.Chat.PageSize,
			SystemPromptInterval: cfg.Chat.SystemPromptInterval,
		},
		Session: configSession{
			MaxCount: cfg.Session.MaxCount,
		},
		RecentProjects: configRecentProjects{
			MaxCount: cfg.RecentProjects.MaxCount,
		},
		Upload: configUpload{
			MaxSizeMB: cfg.Upload.MaxSizeMB,
			MaxFiles:  cfg.Upload.MaxFiles,
		},
		Terminal: configTerminal{
			Enabled:     cfg.Terminal.Enabled,
			IdleTimeout: cfg.Terminal.IdleTimeout,
			MaxSessions: cfg.Terminal.MaxSessions,
			BufferLines: cfg.Terminal.BufferLines,
		},
		TTS: configTTS{
			Engine:        cfg.TTS.Engine,
			TTSModel:      cfg.TTS.TTSModel,
			Format:        cfg.TTS.Format,
			Speed:         cfg.TTS.Speed,
			Voice:         cfg.TTS.Voice,
			MaxCacheFiles: cfg.TTS.MaxCacheFiles,
		},
		RAG: configRAG{
			BaseURL:        cfg.RAG.BaseURL,
			Model:          cfg.RAG.Model,
			APIKey:         maskAPIKey(cfg.RAG.APIKey),
			ChunkSize:      cfg.RAG.ChunkSize,
			SearchLimit:    cfg.RAG.SearchLimit,
			SearchPoolSize: cfg.RAG.SearchPoolSize,
			RetentionDays:  cfg.RAG.RetentionDays,
		},
		PortForward: configPortForward{
			Enabled: cfg.PortForward.Enabled,
			Port:    cfg.PortForward.Port,
		},
		Summarize: configSummarize{
			Backend: cfg.Summarize.Backend,
			Model:   cfg.Summarize.Model,
		},
	}

	// Conditionally populate Summarize API sub-config when backend is "api"
	if cfg.Summarize.Backend == "api" {
		resp.Summarize.API = &configAPI{
			BaseURL: cfg.Summarize.API.BaseURL,
			Key:     maskAPIKey(cfg.Summarize.API.Key),
			Format:  cfg.Summarize.API.Format,
		}
	}

	// Conditionally populate engine-specific sub-configs
	switch cfg.TTS.Engine {
	case "piper":
		resp.TTS.Piper = &configPiper{
			ModelPath:       cfg.TTS.Piper.ModelPath,
			NoiseScale:      cfg.TTS.Piper.NoiseScale,
			LengthScale:     cfg.TTS.Piper.LengthScale,
			SentenceSilence: cfg.TTS.Piper.SentenceSilence,
		}
	case "kokoro":
		resp.TTS.Kokoro = &configKokoro{
			ModelPath:  cfg.TTS.Kokoro.ModelPath,
			VoicesPath: cfg.TTS.Kokoro.VoicesPath,
			Lang:       cfg.TTS.Kokoro.Lang,
		}
	case "moss-nano":
		resp.TTS.MossNano = &configMossNano{
			ModelDir: cfg.TTS.MossNano.ModelDir,
			Backend:  cfg.TTS.MossNano.Backend,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func serveConfigPatch(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_json",
			"message": "failed to parse request body as JSON",
		})
		return
	}

	changedFields, err := validatePatchFields(patch, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "forbidden_field",
			"message": err.Error(),
		})
		return
	}

	if err := validatePatchValues(patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_value",
			"message": err.Error(),
		})
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	// Snapshot current config for rollback on write failure
	snapshot := model.ConfigInstance

	if err := applyConfigPatch(patch); err != nil {
		model.ConfigInstance = snapshot
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "apply_failed",
			"message": err.Error(),
		})
		return
	}

	if err := writeConfigYAML(patch); err != nil {
		// Rollback memory state — disk write failed
		model.ConfigInstance = snapshot
		// Re-apply hot-reload globals from snapshot
		applyHotReloadGlobals()
		slog.Error("failed to write config.yaml after patch", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "write_failed",
			"message": fmt.Sprintf("failed to write config.yaml: %v", err),
		})
		return
	}

	// Sort changed fields for deterministic response
	sort.Strings(changedFields)

	// Classify fields into hot (no restart) and cold (needs restart)
	coldFields := make([]string, 0) // always non-nil so JSON serializes as [] not null
	for _, f := range changedFields {
		if !hotReloadFields[f] {
			coldFields = append(coldFields, f)
		}
	}
	sort.Strings(coldFields)

	writeJSON(w, http.StatusOK, map[string]any{
		"needs_restart":       len(coldFields) > 0,
		"changed_cold_fields": coldFields,
	})
}

func validatePatchFields(patch map[string]any, prefix string) ([]string, error) {
	var fields []string
	for key, value := range patch {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			nestedFields, err := validatePatchFields(nested, path)
			if err != nil {
				return nil, err
			}
			fields = append(fields, nestedFields...)
		} else {
			if !PatchableConfigPaths[path] {
				return nil, fmt.Errorf("field '%s' is not allowed", path)
			}
			fields = append(fields, path)
		}
	}
	return fields, nil
}

func validatePatchValues(patch map[string]any) error { //nolint:gocognit,gocyclo // exhaustive config validation
	tts, ok := patch["tts"].(map[string]any)
	if ok {
		if engine, ok := tts["engine"].(string); ok {
			if !validTTSEngines[engine] {
				return fmt.Errorf("tts.engine must be one of: edge,piper,kokoro,moss-nano")
			}
		}
		if format, ok := tts["format"].(string); ok {
			if format != "" && !validTTSFormats[format] {
				return fmt.Errorf("tts.format must be one of: mp3,wav,pcm")
			}
		}
		if speed, ok := tts["speed"].(float64); ok {
			if speed < 0.5 || speed > 3.0 {
				return fmt.Errorf("tts.speed must be between 0.5 and 3.0")
			}
		}
		// Validate engine-specific sub-configs
		if piper, ok := tts["piper"].(map[string]any); ok {
			if v, ok := piper["noise_scale"].(float64); ok && (v < 0 || v > 1) {
				return fmt.Errorf("tts.piper.noise_scale must be between 0 and 1")
			}
			if v, ok := piper["length_scale"].(float64); ok && v <= 0 {
				return fmt.Errorf("tts.piper.length_scale must be positive")
			}
			if v, ok := piper["sentence_silence"].(float64); ok && v < 0 {
				return fmt.Errorf("tts.piper.sentence_silence must be non-negative")
			}
		}
		if kokoro, ok := tts["kokoro"].(map[string]any); ok {
			if v, ok := kokoro["lang"].(string); ok && v == "" {
				return fmt.Errorf("tts.kokoro.lang must not be empty")
			}
		}
		if mossNano, ok := tts["moss_nano"].(map[string]any); ok {
			if v, ok := mossNano["backend"].(string); ok {
				if !validMossNanoBackends[v] {
					return fmt.Errorf("tts.moss_nano.backend must be one of: onnx,pytorch")
				}
			}
		}
	}

	// ── Cross-field consistency checks ──────────────────────────
	cfg := model.ConfigInstance

	// 1. When summarize.backend is "api", summarize.api.base_url must not be empty.
	//    Skip when the patch *switches* backend to "api" — the user hasn't had a
	//    chance to fill in the API sub-config yet (frontend auto-saves one field at a time).
	effectiveBackend := cfg.Summarize.Backend
	backendSwitchedToAPI := false
	if summarize, ok := patch["summarize"].(map[string]any); ok {
		if v, ok := summarize["backend"].(string); ok {
			if v == "api" && cfg.Summarize.Backend != "api" {
				backendSwitchedToAPI = true
			}
			effectiveBackend = v
		}
	}
	if effectiveBackend == "api" && !backendSwitchedToAPI {
		effectiveBaseURL := cfg.Summarize.API.BaseURL
		if summarize, ok := patch["summarize"].(map[string]any); ok {
			if api, ok := summarize["api"].(map[string]any); ok {
				if v, ok := api["base_url"].(string); ok {
					effectiveBaseURL = v
				}
			}
		}
		if effectiveBaseURL == "" {
			return fmt.Errorf("summarize.api.base_url is required when summarize.backend is \"api\"")
		}
	}

	// 2. Engine-specific model path requirements.
	// When the patch switches tts.engine to a new value, skip required-field
	// validation for the *target* engine — the user hasn't had a chance to
	// fill in the sub-config yet (frontend auto-saves one field at a time).
	// Only enforce when the engine is already set to that value (i.e. the
	// user is saving sub-config fields for the current engine).
	effectiveEngine := cfg.TTS.Engine
	engineSwitched := false
	if tts, ok := patch["tts"].(map[string]any); ok {
		if v, ok := tts["engine"].(string); ok && v != cfg.TTS.Engine {
			effectiveEngine = v
			engineSwitched = true
		}
	}
	switch effectiveEngine {
	case "piper":
		if engineSwitched {
			break // will be validated after user fills in sub-config
		}
		effectiveModelPath := cfg.TTS.Piper.ModelPath
		if tts, ok := patch["tts"].(map[string]any); ok {
			if piper, ok := tts["piper"].(map[string]any); ok {
				if v, ok := piper["model_path"].(string); ok {
					effectiveModelPath = v
				}
			}
		}
		if effectiveModelPath == "" {
			return fmt.Errorf("tts.piper.model_path is required when tts.engine is \"piper\"")
		}
	case "kokoro":
		if engineSwitched {
			break // will be validated after user fills in sub-config
		}
		effectiveKokoroModel := cfg.TTS.Kokoro.ModelPath
		effectiveVoicesPath := cfg.TTS.Kokoro.VoicesPath
		if tts, ok := patch["tts"].(map[string]any); ok {
			if kokoro, ok := tts["kokoro"].(map[string]any); ok {
				if v, ok := kokoro["model_path"].(string); ok {
					effectiveKokoroModel = v
				}
				if v, ok := kokoro["voices_path"].(string); ok {
					effectiveVoicesPath = v
				}
			}
		}
		if effectiveKokoroModel == "" {
			return fmt.Errorf("tts.kokoro.model_path is required when tts.engine is \"kokoro\"")
		}
		if effectiveVoicesPath == "" {
			return fmt.Errorf("tts.kokoro.voices_path is required when tts.engine is \"kokoro\"")
		}
	case "moss-nano":
		if engineSwitched {
			break // will be validated after user fills in sub-config
		}
		// model_dir is optional — empty value is valid (ResolveMossNanoModelDir
		// auto-detects the default dir or lets the CLI auto-download models).
	}

	// 3. default_agent must be an existing agent ID.
	if v, ok := patch["default_agent"].(string); ok && v != "" {
		if model.Agents != nil {
			if _, exists := model.Agents[v]; !exists {
				available := make([]string, 0, len(model.AgentList))
				for _, a := range model.AgentList {
					available = append(available, a.ID)
				}
				return fmt.Errorf("default_agent \"%s\" not found (available: %s)", v, strings.Join(available, ", "))
			}
		}
	}

	// Validate summarize section
	if summarize, ok := patch["summarize"].(map[string]any); ok {
		if v, ok := summarize["backend"].(string); ok && v != "" {
			if !validSummarizeBackends[v] {
				return fmt.Errorf("summarize.backend must be one of: simple,api,claude,codebuddy,opencode,codex,qoder,vecli,deepseek,pi,mimo")
			}
		}
		// Validate Summarize API sub-config
		if api, ok := summarize["api"].(map[string]any); ok {
			if v, ok := api["format"].(string); ok {
				if v != "" && !validAPIFormats[v] {
					return fmt.Errorf("summarize.api.format must be one of: openai, anthropic")
				}
			}
			if v, ok := api["key"].(string); ok && strings.Contains(v, "***") {
				return fmt.Errorf("summarize.api.key must not contain '***' — please provide the full key value")
			}
		}
	}

	chat, ok := patch["chat"].(map[string]any)
	if ok {
		for _, key := range []string{"initial_messages", "page_size", "system_prompt_interval"} {
			if v, ok := chat[key].(float64); ok && v < 0 {
				return fmt.Errorf("chat.%s must be non-negative", key)
			}
		}
	}
	session, ok := patch["session"].(map[string]any)
	if ok {
		if v, ok := session["max_count"].(float64); ok && v < 0 {
			return fmt.Errorf("session.max_count must be non-negative")
		}
	}
	recentProjects, ok := patch["recent_projects"].(map[string]any)
	if ok {
		if v, ok := recentProjects["max_count"].(float64); ok && v < 1 {
			return fmt.Errorf("recent_projects.max_count must be at least 1")
		}
	}
	upload, ok := patch["upload"].(map[string]any)
	if ok {
		if v, ok := upload["max_size_mb"].(float64); ok && v < 0 {
			return fmt.Errorf("upload.max_size_mb must be non-negative")
		}
		if v, ok := upload["max_files"].(float64); ok && v < 0 {
			return fmt.Errorf("upload.max_files must be non-negative")
		}
	}

	return nil
}

func applyConfigPatch(patch map[string]any) error { //nolint:gocognit,gocyclo // exhaustive config patch application
	cfg := &model.ConfigInstance

	// Top-level fields
	if v, ok := patch["default_agent"].(string); ok {
		cfg.DefaultAgent = v
		model.DefaultAgentID = v
	}

	if v, ok := patch["localhost_auth_exempt"].(bool); ok {
		cfg.LocalhostAuthExempt = v
	}

	if chat, ok := patch["chat"].(map[string]any); ok {
		if v, ok := chat["initial_messages"].(float64); ok {
			cfg.Chat.InitialMessages = int(v)
		}
		if v, ok := chat["page_size"].(float64); ok {
			cfg.Chat.PageSize = int(v)
		}
		if v, ok := chat["system_prompt_interval"].(float64); ok {
			cfg.Chat.SystemPromptInterval = int(v)
		}
	}

	if session, ok := patch["session"].(map[string]any); ok {
		if v, ok := session["max_count"].(float64); ok {
			cfg.Session.MaxCount = int(v)
		}
	}

	if rp, ok := patch["recent_projects"].(map[string]any); ok {
		if v, ok := rp["max_count"].(float64); ok {
			cfg.RecentProjects.MaxCount = int(v)
		}
	}

	if upload, ok := patch["upload"].(map[string]any); ok {
		if v, ok := upload["max_size_mb"].(float64); ok {
			cfg.Upload.MaxSizeMB = int(v)
		}
		if v, ok := upload["max_files"].(float64); ok {
			cfg.Upload.MaxFiles = int(v)
		}
	}

	if terminal, ok := patch["terminal"].(map[string]any); ok {
		if v, ok := terminal["enabled"].(bool); ok {
			cfg.Terminal.Enabled = v
		}
		if v, ok := terminal["idle_timeout"].(string); ok {
			cfg.Terminal.IdleTimeout = v
		}
		if v, ok := terminal["max_sessions"].(float64); ok {
			cfg.Terminal.MaxSessions = int(v)
		}
		if v, ok := terminal["buffer_lines"].(float64); ok {
			cfg.Terminal.BufferLines = int(v)
		}
	}

	if tts, ok := patch["tts"].(map[string]any); ok {
		if v, ok := tts["engine"].(string); ok {
			cfg.TTS.Engine = v
		}
		if v, ok := tts["tts_model"].(string); ok {
			cfg.TTS.TTSModel = v
		}
		if v, ok := tts["format"].(string); ok {
			cfg.TTS.Format = v
		}
		if v, ok := tts["speed"].(float64); ok {
			cfg.TTS.Speed = v
		}
		if v, ok := tts["voice"].(string); ok {
			cfg.TTS.Voice = v
		}
		if v, ok := tts["max_cache_files"].(float64); ok {
			cfg.TTS.MaxCacheFiles = int(v)
		}
		// Piper sub-config
		if piper, ok := tts["piper"].(map[string]any); ok {
			if v, ok := piper["model_path"].(string); ok {
				cfg.TTS.Piper.ModelPath = v
			}
			if v, ok := piper["noise_scale"].(float64); ok {
				cfg.TTS.Piper.NoiseScale = v
			}
			if v, ok := piper["length_scale"].(float64); ok {
				cfg.TTS.Piper.LengthScale = v
			}
			if v, ok := piper["sentence_silence"].(float64); ok {
				cfg.TTS.Piper.SentenceSilence = v
			}
		}
		// Kokoro sub-config
		if kokoro, ok := tts["kokoro"].(map[string]any); ok {
			if v, ok := kokoro["model_path"].(string); ok {
				cfg.TTS.Kokoro.ModelPath = v
			}
			if v, ok := kokoro["voices_path"].(string); ok {
				cfg.TTS.Kokoro.VoicesPath = v
			}
			if v, ok := kokoro["lang"].(string); ok {
				cfg.TTS.Kokoro.Lang = v
			}
		}
		// MossNano sub-config
		if mossNano, ok := tts["moss_nano"].(map[string]any); ok {
			if v, ok := mossNano["model_dir"].(string); ok {
				cfg.TTS.MossNano.ModelDir = v
			}
			if v, ok := mossNano["backend"].(string); ok {
				cfg.TTS.MossNano.Backend = v
			}
		}
	}

	if rag, ok := patch["rag"].(map[string]any); ok {
		if v, ok := rag["base_url"].(string); ok {
			cfg.RAG.BaseURL = v
		}
		if v, ok := rag["model"].(string); ok {
			cfg.RAG.Model = v
		}
		if v, ok := rag["api_key"].(string); ok {
			if strings.Contains(v, "***") {
				return fmt.Errorf("rag.api_key must not contain '***' — please provide the full key value")
			}
			cfg.RAG.APIKey = v
		}
		if v, ok := rag["chunk_size"].(float64); ok {
			cfg.RAG.ChunkSize = int(v)
		}
		if v, ok := rag["search_limit"].(float64); ok {
			cfg.RAG.SearchLimit = int(v)
		}
		if v, ok := rag["search_pool_size"].(float64); ok {
			cfg.RAG.SearchPoolSize = int(v)
		}
		if v, ok := rag["retention_days"].(float64); ok {
			cfg.RAG.RetentionDays = int(v)
		}
	}

	if pf, ok := patch["port_forward"].(map[string]any); ok {
		if v, ok := pf["enabled"].(bool); ok {
			cfg.PortForward.Enabled = v
		}
		if v, ok := pf["port"].(float64); ok {
			cfg.PortForward.Port = int(v)
		}
	}

	if summarize, ok := patch["summarize"].(map[string]any); ok {
		if v, ok := summarize["backend"].(string); ok {
			cfg.Summarize.Backend = v
		}
		if v, ok := summarize["model"].(string); ok {
			cfg.Summarize.Model = v
		}
		// Summarize API sub-config
		if api, ok := summarize["api"].(map[string]any); ok {
			if v, ok := api["base_url"].(string); ok {
				cfg.Summarize.API.BaseURL = v
			}
			if v, ok := api["key"].(string); ok {
				cfg.Summarize.API.Key = v
			}
			if v, ok := api["format"].(string); ok {
				cfg.Summarize.API.Format = v
			}
		}
	}

	// Also update global variables for hot-reloadable fields
	applyHotReloadGlobals()

	return nil
}

// applyHotReloadGlobals syncs the package-level "hot-reload" variables from
// ConfigInstance. Called after a successful patch (and on rollback).
func applyHotReloadGlobals() {
	cfg := model.ConfigInstance
	model.ChatInitialMessages = cfg.Chat.InitialMessages
	model.ChatPageSize = cfg.Chat.PageSize
	model.ChatSystemPromptInterval = cfg.Chat.SystemPromptInterval
	model.SessionMaxCount = cfg.Session.MaxCount
	model.RecentProjectsMaxCount = cfg.RecentProjects.MaxCount
	model.UploadMaxSizeMB = cfg.Upload.MaxSizeMB
	model.UploadMaxFiles = cfg.Upload.MaxFiles
	model.TTSMaxCacheFiles = cfg.TTS.MaxCacheFiles
	model.DefaultAgentID = cfg.DefaultAgent
	model.LocalhostAuthExempt = cfg.LocalhostAuthExempt

	// Hot-reload TTS voice and speed on the existing speech provider.
	// This is a defensive fallback for when reconfigureOnHotReload is nil
	// (e.g., in tests). When reconfigureOnHotReload is set (production),
	// it recreates the entire provider from the full config, making these
	// mutations redundant. Kept for test compatibility.
	curProvider := GetSpeechProvider()
	if cfg.TTS.Voice != "" {
		if p, ok := curProvider.(*speech.EdgeTTSProvider); ok {
			p.Voice = cfg.TTS.Voice
		}
		if p, ok := curProvider.(*speech.KokoroProvider); ok {
			p.Voice = cfg.TTS.Voice
		}
		// Piper: voice is embedded in model_path, not a standalone field
		// MOSS-Nano: uses tts.voice (shared with Edge/Kokoro)
	}
	if cfg.TTS.Speed > 0 {
		if p, ok := curProvider.(*speech.EdgeTTSProvider); ok {
			ratePercent := int((cfg.TTS.Speed - 1.0) * 100)
			if ratePercent > 0 {
				p.Rate = fmt.Sprintf("+%d%%", ratePercent)
			} else if ratePercent < 0 {
				p.Rate = fmt.Sprintf("%d%%", ratePercent)
			} else {
				p.Rate = "+0%"
			}
		}
		if p, ok := curProvider.(*speech.KokoroProvider); ok {
			p.Speed = cfg.TTS.Speed
		}
		if p, ok := curProvider.(*speech.PiperProvider); ok {
			// Only update if no explicit length_scale is set
			if cfg.TTS.Piper.LengthScale <= 0 {
				p.LengthScale = 1.0 / cfg.TTS.Speed
			}
		}
		// MOSS-Nano: speed not supported
	}

	// Reconfigure subsystems (TTS engine swap, summarize reconstruction,
	// terminal reconfigure). Set by main.go.
	if reconfigureOnHotReload != nil {
		reconfigureOnHotReload()
	}
}

// writeConfigYAML writes the patched fields back to config/config.yaml atomically.
// Instead of rewriting the entire ConfigInstance (which would expand zero values
// and break ApplyDefaults presence semantics), it reads the existing YAML into a
// generic map, patches only the changed fields, and writes back. This preserves
// user comments, field ordering, and absent-as-default semantics for untouched fields.
func writeConfigYAML(patch map[string]any) error {
	configDir := filepath.Join(model.BinDir, "config")
	configPath := filepath.Join(configDir, "config.yaml")
	tmpPath := configPath + ".tmp"
	bakPath := configPath + ".bak"

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Read existing YAML content into a generic map
	var raw map[string]any
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			slog.Warn("failed to parse existing config.yaml, starting fresh", "err", err)
			raw = make(map[string]any)
		}
	} else {
		// No existing file — marshal the full ConfigInstance as base
		data, err := yaml.Marshal(&model.ConfigInstance) //nolint:gosec // Password field contains SHA-256 hash, not plaintext
		if err != nil {
			return fmt.Errorf("failed to marshal initial config: %w", err)
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("failed to unmarshal initial config: %w", err)
		}
	}

	// Backup existing file before overwriting
	if _, err := os.Stat(configPath); err == nil {
		if err := copyFile(configPath, bakPath); err != nil {
			slog.Warn("failed to backup config.yaml", "err", err)
		}
	}

	// Merge patch into raw map (only touched fields are modified)
	mergePatchIntoRaw(raw, patch)

	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

// mergePatchIntoRaw merges the PATCH payload into the raw YAML map.
// Only the leaf values present in the patch are updated; all other fields
// in the raw map are preserved untouched.
func mergePatchIntoRaw(raw map[string]any, patch map[string]any) {
	for key, value := range patch {
		if nested, ok := value.(map[string]any); ok {
			// Recursively merge nested maps
			existing, ok := raw[key].(map[string]any)
			if !ok {
				existing = make(map[string]any)
			}
			mergePatchIntoRaw(existing, nested)
			raw[key] = existing
		} else {
			raw[key] = value
		}
	}
}

// ServeConfigPassword handles POST /api/config/password.
// Validates the current password, then writes the new password as a SHA-256
// hash to config.yaml. The change takes effect after server restart.
func ServeConfigPassword(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // password change with verification
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Rate limiting — reuse the login limiter
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	limiter := getLoginLimiter()
	if limiter.isBlocked(remoteIP) {
		slog.Warn("password change blocked: too many failures", slog.String("ip", remoteIP))
		writeLocalizedErrorf(w, r, http.StatusTooManyRequests, "TooManyLoginAttempts")
		return
	}

	// Parse request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024))
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_json",
			"message": "failed to parse request body as JSON",
		})
		return
	}

	// Validate inputs
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "empty_password",
			"message": "current_password and new_password are required",
		})
		return
	}
	if utf8.RuneCountInString(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "password_too_short",
			"message": "new password must be at least 8 characters",
		})
		return
	}
	if utf8.RuneCountInString(req.NewPassword) > 32 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "password_too_long",
			"message": "new password must be at most 32 characters",
		})
		return
	}
	hasLetter := false
	hasDigit := false
	for _, ch := range req.NewPassword {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			hasLetter = true
		}
		if ch >= '0' && ch <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "password_no_letter_digit",
			"message": "new password must contain both letters and digits",
		})
		return
	}

	// Verify current password
	configMutex.RLock()
	var authenticated bool
	if model.PasswordIsSHA256 {
		// Password stored as SHA-256 hash — hash submitted password and compare
		hash := sha256.Sum256([]byte(req.CurrentPassword + "clawbench-salt"))
		candidate := hex.EncodeToString(hash[:])
		authenticated = subtle.ConstantTimeCompare([]byte(candidate), []byte(model.SessionToken)) == 1
	} else if model.PasswordHash != nil {
		// Plaintext password — bcrypt verification
		authenticated = bcrypt.CompareHashAndPassword(model.PasswordHash, []byte(req.CurrentPassword)) == nil
	}
	configMutex.RUnlock()

	if !authenticated {
		limiter.recordFailure(remoteIP)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":   "wrong_password",
			"message": "current password is incorrect",
		})
		return
	}

	limiter.reset(remoteIP)

	// Compute SHA-256 hash of the new password
	hash := sha256.Sum256([]byte(req.NewPassword + "clawbench-salt"))
	hashedPassword := "sha256:" + hex.EncodeToString(hash[:])

	// Write to config.yaml under write lock
	configMutex.Lock()
	defer configMutex.Unlock()

	// Save old value for rollback on disk write failure
	oldPassword := model.ConfigInstance.Password

	// Update in-memory config
	model.ConfigInstance.Password = hashedPassword

	// Write to disk
	patch := map[string]any{"password": hashedPassword}
	if err := writeConfigYAML(patch); err != nil {
		// Rollback memory state
		model.ConfigInstance.Password = oldPassword
		slog.Error("failed to write config.yaml after password change", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "write_failed",
			"message": fmt.Sprintf("failed to write config.yaml: %v", err),
		})
		return
	}

	// Also remove the auto-password file (if any) since user has set an explicit password
	autoPasswordFile := filepath.Join(model.DataDir, "auto-password")
	// Read the old auto-password before deleting it (needed for API key rotation)
	oldAutoPassword, _ := os.ReadFile(autoPasswordFile)
	_ = os.Remove(autoPasswordFile)

	// Rotate API key encryption: the auto-password is being removed, which changes
	// the encryption key derivation. Re-encrypt all stored API keys with the new key
	// (which will fall back to deriveFallbackKey since auto-password is gone).
	if service.DBReady() && len(oldAutoPassword) > 0 {
		if err := service.RotateAPIKeyEncryption(string(oldAutoPassword)); err != nil {
			slog.Error("failed to rotate API key encryption after password change", "error", err)
			// Don't fail the password change — the user can re-enter API keys later
		}
	}

	// Update in-memory auth state so the change takes effect immediately
	// (ISS-197 partial fix: password change should not require restart for auth)
	newSHA256Hash := hex.EncodeToString(hash[:])
	model.SessionToken = newSHA256Hash
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil // SHA-256 mode: no bcrypt
	// Rotate the cookie token so old sessions are invalidated
	model.CookieToken = model.GenerateRandomToken(32)
	model.PersistCookieToken(model.CookieToken)

	slog.Info("password changed via settings API (in-memory auth state updated, old sessions invalidated)")
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_restart": false, // Auth state updated in-memory; no restart needed for auth
	})
}

// ServeConfigRestart handles POST /api/config/restart — triggers server restart.
func ServeConfigRestart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	go func() {
		time.Sleep(restartGracePeriod)

		if restartFunc != nil {
			restartFunc()
		} else {
			slog.Warn("restart function not set, cannot restart server")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "restarting",
	})
}

// LaunchSentinelProcess starts a sentinel process that waits for the current
// process to exit, then starts a new one. Returns the sentinel cmd on success.
func LaunchSentinelProcess() (*exec.Cmd, error) {
	return launchSentinel()
}

// IsRunningUnderSupervisor detects if the process is managed by systemd, Docker, etc.
func IsRunningUnderSupervisor() bool {
	if os.Getenv("CLAWBENCH_NO_SUPERVISOR") != "" {
		return false
	}
	if os.Getenv("INVOCATION_ID") != "" {
		return true
	}
	if os.Getenv("container") != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if os.Getppid() == 1 {
		return true
	}
	return false
}

// joinArgs joins command-line args into a space-separated string with proper
// shell quoting. Single quotes within args are escaped using the '\” idiom.
func joinArgs(args []string) string {
	var buf strings.Builder
	for i, arg := range args {
		if i > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(shellQuote(arg))
	}
	return buf.String()
}

// shellQuote wraps a string in single quotes, escaping any embedded single
// quotes using the '\” idiom safe for POSIX shells.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
