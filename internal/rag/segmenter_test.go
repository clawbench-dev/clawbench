package rag

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSegmentTokens_Chinese(t *testing.T) {
	// TestMain initializes the segmenter, so Cut mode should be active
	tokens := SegmentTokens("你好啊")
	if len(tokens) == 0 {
		t.Fatal("expected non-empty tokens for Chinese text")
	}
	// Check that key tokens are present (gse Cut with HMM should produce "你好" and "啊")
	hasNiHao := false
	hasA := false
	for _, tok := range tokens {
		if tok == "你好" {
			hasNiHao = true
		}
		if tok == "啊" {
			hasA = true
		}
	}
	if !hasNiHao {
		t.Errorf("expected token '你好' in result, got tokens: %v", tokens)
	}
	if !hasA {
		t.Errorf("expected token '啊' in result, got tokens: %v", tokens)
	}
}

func TestSegmentTokens_English(t *testing.T) {
	tokens := SegmentTokens("continue debugging")
	if len(tokens) == 0 {
		t.Fatal("expected non-empty tokens for English text")
	}
	hasContinue := false
	hasDebugging := false
	for _, tok := range tokens {
		if tok == "continue" {
			hasContinue = true
		}
		if tok == "debugging" {
			hasDebugging = true
		}
	}
	if !hasContinue {
		t.Errorf("expected token 'continue' in result, got tokens: %v", tokens)
	}
	if !hasDebugging {
		t.Errorf("expected token 'debugging' in result, got tokens: %v", tokens)
	}
}

func TestSegmentTokens_Empty(t *testing.T) {
	tokens := SegmentTokens("")
	if tokens != nil {
		t.Errorf("expected nil for empty input, got %v", tokens)
	}
}

func TestSegmentTokens_NoSegmenter(t *testing.T) {
	// Temporarily nil the segmenter to test fallback
	orig := segmenter
	segmenter = nil
	defer func() { segmenter = orig }()

	text := "hello world foo"
	tokens := SegmentTokens(text)

	expected := strings.Fields(text)
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens from Fields fallback, got %d", len(expected), len(tokens))
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

// TestSegmenter_UsesEmbeddedDictionary is the regression guard for the released
// binaries silently losing Chinese segmentation.
//
// seg.LoadDict() (no args) derives its dictionary directory from the source path
// baked into the binary at build time via runtime.Caller, so it only loads on a
// machine that still has that Go module cache. On the dev box and on the CI runner
// that path happens to exist, which is why the behavioral tests below passed even
// while every released artifact — Docker image, GitHub release binaries, Android
// build — was starting with a nil segmenter and falling back to whitespace
// splitting (FTS5 'unicode61' then indexes a whole CJK sentence as a single token,
// so any query shorter than the full sentence misses).
//
// The failure is therefore not observable at runtime on a build machine, so this
// test asserts on the source instead: the segmenter must be initialized from the
// compile-time embedded dictionary.
func TestSegmenter_UsesEmbeddedDictionary(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate the test file")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "segmenter.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Only reachable when a precompiled test binary is run away from the
			// source tree. Normal `go test` (including CI) always has the source,
			// so this cannot mask the guard where it matters.
			t.Skipf("source not available at %s; run via `go test`", srcPath)
		}
		t.Fatalf("parse %s: %v", srcPath, err)
	}

	var calls []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if sel.Sel.Name == "LoadDict" || sel.Sel.Name == "LoadDictEmbed" {
			calls = append(calls, sel.Sel.Name)
		}
		return true
	})

	if len(calls) == 0 {
		t.Fatal("segmenter.go no longer loads a gse dictionary; " +
			"if the loader moved, move this guard with it")
	}
	for _, name := range calls {
		if name == "LoadDict" {
			t.Error("segmenter.go calls the file-based seg.LoadDict(), which resolves " +
				"its dictionary path from the build-time source location and fails in " +
				"released binaries; use seg.LoadDictEmbed(\"zh\") instead")
		}
	}
}

// TestInitSegmenter_ChineseSegmentationActive asserts the observable contract:
// after InitSegmenter the segmenter is live and CJK text is actually split into
// words. It would pass on a dev box even with the file-based loader, so it is a
// companion to TestSegmenter_UsesEmbeddedDictionary rather than a replacement —
// together they cover both "the right loader is used" and "the loader works".
func TestInitSegmenter_ChineseSegmentationActive(t *testing.T) {
	orig := segmenter
	t.Cleanup(func() { segmenter = orig })

	segmenter = nil
	if err := InitSegmenter(); err != nil {
		t.Fatalf("InitSegmenter failed: %v", err)
	}
	if segmenter == nil {
		t.Fatal("InitSegmenter returned nil error but left segmenter unset")
	}

	// "你好啊" must split into the word "你好" plus "啊"; whitespace splitting
	// would return the whole string as one token and index nothing searchable.
	tokens := SegmentTokens("你好啊")
	if len(tokens) < 2 {
		t.Fatalf("expected Chinese text to split into multiple tokens, got %v", tokens)
	}
	if !containsToken(tokens, "你好") {
		t.Errorf("expected token %q from embedded dictionary, got %v", "你好", tokens)
	}
}

func containsToken(tokens []string, want string) bool {
	for _, tok := range tokens {
		if tok == want {
			return true
		}
	}
	return false
}

// TestSearchFTS_ChinesePartialQuery covers the user-visible consequence of a
// missing segmenter: a query shorter than the indexed sentence must still hit.
//
// Chunks are indexed pre-segmented, but FTS5 is created with tokenize='unicode61',
// which has no notion of Chinese word boundaries. Without segmentation the whole
// CJK sentence becomes a single token, so only an exact full-sentence query
// matches. This asserts the end-to-end behavior rather than the loader, so it
// also catches a future change to the tokenizer or a bypass of SegmentText.
func TestSearchFTS_ChinesePartialQuery(t *testing.T) {
	store := setupSQLiteStore(t)

	const sentence = "这是一个中文分词测试"
	require.NoError(t, store.InsertChunks([]Chunk{makeTestChunk("sess-fts-zh", 1, 0, sentence)}))

	// Guard the precondition: the stored form must be word-segmented, otherwise
	// the assertions below could pass vacuously on an English-only tokenizer.
	segmented := SegmentText(sentence)
	if !strings.Contains(segmented, " ") {
		t.Fatalf("expected %q to be segmented into multiple words, got %q "+
			"(segmenter inactive: FTS5 will index the whole sentence as one token)",
			sentence, segmented)
	}

	for _, query := range []string{"中文", "分词"} {
		hits, err := store.SearchFTS(query, 5, "", "", "", "", "", "", "")
		require.NoError(t, err, "SearchFTS(%q)", query)
		if len(hits) == 0 {
			t.Errorf("Chinese query %q matched nothing; partial queries are broken "+
				"(indexed text %q)", query, segmented)
		}
	}
}
