// Package mcptools adapts MCP servers to the built-in agent's tool interface.
//
// The adapter is deliberately thin: it renames, caps and maps results. The
// protocol lives in internal/mcp, and the tool loop stays unaware that MCP
// exists — it sees a tools.Tool like read or bash.
//
// Two things are NOT thin and must stay here:
//
//   - Names are namespaced and sanitized. Providers cap tool names at 64
//     characters from [A-Za-z0-9_-], and MCP servers are free to publish
//     anything (dots, spaces, CJK). Passing those through is a 400 the user
//     cannot diagnose.
//   - A tool list is capped. Every tool's schema is sent with every model call;
//     a server with 400 tools would spend the whole context window on
//     declarations.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clawbench/internal/agentcore/tools"
	"clawbench/internal/mcp"
)

const (
	// maxToolNameLen is the strictest provider limit (OpenAI's function name).
	maxToolNameLen = 64
	// maxDescriptionRunes bounds the schema description. MCP descriptions are
	// sometimes a whole README; the model needs the first useful sentence.
	maxDescriptionRunes = 1024
	// maxSnippetRunes bounds the one-line entry in the system prompt's tool
	// list, which lists every tool.
	maxSnippetRunes = 80
	// maxTools bounds how many MCP tools are attached in one turn.
	maxTools = 100
)

// Server is the slice of one connected MCP server this package needs.
//
// An interface rather than *mcp.Session so the adapter can be tested without a
// process or an HTTP server.
type Server interface {
	ServerName() string
	Tools() []mcp.ToolDef
	Call(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error)
}

// FromBundle converts a pool bundle into servers for Build.
func FromBundle(b *mcp.Bundle) []Server {
	if b == nil {
		return nil
	}
	out := make([]Server, 0, len(b.Servers))
	for _, cs := range b.Servers {
		if cs != nil {
			out = append(out, cs)
		}
	}
	return out
}

// Build turns connected servers into tools the agent loop can call.
//
// supportsVision decides whether an image result is handed to the model or
// replaced with a note: sending an image to a text-only model produces a 400
// the user cannot diagnose.
func Build(servers []Server, supportsVision bool) ([]tools.Tool, []string) {
	var (
		out   []tools.Tool
		notes []string
		used  = make(map[string]bool)
	)

	for _, srv := range servers {
		for _, def := range srv.Tools() {
			if len(out) >= maxTools {
				notes = append(notes, fmt.Sprintf(
					"only the first %d MCP tools are available; narrow the agent's MCP selection to expose the rest",
					maxTools,
				))
				return out, notes
			}
			name := uniqueName(used, srv.ServerName(), def.Name)
			out = append(out, &tool{
				server: srv.ServerName(),
				srv:    srv,
				def:    def,
				name:   name,
				vision: supportsVision,
			})
		}
	}
	return out, notes
}

// tool is one MCP tool exposed to the loop.
type tool struct {
	server string
	srv    Server
	def    mcp.ToolDef
	name   string
	vision bool
}

func (t *tool) Name() string { return t.name }

func (t *tool) Snippet() string {
	if s := firstLine(t.def.Description); s != "" {
		return truncateRunes(s, maxSnippetRunes)
	}
	return fmt.Sprintf("%q tool from the %q MCP server", t.def.Name, t.server)
}

// Guidelines returns nil: MCP tools have no shared footguns. The boundary rule
// (these tools reach outside the working directory) is stated once in the
// prompt's rules section, not repeated per tool.
func (t *tool) Guidelines() []string { return nil }

func (t *tool) Spec() tools.Spec {
	return tools.Spec{
		Name:        t.name,
		Description: t.description(),
		Parameters:  schema(t.def.InputSchema),
	}
}

func (t *tool) description() string {
	d := strings.TrimSpace(t.def.Description)
	if d == "" {
		d = fmt.Sprintf("MCP tool %q provided by the %q server.", t.def.Name, t.server)
	}
	return truncateRunes(d, maxDescriptionRunes)
}

func (t *tool) Run(env tools.Env, raw json.RawMessage) tools.Result {
	ctx := env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := t.srv.Call(ctx, t.def.Name, raw)
	if err != nil {
		// A cancelled turn is not a tool failure the model should retry; saying
		// so stops it from immediately calling the same tool again.
		if ctx.Err() != nil {
			return tools.Result{
				Text:    fmt.Sprintf("MCP tool %s.%s was cancelled.", t.server, t.def.Name),
				IsError: true,
			}
		}
		return tools.Result{
			Text:    fmt.Sprintf("MCP tool %s.%s failed: %v", t.server, t.def.Name, err),
			IsError: true,
		}
	}

	text := res.Text
	var image *tools.Image
	if len(res.Images) > 0 {
		if t.vision {
			image = &tools.Image{MediaType: res.Images[0].MediaType, Data: res.Images[0].Data}
			if len(res.Images) > 1 {
				text += fmt.Sprintf("\n(%d more image(s) were returned and are not shown.)", len(res.Images)-1)
			}
		} else {
			text += fmt.Sprintf("\n(The tool returned %d image(s); this model cannot view images.)", len(res.Images))
		}
	}
	if strings.TrimSpace(text) == "" {
		text = "(the tool returned no output)"
	}

	return tools.Result{
		Text:    text,
		Image:   image,
		IsError: res.IsError,
		Display: t.server + " · " + t.def.Name,
	}
}

// uniqueName builds a provider-safe name and resolves collisions.
func uniqueName(used map[string]bool, server, tool string) string {
	base := tools.MCPNamePrefix + sanitize(server) + "__" + sanitize(tool)
	name := truncateName(base)
	if !used[name] {
		used[name] = true
		return name
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("_%d", i)
		candidate := truncateWithSuffix(base, suffix)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// sanitize keeps the characters every provider accepts. Replacing rather than
// dropping keeps two different names from collapsing into the same string in a
// way the collision suffix would hide.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "tool"
	}
	return out
}

func truncateName(s string) string {
	if len(s) <= maxToolNameLen {
		return s
	}
	return s[:maxToolNameLen]
}

// truncateWithSuffix shortens base so that base+suffix fits the provider limit.
func truncateWithSuffix(base, suffix string) string {
	limit := maxToolNameLen - len(suffix)
	if limit < 1 {
		limit = 1
	}
	if len(base) > limit {
		base = base[:limit]
	}
	return base + suffix
}

// schema copies the server's declaration, dropping keys some gateways reject.
//
// The copy matters: ToolDef is shared across turns, and mutating it in place
// would leak the sanitized version into the next Build.
func schema(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	delete(out, "$schema")
	delete(out, "$id")
	delete(out, "title")
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
