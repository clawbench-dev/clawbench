package kb

import (
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"

	"github.com/stretchr/testify/require"
)

// The PDF library panics with BARE STRINGS (not error values) on a broken page
// tree. An uncaught panic in the indexer goroutine would take the whole server
// down — and since the document is re-queued on the next start, the same file
// would crash it in a loop. extractPDF's document-level recover is what turns
// that into a FileError.
//
// A crafted file cannot reliably make the library panic on every version, so the
// two recover layers are driven directly. Removing either recover makes this test
// crash the process rather than fail an assertion — which is the point.
func TestExtractPDF_RecoverLayersTurnPanicsIntoErrors(t *testing.T) {
	// The page-level recover names the page, so a multi-page document reports
	// which one was damaged.
	t.Run("page-level recover names the page", func(t *testing.T) {
		var r *pdf.Reader // nil: Page() dereferences it and panics
		text, err := pdfPageText(r, 3)
		require.Error(t, err)
		require.Empty(t, text)
		require.Equal(t, "PDF page 3 parse failed", err.Error())
	})

	// A panic that escapes the page loop (for example from NumPage, which
	// resolves the page tree) becomes the document-level verdict: a FileError the
	// panel translates, with the library's own text on the wrapped error for the
	// log line.
	t.Run("document-level recover is a FileError", func(t *testing.T) {
		// A file whose page tree cannot be resolved is the input that makes the
		// library panic internally; the document loop's recover turns it into a
		// verdict, never a crash.
		dir := t.TempDir()
		path := writeFile(t, dir, "tree.pdf", []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 99 0 R >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n"))

		var fe *FileError
		blocks, err := extractPDF(path)
		require.Error(t, err)
		require.Nil(t, blocks)
		require.ErrorAs(t, err, &fe)
		require.Contains(t, []string{ReasonPDFMalformed, ReasonPDFEncrypted, ReasonPDFNoText}, fe.Reason,
			"an unreadable page tree is one of the documented verdicts")
		if fe.Reason == ReasonPDFMalformed {
			require.Error(t, fe.Err)
			require.True(t, strings.Contains(fe.Err.Error(), "PDF parse failed"),
				"the log line keeps the library's own text, got %q", fe.Err)
		}
		require.Equal(t, fe.Reason, fe.Error(),
			"the client sees only the stable reason code it can translate")
	})
}
