package builtin

import (
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// maxTurnImages bounds how many pictures one message may send.
//
// Only THIS turn's images are sent (see buildSpec) — replaying them from the
// whole history made every turn pay again for pictures the model had already
// described. The cap is a separate, smaller guard: a message with twenty
// screenshots still fits one request.
const maxTurnImages = 4

// maxTurnImageBytes bounds one image.
//
// Exceeding it is REPORTED to the model rather than skipped: "the user attached
// a picture and the model cannot see it" is the failure this file exists to
// prevent, and silence is what made it invisible.
const maxTurnImageBytes = 10 * 1024 * 1024

// turnImageParts renders this turn's image attachments as message parts.
//
// The prompt already names each attachment ("[User uploaded 1 file(s): …]"), so
// the model knows a file exists — but a name is all it got. With only an image
// attached, the model read a sentence about a path, answered that sentence, and
// nothing in the reply showed it had seen a picture.
//
// A picture that is already an internet address is passed as the address: the
// endpoint fetches it itself, so no bytes and no download on our side. A local
// file has no address anyone else can open, so its bytes are the only way.
//
// The bool is false when the model cannot read images at all; the caller then
// keeps the name-only prompt instead of a request the endpoint rejects.
func turnImageParts(files []model.FileEntry) ([]llm.Part, []string) {
	var parts []llm.Part
	var notes []string
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if f.IsQuote() || f.IsDir {
			continue
		}
		if f.IsURL() {
			// Only an address that points at an image is worth sending; anything
			// else stays a link in the prompt text.
			if _, ok := imageMediaType(f.URL); !ok {
				continue
			}
			if seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			if len(parts) >= maxTurnImages {
				notes = append(notes, notAttachedNotice(f.URL, "only the first few images of a message are sent"))
				continue
			}
			parts = append(parts, llm.Part{Image: &llm.Image{URL: f.URL}})
			continue
		}
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		mediaType, ok := imageMediaType(path)
		if !ok {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		name := filepath.Base(path)
		if len(parts) >= maxTurnImages {
			notes = append(notes, notAttachedNotice(name, "only the first few images of a message are sent"))
			continue
		}
		// One read serves both the size check and the payload: a separate Stat
		// could report a size the bytes no longer match.
		data, err := os.ReadFile(path)
		if err != nil {
			notes = append(notes, notAttachedNotice(name, "the file could not be opened"))
			continue
		}
		if len(data) > maxTurnImageBytes {
			notes = append(notes, notAttachedNotice(name, "the file is too large to send"))
			continue
		}
		parts = append(parts, llm.Part{Image: &llm.Image{MediaType: mediaType, Data: data}})
	}
	return parts, notes
}

// notAttachedNotice tells the model an attachment existed but was not sent.
//
// Bracketed so it reads as frame information, not as the user's own words — the
// attachment header in the prompt uses the same style.
func notAttachedNotice(name, reason string) string {
	return "[image not attached: " + name + " — " + reason + "]"
}

// imageMediaType maps a path or address to a media type both wire protocols
// accept.
//
// Deliberately narrower than model.IsImageFile: that list also covers formats
// the chat UI can render (.pdf, .svg, .bmp …) which the vision APIs reject, and
// sending one fails the whole turn with an opaque error.
func imageMediaType(pathOrURL string) (string, bool) {
	p := pathOrURL
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	switch strings.ToLower(filepath.Ext(p)) {
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
