package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// writePNG writes a tiny file the replay path reads as an image. The bytes are
// never decoded here — the contract under test is which parts reach the model,
// not whether the pixels are valid.
func writeImage(t *testing.T, dir, name string, size int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// userTurnWithImage is the shape the service layer hands over for a message
// that only carried a picture: the stored prompt is the attachment header, and
// the file rides Files.
func userTurnWithImage(path string) []ai.HistoryMessage {
	return []ai.HistoryMessage{{
		Role:  "user",
		Text:  "[User uploaded 1 file(s): " + path + "]\n",
		ID:    1,
		Files: []model.FileEntry{{Path: path}},
	}}
}

// The core regression: an attached image must reach the model as an IMAGE part,
// not merely as a sentence naming its path.
func TestReplayImages_AttachesUserImage(t *testing.T) {
	path := writeImage(t, t.TempDir(), "shot.png", 8)
	history := userTurnWithImage(path)
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, true)
	if len(got) != 1 {
		t.Fatalf("expected one message, got %d", len(got))
	}
	parts := got[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected text + image, got %+v", parts)
	}
	if parts[0].Text == "" {
		t.Fatal("the user's text must stay first")
	}
	if parts[1].Image == nil {
		t.Fatalf("the attachment must be an image part, got %+v", parts[1])
	}
	if parts[1].Image.MediaType != "image/png" {
		t.Fatalf("unexpected media type %q", parts[1].Image.MediaType)
	}
	if len(parts[1].Image.Data) != 8 {
		t.Fatalf("the file bytes must be sent, got %d bytes", len(parts[1].Image.Data))
	}
}

// A text-only model must keep the name-only prompt: an image part would make
// every turn fail at the endpoint.
func TestReplayImages_NoVisionKeepsTextOnly(t *testing.T) {
	path := writeImage(t, t.TempDir(), "shot.png", 8)
	history := userTurnWithImage(path)
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, false)
	for _, p := range got[0].Parts {
		if p.Image != nil {
			t.Fatal("a text-only model must not receive an image part")
		}
	}
}

// The replay alignment is the trap this guards: an assistant turn with tool
// calls expands into TWO messages, so a later user turn's image must land on
// the user message and not on the tool result.
func TestReplayImages_LandsOnTheRightMessageAfterToolTurn(t *testing.T) {
	path := writeImage(t, t.TempDir(), "after.png", 4)
	history := []ai.HistoryMessage{
		{Role: "user", Text: "do it", ID: 1},
		{Role: "assistant", Text: "running", ID: 2, Tools: []ai.HistoryToolCall{{ID: "c1", Name: "bash", Output: "ok"}}},
		{Role: "user", Text: "now look", ID: 3, Files: []model.FileEntry{{Path: path}}},
	}
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages
	if len(messages) != 4 {
		t.Fatalf("expected user, assistant, tool, user; got %d", len(messages))
	}

	got := replayImages(messages, history, true)
	for i, m := range got {
		hasImage := false
		for _, p := range m.Parts {
			if p.Image != nil {
				hasImage = true
			}
		}
		if want := i == 3; hasImage != want {
			t.Fatalf("message %d (%s): image=%v, want %v", i, m.Role, hasImage, want)
		}
	}
}

// A picture the model cannot receive must be NAMED in the conversation. Silence
// is what made the failure invisible: the model read "a.png" and answered as if
// nothing was there.
func TestReplayImages_OversizeImageIsReported(t *testing.T) {
	path := writeImage(t, t.TempDir(), "huge.png", maxHistoryImageBytes+1)
	history := userTurnWithImage(path)
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, true)
	var note string
	for _, p := range got[0].Parts {
		if p.Image != nil {
			t.Fatal("an oversize image must not be sent")
		}
		if strings.Contains(p.Text, "huge.png") {
			note = p.Text
		}
	}
	if !strings.Contains(note, "image not attached") {
		t.Fatalf("the model must be told the image was not attached, got parts %+v", got[0].Parts)
	}
}

// Only the first images of a message are sent; the rest are reported, so the
// model knows the message had more pictures than it can see.
func TestReplayImages_RespectsPerTurnCap(t *testing.T) {
	dir := t.TempDir()
	var files []model.FileEntry
	for i := 0; i < maxHistoryImages+1; i++ {
		files = append(files, model.FileEntry{Path: writeImage(t, dir, string(rune('a'+i))+".png", 4)})
	}
	history := []ai.HistoryMessage{{Role: "user", Text: "many", ID: 1, Files: files}}
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, true)
	sent, reported := 0, false
	for _, p := range got[0].Parts {
		switch {
		case p.Image != nil:
			sent++
		case strings.Contains(p.Text, "only the first few images"):
			reported = true
		}
	}
	if sent != maxHistoryImages {
		t.Fatalf("expected %d images, got %d", maxHistoryImages, sent)
	}
	if !reported {
		t.Fatal("the dropped image must be reported to the model")
	}
}

// Formats the vision APIs do not accept must never be sent: one of them fails
// the whole turn with an opaque endpoint error.
func TestReplayImages_SkipsUnsupportedFormats(t *testing.T) {
	dir := t.TempDir()
	history := []ai.HistoryMessage{{Role: "user", Text: "docs", ID: 1, Files: []model.FileEntry{
		{Path: writeImage(t, dir, "scan.pdf", 4)},
		{Path: writeImage(t, dir, "logo.svg", 4)},
		{Path: writeImage(t, dir, "notes.txt", 4)},
		{Path: writeImage(t, dir, "shot.webp", 4)},
	}}}
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, true)
	var types []string
	for _, p := range got[0].Parts {
		if p.Image != nil {
			types = append(types, p.Image.MediaType)
		}
	}
	if len(types) != 1 || types[0] != "image/webp" {
		t.Fatalf("only the webp may be sent, got %v", types)
	}
}

// A missing file is reported rather than dropped: the bubble still shows the
// attachment, so the model must not answer as if the user had sent none.
func TestReplayImages_MissingFileIsReported(t *testing.T) {
	history := userTurnWithImage(filepath.Join(t.TempDir(), "gone.jpg"))
	messages := buildHistoryReplay(ai.ChatRequest{History: history}).Messages

	got := replayImages(messages, history, true)
	if len(got[0].Parts) != 2 || !strings.Contains(got[0].Parts[1].Text, "could not be opened") {
		t.Fatalf("a missing attachment must be reported, got %+v", got[0].Parts)
	}
}
