package avatar

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_SVG(t *testing.T) {
	safe := []string{
		`<svg viewBox="0 0 2 2"><rect width="2" height="2" fill="#123"/></svg>`,
		// Same-document <use href="#..."> — DiceBear's real output shape.
		`<svg xmlns="http://www.w3.org/2000/svg"><defs><g id="t"/></defs><use href="#t"/></svg>`,
		// url(#...) fills.
		`<svg><rect fill="url(#grad)"/></svg>`,
	}
	for i, s := range safe {
		assert.True(t, Validate([]byte(s)), "safe case %d should pass", i)
	}

	unsafe := []string{
		``, // empty
		`not an svg at all`,
		`<svg><script>alert(1)</script></svg>`,
		`<svg onload="alert(1)"></svg>`,
		`<svg><image href="https://evil.example/x.png"/></svg>`,
		`<svg><use href="https://evil.example/x.svg#a"/></svg>`,
		`<svg><foreignObject/></svg>`,
		`<svg><iframe src="x"/></svg>`,
		`<svg><style>@import url(https://evil.example/x)</style></svg>`,
		`<svg><a href="javascript:alert(1)"/></svg>`,
	}
	for i, s := range unsafe {
		assert.False(t, Validate([]byte(s)), "unsafe case %d should be rejected", i)
	}
}

// testPNGDataURI builds a solid w×h PNG as a data URI.
func testPNGDataURI(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestValidate_Raster(t *testing.T) {
	pngURI := testPNGDataURI(t, 192, 192)
	assert.True(t, Validate([]byte(pngURI)), "valid PNG data URI should pass")

	// A real 192x192 WebP produced by the client's canvas path.
	const webpB64 = "UklGRoIAAABXRUJQVlA4IHYAAADwCgCdASrAAMAAPm02mUmkIyKhIEgAgA2JaW7hdrEbQBPa9FXCDSdFXCDIIaqk122i4QZBDVUmu20XCDIIaqk122i4QZBDVUmu20XCDIIaqk122i4QZBDVUmu20XCDHIAA/v/B/l87HuD/0Hw07jcVAAAAAAAA"
	assert.True(t, Validate([]byte("data:image/webp;base64,"+webpB64)), "valid WebP data URI should pass")

	bad := []string{
		"",                           // empty
		"data:image/png;base64,",     // empty payload
		"data:image/png;base64,AAAA", // decodes, not an image
		"data:image/gif;base64," + base64.StdEncoding.EncodeToString([]byte("GIF89a")),     // unsupported MIME
		"data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg/>`)), // SVG must use the raw form
		"data:image/jpeg;base64," + strings.TrimPrefix(pngURI, "data:image/png;base64,"),   // declared format != real format
		"data:image/png;base64,!!!!", // invalid base64 alphabet
		testPNGDataURI(t, 600, 600),  // dimensions above the cap
	}
	for i, s := range bad {
		assert.False(t, Validate([]byte(s)), "bad raster case %d should be rejected", i)
	}
}

func TestRasterLooksSafe_EdgeCases(t *testing.T) {
	assert.False(t, rasterLooksSafe("png", nil), "empty payload")
	assert.False(t, rasterLooksSafe("png", []byte("A")), "invalid base64")
	assert.False(t, rasterLooksSafe("png", make([]byte, MaxDecodedBytes*4/3+9)), "oversized payload")
}
