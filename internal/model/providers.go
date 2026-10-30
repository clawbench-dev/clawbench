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
	if p.AuthMode == "" {
		p.AuthMode = ProviderAuthAuto
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
func ProviderEnvVersion(agentID string) string {
	p := AgentProvider(agentID)
	if p == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		p.Name, p.Protocol, p.BaseURL, p.APIKey, p.AuthMode,
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}
