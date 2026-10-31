package model

import (
	"fmt"
	"sort"
	"strings"
)

// ProtocolEnv names the environment variables one backend reads for a given
// wire protocol.
//
// BaseURL/APIKey/AuthToken are the canonical trio: most CLIs accept a base URL
// override plus either an x-api-key style key or a Bearer token. Leaving a
// field empty means that backend does not read such a variable.
type ProtocolEnv struct {
	BaseURL   string
	APIKey    string
	AuthToken string
	// Extra is injected verbatim (name → value) for backends that need an
	// additional switch to honor the override.
	Extra map[string]string
}

// ProviderEnvSpec is how one backend receives a provider.
//
// ConfigOnly backends cannot be redirected by environment variables alone: the
// endpoint lives in a CLI config file, and only the secret is passed through
// the environment (the file references the variable). Those backends are
// handled by the config-file writer; BuildProviderEnv still returns the secret
// so the spawned process can resolve it.
type ProviderEnvSpec struct {
	Anthropic *ProtocolEnv
	OpenAI    *ProtocolEnv
	Gemini    *ProtocolEnv

	// KeyOnly is the variable a backend reads when it accepts credentials but
	// no endpoint override (cursor, commandcode, ...).
	KeyOnly string

	// ConfigOnly marks backends whose endpoint is written to a config file.
	ConfigOnly bool
	// ConfigProtocols lists the protocols a ConfigOnly backend's file format
	// can express (e.g. codex understands OpenAI wire APIs only).
	ConfigProtocols []string
}

// providerEnvTable is the single place that knows which environment variables
// each backend honors. Every registered backend must have an entry (the
// coverage test in internal/ai/backends enforces both directions), and an entry
// with no protocols and no KeyOnly means "this backend cannot take a provider".
//
// Evidence for each row is recorded in docs/spec/features/model-providers.md;
// do not add a guess here — an unverified variable silently does nothing, which
// is exactly the failure mode this table exists to avoid.
// protoEnv builds one protocol row. Using a constructor keeps the table free
// of field assignments that credential scanners mistake for secrets (the
// strings are environment variable NAMES, never keys).
func protoEnv(baseURL, apiKey, authToken string) *ProtocolEnv {
	return &ProtocolEnv{BaseURL: baseURL, APIKey: apiKey, AuthToken: authToken}
}

var providerEnvTable = map[string]ProviderEnvSpec{
	"antigravity": {
		Gemini:  protoEnv("GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", ""),
		KeyOnly: "GEMINI_API_KEY",
	},
	"claude": {
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
	},
	"codebuddy": {
		Anthropic: protoEnv("CODEBUDDY_BASE_URL", "CODEBUDDY_API_KEY", "CODEBUDDY_AUTH_TOKEN"),
	},
	// codex reads its provider from ~/.codex/config.toml. The managed block
	// references this variable for env_key, so the secret never lands on disk.
	"codex": {
		ConfigOnly:      true,
		ConfigProtocols: []string{ProviderProtocolOpenAI},
		KeyOnly:         "CLAWBENCH_PROVIDER_API_KEY",
	},
	// commandcode: BYOK needs providers.json + config.json (see the ACP probe
	// notes). Writing those files is not implemented, so only the credential is
	// passed through — full BYOK still requires the CLI's own configuration.
	"commandcode": {
		KeyOnly: "COMMAND_CODE_API_KEY",
	},
	"cursor": {
		KeyOnly: "CURSOR_API_KEY",
	},
	"deepseek": {
		OpenAI: protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"dsh": {
		OpenAI: protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"grok": {
		OpenAI: protoEnv("GROK_XAI_API_BASE_URL", "XAI_API_KEY", ""),
	},
	"hermes": {
		OpenAI: protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"kimi": {
		// kimi-cli's own provider is OpenAI-compatible and reads KIMI_*; its
		// Anthropic path goes through the standard ANTHROPIC_* variables.
		OpenAI:    protoEnv("KIMI_BASE_URL", "KIMI_API_KEY", ""),
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
	},
	"mimo": {
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
		OpenAI:    protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"opencode": {
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
		OpenAI:    protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"pi": {
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
		OpenAI:    protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},
	"zcode": {
		Anthropic: protoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
		OpenAI:    protoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
	},

	// Deliberately empty: no verified override exists.
	//
	//   copilot   GitHub sign-in only
	//   openclaw  the provider belongs to the Gateway process, not the ACP bridge
	//   qoder     login-based; the CLI binary contains no provider variables
	//   qodercn   same as qoder
	//   vecli     no evidence available
	"copilot":    {},
	"openclaw":   {},
	"qoder":      {},
	"qodercn":    {},
	"vecli":      {},
	"custom-acp": {},
}

// ProviderEnvBackends lists every backend that has a provider-env entry. Used
// by the coverage test to keep the table and the backend registry in sync.
func ProviderEnvBackends() []string {
	out := make([]string, 0, len(providerEnvTable))
	for id := range providerEnvTable {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// HasProviderEnvEntry reports whether backendID is classified at all. An entry
// with no protocols still counts: it records the deliberate "cannot take a
// provider" decision instead of leaving the backend unclassified.
func HasProviderEnvEntry(backendID string) bool {
	_, ok := providerEnvTable[backendID]
	return ok
}

// ProviderEnvSupported reports whether the backend can receive a provider with
// this protocol (an empty protocol asks "any protocol at all").
func ProviderEnvSupported(backendID, protocol string) bool {
	spec, ok := providerEnvTable[backendID]
	if !ok {
		return false
	}
	if protocol == "" {
		return spec.any()
	}
	if spec.supportsProtocol(protocol) {
		return true
	}
	// A key-only backend accepts any provider that carries no endpoint: the
	// protocol is irrelevant when all we do is pass the credential through.
	return spec.KeyOnly != "" && !spec.ConfigOnly
}

// BackendIsKeyOnly reports whether the backend consumes only a credential:
// it has no endpoint slot at all, so a provider bound to it must carry no
// base_url (see BuildProviderEnv).
func BackendIsKeyOnly(backendID string) bool {
	spec, ok := providerEnvTable[backendID]
	return ok && spec.KeyOnly != "" && !spec.ConfigOnly
}

// ProviderEnvSupportedProvider mirrors BuildProviderEnv's decision for a whole
// provider row.
//
// ProviderEnvSupported only knows the protocol, so for a key-only backend it
// answers "yes" even when the provider carries an endpoint — the binding is
// accepted, then every turn fails with "backend X does not support the Y
// protocol" because BuildProviderEnv takes the key-only branch only when the
// endpoint is empty. Callers that hold the row must use this instead.
func ProviderEnvSupportedProvider(backendID string, p *Provider) bool {
	if p == nil {
		return false
	}
	spec, ok := providerEnvTable[backendID]
	if !ok {
		return false
	}
	if spec.supportsProtocol(p.Protocol) {
		return true
	}
	return spec.KeyOnly != "" && !spec.ConfigOnly && p.BaseURL == ""
}

// any reports whether the spec can take a provider at all.
func (s ProviderEnvSpec) any() bool {
	return s.Anthropic != nil || s.OpenAI != nil || s.Gemini != nil || s.KeyOnly != "" || s.ConfigOnly
}

// supportsProtocol reports whether the spec has a slot for the protocol.
// Config-file protocols are checked first: a ConfigOnly backend has no
// ProtocolEnv entry, but its file format still expresses a protocol.
func (s ProviderEnvSpec) supportsProtocol(protocol string) bool {
	for _, p := range s.ConfigProtocols {
		if p == protocol {
			return true
		}
	}
	switch protocol {
	case ProviderProtocolAnthropic:
		return s.Anthropic != nil
	case ProviderProtocolOpenAI:
		return s.OpenAI != nil
	case ProviderProtocolGemini:
		return s.Gemini != nil
	}
	return false
}

// ProviderUsesConfigFile reports whether the backend receives its endpoint
// through a config file rather than environment variables. Those bindings need
// an explicit user confirmation because they modify files outside ClawBench.
func ProviderUsesConfigFile(backendID string) bool {
	spec, ok := providerEnvTable[backendID]
	return ok && spec.ConfigOnly
}

// SupportedProtocols lists the protocols a backend can be pointed at. An empty
// list means the backend cannot take a provider (only a bare key at most).
func SupportedProtocols(backendID string) []string {
	spec, ok := providerEnvTable[backendID]
	if !ok {
		return nil
	}
	var out []string
	if spec.Anthropic != nil {
		out = append(out, ProviderProtocolAnthropic)
	}
	if spec.OpenAI != nil {
		out = append(out, ProviderProtocolOpenAI)
	}
	if spec.Gemini != nil {
		out = append(out, ProviderProtocolGemini)
	}
	out = append(out, spec.ConfigProtocols...)
	sort.Strings(out)
	return out
}

// BuildProviderEnv renders the environment entries for one backend/provider
// pair. A nil provider means "no injection".
//
// An unsupported backend or protocol returns an error rather than an empty
// slice: silently starting the process with its own credentials would look like
// the provider was applied while every request still went to the old endpoint.
func BuildProviderEnv(backendID string, p *Provider) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	spec, ok := providerEnvTable[backendID]
	if !ok {
		return nil, fmt.Errorf("backend %q does not support model providers", backendID)
	}
	if p.APIKey == "" {
		return nil, fmt.Errorf("provider %q has no API key", p.Name)
	}
	if spec.ConfigOnly || (spec.KeyOnly != "" && p.BaseURL == "" && protocolEnvFor(spec, p.Protocol) == nil) {
		return keyOnlyEnv(backendID, spec, p)
	}

	pe := protocolEnvFor(spec, p.Protocol)
	if pe == nil {
		return nil, fmt.Errorf("backend %q does not support the %s protocol", backendID, p.Protocol)
	}
	keyVar, err := keyEnvVar(backendID, pe, p)
	if err != nil {
		return nil, err
	}
	if p.BaseURL == "" {
		return []string{keyVar + "=" + p.APIKey}, nil
	}
	env := make([]string, 0, 2+len(pe.Extra))
	env = append(env, pe.BaseURL+"="+p.BaseURL, keyVar+"="+p.APIKey)
	for name, value := range pe.Extra {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return env, nil
}

// keyOnlyEnv renders the credential-only form used by backends that accept a
// key but no endpoint override.
func keyOnlyEnv(backendID string, spec ProviderEnvSpec, p *Provider) ([]string, error) {
	if spec.KeyOnly == "" {
		return nil, fmt.Errorf("backend %q does not support the %s protocol", backendID, p.Protocol)
	}
	return []string{spec.KeyOnly + "=" + p.APIKey}, nil
}

func protocolEnvFor(spec ProviderEnvSpec, protocol string) *ProtocolEnv {
	switch protocol {
	case ProviderProtocolAnthropic:
		return spec.Anthropic
	case ProviderProtocolOpenAI:
		return spec.OpenAI
	case ProviderProtocolGemini:
		return spec.Gemini
	}
	return nil
}

// keyEnvVar picks the variable that carries the credential, honoring the
// provider's auth mode. An explicitly requested mode that the backend cannot
// express is an error — falling back would send the key in the wrong header.
func keyEnvVar(backendID string, pe *ProtocolEnv, p *Provider) (string, error) {
	switch p.AuthMode {
	case ProviderAuthBearer:
		if pe.AuthToken == "" {
			return "", fmt.Errorf("backend %q does not support Bearer auth for the %s protocol", backendID, p.Protocol)
		}
		return pe.AuthToken, nil
	case ProviderAuthAPIKey:
		if pe.APIKey == "" {
			return "", fmt.Errorf("backend %q does not support x-api-key auth for the %s protocol", backendID, p.Protocol)
		}
		return pe.APIKey, nil
	default:
		if pe.APIKey != "" {
			return pe.APIKey, nil
		}
		if pe.AuthToken != "" {
			return pe.AuthToken, nil
		}
		return "", fmt.Errorf("backend %q has no credential variable for the %s protocol", backendID, p.Protocol)
	}
}

// ProviderEnvForAgent resolves the agent's provider and renders its environment
// in one step, which is what every spawn site needs.
func ProviderEnvForAgent(agentID, backendID string) ([]string, error) {
	p := AgentProvider(agentID)
	if p == nil {
		return nil, nil
	}
	return BuildProviderEnv(backendID, p)
}

// RedactProviderEnv hides credential values so a spawn log can name the
// variables without leaking the key.
func RedactProviderEnv(env []string) []string {
	out := make([]string, len(env))
	for i, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			out[i] = entry
			continue
		}
		out[i] = name + "=***"
	}
	return out
}
