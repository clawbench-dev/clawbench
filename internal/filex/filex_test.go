package filex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMimeForLocalFile(t *testing.T) {
	assert.Equal(t, "image/png", MimeForLocalFile(".png"))
	assert.Equal(t, "text/html", MimeForLocalFile(".html"), "web-asset table is consulted second")
	assert.Equal(t, MimeOctetStream, MimeForLocalFile(".nope"))
}

func TestMimeTypesHaveNoActiveTypes(t *testing.T) {
	// The public share endpoints serve from this table; an active type here
	// would turn a shared .html into same-origin script.
	for ext, mime := range MimeTypes {
		assert.NotEqual(t, "text/html", mime, "extension %s must not be active", ext)
		assert.NotEqual(t, "text/css", mime, "extension %s must not be active", ext)
		assert.NotEqual(t, "text/javascript", mime, "extension %s must not be active", ext)
	}
}

func TestSniffBinaryContent(t *testing.T) {
	dir := t.TempDir()

	text := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(text, []byte("hello"), 0o644))
	binary, err := SniffBinaryContent(text)
	require.NoError(t, err)
	assert.False(t, binary)

	bin := filepath.Join(dir, "b.bin")
	require.NoError(t, os.WriteFile(bin, []byte{1, 0, 2}, 0o644))
	binary, err = SniffBinaryContent(bin)
	require.NoError(t, err)
	assert.True(t, binary)

	_, err = SniffBinaryContent(filepath.Join(dir, "missing"))
	assert.Error(t, err)
}

func TestResponsePath(t *testing.T) {
	base := filepath.Join("/tmp", "proj")

	// Project files are reported relative; external files absolute.
	assert.Equal(t, filepath.Join("sub", "f.txt"),
		ResponsePath(filepath.Join(base, "sub", "f.txt"), base, false))
	assert.Equal(t, "/elsewhere/f.txt", ResponsePath("/elsewhere/f.txt", base, true))
}

func TestResolveLinkTarget(t *testing.T) {
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain.txt")
	require.NoError(t, os.WriteFile(plain, []byte("x"), 0o644))
	target, isLink := ResolveLinkTarget(plain, dir, false)
	assert.False(t, isLink)
	assert.Empty(t, target)

	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.Symlink(plain, link))
	target, isLink = ResolveLinkTarget(link, dir, false)
	assert.True(t, isLink)
	assert.Equal(t, "plain.txt", target, "project-internal targets are reported relative")

	// A dangling link still reports "is a symlink", with the target path.
	dangling := filepath.Join(dir, "dangling.txt")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.txt"), dangling))
	target, isLink = ResolveLinkTarget(dangling, dir, false)
	assert.True(t, isLink)
	assert.NotEmpty(t, target)
}

func TestSanitizeTextContent(t *testing.T) {
	// Empty input passes through.
	out, truncated := SanitizeTextContent(nil)
	assert.Empty(t, out)
	assert.False(t, truncated)

	// Plain text is untouched.
	out, truncated = SanitizeTextContent([]byte("hello"))
	assert.Equal(t, "hello", string(out))
	assert.False(t, truncated)

	// Binary content is truncated to MaxBinaryTextSize and made printable.
	raw := bytes.Repeat([]byte{0, 1, 2}, MaxBinaryTextSize)
	out, truncated = SanitizeTextContent(raw)
	assert.True(t, truncated)
	assert.Len(t, out, MaxBinaryTextSize)
	assert.False(t, bytes.ContainsRune(out, 0))

	// Large text is truncated at MaxForceTextSize.
	large := bytes.Repeat([]byte("a"), MaxForceTextSize+10)
	out, truncated = SanitizeTextContent(large)
	assert.True(t, truncated)
	assert.Len(t, out, MaxForceTextSize)
}

func TestHasBinaryContent(t *testing.T) {
	assert.False(t, HasBinaryContent([]byte("plain text")))
	assert.True(t, HasBinaryContent([]byte{'a', 0, 'b'}))

	// Null bytes beyond the sniff window are not detected.
	long := append(bytes.Repeat([]byte("a"), BinarySniffSize+1), 0)
	assert.False(t, HasBinaryContent(long))
}

func TestSanitizeBinaryContent(t *testing.T) {
	out, truncated := SanitizeBinaryContent([]byte{0x00, 0x41, 0x01})
	assert.False(t, truncated)
	assert.Equal(t, ".A.", string(out))

	big := bytes.Repeat([]byte{0}, MaxBinaryTextSize+1)
	out, truncated = SanitizeBinaryContent(big)
	assert.True(t, truncated)
	assert.Len(t, out, MaxBinaryTextSize)
}

func TestTruncateAtUTF8Boundary(t *testing.T) {
	// Build a string whose MaxForceTextSize-th byte falls inside a 3-byte rune.
	prefix := strings.Repeat("a", MaxForceTextSize-1)
	data := []byte(prefix + "中")
	out := TruncateAtUTF8Boundary(data)
	assert.LessOrEqual(t, len(out), MaxForceTextSize)
	assert.True(t, len(out) == 0 || out[len(out)-1] < 0x80 || isRuneStart(out[len(out)-1]),
		"the cut must not split a multi-byte character")
}

func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
