package codebuddy

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai/acptest"
)

// The bridge only calls this for a completed update, so the name has to be
// resolvable from whatever the frame carries: the flat _meta name first, then
// the title (with the kind, when the frame has one).
func TestCodebuddyToolCallNameFromUpdate_MetaNameWins(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call-1",
		Title:      acptest.Patch("Edit file.go"),
		Meta: acptest.PatchMeta(map[string]any{
			"codebuddy.ai/toolName": "Bash",
		}),
	}
	if got := codebuddyToolCallNameFromUpdate(tcu); got != "Bash" {
		t.Fatalf("the _meta name must win over the title, got %q", got)
	}
}

func TestCodebuddyToolCallNameFromUpdate_FallsBackToTitleWithKind(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call-2",
		Title:      acptest.Patch("Edit file.go"),
		Kind:       acptest.Patch(acp.ToolKindEdit),
	}
	if got := codebuddyToolCallNameFromUpdate(tcu); got != "Edit" {
		t.Fatalf("expected the title-derived name, got %q", got)
	}
}

func TestCodebuddyToolCallNameFromUpdate_FallsBackToTitleWithoutKind(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call-3",
		Title:      acptest.Patch("Edit file.go"),
	}
	if got := codebuddyToolCallNameFromUpdate(tcu); got != "Edit" {
		t.Fatalf("expected the title-derived name, got %q", got)
	}
}

func TestCodebuddyToolCallNameFromUpdate_NoNameAtAll(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{ToolCallId: "call-4"}
	if got := codebuddyToolCallNameFromUpdate(tcu); got != "" {
		t.Fatalf("a frame with neither name nor title must yield empty, got %q", got)
	}
}
