package kb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// officePart builds a one-part document whose part body is the given XML.
func officePart(t *testing.T, name string, parts map[string]string) string {
	t.Helper()
	return writeZip(t, t.TempDir(), name, parts)
}

// A Word file that has no word/document.xml at all is "no text", not "unknown
// format": the user picked a real .docx, there is simply nothing to index.
func TestExtractDocx_NoBodyPartIsNoText(t *testing.T) {
	path := officePart(t, "empty.docx", map[string]string{
		"word/styles.xml": `<?xml version="1.0"?><styles/>`,
	})
	_, err := extractDocx(path)
	require.ErrorIs(t, err, ErrNoText)
}

// A file that is not a zip at all is an unknown format, and the detail names the
// format so the panel can say "this .docx is not a real .docx".
func TestExtractOffice_NonZipIsUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	bogus := writeFile(t, dir, "broken.docx", []byte("this is not a zip archive"))

	_, err := extractDocx(bogus)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonUnknownFormat, fe.Reason)
	require.Equal(t, "docx", fe.Detail)

	_, err = extractXlsx(bogus)
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "xlsx", fe.Detail)

	_, err = extractPptx(bogus)
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "pptx", fe.Detail)
}

// A document whose body holds markup but no text still has no text. Returning
// empty blocks would make the indexer store a document with nothing to search.
func TestExtractDocx_MarkupWithoutTextIsNoText(t *testing.T) {
	path := officePart(t, "blank.docx", map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document><w:body><w:p/></w:body></w:document>`,
	})
	_, err := extractDocx(path)
	require.ErrorIs(t, err, ErrNoText)
}

// Tab and line-break runs are layout the extractor preserves: they separate two
// fields that would otherwise be glued into one word.
func TestExtractDocx_PreservesTabsAndLineBreaks(t *testing.T) {
	path := officePart(t, "form.docx", map[string]string{
		"word/document.xml": `<?xml version="1.0"?>
<w:document><w:body><w:p>
  <w:r><w:t>姓名</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>张三</w:t></w:r>
  <w:r><w:br/></w:r><w:r><w:t>部门</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>财务</w:t></w:r>
</w:p></w:body></w:document>`,
	})

	blocks, err := extractDocx(path)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "姓名\t张三\n部门\t财务", blocks[0].Text)
}

// Paragraphs are grouped by a rune budget: a document of many short paragraphs
// must become a few blocks whose location label spans the paragraphs they cover,
// and no block may exceed the budget on its own.
func TestGroupParagraphs_SplitsOnTheRuneBudget(t *testing.T) {
	paras := []string{
		strings.Repeat("甲", 6),
		strings.Repeat("乙", 6),
		strings.Repeat("丙", 6),
		strings.Repeat("丁", 6),
	}

	// Budget 10: 6 fits, 6+6 does not, so every paragraph is its own block.
	tight := groupParagraphs(paras, 10)
	require.Len(t, tight, 4)
	require.Equal(t, "¶1", tight[0].Location.Label(), "a one-paragraph block names one paragraph")
	require.Equal(t, "¶4", tight[3].Location.Label())

	// Budget 100: everything is one block covering every paragraph.
	wide := groupParagraphs(paras, 100)
	require.Len(t, wide, 1)
	require.Equal(t, "¶1-¶4", wide[0].Location.Label())
	require.Equal(t, strings.Join(paras, "\n\n"), wide[0].Text)

	// Two paragraphs fit together and the third starts a new block, so the
	// location of the second block starts where the first one ended.
	mid := groupParagraphs(paras, 12)
	require.Len(t, mid, 2)
	require.Equal(t, "¶1-¶2", mid[0].Location.Label())
	require.Equal(t, "¶3-¶4", mid[1].Location.Label())

	require.Empty(t, groupParagraphs(nil, 100))
}

// A workbook whose sheets have no relationship target is read through the
// fallback enumeration, which must follow natural numeric order (sheet2 before
// sheet10) instead of lexical order.
func TestExtractXlsx_FallsBackToEnumeratingWorksheetsInNumericOrder(t *testing.T) {
	sheet := func(text string) string {
		return `<?xml version="1.0"?><worksheet><sheetData><row r="1"><c t="inlineStr"><is><t>` +
			text + `</t></is></c></row></sheetData></worksheet>`
	}
	path := officePart(t, "fallback.xlsx", map[string]string{
		"xl/workbook.xml":           `<?xml version="1.0"?><workbook><sheets><sheet name="S10" sheetId="1"/></sheets></workbook>`,
		"xl/worksheets/sheet2.xml":  sheet("第二张"),
		"xl/worksheets/sheet10.xml": sheet("第十张"),
	})

	blocks, err := extractXlsx(path)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	require.Contains(t, blocks[0].Text, "第二张", "sheet2 must come before sheet10")
	require.Contains(t, blocks[1].Text, "第十张")
	// The fallback has no sheet name, so the location names the position
	// (1-based) instead.
	require.Equal(t, "1", blocks[0].Location.Name)
	require.Equal(t, "2", blocks[1].Location.Name)
	require.Equal(t, "sheet 1 · row 1", blocks[0].Location.Label())
}

// A workbook with no worksheets at all is "no text": there is nothing to index
// and the panel should say so rather than show an empty document.
func TestExtractXlsx_NoSheetsIsNoText(t *testing.T) {
	path := officePart(t, "nosheets.xlsx", map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?><workbook><sheets/></workbook>`,
	})
	_, err := extractXlsx(path)
	require.ErrorIs(t, err, ErrNoText)
}

// A sheet relationship that points at a part the archive does not hold is
// skipped; the other sheets are still read.
func TestExtractXlsx_MissingSheetPartIsSkipped(t *testing.T) {
	path := officePart(t, "partial.xlsx", map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?>
<workbook><sheets>
  <sheet name="缺失" sheetId="1" r:id="rId1"/>
  <sheet name="在" sheetId="2" r:id="rId2"/>
</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?>
<Relationships>
  <Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="worksheet" Target="/xl/worksheets/sheet2.xml"/>
  <Relationship Id="" Type="worksheet" Target="worksheets/ignored.xml"/>
</Relationships>`,
		"xl/worksheets/sheet2.xml": `<?xml version="1.0"?>
<worksheet><sheetData><row r="1"><c t="inlineStr"><is><t>唯一的表</t></is></c></row></sheetData></worksheet>`,
	})

	blocks, err := extractXlsx(path)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Contains(t, blocks[0].Text, "唯一的表")
	require.Equal(t, "在", blocks[0].Location.Name)
}

// Rows are grouped 50 at a time so one long sheet becomes several blocks with
// row ranges, and an all-blank group is dropped instead of becoming an empty
// block.
func TestGroupSheetRows_RowRangesAndBlankGroups(t *testing.T) {
	rows := make([]string, 120)
	for i := range rows {
		rows[i] = "行" + itoa(i+1)
	}
	blocks := groupSheetRows(rows, "明细")
	require.Len(t, blocks, 3)
	require.Equal(t, Location{Kind: "sheet", Name: "明细", Start: 1, End: 50}, blocks[0].Location)
	require.Equal(t, Location{Kind: "sheet", Name: "明细", Start: 101, End: 120}, blocks[2].Location)
	require.Equal(t, "行1", strings.Split(blocks[0].Text, "\n")[0])

	require.Empty(t, groupSheetRows([]string{"   ", ""}, "空"), "blank rows make no block")
}

// A presentation with no slides, and slides that carry no text, are both "no
// text" — the deck exists but has nothing to search.
func TestExtractPptx_NoTextCases(t *testing.T) {
	_, err := extractPptx(officePart(t, "empty.pptx", map[string]string{
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation/>`,
	}))
	require.ErrorIs(t, err, ErrNoText)

	_, err = extractPptx(officePart(t, "silent.pptx", map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0"?><p:sld><p:cSld/></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld><a:r><a:t>  </a:t></a:r></p:sld>`,
	}))
	require.ErrorIs(t, err, ErrNoText)
}

// Slide text keeps its line structure: separate paragraphs and explicit breaks
// must not be glued together, or a bullet list becomes one run-on sentence.
func TestExtractPptx_KeepsLineStructure(t *testing.T) {
	path := officePart(t, "lines.pptx", map[string]string{
		"ppt/slides/slide1.xml": `<p:sld>
  <a:p><a:r><a:t>标题</a:t></a:r></a:p>
  <a:p><a:r><a:t>要点一</a:t></a:r><a:br/><a:r><a:t>要点二</a:t></a:r></a:p>
</p:sld>`,
	})
	blocks, err := extractPptx(path)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "标题\n要点一\n要点二", strings.TrimSpace(blocks[0].Text))
	require.Equal(t, "slide 1", blocks[0].Location.Label())
}

// naturalLess is what keeps slide2 before slide10; equal trailing numbers and
// non-numeric names fall back to plain string order so the sort stays total.
func TestNaturalLess_Ordering(t *testing.T) {
	require.True(t, naturalLess("ppt/slides/slide2.xml", "ppt/slides/slide10.xml"))
	require.False(t, naturalLess("ppt/slides/slide10.xml", "ppt/slides/slide2.xml"))
	require.True(t, naturalLess("sheet1.xml", "sheet1.xml.bak"), "equal numbers fall back to string order")
	require.False(t, naturalLess("sheet1.xml.bak", "sheet1.xml"))

	// No trailing number on either side: plain string comparison.
	require.True(t, naturalLess("alpha.xml", "beta.xml"))
	require.False(t, naturalLess("beta.xml", "alpha.xml"))
}

// The budget reader is the mechanism that stops a part from reading past the
// document's remaining allowance, so it must clamp every read to what is left
// and report EOF at the boundary rather than a short read.
func TestBudgetReader_ClampsToTheRemainingAllowance(t *testing.T) {
	used := int64(0)
	r := &budgetReader{r: strings.NewReader(strings.Repeat("x", 100)), used: &used, limit: 10}
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 10, n, "a single read may not exceed the limit")
	require.Equal(t, int64(10), used)

	n, err = r.Read(buf)
	require.ErrorContains(t, err, "EOF")
	require.Zero(t, n)

	// A zero budget reads nothing at all.
	used = 0
	empty := &budgetReader{r: strings.NewReader("abc"), used: &used, limit: 0}
	n, _ = empty.Read(buf)
	require.Zero(t, n)
}

// An exhausted budget is 0 left, never negative: callers compare it with <= 0
// and a negative value would be indistinguishable from an accounting bug.
func TestOfficeBudget_RemainingNeverGoesNegative(t *testing.T) {
	var nilBudget *officeBudget
	require.Zero(t, nilBudget.remaining(), "a nil budget has nothing left")
	nilBudget.spend(10) // must not panic

	b := &officeBudget{left: 5}
	require.Equal(t, int64(5), b.remaining())
	b.spend(7)
	require.Equal(t, int64(0), b.remaining(), "an over-spent budget reports 0 left, not a negative allowance")

	empty := &officeBudget{left: 0}
	require.Zero(t, empty.remaining())
}

// A workbook with no shared strings part still reads inline strings and numbers:
// sharedStrings.xml is optional in the format.
func TestReadSharedStrings_AbsentPartYieldsNoStrings(t *testing.T) {
	path := officePart(t, "inline.xlsx", map[string]string{
		"xl/workbook.xml": `<workbook><sheets><sheet name="T" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships>
  <Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>
  <row r="1"><c t="inlineStr"><is><t>现场填写</t></is></c><c><v>42</v></c></row>
</sheetData></worksheet>`,
	})
	blocks, err := extractXlsx(path)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "现场填写\t42", blocks[0].Text)
}

// normalizeZipTarget makes relationship targets absolute inside the package, and
// must not double the xl/ prefix for targets that already carry it.
func TestNormalizeZipTarget(t *testing.T) {
	require.Equal(t, "xl/worksheets/sheet1.xml", normalizeZipTarget("worksheets/sheet1.xml"))
	require.Equal(t, "xl/worksheets/sheet1.xml", normalizeZipTarget("/worksheets/sheet1.xml"))
	require.Equal(t, "xl/worksheets/sheet1.xml", normalizeZipTarget("/xl/worksheets/sheet1.xml"))
}

// The office extractor must be reachable through the public entry point with the
// same verdicts, since that is what the indexer calls.
func TestExtractFile_OfficeErrorReasonsReachTheCaller(t *testing.T) {
	dir := t.TempDir()
	bogus := writeFile(t, dir, "broken.xlsx", []byte("not a zip"))

	_, _, err := ExtractFile(bogus, ".xlsx", 1<<20)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonUnknownFormat, fe.Reason)

	// A .docx with no body goes through the same path and reports no text.
	empty := officePart(t, "nobody.docx", map[string]string{"word/styles.xml": "<styles/>"})
	_, _, err = ExtractFile(empty, ".docx", 1<<20)
	require.ErrorIs(t, err, ErrNoText)
}
