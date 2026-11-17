package kb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeBytes writes a raw file and returns its path.
func writeBytes(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// TestExtractPDF_ReportsWhyItCouldNotRead pins the three failure codes the panel
// translates for a PDF. The user's fix differs per code ("remove the password" /
// "this file is damaged" / "this PDF has no text layer, OCR it"), so a generic
// failure would be unactionable.
func TestExtractPDF_ReportsWhyItCouldNotRead(t *testing.T) {
	// Not a PDF at all: the library cannot even open it.
	notPDF := writeBytes(t, "notes.pdf", []byte("this is not a pdf"))
	_, err := extractPDF(notPDF)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFEncrypted, fe.Reason, "pdf.Open failing is reported as unopenable/encrypted")

	// A PDF whose page has no text layer (an image-only scan) reads as "no
	// text", not as a damaged file: that is the case OCR fixes.
	blank := writeBytes(t, "scanned.pdf", minimalPDF("   "))
	_, err = extractPDF(blank)
	require.ErrorIs(t, err, ErrNoText)
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFNoText, fe.Reason)

	// The extension is not consulted: a .txt holding a PDF still goes through
	// the PDF extractor when the caller says so, and one page of text survives.
	pdf := writeBytes(t, "manual.pdf", minimalPDF("Hello"))
	blocks, _, err := ExtractFile(pdf, ".PDF", 1<<20)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "p.1", blocks[0].Location.Label())
}

// TestExtractFile_TruncatesAtTheBudget: a document larger than the configured
// text cap keeps what fits and reports the stable warning code — the panel shows
// "truncated" instead of silently indexing half a policy.
func TestExtractFile_TruncatesAtTheBudget(t *testing.T) {
	body := ""
	for i := range 200 {
		body += "第 " + string(rune('A'+i%26)) + " 行：报销流程说明。\n"
	}
	path := writeBytes(t, "policy.txt", []byte(body))

	blocks, warning, err := ExtractFile(path, ".txt", 100)
	require.NoError(t, err)
	require.Equal(t, WarningTruncated, warning)
	require.NotEmpty(t, blocks)
	total := 0
	for _, b := range blocks {
		total += len(b.Text)
	}
	require.LessOrEqual(t, total, 100, "the retained text must respect the cap")

	// A cap that fits everything reports no warning.
	_, warning, err = ExtractFile(path, ".txt", 1<<20)
	require.NoError(t, err)
	require.Empty(t, warning)
}
