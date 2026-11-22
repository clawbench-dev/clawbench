package builtin

import (
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// maxHistoryImages bounds how many attached images are replayed with a turn.
//
// The history is rebuilt on EVERY turn, so every replayed image is re-sent each
// time. Without a cap a conversation with many screenshots grows without bound
// and every turn pays for pictures the model already described. The most recent
// turn is the one being asked about, so the cap applies per turn and keeps the
// first images of that turn.
const maxHistoryImages = 4

// maxHistoryImageBytes bounds one replayed image.
//
// Unlike the per-turn cap, exceeding this is REPORTED to the model: "the user
// attached a picture and the model cannot see it" is the exact failure this
// file exists to prevent, so it must never happen silently.
const maxHistoryImageBytes = 10 * 1024 * 1024

// replayImages attaches the image files of user turns as real image parts.
//
// The prompt already names each attachment ("[User uploaded 1 file(s): …]"), so
// the model knows a file exists — but a name is all it got. With only an image
// attached, the model read a sentence about a path, answered that sentence, and
// nothing in the reply showed it had seen a picture.
//
// Images are attached only when the model can read them. A text-only model
// keeps the name-only prompt instead of a request the endpoint rejects.
func replayImages(messages []llm.Message, history []ai.HistoryMessage, vision bool) []llm.Message {
	if !vision {
		return messages
	}
	// messages and history are parallel except where an assistant turn expanded
	// into an extra tool message. Advance the message index by the SAME
	// expansion rule buildHistoryReplay uses, so an image can never be appended
	// to a turn it does not belong to.
	mi := 0
	for _, h := range history {
		if mi >= len(messages) {
			break
		}
		switch h.Role {
		case "user":
			if h.Text == "" {
				continue // buildHistoryReplay skips a text-less user turn
			}
			messages[mi].Parts = appendUserImages(messages[mi].Parts, h.Files)
			mi++
		case "assistant":
			if h.Text == "" && len(h.Tools) == 0 {
				continue // not replayable
			}
			mi++
			if len(h.Tools) > 0 {
				mi++ // its tool-result message
			}
		}
	}
	return messages
}

// appendUserImages appends the readable image attachments to a user message's
// parts, after its text.
func appendUserImages(parts []llm.Part, files []model.FileEntry) []llm.Part {
	images, notes := loadAttachedImages(files, maxHistoryImages)
	for _, n := range notes {
		parts = append(parts, llm.Part{Text: "\n" + n})
	}
	for _, img := range images {
		parts = append(parts, llm.Part{Image: &img})
	}
	return parts
}

// loadAttachedImages reads the image attachments of one user turn.
//
// It returns the images to send plus one note per attachment that could not be
// sent, so the model knows a picture exists rather than answering about a
// conversation it cannot see. Paths are stored resolved by the handler, so an
// absolute path is the normal case.
func loadAttachedImages(files []model.FileEntry, limit int) ([]llm.Image, []string) {
	var images []llm.Image
	var notes []string
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if f.IsQuote() || f.IsURL() || f.IsDir || strings.TrimSpace(f.Path) == "" {
			continue
		}
		mediaType, ok := historyImageMediaType(f.Path)
		if !ok {
			continue
		}
		// The same file may be attached AND re-sent by the client's current-file
		// channel; one copy of the bytes is enough.
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		name := filepath.Base(f.Path)
		if len(images) >= limit {
			notes = append(notes, notAttachedNotice(name, "only the first few images of a message are sent"))
			continue
		}
		// One read serves both the size check and the payload. A separate Stat
		// would report a size the bytes may no longer match.
		data, err := os.ReadFile(f.Path)
		if err != nil {
			notes = append(notes, notAttachedNotice(name, "the file could not be opened"))
			continue
		}
		if len(data) > maxHistoryImageBytes {
			notes = append(notes, notAttachedNotice(name, "the file is too large to send"))
			continue
		}
		images = append(images, llm.Image{MediaType: mediaType, Data: data})
	}
	return images, notes
}

// notAttachedNotice tells the model an attachment existed but was not sent.
//
// Bracketed so it reads as frame information, not as the user's own words — the
// user message also carries the attachment header in the same style.
func notAttachedNotice(name, reason string) string {
	return "[image not attached: " + name + " — " + reason + "]"
}

// historyImageMediaType maps an extension to a media type both wire protocols
// accept.
//
// Deliberately narrower than model.IsImageFile: that list also covers formats
// the chat UI can render (.pdf, .svg, .bmp …) which the vision APIs reject, and
// sending one fails the whole turn with an opaque error.
func historyImageMediaType(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	return "", false
}
