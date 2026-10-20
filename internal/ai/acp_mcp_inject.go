package ai

import (
	"sort"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// mcpServersForSession returns the MCP servers to send on session/new, load or
// resume for this connection.
//
// Reading the policy through model.GetAgent (inside ResolveMCPServers) rather
// than c.agent is deliberate and load-bearing: ACPConn keeps a one-shot pointer
// to the agent (newACPConn stores it and nothing refreshes it), while
// model.UpdateAgent publishes a copy-on-write replacement. Reading the snapshot
// would make a policy edit invisible to an existing connection forever — the
// user unticks MCP for an agent and the next turn still sends it.
//
// Capabilities come from the registry, which is populated by Initialize. Before
// that (or after a restart, before the first session) the registry holds the
// values loaded from DB; if it holds nothing, only stdio is returned, which is
// always safe.
func (c *ACPConn) mcpServersForSession() []acp.McpServer {
	if c.agent == nil || c.agent.ID == "" {
		return nil
	}
	servers := model.ResolveMCPServers(c.agent.ID, GetAgentCapabilityRegistry().GetMCPCapabilities(c.agent.ID))
	return toACPMcpServers(servers)
}

// mcpPolicyVersion returns the hash of what this agent WOULD receive. See
// model.Config.MCPPolicyVersion for why capabilities are excluded.
func (c *ACPConn) mcpPolicyVersion() string {
	if c.agent == nil || c.agent.ID == "" {
		return ""
	}
	return model.MCPPolicyVersion(c.agent.ID)
}

// toACPMcpServers converts the config shape into the SDK's.
//
// The two are NOT the same: the config stores env/headers as maps (good for
// YAML and for the settings UI) while the SDK wants slices of name/value pairs.
// Both slices are built sorted by name so the wire payload is deterministic —
// tests assert on it, and map iteration order is randomized.
func toACPMcpServers(servers []model.MCPServer) []acp.McpServer {
	// MUST be a non-nil empty slice when there is nothing to send.
	//
	// The SDK's field is `json:"mcpServers"` WITHOUT omitempty, and every
	// request type's Validate() rejects a nil slice with "mcpServers is
	// required". Returning nil therefore serializes to null and fails
	// session/new, load and resume — a regression that made every agent with no
	// MCP servers unable to start a session at all.
	out := make([]acp.McpServer, 0, len(servers))
	if len(servers) == 0 {
		return out
	}
	for _, s := range servers {
		switch s.Transport {
		case model.MCPTransportStdio:
			out = append(out, acp.McpServer{Stdio: &acp.McpServerStdio{
				Name:    s.Name,
				Command: s.Command,
				Args:    nonNilStrings(s.Args),
				Env:     toEnvVariables(s.Env),
			}})
		case model.MCPTransportHTTP:
			out = append(out, acp.McpServer{Http: &acp.McpServerHttpInline{
				Name:    s.Name,
				Type:    model.MCPTransportHTTP,
				Url:     s.URL,
				Headers: toHTTPHeaders(s.Headers),
			}})
		case model.MCPTransportSSE:
			out = append(out, acp.McpServer{Sse: &acp.McpServerSseInline{
				Name:    s.Name,
				Type:    model.MCPTransportSSE,
				Url:     s.URL,
				Headers: toHTTPHeaders(s.Headers),
			}})
		}
	}
	return out
}

// nonNilStrings returns an empty slice rather than nil: the SDK marshals Args
// without omitempty, so nil becomes JSON null and strict consumers (codex-acp's
// zod schema validates `args` as an array) reject the whole session/new.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// toEnvVariables converts the env map to the SDK's name/value slice, sorted.
func toEnvVariables(env map[string]string) []acp.EnvVariable {
	// Same nil→null reason as nonNilStrings.
	if len(env) == 0 {
		return []acp.EnvVariable{}
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]acp.EnvVariable, 0, len(keys))
	for _, k := range keys {
		out = append(out, acp.EnvVariable{Name: k, Value: env[k]})
	}
	return out
}

// toHTTPHeaders converts the header map to the SDK's name/value slice, sorted.
func toHTTPHeaders(headers map[string]string) []acp.HttpHeader {
	if len(headers) == 0 {
		return []acp.HttpHeader{}
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]acp.HttpHeader, 0, len(keys))
	for _, k := range keys {
		out = append(out, acp.HttpHeader{Name: k, Value: headers[k]})
	}
	return out
}
