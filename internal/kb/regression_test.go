package kb

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// TestSearchHybridReportsVectorFailure covers the silent degradation: with a
// stale kb_vec the model answers a query vector of the wrong width, sqlite-vec
// fails with a dimension mismatch, and the old code dropped that error — the
// user saw keyword results labeled "hybrid" with no hint the semantic half was
// missing. The keyword hits must survive, but the response must say so.
func TestSearchHybridReportsVectorFailure(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "handbook.md", "报销流程：先提交申请，再等财务审批。", "")

	// The index was built with a 3-dim model...
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))
	// ...and the configured model now answers with 4 dims.
	srv := newFakeEmbedServerDim(t, 4)
	mu.Lock()
	globalEmbedder = rag.NewEmbeddingClient(srv.URL, "new-model", "")
	mu.Unlock()

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0})
	require.NoError(t, err, "a broken semantic leg must not fail the search")
	require.NotEmpty(t, res.Results, "keyword hits must still be returned")
	require.True(t, res.VectorDegraded)
	require.Equal(t, SearchModeFTS, res.Mode)
	require.NotEmpty(t, res.VectorError)
	require.NotContains(t, res.VectorError, "kb_vec", "the reason must be user-facing, not SQL")

	// Explicit vector mode must answer the same way instead of a 500.
	res, err = Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0, Mode: SearchModeVector})
	require.NoError(t, err)
	require.True(t, res.VectorDegraded)
	require.Equal(t, SearchModeFTS, res.Mode)
	require.NotEmpty(t, res.Results)
}

// TestSearchHybridHealthyIsNotMarkedDegraded is the control: the flag must only
// appear when something actually failed.
func TestSearchHybridHealthyIsNotMarkedDegraded(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "handbook.md", "报销流程：先提交申请。", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))
	srv := newFakeEmbedServerDim(t, 3)
	mu.Lock()
	globalEmbedder = rag.NewEmbeddingClient(srv.URL, "m", "")
	mu.Unlock()

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0})
	require.NoError(t, err)
	require.Equal(t, SearchModeHybrid, res.Mode)
	require.False(t, res.VectorDegraded)
	require.Empty(t, res.VectorError)
}

// TestSearchVectorWithoutEmbedderIsDegraded: "no embedding service" is the most
// common reason the semantic half is missing and it must be named.
func TestSearchVectorWithoutEmbedderIsDegraded(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, colID, "handbook.md", "报销流程", "")

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0, Mode: SearchModeVector})
	require.NoError(t, err)
	require.True(t, res.VectorDegraded)
	require.Contains(t, res.VectorError, "embedding")
	require.NotEmpty(t, res.Results)
}

// TestExtractXlsx_StopsAtDecompressionBudget: per-part caps alone leave the
// total unbounded. A workbook that decompresses past the budget must stop
// early, keep what it read and report the text as truncated instead of trying
// every remaining part.
func TestExtractXlsx_StopsAtDecompressionBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.xlsx")

	// Each part is far below the per-part cap; only their sum is not. The
	// worksheets come AFTER the big shared-strings part on purpose: the budget
	// must cut them off instead of reading all of them.
	files := map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="S1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
	}
	big := strings.Repeat("A", 6<<20)
	files["xl/sharedStrings.xml"] = "<?xml version=\"1.0\"?><sst><si><t>" + big + "</t></si></sst>"
	files["xl/worksheets/sheet1.xml"] = `<?xml version="1.0"?>
<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>报销流程</t></is></c></row></sheetData></worksheet>`

	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	// A tiny budget stands in for "this file would have decompressed far more
	// than allowed", which would otherwise need 256MB of test data.
	blocks, spent, reads := extractXlsxWithBudget(t, path, 4096)
	require.True(t, spent, "the budget must report that it ran out")
	require.Empty(t, blocks, "nothing may be read once the budget is gone")
	// Only workbook.xml and the rels are small enough to fit; the 6MB shared
	// table already exceeds what is left, and the worksheet after it is never
	// opened at all.
	require.LessOrEqual(t, reads, 2, "parts must not be opened once the budget is gone")

	// End to end with a budget the file exceeds: truncated, not failed, and
	// whatever was read is kept.
	withOfficeBudget(t, 4096)
	_, warning, err := ExtractFile(path, ".xlsx", 1<<20)
	require.ErrorIs(t, err, ErrNoText)
	require.Equal(t, WarningTruncated, warning)

	// A generous budget parses normally.
	withOfficeBudget(t, maxOfficeTotalBytes)
	blocks, warning, err = ExtractFile(path, ".xlsx", 1<<20)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Contains(t, joinBlocks(blocks), "报销流程")
}

// withOfficeBudget shrinks the per-document decompression budget for one test.
func withOfficeBudget(t *testing.T, budget int64) {
	t.Helper()
	old := officeTotalBudget
	officeTotalBudget = budget
	t.Cleanup(func() { officeTotalBudget = old })
}

// extractXlsxWithBudget runs the real extractor with a substituted budget so the
// test does not have to zip hundreds of megabytes.
func extractXlsxWithBudget(t *testing.T, path string, budget int64) ([]Block, bool, int) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = zr.Close() }()

	b := &officeBudget{left: budget}
	shared := readSharedStrings(zr, b)
	sheets := readSheetRefs(zr, b)
	require.NotEmpty(t, sheets)
	var out []Block
	reads := 0
	for _, sh := range sheets {
		if b.remaining() <= 0 {
			break
		}
		reads++
		rows, err := readSheetRows(zr, sh.Target, shared, b)
		if err != nil {
			continue
		}
		out = append(out, groupSheetRows(rows, sh.Name)...)
	}
	return out, b.remaining() <= 0, reads
}

// TestBudgetReaderStopsAtLimit pins the reader itself: it must never hand out
// more bytes than the remaining allowance.
func TestBudgetReaderStopsAtLimit(t *testing.T) {
	var used int64
	r := &budgetReader{r: bytes.NewReader(bytes.Repeat([]byte("x"), 100)), used: &used, limit: 10}
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, int64(10), used)
	_, err = r.Read(buf)
	require.ErrorIs(t, err, io.EOF)
}

// TestDialPublicOnlyRefusesLoopback: the dial guard is what stops a public
// hostname that resolves to a private address.
func TestDialPublicOnlyRefusesLoopback(t *testing.T) {
	dial := dialPublicOnly(time.Second)
	conn, err := dial(context.Background(), "tcp", "127.0.0.1:9")
	if conn != nil {
		_ = conn.Close()
	}
	require.ErrorIs(t, err, ErrPrivateAddress)

	conn, err = dial(context.Background(), "tcp", "[::1]:9")
	if conn != nil {
		_ = conn.Close()
	}
	require.ErrorIs(t, err, ErrPrivateAddress)
}
