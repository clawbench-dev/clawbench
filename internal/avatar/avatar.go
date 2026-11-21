// Package avatar owns the validation of every stored avatar value.
//
// A stored avatar is either a raw SVG string (DiceBear-generated or picked in
// the settings dialog) or a `data:image/...` URI produced by the image-upload
// path. Both shapes are written by more than one entry point (the agent PATCH,
// the group-member endpoint, and YAML import), so the rules live here rather
// than in any one HTTP handler.
package avatar

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg" // register the JPEG decoder for image.DecodeConfig
	_ "image/png"  // register the PNG decoder for image.DecodeConfig
	"regexp"
	"strings"

	_ "golang.org/x/image/webp" // register the WebP decoder for image.DecodeConfig
)

// MaxBytes caps the stored string. Real DiceBear SVGs are ~1–8 KB and a 192px
// uploaded image is a few tens of KB; 256 KiB is generous headroom for either.
const MaxBytes = 256 * 1024

// MaxDecodedBytes bounds the decoded raster payload. A 192x192 WebP or JPEG is
// a few tens of KB; 160 KiB also leaves room for the PNG last resort.
const MaxDecodedBytes = 160 * 1024

// MaxDimension bounds the decoded raster's width/height. The client produces
// 192px; 512 leaves room for a future size change without accepting wallpapers.
const MaxDimension = 512

// dataURIRE matches the only raster forms the picker produces. The payload is
// standard base64 (no whitespace) because it comes from canvas.toDataURL.
var dataURIRE = regexp.MustCompile(`(?i)^data:image/(png|jpeg|webp);base64,([A-Za-z0-9+/=]+)$`)

// Validate reports whether a stored avatar is acceptable.
func Validate(src []byte) bool {
	if len(src) == 0 {
		return false
	}
	if m := dataURIRE.FindSubmatch(src); m != nil {
		return rasterLooksSafe(strings.ToLower(string(m[1])), m[2])
	}
	return svgLooksSafe(src)
}

// rasterLooksSafe validates an uploaded image without trusting the client's
// declared MIME. It decodes the base64, caps the decoded size, and asks the
// image package for the real format and dimensions. The rendered surface is an
// <img>, so a malformed image is inert; this gate is about bounding work and
// keeping the stored value to a sane shape.
func rasterLooksSafe(declared string, payload []byte) bool {
	if len(payload) == 0 || len(payload) > (MaxDecodedBytes*4/3)+8 {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil || len(decoded) == 0 || len(decoded) > MaxDecodedBytes {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		return false
	}
	if !strings.EqualFold(format, declared) {
		return false
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return false
	}
	return true
}

// svgLooksSafe is the safety gate for a user-supplied SVG.
//
// It is deliberately NOT wallpaper.SVGLooksSafe: that helper rejects ANY
// `href=` / `<use>`, because it guards wallpapers rendered as a CSS background
// where same-document references are pointless. DiceBear's rendered output
// legitimately emits same-document fragment references
// (`<use href="#texture-..."/>`, `fill="url(#...)"/>`) for its texture/color
// layers, so reusing it would reject every real DiceBear avatar. Here we only
// forbid *external* references and script vectors.
//
// This is a best-effort structural gate; the authoritative XSS boundary is that
// the client renders the stored SVG inside an <img> (sandboxed: no script
// execution, no external resource loads).
func svgLooksSafe(src []byte) bool {
	// Structural sanity: must look like an SVG document.
	head := src
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
		return false
	}

	lower := bytes.ToLower(src)
	for _, forbidden := range [][]byte{
		[]byte("<script"),
		[]byte("foreignobject"),
		[]byte("<image"),
		[]byte("javascript:"),
		[]byte("<iframe"),
		[]byte("<embed"),
		[]byte("<object"),
		[]byte("<style"), // no CSS import / url() exfiltration vectors
	} {
		if bytes.Contains(lower, forbidden) {
			return false
		}
	}
	// Reject any inline event handler (on*="...") — e.g. onload=, onerror=.
	if inlineEventHandlerRE.Match(lower) {
		return false
	}
	// Every href/xlink:href must be a same-document fragment ("#...").
	for _, m := range hrefRE.FindAllSubmatch(lower, -1) {
		if !bytes.HasPrefix(m[1], []byte("#")) {
			return false
		}
	}
	return true
}

var (
	// Matches an inline event handler attribute: on<word>= preceded by whitespace
	// or the start of a tag. `\bon` alone would false-positive on words like
	// "only", so require the "=".
	inlineEventHandlerRE = regexp.MustCompile(`[\s"']on[a-z]+\s*=`)
	// Matches href="..." / xlink:href='...' and captures the target.
	hrefRE = regexp.MustCompile(`(?:xlink:)?href\s*=\s*["']([^"']*)["']`)
)
