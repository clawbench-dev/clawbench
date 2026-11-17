package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// An unknown extension is decided by the head of the file: a NUL byte means
// binary. A file the server cannot even open is reported as an open failure (not
// "binary"), because that is the message the panel shows when a folder was moved.
func TestValidateFile_ContentSniffingAndOpenFailures(t *testing.T) {
	dir := t.TempDir()

	text := writeFile(t, dir, "notes.dat", []byte("纯文本，没有 NUL。"))
	require.NoError(t, ValidateFile(text, ".dat", 32, 0), "text with no NUL passes")

	binary := writeFile(t, dir, "blob.dat", []byte{'a', 0, 'b'})
	err := ValidateFile(binary, ".dat", 3, 0)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonBinary, fe.Reason)

	// A file that no longer exists cannot be sniffed.
	missing := filepath.Join(dir, "gone.dat")
	err = ValidateFile(missing, ".dat", 0, 0)
	require.ErrorContains(t, err, "open file")

	// A known text extension is not sniffed at all, so a missing file with a
	// .md extension passes validation (the read happens later).
	require.NoError(t, ValidateFile(filepath.Join(dir, "gone.md"), ".md", 0, 0))

	// Known-unsupported extensions carry their reason and the extension itself.
	err = ValidateFile(binary, ".zip", 3, 0)
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonArchive, fe.Reason)
	require.Equal(t, ".zip", fe.Detail)

	// The size cap is checked before the extension.
	err = ValidateFile(binary, ".txt", 3<<20, 1<<20)
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonFileTooLarge, fe.Reason)
}

// decodeText handles the encodings the knowledge base promises: UTF-8 (plain,
// with BOM), UTF-16 in both byte orders, and GBK/GB18030. Anything else becomes
// UTF-8 with replacement characters instead of failing the document.
func TestDecodeText_Encodings(t *testing.T) {
	// UTF-8 BOM is stripped, not kept as a leading zero-width space.
	got, err := decodeText([]byte{0xEF, 0xBB, 0xBF, 'a', 'b'})
	require.NoError(t, err)
	require.Equal(t, "ab", got)

	// UTF-16 LE and BE, both with BOM.
	le := make([]byte, 0, 2+2*len([]rune("报销")))
	le = append(le, 0xFF, 0xFE)
	be := make([]byte, 0, 2+2*len([]rune("报销")))
	be = append(be, 0xFE, 0xFF)
	for _, r := range "报销" {
		le = append(le, byte(r), byte(r>>8))
		be = append(be, byte(r>>8), byte(r))
	}
	got, err = decodeText(le)
	require.NoError(t, err)
	require.Equal(t, "报销", got)
	got, err = decodeText(be)
	require.NoError(t, err)
	require.Equal(t, "报销", got)

	// GBK bytes for "报销" (0xB1 0xA8 0xCF 0xFA) are not valid UTF-8.
	got, err = decodeText([]byte{0xB1, 0xA8, 0xCF, 0xFA})
	require.NoError(t, err)
	require.Equal(t, "报销", got)

	// Truly invalid bytes (and no NUL, so not classified as binary) become
	// replacement characters: a damaged file still contributes its readable part.
	got, err = decodeText([]byte{'a', 0xFF, 0xFE, 0xFF, 'b'})
	require.NoError(t, err)
	require.Contains(t, got, "a")
	require.Contains(t, got, "b")

	// Uncompressed UTF-8 passes through unchanged.
	got, err = decodeText([]byte("报销流程"))
	require.NoError(t, err)
	require.Equal(t, "报销流程", got)

	// NUL bytes without a UTF-16 BOM are binary.
	_, err = decodeText([]byte{'a', 0, 'b'})
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonBinary, fe.Reason)

	// Empty input is empty text, not an error.
	got, err = decodeText(nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

// capBlocks truncates at a byte budget without splitting a rune, and the warning
// code says the text was cut. A zero budget means "no cap".
func TestCapBlocks_ByteBudget(t *testing.T) {
	blocks := []Block{
		{Text: "甲乙", Location: Location{Kind: "line", Start: 1}},
		{Text: "丙丁", Location: Location{Kind: "line", Start: 2}},
	}

	kept, warning := capBlocks(blocks, 0)
	require.Equal(t, blocks, kept)
	require.Empty(t, warning)

	// Exactly the whole text fits: neither block is cut, even though the loop
	// reaches the "budget is now zero" branch for the second one.
	kept, warning = capBlocks(blocks, 12)
	require.Equal(t, blocks, kept)
	require.Empty(t, warning)

	// Exactly the first block fits: the second does not, so the cut is reported
	// and nothing of it is kept.
	kept, warning = capBlocks(blocks, 6)
	require.Len(t, kept, 1)
	require.Equal(t, WarningTruncated, warning)

	// 9 bytes: the first block fits (6), the second is cut to 3 bytes — but 3
	// bytes is mid-rune, so the cut backs off to the rune boundary (a 3-byte
	// CJK rune does fit exactly).
	kept, warning = capBlocks(blocks, 9)
	require.Equal(t, WarningTruncated, warning)
	require.Len(t, kept, 2)
	require.Equal(t, "丙", kept[1].Text, "exactly one 3-byte rune fits in the remaining 3 bytes")

	// 7 bytes leaves room for a single byte of the next rune, which cannot be
	// split: the truncated block is kept with no text, and the budget is spent.
	kept, warning = capBlocks(blocks, 7)
	require.Equal(t, WarningTruncated, warning)
	require.Len(t, kept, 2)
	require.Empty(t, kept[1].Text)

	// Nothing of the first block fits. The cut block is still emitted with no
	// text and the same location — that empty block is what the block list looks
	// like at a 1-byte budget.
	kept, warning = capBlocks(blocks, 1)
	require.Equal(t, WarningTruncated, warning)
	require.Len(t, kept, 1)
	require.Empty(t, kept[0].Text)
}

// extractPlainText rejects a binary file through decodeText: the extractor is
// where a file whose extension looked like text but whose bytes are not gets
// stopped.
func TestExtractPlainText_RejectsBinary(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "fake.txt", []byte{'a', 0, 'b'})
	_, err := extractPlainText(path)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonBinary, fe.Reason)

	// A missing file is an I/O error, not a format verdict.
	_, err = extractPlainText(filepath.Join(dir, "gone.txt"))
	require.ErrorContains(t, err, "read file")
}

// Groups of about 80 lines keep the location label useful for a long log-like
// file; a file shorter than that is one block.
func TestLineBlocks_GroupSize(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		b.WriteString("第")
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	blocks := lineBlocks(b.String(), 80)
	require.Len(t, blocks, 3)
	require.Equal(t, "L1-L80", blocks[0].Location.Label())
	// The trailing newline ends the 200th line, but Split leaves an empty final
	// element, so the last group spans 41 line slots.
	require.Equal(t, "L161-L201", blocks[2].Location.Label())

	short := lineBlocks("一行\n两行\n", 80)
	require.Len(t, short, 1)
}

// extractHTML skips script/style/svg bodies (code and icons are not knowledge),
// puts the document title first, and numbers the paragraphs so a citation can
// point at one.
func TestExtractHTML_Structure(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "page.html", []byte(`<!doctype html>
<html><head><title>  员工手册  </title>
<style>p { color: red }</style>
<script>var x = 1;</script>
</head>
<body>
  <svg><text>图标文字</text></svg>
  <p>报销流程：先提交申请。</p>
  <div>财务在<p>五个工作日内</p>审批。</div>
  <noscript>请开启脚本</noscript>
</body></html>`))

	blocks, err := extractHTML(path)
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Equal(t, "员工手册", blocks[0].Text, "the title becomes the first block (trimmed)")
	require.Equal(t, locTitle, blocks[0].Location.Kind)

	joined := joinBlocks(blocks)
	require.Contains(t, joined, "报销流程：先提交申请。")
	require.Contains(t, joined, "五个工作日内")
	for _, hidden := range []string{"var x = 1", "color: red", "图标文字", "请开启脚本"} {
		require.NotContains(t, joined, hidden, "%q must not become searchable text", hidden)
	}
	for _, b := range blocks[1:] {
		require.Equal(t, "paragraph", b.Location.Kind)
		require.Positive(t, b.Location.Start)
	}

	// A page with no title has no title block.
	plain := writeFile(t, dir, "plain.html", []byte(`<html><body><p>只有正文</p></body></html>`))
	blocks, err = extractHTML(plain)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "只有正文", blocks[0].Text)

	// A missing file is an open failure the caller can report.
	_, err = extractHTML(filepath.Join(dir, "gone.html"))
	require.ErrorContains(t, err, "open html")
}

// A file that is not a PDF at all is refused as encrypted/malformed rather than
// panicking: the library's own panics are recovered per document and per page.
func TestExtractPDF_RejectsNonPDF(t *testing.T) {
	dir := t.TempDir()
	bogus := writeFile(t, dir, "broken.pdf", []byte("this is not a pdf"))

	_, err := extractPDF(bogus)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFEncrypted, fe.Reason, "an unreadable container is the encrypted/unknown case")

	// Through the public entry point the same verdict comes back with no blocks.
	blocks, _, err := ExtractFile(bogus, ".PDF", 1<<20)
	require.Nil(t, blocks)
	require.ErrorAs(t, err, &fe)

	// A missing file is the same verdict: pdf.Open cannot open it.
	_, err = extractPDF(filepath.Join(dir, "gone.pdf"))
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFEncrypted, fe.Reason)
}

// A PDF with no pages is "no text" rather than a parse failure: the file parsed
// fine, it simply carries nothing to index.
func TestExtractPDF_EmptyDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.pdf")
	// A valid container with an empty page tree (Count 0).
	require.NoError(t, os.WriteFile(path, minimalPDFWithNoPages(), 0o644))

	_, err := extractPDF(path)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPDFNoText, fe.Reason)
}

// minimalPDFWithNoPages builds a well-formed PDF whose page tree is empty.
func minimalPDFWithNoPages() []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, 3)
	writeObj := func(i int, body string) {
		offsets[i] = b.Len()
		b.WriteString(itoa(i) + " 0 obj\n" + body + "\nendobj\n")
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [] /Count 0 >>")
	xref := b.Len()
	b.WriteString("xref\n0 3\n0000000000 65535 f \n")
	for i := 1; i <= 2; i++ {
		// Ten-digit offsets, as the xref table requires.
		off := itoa(offsets[i])
		for len(off) < 10 {
			off = "0" + off
		}
		b.WriteString(off + " 00000 n \n")
	}
	b.WriteString("trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n" + itoa(xref) + "\n%%EOF\n")
	return []byte(b.String())
}
