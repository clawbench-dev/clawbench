package handler

import (
	"os"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A whole-file attachment travels ONLY the legacy filePaths channel, while an
// upload travels as a structured entry. Reading one channel alone therefore
// makes the same picture visible or invisible depending on how it was attached —
// which is exactly what happened: an image picked from the file manager reached
// the model as a bare "[Current file: …]" line.
func TestTurnFilesForModel_MergesBothChannels(t *testing.T) {
	got := turnFilesForModel(
		[]model.FileEntry{{Path: "/p/uploaded.png"}},
		[]string{"/p/picked.png", "/p/uploaded.png", ""},
	)
	require.Len(t, got, 2, "both channels, without duplicates or empty paths: %+v", got)
	assert.Equal(t, "/p/uploaded.png", got[0].Path)
	assert.Equal(t, "/p/picked.png", got[1].Path)
}

// No legacy paths means the structured list is returned untouched — the common
// case, and it must not pay for an allocation or reorder entries.
func TestTurnFilesForModel_NoLegacyPathsKeepsList(t *testing.T) {
	files := []model.FileEntry{{Path: "/p/a.png"}}
	got := turnFilesForModel(files, nil)
	require.Len(t, got, 1)
	assert.Equal(t, files[0], got[0])
}

// The wiring: a functional test cannot reach these lines (the direct-send
// branch only runs inside a live HTTP turn, and buildChatRequestFromQueue needs
// a whole session), so the call sites are pinned at the source.
//
// Losing either one is silent: the picture is still named in the prompt, so
// nothing errors — the model just never sees it.
func TestChatSendPathsPassTurnFiles(t *testing.T) {
	src, err := os.ReadFile("chat.go")
	require.NoError(t, err)
	text := strings.ReplaceAll(string(src), "\r\n", "\n")

	direct := funcBody(text, "func AIChat(")
	require.Contains(t, direct, "service.WithTurnFiles(turnFiles)",
		"the direct send must hand this turn's attachments to the backend")

	queued := funcBody(text, "func buildChatRequestFromQueue(")
	require.Contains(t, queued, "service.WithTurnFiles(qMsg.Files)",
		"a drained message must hand its attachments to the backend")

	// And the direct-send call site must pass the MERGED list, not allFiles:
	// otherwise the filePaths channel is dropped again.
	require.Contains(t, direct, "turnFilesForModel(allFiles, validatedFilePaths)")
}

// funcBody returns the source of one top-level function, from its declaration to
// the next top-level closing brace.
func funcBody(src, decl string) string {
	start := strings.Index(src, decl)
	if start < 0 {
		return ""
	}
	rest := src[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
