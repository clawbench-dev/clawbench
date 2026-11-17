package kb

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// panickyPDF writes a 1-page PDF whose /Pages entry is declared as living
// inside an object stream (xref type 2) that is in fact a plain dictionary.
// The underlying pdf library answers that with a bare panic("not an object
// stream") from Reader.resolve, not with an error.
func panickyPDF(t *testing.T) string {
	t.Helper()
	var body bytes.Buffer
	body.WriteString("%PDF-1.5\n")
	offsets := map[int]int{}
	offsets[1] = body.Len()
	body.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	offsets[2] = body.Len()
	body.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	offsets[3] = body.Len()
	body.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")
	offsets[4] = body.Len()
	body.WriteString("4 0 obj\n<< /Type /Foo >>\nendobj\n")

	type row struct{ off2, off3, gen byte }
	rows := []row{
		{0, 0, 255},
		{byte(offsets[1] >> 8), byte(offsets[1]), 0},
		{0, 4, 0}, // /Pages "lives" in stream object 4, which is not a stream
		{byte(offsets[3] >> 8), byte(offsets[3]), 0},
		{byte(offsets[4] >> 8), byte(offsets[4]), 0},
	}
	var xref []byte
	for _, r := range rows {
		xref = append(xref, 2, r.off2, r.off3, r.gen) // type 2 = inside an object stream
	}
	xrefOff := body.Len()
	fmt.Fprintf(&body, "5 0 obj\n<< /Type /XRef /Size 6 /W [1 2 1] /Root 1 0 R /Index [0 5] /Length %d >>\nstream\n", len(xref))
	body.Write(xref)
	body.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&body, "startxref\n%d\n%%%%EOF\n", xrefOff)

	p := filepath.Join(t.TempDir(), "broken.pdf")
	require.NoError(t, os.WriteFile(p, body.Bytes(), 0o644))
	return p
}

// TestExtractPDF_RecoversFromLibraryPanic pins the P0: NumPage() goes through
// the library's bare panic("not an object stream"), and an uncaught panic in
// the indexer goroutine kills the whole server. Worse, the document is still
// pending, so the next start re-queues it and crashes again.
func TestExtractPDF_RecoversFromLibraryPanic(t *testing.T) {
	blocks, err := extractPDF(panickyPDF(t))
	require.Error(t, err)
	require.Empty(t, blocks)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFMalformed, fe.Reason)
}
