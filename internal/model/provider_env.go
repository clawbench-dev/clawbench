package model

import (
	"encoding/json"
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

	// InlineConfigEnv names an environment variable that takes config CONTENT
	// (not a path). It exists for backends that validate a model id against
	// their own provider catalog before sending anything: the endpoint
	// variables above redirect where requests go, but they do not ADD models to
	// that catalog, so a provider model outside it is rejected (opencode:
	// "model not found") before the request ever reaches the endpoint. Declaring
	// the provider's models in the config content is what makes them real.
	InlineConfigEnv string
	// InlineConfigProviderIDs maps a wire protocol to the config provider id
	// this backend registers the models under (opencode: "clawbench-openai" /
	// "clawbench-anthropic"). Required whenever InlineConfigEnv is set; a
	// protocol absent here is left undeclared rather than guessed.
	//
	// A dedicated id, NOT the backend's built-in one, because the backend
	// addresses a declared model as "<provider>/<declared key>" — reusing the
	// built-in id would make a curated id that already carries that prefix
	// addressable only as "openai/openai/…". With our own id the prefix is
	// unambiguous: "<our id>/<curated id verbatim>".
	InlineConfigProviderIDs map[string]string
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
		// opencode resolves a model id against its own provider catalog before
		// it sends anything, and a provider redirect does not extend that
		// catalog: OPENAI_BASE_URL/OPENAI_API_KEY alone leave "glm-…"
		// unresolvable ("model not found"), which is issue #651. Declaring the
		// provider's models through the config content is the only lever that
		// makes them resolvable. Content (not a file path) so nothing is
		// written to the user's home.
		//
		// The ids are ours, not opencode's built-in ones: it addresses a
		// declared model as "<provider>/<key>", so reusing "openai" would turn a
		// curated id that already says "openai/gpt-4o" into "openai/openai/…"
		// (verified against 2.2.6). With a dedicated id the rule is simply
		// "<our id>/<curated id verbatim>".
		InlineConfigEnv: "OPENCODE_CONFIG_CONTENT",
		InlineConfigProviderIDs: map[string]string{
			ProviderProtocolOpenAI:    "clawbench-openai",
			ProviderProtocolAnthropic: "clawbench-anthropic",
		},
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

// ProviderAuthModeError explains why backendID cannot express the provider's
// auth mode, or returns nil when it can.
//
// This is the bind-time counterpart of keyEnvVar: an auth mode the backend has
// no variable for used to be accepted at every entry point (create, edit,
// bind) and only failed on the first turn, with nothing pointing at the auth
// mode as the cause. Key-only and config-file backends take the credential (or
// the config file) and never read an auth mode, so no mode can be wrong there.
func ProviderAuthModeError(backendID, protocol, authMode string) error {
	spec, ok := providerEnvTable[backendID]
	if !ok {
		return fmt.Errorf("backend %q does not support model providers", backendID)
	}
	pe := protocolEnvFor(spec, protocol)
	if pe == nil {
		if spec.ConfigOnly || spec.KeyOnly != "" {
			return nil
		}
		return fmt.Errorf("backend %q does not support the %s protocol", backendID, protocol)
	}
	if authModeFits(pe, authMode) {
		return nil
	}
	switch authMode {
	case ProviderAuthBearer:
		return fmt.Errorf("backend %q does not support Bearer auth for the %s protocol", backendID, protocol)
	case ProviderAuthAPIKey:
		return fmt.Errorf("backend %q does not support x-api-key auth for the %s protocol", backendID, protocol)
	default:
		return fmt.Errorf("backend %q has no credential variable for the %s protocol", backendID, protocol)
	}
}

// ProtocolSupportsAuthMode reports whether ANY backend that reads an auth mode
// can express it for the protocol.
//
// A provider row is not tied to a backend, so create/update cannot check one.
// This answers the only question they can: whether the pair is dead everywhere.
// Config-file and key-only backends are skipped because they ignore the auth
// mode — they neither prove nor disprove that the pair works.
func ProtocolSupportsAuthMode(protocol, authMode string) bool {
	for _, spec := range providerEnvTable {
		pe := protocolEnvFor(spec, protocol)
		if pe != nil && authModeFits(pe, authMode) {
			return true
		}
	}
	return false
}

// authModeFits mirrors keyEnvVar's choice without building the message: it is
// the single predicate behind both the bind-time error and the protocol-level
// check, so the two can never disagree.
func authModeFits(pe *ProtocolEnv, authMode string) bool {
	switch authMode {
	case ProviderAuthBearer:
		return pe.AuthToken != ""
	case ProviderAuthAPIKey:
		return pe.APIKey != ""
	default:
		return pe.APIKey != "" || pe.AuthToken != ""
	}
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
//
// pinnedModel is the model the agent may actually run (its preference). Some
// backends only accept ids their own catalog declares, and the provider's list
// is the set the endpoint serves — so a pinned id outside that list must be
// declared too, or the user can save it and still fail every turn. Empty means
// "no pin"; the provider's whole list is declared either way.
func BuildProviderEnv(backendID string, p *Provider, pinnedModel string) ([]string, error) {
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
		return withInlineConfig(spec, p, pinnedModel, []string{keyVar + "=" + p.APIKey}), nil
	}
	env := make([]string, 0, 3+len(pe.Extra))
	env = append(env, pe.BaseURL+"="+p.BaseURL, keyVar+"="+p.APIKey)
	for name, value := range pe.Extra {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return withInlineConfig(spec, p, pinnedModel, env), nil
}

// withInlineConfig appends the backend's inline config-content variable when the
// spec declares one and there is at least one model to declare.
//
// The models are declared under the config provider id for the protocol (see
// InlineConfigProviderIDs), with the endpoint and credential taken from the SAME
// environment variables the spawn already sets. That indirection is deliberate:
// the secret is never written into the content, so it cannot leak through a
// process listing, and the endpoint stays a single source of truth.
//
// Declared set = the provider's curated list plus pinnedModel when it is not in
// that list. The pin is accepted by PATCH on purpose (a bound provider makes the
// model list advisory), so it has to become real here — otherwise the "custom
// model" escape hatch saves an id the backend then refuses to resolve.
//
// Nothing to declare means no content at all: an empty provider block would
// merely shadow the user's own backend configuration.
func withInlineConfig(spec ProviderEnvSpec, p *Provider, pinnedModel string, env []string) []string {
	if spec.InlineConfigEnv == "" {
		return env
	}
	providerID, ok := spec.InlineConfigProviderIDs[p.Protocol]
	if !ok {
		return env
	}
	ids := declaredModelIDs(p, pinnedModel, providerID)
	if len(ids) == 0 {
		return env
	}
	pe := protocolEnvFor(spec, p.Protocol)
	if pe == nil {
		return env
	}
	content, ok := inlineConfigJSON(p, pe, providerID, ids)
	if !ok {
		return env
	}
	return append(env, spec.InlineConfigEnv+"="+content)
}

// declaredModelIDs is the set of model ids to declare to the backend: the
// provider's curated list, plus the pinned model when it is not already covered.
//
// Both are used VERBATIM. The curated list holds the ids the endpoint serves,
// including a namespace the gateway itself uses ("openai/gpt-4o" on an
// OpenRouter-style endpoint) — the backend sends the declared key to the
// endpoint unchanged, so rewriting it would send an id the endpoint rejects.
//
// The pin comes from the picker, which prefixes the curated id with the config
// provider id (ProviderModelIDFor). That prefix is ours, so it is removed before
// the comparison — otherwise the same model would be declared twice.
func declaredModelIDs(p *Provider, pinnedModel, providerID string) []string {
	ids := make([]string, 0, len(p.Models)+1)
	for _, id := range p.Models {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	pin := strings.TrimPrefix(strings.TrimSpace(pinnedModel), providerID+"/")
	if pin != "" && !containsString(ids, pin) {
		ids = append(ids, pin)
	}
	return ids
}

// inlineConfigJSON renders the provider block. ok is false when the value could
// not be encoded, which only happens for inputs the caller never produces.
func inlineConfigJSON(p *Provider, pe *ProtocolEnv, providerID string, ids []string) (string, bool) {
	keyVar := pe.APIKey
	if p.AuthMode == ProviderAuthBearer {
		keyVar = pe.AuthToken
	}
	models := make(map[string]any, len(ids))
	for _, id := range ids {
		// tool_call must be on: a model declared without it is advertised but
		// cannot call tools, which turns every coding turn into a chat reply.
		models[id] = map[string]any{"name": id, "tool_call": true}
	}
	// {env:NAME} is opencode's own interpolation syntax, and it is the only one
	// that works for a provider we declare ourselves: "${NAME}" resolves inside
	// opencode's BUILT-IN provider definitions but is passed through literally
	// for a custom provider (verified against 2.2.6 — the request went out with
	// "Bearer ${OPENAI_API_KEY}").
	opts := map[string]any{"apiKey": "{env:" + keyVar + "}"}
	// Only reference the endpoint variable when the spawn actually sets it: a
	// provider with no base_url (an official API key) leaves it unset, and an
	// unresolved reference would either fail to parse or override opencode's own
	// default endpoint with garbage.
	if p.BaseURL != "" {
		opts["baseURL"] = "{env:" + pe.BaseURL + "}"
	}
	// npm names the wire adapter. A provider opencode does not know needs it
	// explicitly: without it the ACP surface still lists the models and
	// set_config_option still succeeds, but the actual turn dies with
	// "Unsupported package for <provider>/<model>" and NOT ONE BYTE reaches the
	// endpoint. Reusing a built-in provider id used to imply it; a dedicated id
	// does not (verified against 2.2.6).
	provider := map[string]any{
		"npm":     npmForProtocol(p.Protocol),
		"name":    "ClawBench " + p.Name,
		"options": opts,
		"models":  models,
	}
	block := map[string]any{"provider": map[string]any{providerID: provider}}
	content, err := json.Marshal(block)
	if err != nil {
		return "", false
	}
	return string(content), true
}

// npmForProtocol is the wire adapter opencode must load for a protocol. An
// unknown protocol returns "" and the caller declares nothing — the binding was
// already rejected earlier, so this is only a safety net.
func npmForProtocol(protocol string) string {
	switch protocol {
	case ProviderProtocolOpenAI:
		return "@ai-sdk/openai-compatible"
	case ProviderProtocolAnthropic:
		return "@ai-sdk/anthropic"
	case ProviderProtocolGemini:
		return "@ai-sdk/google"
	}
	return ""
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
	if err := ProviderAuthModeError(backendID, p.Protocol, p.AuthMode); err != nil {
		return "", err
	}
	switch p.AuthMode {
	case ProviderAuthBearer:
		return pe.AuthToken, nil
	case ProviderAuthAPIKey:
		return pe.APIKey, nil
	default:
		if pe.APIKey != "" {
			return pe.APIKey, nil
		}
		return pe.AuthToken, nil
	}
}

// ProviderModelIDFor renders a provider model id in the form the backend
// actually accepts.
//
// Most backends take the bare id the endpoint serves. opencode does not: its
// model ids are "<provider>/<model>", so a bare id is unusable in a picker and
// in a turn alike (issue #651). Callers that build a picker entry, or that
// declare the model to the backend, must go through this so the two agree.
//
// A protocol the backend cannot express, or one with no provider id mapping,
// returns the id unchanged — the caller's own validation already rejected those
// combinations.
func ProviderModelIDFor(backendID, protocol, id string) string {
	spec, ok := providerEnvTable[backendID]
	if !ok || spec.InlineConfigProviderIDs == nil {
		return id
	}
	providerID, ok := spec.InlineConfigProviderIDs[protocol]
	if !ok || providerID == "" || id == "" {
		return id
	}
	if strings.HasPrefix(id, providerID+"/") {
		return id
	}
	return providerID + "/" + id
}

// ProviderBareModelID is the inverse of ProviderModelIDFor: it drops the
// "<provider>/" namespace so a wire-form id can be compared with the provider's
// stored list.
func ProviderBareModelID(backendID, protocol, id string) string {
	spec, ok := providerEnvTable[backendID]
	if !ok || spec.InlineConfigProviderIDs == nil {
		return id
	}
	providerID, ok := spec.InlineConfigProviderIDs[protocol]
	if !ok || providerID == "" {
		return id
	}
	return strings.TrimPrefix(id, providerID+"/")
}

// ProviderPickerModels renders the provider's curated list as picker entries,
// with ids in the form this backend accepts. Nil when the agent is unbound or
// the provider has not curated models.
func (a *Agent) ProviderPickerModels() []AgentModel {
	p := a.Provider()
	if p == nil || len(p.Models) == 0 {
		return nil
	}
	out := make([]AgentModel, 0, len(p.Models))
	for _, id := range p.Models {
		out = append(out, AgentModel{
			ID:      ProviderModelIDFor(a.Backend, p.Protocol, id),
			Name:    id,
			Default: id == p.DefaultModel,
		})
	}
	return out
}

// MergeProviderModels prepends this agent's provider models to a resolved list,
// dropping duplicates.
//
// Every channel that ships a model list must go through this: the provider's
// models are what the user configured the agent to run, so a list built only
// from the agent's own discovery makes the binding invisible (issue #651). It
// lives here, on the model, because both the HTTP handlers and the ACP event
// enrichment build the same list and must not each re-implement the rule.
//
// The merged list keeps exactly ONE default. `resolved` has already been
// normalised (ResolveModels picks the session's current model), and the provider
// entries carry their own Default flag — leaving both set would give the picker
// two "default" rows and let a frontend `find(m => m.default)` fallback move a
// session off the model it is actually running.
func (a *Agent) MergeProviderModels(resolved []AgentModel) []AgentModel {
	pm := a.ProviderPickerModels()
	if len(pm) == 0 {
		return resolved
	}
	seen := make(map[string]struct{}, len(pm)+len(resolved))
	merged := make([]AgentModel, 0, len(pm)+len(resolved))
	for _, m := range pm {
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		merged = append(merged, m)
	}
	for _, m := range resolved {
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		merged = append(merged, m)
	}
	// Exactly one default. The resolved list's pick wins (it is the session's
	// current model, or the agent's own default); the provider's flag is only a
	// fallback for a resolved list that brought none. Scanning `merged` instead
	// would find the provider entry first, since it is prepended.
	defaultID := ""
	for _, m := range resolved {
		if m.Default {
			defaultID = m.ID
			break
		}
	}
	if defaultID == "" {
		for _, m := range merged {
			if m.Default {
				defaultID = m.ID
				break
			}
		}
	}
	for i := range merged {
		merged[i].Default = merged[i].ID == defaultID
	}
	return merged
}

// ProviderEnvForAgent resolves the agent's provider and renders its environment
// in one step, which is what every spawn site needs.
//
// The agent's preferred model rides along because a backend that validates ids
// against its own catalog needs the pin declared too, not only the curated list
// (see withInlineConfig).
func ProviderEnvForAgent(agentID, backendID string) ([]string, error) {
	p := AgentProvider(agentID)
	if p == nil {
		return nil, nil
	}
	pinned := ""
	if agent := GetAgent(agentID); agent != nil {
		pinned = agent.PreferredModel
	}
	return BuildProviderEnv(backendID, p, pinned)
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
