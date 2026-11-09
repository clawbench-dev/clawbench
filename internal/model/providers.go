package model

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

// Provider protocols. The protocol decides which environment variables a
// backend receives (see provider_env.go) and which request shape the upstream
// endpoint speaks.
const (
	ProviderProtocolAnthropic = "anthropic"
	ProviderProtocolOpenAI    = "openai"
	ProviderProtocolGemini    = "gemini"
)

// Provider auth modes. "auto" lets the protocol decide (x-api-key for
// Anthropic-style endpoints, Bearer for OpenAI-compatible ones). The explicit
// modes exist because many third-party Anthropic-compatible gateways only
// accept one of the two headers.
const (
	ProviderAuthAuto   = "auto"
	ProviderAuthAPIKey = "x-api-key"
	ProviderAuthBearer = "bearer"
)

// Provider is a user-configured model provider (endpoint + credentials) that
// agents can reference instead of configuring their CLI by hand.
//
// APIKey is never serialized: the settings page only learns whether a key is
// set (see the handler's response DTO). This mirrors the Forge credentials
// contract — a stored secret is write-only.
type Provider struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"-"`
	AuthMode string `json:"authMode"`
	// PresetID is the models.dev provider id this row was created from ("" for
	// a hand-written endpoint). It is the only reliable link from a user row to
	// the authoritative catalog: ids there are provider keys, not hosts.
	PresetID string `json:"presetId,omitempty"`
	// Models is the model set this endpoint actually serves. Agents bound to
	// the provider inherit it, so a picker can never offer a model the endpoint
	// rejects. Empty = "not curated": fall back to discovery.
	Models []string `json:"models,omitempty"`
	// DefaultModel is the model a bound agent uses when it has no preference of
	// its own. Must be one of Models (Normalized enforces it).
	DefaultModel string `json:"defaultModel,omitempty"`
	// WireAPI is the OpenAI wire protocol a Responses-first client has to speak
	// to this endpoint: "responses" (default) or "chat".
	//
	// Only codex reads it, and it is written into codex's managed config block.
	// A gateway that serves only /chat/completions (DeepSeek, MiniMax, most
	// self-hosted ones) 404s on /responses, so binding it while the block said
	// "responses" made every codex turn fail with no visible reason.
	WireAPI string `json:"wireApi,omitempty"`

	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Normalized returns a copy with trimmed values and every optional field
// resolved to its documented default. Callers persist the normalized form so a
// row never stores an empty protocol or auth mode.
func (p Provider) Normalized() Provider {
	p.Name = strings.TrimSpace(p.Name)
	p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.AuthMode = strings.ToLower(strings.TrimSpace(p.AuthMode))
	p.PresetID = strings.TrimSpace(p.PresetID)
	p.Models = normalizeModelIDs(p.Models)
	p.DefaultModel = strings.TrimSpace(p.DefaultModel)
	if len(p.Models) == 0 {
		p.DefaultModel = ""
	} else if p.DefaultModel == "" || !containsString(p.Models, p.DefaultModel) {
		p.DefaultModel = p.Models[0]
	}
	if p.AuthMode == "" {
		p.AuthMode = ProviderAuthAuto
	}
	p.WireAPI = strings.ToLower(strings.TrimSpace(p.WireAPI))
	if p.WireAPI == "" {
		p.WireAPI = ProviderWireResponses
	}
	return p
}

// ValidProtocol reports whether protocol is one this build understands.
func ValidProtocol(protocol string) bool {
	switch protocol {
	case ProviderProtocolAnthropic, ProviderProtocolOpenAI, ProviderProtocolGemini:
		return true
	}
	return false
}

// ValidAuthMode reports whether mode is one this build understands.
func ValidAuthMode(mode string) bool {
	switch mode {
	case ProviderAuthAuto, ProviderAuthAPIKey, ProviderAuthBearer:
		return true
	}
	return false
}

// Wire protocols codex can be told to speak. "responses" is the OpenAI
// Responses API (codex's default); "chat" is plain /chat/completions, which is
// all most compatible gateways implement.
const (
	ProviderWireResponses = "responses"
	ProviderWireChat      = "chat"
)

// ValidWireAPI reports whether api is a wire protocol codex understands.
func ValidWireAPI(api string) bool {
	switch api {
	case ProviderWireResponses, ProviderWireChat:
		return true
	}
	return false
}

// Providers are read on every turn (CLI spawn, ACP version gate) and written
// only through the settings API, so an in-memory snapshot with copy-on-write
// replacement is enough — and it keeps the spawn path off the database.
var (
	providersMu   sync.RWMutex
	providersByID map[int64]*Provider
	providerList  []*Provider
)

// ReplaceProviders atomically swaps in a new provider snapshot. The list is
// copied and sorted by name so every reader sees a stable order.
func ReplaceProviders(list []*Provider) {
	byID := make(map[int64]*Provider, len(list))
	ordered := make([]*Provider, 0, len(list))
	for _, p := range list {
		if p == nil {
			continue
		}
		normalized := p.Normalized()
		copied := normalized
		byID[copied.ID] = &copied
		ordered = append(ordered, &copied)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Name != ordered[j].Name {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].ID < ordered[j].ID
	})

	providersMu.Lock()
	defer providersMu.Unlock()
	providersByID = byID
	providerList = ordered
}

// ListProviders returns the current snapshot in display order.
func ListProviders() []*Provider {
	providersMu.RLock()
	defer providersMu.RUnlock()
	out := make([]*Provider, len(providerList))
	copy(out, providerList)
	return out
}

// GetProvider returns the provider with the given id, or nil.
func GetProvider(id int64) *Provider {
	if id <= 0 {
		return nil
	}
	providersMu.RLock()
	defer providersMu.RUnlock()
	return providersByID[id]
}

// AgentProvider resolves the provider an agent references. nil means the agent
// uses its CLI's own configuration (no injection).
//
// Resolution goes through GetAgent every time: agents are replaced wholesale by
// a refresh, so a cached *Agent would keep a stale provider binding.
func AgentProvider(agentID string) *Provider {
	agent := GetAgent(agentID)
	if agent == nil || agent.ProviderID == nil {
		return nil
	}
	return GetProvider(*agent.ProviderID)
}

// ProviderEnvVersion fingerprints the provider environment an agent would get
// right now. ACP connections compare it before reuse: when the user edits a
// provider (or rebinds an agent) the running process still carries the old
// environment, so it must be respawned. Empty means "no provider".
//
// Every field that reaches the spawned process belongs here. WireAPI is written
// into codex's config file, which the process reads once at startup, so
// changing it without changing the fingerprint left a live connection talking
// the old wire API while the file already said otherwise.
//
// The curated model list and the agent's pinned model belong here only for
// backends that render them into the inline config content — that is the only
// way either value reaches the spawned process. Adding them unconditionally
// would respawn every ACP connection on a model-pick change for backends where
// the value has no effect on the environment at all.
func ProviderEnvVersion(agentID string) string {
	p := AgentProvider(agentID)
	if p == nil {
		return ""
	}
	declared := ""
	if spec, ok := providerEnvTable[backendOf(agentID)]; ok && spec.InlineConfigEnv != "" {
		declared = strings.Join(p.Models, ",")
		if agent := GetAgent(agentID); agent != nil {
			declared += "\x00" + agent.PreferredModel
		}
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		p.Name, p.Protocol, p.BaseURL, p.APIKey, p.AuthMode, p.WireAPI, declared,
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// backendOf returns the backend id of an agent, or "" when it is unknown.
func backendOf(agentID string) string {
	if agent := GetAgent(agentID); agent != nil {
		return agent.Backend
	}
	return ""
}

// normalizeModelIDs trims, drops empties and removes duplicates while keeping
// the user's order (the first entry becomes the default).
func normalizeModelIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// Provider returns the provider this agent is bound to, or nil.
func (a *Agent) Provider() *Provider {
	if a == nil || a.ProviderID == nil {
		return nil
	}
	return GetProvider(*a.ProviderID)
}
