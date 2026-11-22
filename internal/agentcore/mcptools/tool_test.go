package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/agentcore/tools"
	"clawbench/internal/mcp"
)

type fakeServer struct {
	name  string
	tools []mcp.ToolDef
	call  func(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error)
}

func (f *fakeServer) ServerName() string   { return f.name }
func (f *fakeServer) Tools() []mcp.ToolDef { return f.tools }
func (f *fakeServer) Call(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error) {
	if f.call != nil {
		return f.call(ctx, tool, args)
	}
	return mcp.CallResult{}, nil
}

// TestBuild_NamespacesAndSanitizes pins the provider-safe name. A dot or a
// space in a server's tool name is a 400 from the model API, not a warning.
func TestBuild_NamespacesAndSanitizes(t *testing.T) {
	servers := []Server{&fakeServer{
		name: "git hub",
		tools: []mcp.ToolDef{
			{Name: "create.issue"},
			{Name: "中文工具"},
		},
	}}

	got, notes := Build(servers, true)
	require.Empty(t, notes)
	require.Len(t, got, 2)
	assert.Equal(t, "mcp__git_hub__create_issue", got[0].Name())
	assert.Equal(t, "mcp__git_hub__tool", got[1].Name())
	for _, tool := range got {
		assert.LessOrEqual(t, len(tool.Name()), maxToolNameLen)
		assert.Regexp(t, `^[A-Za-z0-9_-]+$`, tool.Name())
	}
}

// TestBuild_DedupesCollisions covers two servers whose names sanitize to the
// same string: the second tool must get a suffix rather than be dropped or
// declared twice.
func TestBuild_DedupesCollisions(t *testing.T) {
	servers := []Server{
		&fakeServer{name: "a.b", tools: []mcp.ToolDef{{Name: "run"}}},
		&fakeServer{name: "a_b", tools: []mcp.ToolDef{{Name: "run"}}},
	}
	got, _ := Build(servers, true)
	require.Len(t, got, 2)
	assert.Equal(t, "mcp__a_b__run", got[0].Name())
	assert.Equal(t, "mcp__a_b__run_2", got[1].Name())
}

// TestBuild_TruncatesLongNames keeps the provider limit even when both halves
// are long.
func TestBuild_TruncatesLongNames(t *testing.T) {
	servers := []Server{&fakeServer{
		name:  strings.Repeat("s", 40),
		tools: []mcp.ToolDef{{Name: strings.Repeat("t", 40)}},
	}}
	got, _ := Build(servers, true)
	require.Len(t, got, 1)
	assert.Len(t, got[0].Name(), maxToolNameLen)
}

// TestBuild_CapsToolCount protects the context window.
func TestBuild_CapsToolCount(t *testing.T) {
	defs := make([]mcp.ToolDef, maxTools+3)
	for i := range defs {
		defs[i] = mcp.ToolDef{Name: "t" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)}
	}
	got, notes := Build([]Server{&fakeServer{name: "big", tools: defs}}, true)
	assert.Len(t, got, maxTools)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "narrow")
}

// TestBuild_FairShareAcrossServers pins the round-robin: one huge server must
// not starve the others out of the tool list entirely.
func TestBuild_FairShareAcrossServers(t *testing.T) {
	many := make([]mcp.ToolDef, maxTools*2)
	for i := range many {
		many[i] = mcp.ToolDef{Name: "a" + strings.Repeat("x", i)}
	}
	got, notes := Build([]Server{
		&fakeServer{name: "big", tools: many},
		&fakeServer{name: "small", tools: []mcp.ToolDef{{Name: "only"}}},
	}, true)

	require.Len(t, got, maxTools)
	assert.Contains(t, toolNamesOf(got), "mcp__small__only", "the small server must not be starved")
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "big")
	assert.NotContains(t, notes[0], "small")
}

func toolNamesOf(list []tools.Tool) []string {
	names := make([]string, 0, len(list))
	for _, t := range list {
		names = append(names, t.Name())
	}
	return names
}

// TestSpec_StripsUnsupportedSchemaKeys pins the schema copy: some gateways
// reject $schema/title, and mutating the shared ToolDef would leak into the
// next turn.
func TestSpec_StripsUnsupportedSchemaKeys(t *testing.T) {
	def := mcp.ToolDef{
		Name:        "run",
		Description: "Run it.",
		InputSchema: map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"title":   "Run",
			"$id":     "run",
			"type":    "object",
			"properties": map[string]any{
				"x": map[string]any{"type": "string"},
			},
		},
	}
	got, _ := Build([]Server{&fakeServer{name: "s", tools: []mcp.ToolDef{def}}}, true)
	require.Len(t, got, 1)

	spec := got[0].Spec()
	assert.NotContains(t, spec.Parameters, "$schema")
	assert.NotContains(t, spec.Parameters, "title")
	assert.NotContains(t, spec.Parameters, "$id")
	assert.Equal(t, "object", spec.Parameters["type"])
	assert.Contains(t, def.InputSchema, "$schema", "the source declaration must not be mutated")
}

// TestSpec_DefaultsMissingType covers servers that omit `type`.
func TestSpec_DefaultsMissingType(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run", InputSchema: nil}},
	}}, true)
	require.Len(t, got, 1)
	assert.Equal(t, map[string]any{"type": "object"}, got[0].Spec().Parameters)
}

// TestSnippet_UsesFirstLine keeps the prompt's tool list to one line per tool.
func TestSnippet_UsesFirstLine(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name: "s",
		tools: []mcp.ToolDef{{
			Name:        "run",
			Description: "  First line.  \nSecond line that must not appear.",
		}},
	}}, true)
	require.Len(t, got, 1)
	assert.Equal(t, "First line.", got[0].Snippet())

	empty, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
	}}, true)
	assert.Contains(t, empty[0].Snippet(), "s")
}

// TestRun_MapsTextAndImage covers the happy path for both content shapes.
func TestRun_MapsTextAndImage(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
		call: func(_ context.Context, _ string, _ json.RawMessage) (mcp.CallResult, error) {
			return mcp.CallResult{
				Text:   "done",
				Images: []mcp.Image{{MediaType: "image/png", Data: []byte{1, 2}}},
			}, nil
		},
	}}, true)
	require.Len(t, got, 1)

	res := got[0].Run(tools.Env{Ctx: context.Background()}, json.RawMessage(`{}`))
	assert.False(t, res.IsError)
	assert.Equal(t, "done", res.Text)
	require.NotNil(t, res.Image)
	assert.Equal(t, []byte{1, 2}, res.Image.Data)
}

// TestRun_TextOnlyModelGetsANote stops an image from reaching a model that
// cannot read it (which is a 400 the user cannot diagnose).
func TestRun_TextOnlyModelGetsANote(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
		call: func(_ context.Context, _ string, _ json.RawMessage) (mcp.CallResult, error) {
			return mcp.CallResult{Images: []mcp.Image{{MediaType: "image/png", Data: []byte{1}}}}, nil
		},
	}}, false)
	require.Len(t, got, 1)

	res := got[0].Run(tools.Env{Ctx: context.Background()}, json.RawMessage(`{}`))
	assert.Nil(t, res.Image)
	assert.Contains(t, res.Text, "cannot view images")
}

// TestRun_ErrorsAreReadable pins the two failure texts: a protocol error and a
// cancellation are different messages, because a cancelled tool must not invite
// an immediate retry.
func TestRun_ErrorsAreReadable(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
		call: func(_ context.Context, _ string, _ json.RawMessage) (mcp.CallResult, error) {
			return mcp.CallResult{}, errors.New("server exploded")
		},
	}}, true)

	res := got[0].Run(tools.Env{Ctx: context.Background()}, json.RawMessage(`{}`))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "server exploded")
	assert.Contains(t, res.Text, "s.run")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res = got[0].Run(tools.Env{Ctx: ctx}, json.RawMessage(`{}`))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "cancelled")
}

// TestRun_ServerIsErrorReachesTheLoop preserves isError, which the UI shows as
// a failed tool card.
func TestRun_ServerIsErrorReachesTheLoop(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
		call: func(_ context.Context, _ string, _ json.RawMessage) (mcp.CallResult, error) {
			return mcp.CallResult{Text: "no such issue", IsError: true}, nil
		},
	}}, true)

	res := got[0].Run(tools.Env{Ctx: context.Background()}, json.RawMessage(`{}`))
	assert.True(t, res.IsError)
	assert.Equal(t, "no such issue", res.Text)
}

// TestRun_EmptyResultHasAnExplanation: a silent tool must not look like a bug.
func TestRun_EmptyResultHasAnExplanation(t *testing.T) {
	got, _ := Build([]Server{&fakeServer{
		name:  "s",
		tools: []mcp.ToolDef{{Name: "run"}},
	}}, true)
	res := got[0].Run(tools.Env{Ctx: context.Background()}, json.RawMessage(`{}`))
	assert.Equal(t, "(the tool returned no output)", res.Text)
}

// TestFromBundle_Nil covers the "no MCP configured" path.
func TestFromBundle_Nil(t *testing.T) {
	assert.Empty(t, FromBundle(nil))
	assert.Empty(t, FromBundle(&mcp.Bundle{}))
}
