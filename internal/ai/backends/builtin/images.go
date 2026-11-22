package builtin

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// maxTurnImages bounds how many pictures are sent as BYTES for one message.
//
// Only the fallback path sends bytes (the agent normally opens the file itself,
// see readableImageHints), and a message with twenty screenshots would otherwise
// put all of them in one request. The pictures over the cap are still named, so
// the model can open them by path.
const maxTurnImages = 4

// maxTurnImageBytes bounds one image.
//
// Exceeding it is REPORTED to the model rather than skipped: "the user attached
// a picture and the model cannot see it" is the failure this file exists to
// prevent, and silence is what made it invisible.
const maxTurnImageBytes = 10 * 1024 * 1024

// readableImageHints names the pictures the agent can open for itself.
//
// Preferred over sending the bytes: the agent's read tool returns an image, so
// the model can look whenever it actually needs to. The path is the handle — it
// stays in the transcript, so a later turn can re-open an older picture without
// anyone re-sending it.
//
// It returns hints for the files the read tool can reach, plus the ones it
// cannot (outside the working directory while the policy confines paths, or no
// read capability at all). The caller sends those inline instead, because a
// path the agent cannot open is a picture nobody sees.
func readableImageHints(files []model.FileEntry, workDir string, canRead, confined bool) ([]string, []model.FileEntry) {
	var hints []string
	var fallback []model.FileEntry
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if f.IsQuote() || f.IsDir {
			continue
		}
		// A remote picture has no local file to read; it keeps the address.
		if f.IsURL() {
			if _, ok := imageMediaType(f.URL); !ok || seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			fallback = append(fallback, f)
			continue
		}
		path := strings.TrimSpace(f.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if _, ok := imageMediaType(path); !ok {
			continue
		}
		if canRead && (!confined || withinDir(workDir, path)) {
			hints = append(hints, path)
			continue
		}
		fallback = append(fallback, f)
	}
	return hints, fallback
}

// withinDir reports whether path sits inside dir.
//
// A path that cannot be compared (relative, or dir unknown) is treated as
// outside: the caller then sends the picture inline, which always works.
func withinDir(dir, path string) bool {
	if dir == "" || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

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
	var dropped []string
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
				dropped = append(dropped, f.URL)
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
			dropped = append(dropped, name)
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
	// One line for the whole overflow, not one per picture: ten screenshots over
	// the cap used to produce ten identical sentences.
	if len(dropped) > 0 {
		notes = append(notes, notAttachedNotice(strings.Join(dropped, ", "),
			"only the first "+strconv.Itoa(maxTurnImages)+" images of a message are sent"))
	}
	return parts, notes
}

// imageHint points the model at one picture it can open itself.
//
// One line per picture rather than a comma-joined list: a path may itself
// contain a comma or a bracket, and a joined list would then read as several
// paths — the model would call read on a path that does not exist.
//
// The path comes LAST so the closing bracket can never cut it short, and the
// wording is an instruction rather than a label: the standing media rule says
// not to read a file unless the intent is clear, and "the user attached this"
// is that clarity.
func imageHint(path string) string {
	return "[User attached an image — view it with the read tool before answering: " + path + "]"
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
