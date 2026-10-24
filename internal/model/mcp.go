package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
)

// MCPConfig is the global list of MCP servers available to ACP agents.
//
// The definition lives here (one copy, keys included); each agent only stores a
// POLICY saying which of these it may use. Splitting definition from policy is
// what keeps API keys in one place: editing a key once updates every agent,
// instead of asking the user to paste it into ten agent records.
//
// Scope: ACP transport only. CLI backends have no `mcpServers` parameter, so a
// CLI session reads its own config file and this list has no effect there.
type MCPConfig struct {
	Servers []MCPServer `yaml:"servers"`
}

// MCPServer is one MCP server definition.
//
// Transport selects which of the other fields are meaningful:
//
//	stdio  command, args, env
//	http   url, headers
//	sse    url, headers
type MCPServer struct {
	// Name identifies the server. It is sent to the agent as the ACP server
	// name, so it must be stable: several agents key their MCP schema cache by
	// it, and changing it makes them re-discover the tools.
	Name string `yaml:"name"`
	// Transport is "stdio", "http" or "sse".
	Transport string `yaml:"transport"`
	// Command is the executable for stdio. Required for stdio.
	Command string `yaml:"command,omitempty"`
	// Args are the command's arguments.
	Args []string `yaml:"args,omitempty"`
	// Env is the environment for a stdio server. Values are secrets and are
	// returned to the client in plaintext (the settings page behaves like the
	// STT / RAG / TTS api_key fields).
	Env map[string]string `yaml:"env,omitempty"`
	// URL is the endpoint for http / sse.
	URL string `yaml:"url,omitempty"`
	// Headers are request headers for http / sse (typically auth tokens).
	Headers map[string]string `yaml:"headers,omitempty"`
}

// MCP transport names.
const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
	MCPTransportSSE   = "sse"
)

// MCPPolicyMode is how an agent decides which global servers it receives.
type MCPPolicyMode string

const (
	// MCPPolicyAll enables every server in the global list. This is the
	// default because an empty global list makes it a no-op, so the default is
	// safe and the user does not have to tick each agent after adding a server.
	MCPPolicyAll MCPPolicyMode = "all"
	// MCPPolicyNone disables MCP entirely for the agent.
	MCPPolicyNone MCPPolicyMode = "none"
	// MCPPolicySelected enables only the servers named in Names.
	MCPPolicySelected MCPPolicyMode = "selected"
)

// MCPServerPolicy is an agent's MCP enablement policy.
//
// Only the policy is persisted per agent — never the definitions.
type MCPServerPolicy struct {
	Mode MCPPolicyMode `json:"mode"`
	// Names is consulted only when Mode is MCPPolicySelected.
	Names []string `json:"names"`
}

// Normalized returns the policy with its zero value resolved.
//
// An empty Mode means MCPPolicyAll. The zero value must NOT mean "none":
// a failed unmarshal or a row written before this column existed would then
// silently strip MCP from every agent, which is indistinguishable from "the
// feature is broken".
func (p MCPServerPolicy) Normalized() MCPServerPolicy {
	switch p.Mode {
	case MCPPolicyAll, MCPPolicyNone, MCPPolicySelected:
		return p
	default:
		return MCPServerPolicy{Mode: MCPPolicyAll, Names: p.Names}
	}
}

// MCPCapabilities is the subset of an agent's advertised MCP capabilities that
// affects what we may send.
//
// stdio is mandatory in the ACP spec and therefore never advertised; http and
// sse are optional and MUST be filtered. Sending sse to a backend that does not
// support it is a hard failure, not a graceful degradation: codex-acp throws
// `RequestError.invalidRequest` and the whole session/new fails.
//
// Defined here (not in internal/ai) because internal/model must not import
// internal/ai — that would close the dependency cycle the backend registry
// already avoids with function variables.
type MCPCapabilities struct {
	HTTP bool
	SSE  bool
}

// mcpUnsupportedBackends lists backends the probe measured as accepting the
// `mcpServers` field and then ignoring it completely.
//
// Measured 2026-10-19 (docs/dev/acp_mcp_probe_results.md): session/new succeeds,
// no error, and the tool never appears — two turns each, so this is not a
// slow-start artifact. Filtering here (rather than only graying the UI) matters
// because the default policy is `all`: without this list every user would hit
// the silently-ineffective case by default, which is exactly what the UI is
// supposed to prevent.
var mcpUnsupportedBackends = map[string]bool{
	"dsh":      true,
	"deepseek": true,
	"pi":       true,
	// openclaw's `acp` subcommand is a bridge, not an agent: it explicitly
	// refuses per-session MCP servers ("ACP bridge mode does not support
	// per-session MCP servers. Configure MCP on the OpenClaw gateway or agent
	// instead."). Unlike the three above it does not silently ignore the field —
	// session/new FAILS, so every turn would error out if we injected anything.
	//
	// The rejection is payload-dependent (measured 2026-10-24): an entry with
	// only {name, command} is accepted and ignored, while {name, command,
	// args: [], env: []} — exactly what toACPMcpServers emits — is refused.
	// Do not "fix" this by trimming empty slices; that would trade a hard
	// failure for a silently useless injection.
	"openclaw": true,
}

// BackendSupportsMCP reports whether the probe found this backend honoring the
// `mcpServers` field. Unknown backends are assumed to support it: the list above
// only contains measured negatives, and new backends should be probed rather
// than silently disabled.
func BackendSupportsMCP(backend string) bool {
	return !mcpUnsupportedBackends[backend]
}

// ResolveMCPServers returns the servers to inject for one agent, already
// filtered by policy, backend support and the agent's advertised capabilities.
//
// Package-level and snapshot-backed on purpose. It runs on the prompt hot path,
// where reading the mutable ConfigInstance would race with a concurrent PATCH
// (applyConfigPatch rewrites it in place under a lock this package cannot take).
//
// agentID (not *Agent) is equally deliberate: callers must pass the ID and let
// this function read the CURRENT agent record. ACPConn keeps a one-shot snapshot
// of the agent (newACPConn stores the pointer and never refreshes it) while
// UpdateAgent publishes a copy-on-write replacement, so reading that snapshot
// would make a policy change invisible to an existing connection forever.
//
// caps is the agent's advertised capability set. Callers that do not know it
// yet (before Initialize) must pass the zero value: only stdio is returned,
// which is always safe.
func ResolveMCPServers(agentID string, caps MCPCapabilities) []MCPServer {
	agent := GetAgent(agentID)
	if agent == nil {
		// No agent record means no policy to honor. Returning the global list
		// would apply MCP to something we cannot attribute; returning nothing
		// is the conservative choice.
		return nil
	}
	if !BackendSupportsMCP(agent.Backend) {
		return nil
	}

	policy := agent.MCPServers.Normalized()
	if policy.Mode == MCPPolicyNone {
		return nil
	}

	allowed := map[string]bool{}
	if policy.Mode == MCPPolicySelected {
		for _, n := range policy.Names {
			allowed[n] = true
		}
	}

	servers := MCPServersSnapshot()
	out := make([]MCPServer, 0, len(servers))
	for _, s := range servers {
		if policy.Mode == MCPPolicySelected && !allowed[s.Name] {
			continue
		}
		if !transportAllowed(s.Transport, caps) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// transportAllowed applies the capability gate. stdio is always allowed; the
// optional transports require the agent to have advertised them.
func transportAllowed(transport string, caps MCPCapabilities) bool {
	switch transport {
	case MCPTransportStdio:
		return true
	case MCPTransportHTTP:
		return caps.HTTP
	case MCPTransportSSE:
		return caps.SSE
	default:
		// Unknown transport: drop it rather than forwarding something the
		// agent will reject. Validation rejects unknown transports at save
		// time, so this only fires for a hand-edited config.yaml.
		return false
	}
}

// MCPPolicyVersion returns a stable hash of what this agent WOULD receive,
// ignoring capabilities.
//
// Capabilities are excluded on purpose. They are only known after Initialize,
// which happens inside spawnLocked — before every injection point. Computing the
// version from the filtered list would therefore hash "stdio only" at the moment
// the connection is created, while the session actually receives the full list,
// so the next turn would always see a mismatch and reconnect once per process.
//
// The cost of excluding them: adding an sse server for an agent that cannot use
// sse triggers one needless reconnect. That is far cheaper than a reconnect on
// every fresh process.
//
// An empty list returns "": nothing configured, nothing to compare.
func MCPPolicyVersion(agentID string) string {
	agent := GetAgent(agentID)
	if agent == nil || !BackendSupportsMCP(agent.Backend) {
		return ""
	}

	policy := agent.MCPServers.Normalized()
	if policy.Mode == MCPPolicyNone {
		return ""
	}

	allowed := map[string]bool{}
	if policy.Mode == MCPPolicySelected {
		for _, n := range policy.Names {
			allowed[n] = true
		}
	}

	servers := MCPServersSnapshot()
	selected := make([]MCPServer, 0, len(servers))
	for _, s := range servers {
		if policy.Mode == MCPPolicySelected && !allowed[s.Name] {
			continue
		}
		selected = append(selected, s)
	}
	if len(selected) == 0 {
		return ""
	}

	// Sorting is load-bearing. Env and Headers are maps, and Go randomizes map
	// iteration order, so an unsorted encoding would produce a different hash
	// on every call — turning the version check into "reconnect every turn".
	sum := sha256.Sum256([]byte(stableServersJSON(selected)))
	return hex.EncodeToString(sum[:8])
}

// stableServersJSON renders servers deterministically: slices in their declared
// order, map keys sorted.
func stableServersJSON(servers []MCPServer) string {
	type stableServer struct {
		Name      string            `json:"name"`
		Transport string            `json:"transport"`
		Command   string            `json:"command,omitempty"`
		Args      []string          `json:"args,omitempty"`
		Env       map[string]string `json:"env,omitempty"`
		URL       string            `json:"url,omitempty"`
		Headers   map[string]string `json:"headers,omitempty"`
	}
	// encoding/json sorts map keys, which is exactly the determinism we need.
	stable := make([]stableServer, 0, len(servers))
	for _, s := range servers {
		// The two structs are field-for-field identical apart from tags; a
		// conversion is the single place that mapping is expressed, so a new
		// field cannot be added to one side and silently dropped from the hash.
		stable = append(stable, stableServer(s))
	}
	b, err := json.Marshal(stable)
	if err != nil {
		// Marshal of these types cannot fail; fall back to a name-only
		// rendering rather than panicking inside a hot path.
		names := make([]string, 0, len(servers))
		for _, s := range servers {
			names = append(names, s.Name)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	return string(b)
}

// mcpSnapshot holds the current MCP config for lock-free reads.
//
// ConfigInstance is documented as "set once during startup, read-only
// afterwards", but applyConfigPatch rewrites it in place under the handler's
// configMutex — a lock that internal/ai cannot take (wrong dependency
// direction). Readers on the prompt hot path would race with a concurrent
// PATCH. Publishing an immutable snapshot through an atomic pointer gives those
// readers a value that never changes underneath them.
var mcpSnapshot atomic.Pointer[MCPConfig]

// PublishMCPSnapshot installs a deep copy of cfg as the value readers see.
// Called after a config patch (and at startup).
func PublishMCPSnapshot(cfg MCPConfig) {
	clone := MCPConfig{Servers: make([]MCPServer, 0, len(cfg.Servers))}
	for _, s := range cfg.Servers {
		clone.Servers = append(clone.Servers, s.clone())
	}
	mcpSnapshot.Store(&clone)
}

// MCPServersSnapshot returns the current server list. Safe to call from any
// goroutine; the returned slice must not be mutated by the caller.
func MCPServersSnapshot() []MCPServer {
	if snap := mcpSnapshot.Load(); snap != nil {
		return snap.Servers
	}
	return nil
}

// clone deep-copies the server so a later in-place edit of the source maps
// cannot leak into a published snapshot.
func (s MCPServer) clone() MCPServer {
	out := s
	if s.Args != nil {
		out.Args = append([]string(nil), s.Args...)
	}
	if s.Env != nil {
		out.Env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			out.Env[k] = v
		}
	}
	if s.Headers != nil {
		out.Headers = make(map[string]string, len(s.Headers))
		for k, v := range s.Headers {
			out.Headers[k] = v
		}
	}
	return out
}

// ValidateMCPServers checks a candidate list and returns the first problem.
//
// Called from the settings handler before persisting. Name/transport/URL rules
// are enforced here so the API layer does not have to duplicate them.
//
// The command check (exec.LookPath) lives in the handler, not here: it is
// environment-dependent and would make this package's tests depend on which
// binaries happen to be installed.
func ValidateMCPServers(servers []MCPServer) error {
	seen := make(map[string]bool, len(servers))
	for i, s := range servers {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("mcp.servers[%d].name must not be empty", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("mcp.servers[%d].name %q is duplicated", i, s.Name)
		}
		seen[s.Name] = true
		for _, r := range s.Name {
			if !isMCPNameChar(r) {
				return fmt.Errorf("mcp.servers[%d].name %q may only contain letters, digits, '_' and '-'", i, s.Name)
			}
		}

		switch s.Transport {
		case MCPTransportStdio:
			if strings.TrimSpace(s.Command) == "" {
				return fmt.Errorf("mcp.servers[%d].command is required for a stdio server", i)
			}
		case MCPTransportHTTP, MCPTransportSSE:
			if strings.TrimSpace(s.URL) == "" {
				return fmt.Errorf("mcp.servers[%d].url is required for a %s server", i, s.Transport)
			}
		default:
			return fmt.Errorf("mcp.servers[%d].transport must be one of: stdio, http, sse", i)
		}
	}
	return nil
}

func isMCPNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}
