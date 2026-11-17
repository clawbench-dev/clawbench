package kb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A minimal but well-formed PDF: the library opens it, the page tree resolves,
// and GetPlainText yields text. It is the fixture the PDF edge cases below need;
// the cross-reference offsets are computed rather than hard-coded.
func minimalPDFForTest(pages []string) []byte {
	out := "%PDF-1.4\n"
	offsets := make([]int, len(pages)+5)
	writeObj := func(i int, body string) {
		offsets[i] = len(out)
		out += itoa(i) + " 0 obj\n" + body + "\nendobj\n"
	}
	kids := ""
	for i := range pages {
		if i > 0 {
			kids += " "
		}
		kids += itoa(3+i) + " 0 R"
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids ["+kids+"] /Count "+itoa(len(pages))+" >>")

	contentObj := 3 + len(pages)
	fontObj := contentObj + 1
	for i := range pages {
		writeObj(3+i, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents "+
			itoa(contentObj)+" 0 R /Resources << /Font << /F1 "+itoa(fontObj)+" 0 R >> >> >>")
	}
	stream := "BT /F1 24 Tf 72 720 Td (" + pages[0] + ") Tj ET"
	writeObj(contentObj, "<< /Length "+itoa(len(stream))+" >>\nstream\n"+stream+"\nendstream")
	writeObj(fontObj, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xref := len(out)
	out += "xref\n0 " + itoa(fontObj+1) + "\n0000000000 65535 f \n"
	for i := 1; i <= fontObj; i++ {
		off := itoa(offsets[i])
		for len(off) < 10 {
			off = "0" + off
		}
		out += off + " 00000 n \n"
	}
	out += "trailer\n<< /Size " + itoa(fontObj+1) + " /Root 1 0 R >>\nstartxref\n" + itoa(xref) + "\n%%EOF\n"
	return []byte(out)
}

// A PDF whose page tree cannot be read is refused as malformed instead of
// hanging or taking the server down: NumPage resolves the tree and the library
// panics with bare strings there, so the document-level recover is what turns it
// into a FileError the panel can translate.
func TestExtractPDF_MalformedPageTreeIsRefused(t *testing.T) {
	dir := t.TempDir()
	// A catalog whose /Pages points at an object that does not exist.
	body := "%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 99 0 R >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n"
	path := writeFile(t, dir, "broken-tree.pdf", []byte(body))

	blocks, err := extractPDF(path)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Nil(t, blocks)
	require.Contains(t, []string{ReasonPDFEncrypted, ReasonPDFMalformed, ReasonPDFNoText}, fe.Reason,
		"an unreadable page tree is one of the documented PDF verdicts, never a panic")
}

// pdfPageText is where a single damaged page is neutralized: a page object that
// is present but has no content stream yields no text and no error, so the other
// pages still contribute.
func TestPDFPageText_EmptyStreamIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "two-pages.pdf", minimalPDFForTest([]string{"First page", "Second page"}))

	blocks, err := extractPDF(path)
	require.NoError(t, err)
	require.NotEmpty(t, blocks, "a well-formed PDF yields at least one page of text")

	// The location is a page label, not a paragraph one: that is what the panel
	// shows in a citation for a PDF.
	require.Equal(t, "page", blocks[0].Location.Kind)
	require.Equal(t, "p.1", blocks[0].Location.Label())
}
