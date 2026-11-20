package filex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeArchiveName(t *testing.T) {
	assert.Equal(t, "plain.txt", SanitizeArchiveName("plain.txt"))
	assert.Equal(t, "a_b_c.txt", SanitizeArchiveName(`a"b\c.txt`))
	assert.Equal(t, "no_ctrl", SanitizeArchiveName("no\x01ctrl"))
}

func TestContentDispositionAttachment(t *testing.T) {
	got := ContentDispositionAttachment("report.pdf")
	assert.Contains(t, got, "attachment;")
	assert.Contains(t, got, `filename="report.pdf"`)
	assert.Contains(t, got, "filename*=UTF-8''report.pdf")

	// Non-ASCII names must survive via the RFC 5987 form.
	got = ContentDispositionAttachment("会议纪要.pdf")
	assert.Contains(t, got, "filename*=UTF-8''")
	assert.Contains(t, got, "%E4%BC%9A", "the CJK name is percent-encoded")

	// A quote in the name must not break the ASCII fallback.
	got = ContentDispositionAttachment(`a"b.txt`)
	assert.Contains(t, got, `filename="a_b.txt"`)
}

func TestBodyETag(t *testing.T) {
	a := BodyETag([]byte("hello"))
	b := BodyETag([]byte("hello"))
	c := BodyETag([]byte("world"))

	assert.Equal(t, a, b, "the same body yields the same etag")
	assert.NotEqual(t, a, c)
	assert.True(t, len(a) > 2 && a[0] == '"' && a[len(a)-1] == '"', "etags are quoted")
}

func TestETagMatches(t *testing.T) {
	etag := `"abc"`

	assert.False(t, ETagMatches("", etag), "no header never matches")
	assert.False(t, ETagMatches(etag, ""), "no etag never matches")
	assert.True(t, ETagMatches("*", etag), "the wildcard matches any current representation")
	assert.True(t, ETagMatches(etag, etag))
	assert.True(t, ETagMatches(`"other", `+etag, etag), "a comma-separated list is searched")
	assert.True(t, ETagMatches(`W/`+etag, etag), "the weak prefix is ignored")
	assert.False(t, ETagMatches(`"other"`, etag))
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))

	require.NoError(t, CopyFile(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))

	err = CopyFile(filepath.Join(dir, "missing"), dst)
	assert.Error(t, err)
}
