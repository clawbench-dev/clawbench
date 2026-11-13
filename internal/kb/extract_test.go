package kb

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func writeZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for entry, content := range files {
		w, err := zw.Create(entry)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return path
}

func TestExtractPlainTextUTF8(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "notes.txt", []byte("第一行\n第二行\n第三行"))

	blocks, warning, err := ExtractFile(path, ".txt", 1<<20)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.NotEmpty(t, blocks)
	require.Contains(t, blocks[0].Text, "第一行")
	require.Equal(t, "L1-L3", blocks[0].Location.Label())
}

func TestExtractPlainTextGBK(t *testing.T) {
	dir := t.TempDir()
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("知识库的检索"))
	require.NoError(t, err)
	path := writeFile(t, dir, "gbk.txt", encoded)

	blocks, _, err := ExtractFile(path, ".txt", 1<<20)
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Contains(t, blocks[0].Text, "知识库的检索")
}

func TestExtractPlainTextRejectsBinary(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "blob.txt", []byte{0x00, 0x01, 0x02, 0x03})

	_, _, err := ExtractFile(path, ".txt", 1<<20)
	require.ErrorIs(t, err, ErrUnsupportedFile)
}

func TestExtractHTML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "page.html", []byte(`<html><head><title>页面标题</title><style>.x{}</style></head>
<body><h1>第一章</h1><p>这是正文。</p><script>alert(1)</script><ul><li>条目一</li></ul></body></html>`))

	blocks, _, err := ExtractFile(path, ".html", 1<<20)
	require.NoError(t, err)
	joined := joinBlocks(blocks)
	require.Contains(t, joined, "页面标题")
	require.Contains(t, joined, "第一章")
	require.Contains(t, joined, "这是正文")
	require.Contains(t, joined, "条目一")
	require.NotContains(t, joined, "alert(1)")
	require.NotContains(t, joined, ".x{}")
}

func TestExtractDocx(t *testing.T) {
	dir := t.TempDir()
	path := writeZip(t, dir, "doc.docx", map[string]string{
		"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
 <w:body>
  <w:p><w:r><w:t>第一段：报销流程</w:t></w:r></w:p>
  <w:p><w:r><w:t>第二段：年假天数</w:t></w:r></w:p>
 </w:body>
</w:document>`,
	})

	blocks, _, err := ExtractFile(path, ".docx", 1<<20)
	require.NoError(t, err)
	joined := joinBlocks(blocks)
	require.Contains(t, joined, "报销流程")
	require.Contains(t, joined, "年假天数")
	require.Equal(t, "¶1-¶2", blocks[0].Location.Label())
}

func TestExtractXlsx(t *testing.T) {
	dir := t.TempDir()
	path := writeZip(t, dir, "sheet.xlsx", map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="报价" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>产品</t></si><si><t>价格</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
    <row r="2"><c r="A2" t="inlineStr"><is><t>苹果</t></is></c><c r="B2"><v>12.5</v></c></row>
  </sheetData>
</worksheet>`,
	})

	blocks, _, err := ExtractFile(path, ".xlsx", 1<<20)
	require.NoError(t, err)
	joined := joinBlocks(blocks)
	require.Contains(t, joined, "产品")
	require.Contains(t, joined, "价格")
	require.Contains(t, joined, "苹果")
	require.Contains(t, joined, "12.5")
	require.Contains(t, blocks[0].Location.Label(), "sheet 报价")
}

func TestExtractPptx(t *testing.T) {
	dir := t.TempDir()
	path := writeZip(t, dir, "deck.pptx", map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>第一页标题</a:t></a:r></a:p>
    <a:p><a:r><a:t>要点一</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`,
		"ppt/slides/slide2.xml": `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>第二页标题</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`,
	})

	blocks, _, err := ExtractFile(path, ".pptx", 1<<20)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	require.Contains(t, blocks[0].Text, "第一页标题")
	require.Equal(t, "slide 1", blocks[0].Location.Label())
	require.Contains(t, blocks[1].Text, "第二页标题")
	require.Equal(t, "slide 2", blocks[1].Location.Label())
}

func TestValidateFileRejectsKnownUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "old.doc", []byte("anything"))

	err := ValidateFile(path, ".doc", 8, 1<<20)
	require.ErrorIs(t, err, ErrUnsupportedFile)
	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	require.Equal(t, ReasonLegacyDoc, fileErr.Reason)

	err = ValidateFile(path, ".png", 8, 1<<20)
	require.ErrorIs(t, err, ErrUnsupportedFile)

	err = ValidateFile(path, ".txt", 2<<20, 1<<20)
	require.ErrorIs(t, err, ErrFileTooLarge)
}

func TestValidateFileUnknownExtensionSniffsContent(t *testing.T) {
	dir := t.TempDir()
	textPath := writeFile(t, dir, "README", []byte("plain text"))
	require.NoError(t, ValidateFile(textPath, "", 10, 1<<20))

	binPath := writeFile(t, dir, "binary", []byte{0x7f, 0x45, 0x4c, 0x46, 0x00, 0x01})
	require.ErrorIs(t, ValidateFile(binPath, "", 6, 1<<20), ErrUnsupportedFile)
}

func TestExtractTruncatesLongText(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "long.txt", []byte(strings.Repeat("一二三四五六七八九十\n", 100)))

	blocks, warning, err := ExtractFile(path, ".txt", 200)
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Equal(t, WarningTruncated, warning)
	total := 0
	for _, b := range blocks {
		total += len(b.Text)
	}
	require.LessOrEqual(t, total, 200)
}

func TestExtractNoText(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "empty.txt", []byte("   \n\n  "))

	_, _, err := ExtractFile(path, ".txt", 1<<20)
	require.ErrorIs(t, err, ErrNoText)
}

func joinBlocks(blocks []Block) string {
	parts := make([]string, len(blocks))
	for i, b := range blocks {
		parts[i] = b.Text
	}
	return strings.Join(parts, "\n")
}

// minimalPDF builds a one-page PDF with a single text object and a correct xref
// table. Hand-written so the test needs no PDF-generation dependency.
func minimalPDF(text string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, 6)
	writeObj := func(i int, body string) {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i, body)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	stream := fmt.Sprintf("BT /F1 24 Tf 72 720 Td (%s) Tj ET", text)
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	xref := b.Len()
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

func TestExtractPDF(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "doc.pdf", minimalPDF("Hello PDF"))

	blocks, _, err := ExtractFile(path, ".pdf", 1<<20)
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Contains(t, blocks[0].Text, "Hello")
	require.Equal(t, "p.1", blocks[0].Location.Label())
}

func TestExtractPDFNoText(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "blank.pdf", minimalPDF(""))

	_, _, err := ExtractFile(path, ".pdf", 1<<20)
	require.ErrorIs(t, err, ErrNoText)
}

func TestLocationLabel(t *testing.T) {
	cases := []struct {
		loc  Location
		want string
	}{
		{Location{Kind: "page", Start: 12}, "p.12"},
		{Location{Kind: "slide", Start: 3}, "slide 3"},
		{Location{Kind: "sheet", Name: "报价", Start: 1, End: 50}, "sheet 报价 · rows 1-50"},
		{Location{Kind: "sheet", Name: "报价", Start: 2, End: 2}, "sheet 报价 · row 2"},
		{Location{Kind: "line", Start: 1, End: 80}, "L1-L80"},
		{Location{Kind: "line", Start: 5, End: 5}, "L5"},
		{Location{Kind: "paragraph", Start: 1, End: 6}, "¶1-¶6"},
		{Location{Kind: "paragraph", Start: 2, End: 2}, "¶2"},
		{Location{Kind: locTitle}, "title"},
		{Location{}, ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.loc.Label())
	}
}

func TestFileError(t *testing.T) {
	require.Equal(t, "binary", (&FileError{Reason: ReasonBinary}).Error())
	withDetail := &FileError{Reason: ReasonLegacyDoc, Detail: ".doc", Err: ErrUnsupportedFile}
	require.Equal(t, "legacy_doc: .doc", withDetail.Error())
	require.ErrorIs(t, withDetail, ErrUnsupportedFile)
}

func TestPlaceholders(t *testing.T) {
	require.Equal(t, "", placeholders(0))
	require.Equal(t, "?", placeholders(1))
	require.Equal(t, "?,?,?", placeholders(3))
}
