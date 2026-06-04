package symbol

import (
	"testing"
)

func TestExtractSymbols_Go(t *testing.T) {
	src := []byte(`package main

import "fmt"

type Server struct {
	Port int
	Host string
}

type Handler interface {
	Serve(w int)
}

func main() {
	fmt.Println("hello")
}

func (s *Server) Start() error {
	return nil
}

const MaxRetries = 3

var DefaultTimeout = 30
`)

	result := ExtractSymbols("main.go", src)
	if result.Lang != "go" {
		t.Errorf("expected lang=go, got %s", result.Lang)
	}

	// Check that we find key symbols
	symbolNames := make(map[string]bool)
	symbolKinds := make(map[string]string)
	for _, s := range result.Symbols {
		symbolNames[s.Name] = true
		symbolKinds[s.Name] = s.Kind
	}

	for _, name := range []string{"Server", "Handler", "main", "Start", "MaxRetries", "DefaultTimeout"} {
		if !symbolNames[name] {
			t.Errorf("expected symbol %s not found", name)
		}
	}

	// Check kinds
	if symbolKinds["Server"] != "type" {
		t.Errorf("expected Server kind=type, got %s", symbolKinds["Server"])
	}
	if symbolKinds["main"] != "function" {
		t.Errorf("expected main kind=function, got %s", symbolKinds["main"])
	}
	if symbolKinds["Start"] != "method" {
		t.Errorf("expected Start kind=method, got %s", symbolKinds["Start"])
	}
	if symbolKinds["MaxRetries"] != "constant" {
		t.Errorf("expected MaxRetries kind=constant, got %s", symbolKinds["MaxRetries"])
	}
	if symbolKinds["DefaultTimeout"] != "variable" {
		t.Errorf("expected DefaultTimeout kind=variable, got %s", symbolKinds["DefaultTimeout"])
	}

	// Check levels
	for _, s := range result.Symbols {
		switch s.Name {
		case "Server", "Handler":
			if s.Level != 1 {
				t.Errorf("expected %s level=1, got %d", s.Name, s.Level)
			}
		case "main", "Start":
			if s.Level != 2 {
				t.Errorf("expected %s level=2, got %d", s.Name, s.Level)
			}
		}
	}

	// Lines should be 1-based
	for _, s := range result.Symbols {
		if s.Line < 1 {
			t.Errorf("expected line >= 1, got %d for %s", s.Line, s.Name)
		}
	}
}

func TestExtractSymbols_Python(t *testing.T) {
	src := []byte(`import os

class MyClass:
    def __init__(self):
        pass

    def my_method(self):
        return 42

async def async_func():
    pass
`)

	result := ExtractSymbols("test.py", src)
	if result.Lang != "python" {
		t.Errorf("expected lang=python, got %s", result.Lang)
	}

	symbolNames := make(map[string]bool)
	for _, s := range result.Symbols {
		symbolNames[s.Name] = true
	}

	for _, name := range []string{"MyClass", "__init__", "my_method", "async_func"} {
		if !symbolNames[name] {
			t.Errorf("expected symbol %s not found", name)
		}
	}
}

func TestExtractSymbols_TypeScript(t *testing.T) {
	src := []byte(`interface User {
  name: string;
}

type Result<T> = { data: T; };

enum Color { Red, Green, Blue }

class App {
  constructor(name: string) {}
  greet(): string { return ""; }
}

function add(a: number, b: number): number { return a + b; }
`)

	result := ExtractSymbols("test.ts", src)
	if result.Lang != "typescript" {
		t.Errorf("expected lang=typescript, got %s", result.Lang)
	}

	symbolKinds := make(map[string]string)
	for _, s := range result.Symbols {
		symbolKinds[s.Name] = s.Kind
	}

	if symbolKinds["User"] != "interface" {
		t.Errorf("expected User kind=interface, got %s", symbolKinds["User"])
	}
	if symbolKinds["App"] != "class" {
		t.Errorf("expected App kind=class, got %s", symbolKinds["App"])
	}
	if symbolKinds["greet"] != "method" {
		t.Errorf("expected greet kind=method, got %s", symbolKinds["greet"])
	}
	if symbolKinds["add"] != "function" {
		t.Errorf("expected add kind=function, got %s", symbolKinds["add"])
	}
}

func TestExtractSymbols_Rust(t *testing.T) {
	src := []byte(`use std::io;

pub struct Config {
    pub port: u16,
}

pub trait Handler {
    fn handle(&self);
}

impl Config {
    pub fn new() -> Self { Self { port: 8080 } }
}

pub fn start_server() -> io::Result<()> { Ok(()) }
`)

	result := ExtractSymbols("test.rs", src)
	if result.Lang != "rust" {
		t.Errorf("expected lang=rust, got %s", result.Lang)
	}

	symbolNames := make(map[string]bool)
	for _, s := range result.Symbols {
		symbolNames[s.Name] = true
	}

	for _, name := range []string{"Config", "Handler", "start_server"} {
		if !symbolNames[name] {
			t.Errorf("expected symbol %s not found", name)
		}
	}
}

func TestExtractSymbols_UnsupportedLang(t *testing.T) {
	result := ExtractSymbols("test.xyz", []byte("hello"))
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols for unsupported lang, got %d", len(result.Symbols))
	}
}

func TestExtractSymbols_TooLarge(t *testing.T) {
	largeContent := make([]byte, maxFileSize+1)
	result := ExtractSymbols("main.go", largeContent)
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols for large file, got %d", len(result.Symbols))
	}
}

func TestExtractSymbols_EmptyContent(t *testing.T) {
	result := ExtractSymbols("main.go", []byte(""))
	if result.Lang != "go" {
		t.Errorf("expected lang=go, got %s", result.Lang)
	}
	// Empty content should produce no symbols
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for empty content, got %d", len(result.Symbols))
	}
}

func TestExtractSymbols_DefinitionFilter(t *testing.T) {
	// Go source with a reference call (Println) that should be filtered out
	src := []byte(`package main
import "fmt"
func main() {
	fmt.Println("hello")
}
`)
	result := ExtractSymbols("main.go", src)
	for _, s := range result.Symbols {
		if s.Kind == "call" || s.Name == "Println" {
			t.Errorf("reference.call should be filtered out, got %s kind=%s", s.Name, s.Kind)
		}
	}
}

func TestLevelFromKind(t *testing.T) {
	tests := []struct {
		kind     string
		expected int
	}{
		{"class", 1},
		{"struct", 1},
		{"interface", 1},
		{"type", 1},
		{"enum", 1},
		{"module", 1},
		{"namespace", 1},
		{"trait", 1},
		{"impl", 1},
		{"function", 2},
		{"method", 2},
		{"variable", 2},
		{"constant", 2},
		{"field", 2},
		{"property", 2},
		{"constructor", 2},
	}
	for _, tt := range tests {
		got := levelFromKind(tt.kind)
		if got != tt.expected {
			t.Errorf("levelFromKind(%q) = %d, want %d", tt.kind, got, tt.expected)
		}
	}
}

func TestExtractMarkdownSymbols_Basic(t *testing.T) {
	src := []byte(`# Title

Some intro text.

## Section 1

Content under section 1.

### Subsection 1.1

Content under subsection 1.1.

## Section 2

Content under section 2.
`)

	result := ExtractSymbols("test.md", src)
	if result.Lang != "markdown" {
		t.Errorf("expected lang=markdown, got %s", result.Lang)
	}

	// Should find 4 headings
	if len(result.Symbols) != 4 {
		t.Fatalf("expected 4 symbols, got %d", len(result.Symbols))
	}

	// Check headings
	// Lines: 1=# Title, 5=## Section 1, 9=### Subsection 1.1, 13=## Section 2
	// Total lines = 16 (trailing \n creates empty last line)
	// endLine: next heading of same-or-lower level, -1
	expected := []struct {
		name    string
		level   int
		line    int
		endLine int
	}{
		{"Title", 1, 1, 16},          // H1 spans entire file (no other H1)
		{"Section 1", 2, 5, 12},      // H2 ends before ## Section 2 (line 13)
		{"Subsection 1.1", 3, 9, 12}, // H3 ends before ## Section 2 (line 13, level 2 <= 3)
		{"Section 2", 2, 13, 16},     // H2 spans to end of file
	}

	for i, exp := range expected {
		s := result.Symbols[i]
		if s.Name != exp.name {
			t.Errorf("symbol[%d]: expected name=%q, got %q", i, exp.name, s.Name)
		}
		if s.Kind != "heading" {
			t.Errorf("symbol[%d]: expected kind=heading, got %s", i, s.Kind)
		}
		if s.Level != exp.level {
			t.Errorf("symbol[%d]: expected level=%d, got %d", i, exp.level, s.Level)
		}
		if s.Line != exp.line {
			t.Errorf("symbol[%d]: expected line=%d, got %d", i, exp.line, s.Line)
		}
		if s.EndLine != exp.endLine {
			t.Errorf("symbol[%d]: expected endLine=%d, got %d", i, exp.endLine, s.EndLine)
		}
	}
}

func TestExtractMarkdownSymbols_Empty(t *testing.T) {
	result := ExtractSymbols("test.md", []byte("No headings here.\n\nJust text.\n"))
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for markdown without headings, got %d", len(result.Symbols))
	}
}

func TestExtractMarkdownSymbols_HeadingAtEOF(t *testing.T) {
	src := []byte("# Title\n\nSome content\n\n## Section\n")
	result := ExtractSymbols("test.md", src)
	if len(result.Symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(result.Symbols))
	}
	// Last heading's endLine should be the last line of the file (6, includes trailing empty line)
	last := result.Symbols[len(result.Symbols)-1]
	if last.EndLine != 6 {
		t.Errorf("expected endLine=6 for last heading, got %d", last.EndLine)
	}
}

func TestExtractMarkdownSymbols_SameLevelSiblings(t *testing.T) {
	src := []byte("## Alpha\n\nContent A\n\n## Beta\n\nContent B\n\n## Gamma\n\nContent C\n")
	result := ExtractSymbols("test.md", src)
	if len(result.Symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(result.Symbols))
	}
	// Each H2 should end at the line before the next H2
	if result.Symbols[0].EndLine != 4 {
		t.Errorf("Alpha: expected endLine=4, got %d", result.Symbols[0].EndLine)
	}
	if result.Symbols[1].EndLine != 8 {
		t.Errorf("Beta: expected endLine=8, got %d", result.Symbols[1].EndLine)
	}
	// Gamma spans to end of file
	if result.Symbols[2].EndLine != 12 {
		t.Errorf("Gamma: expected endLine=12, got %d", result.Symbols[2].EndLine)
	}
}

func TestExtractMarkdownSymbols_NestedEndLines(t *testing.T) {
	src := []byte("# Main\n\n## A\n\n### A1\n\n### A2\n\n## B\n\n### B1\n")
	result := ExtractSymbols("test.md", src)
	// 6 headings: Main(H1), A(H2), A1(H3), A2(H3), B(H2), B1(H3)
	if len(result.Symbols) != 6 {
		t.Fatalf("expected 6 symbols, got %d", len(result.Symbols))
	}
	// "Main" (H1) spans the entire file (no other H1)
	mainSym := result.Symbols[0]
	if mainSym.EndLine != 12 {
		t.Errorf("Main: expected endLine=12, got %d", mainSym.EndLine)
	}
	// "A" (H2) ends before "## B" (line 9), so endLine=8
	aSym := result.Symbols[1]
	if aSym.EndLine != 8 {
		t.Errorf("A: expected endLine=8, got %d", aSym.EndLine)
	}
	// "A1" (H3) ends before "### A2" (line 7), so endLine=6
	a1Sym := result.Symbols[2]
	if a1Sym.EndLine != 6 {
		t.Errorf("A1: expected endLine=6, got %d", a1Sym.EndLine)
	}
	// "A2" (H3) ends before "## B" (line 9, level 2 <= 3), so endLine=8
	a2Sym := result.Symbols[3]
	if a2Sym.EndLine != 8 {
		t.Errorf("A2: expected endLine=8, got %d", a2Sym.EndLine)
	}
	// "B" (H2) spans to end
	bSym := result.Symbols[4]
	if bSym.EndLine != 12 {
		t.Errorf("B: expected endLine=12, got %d", bSym.EndLine)
	}
	// "B1" (H3) spans to end
	b1Sym := result.Symbols[5]
	if b1Sym.EndLine != 12 {
		t.Errorf("B1: expected endLine=12, got %d", b1Sym.EndLine)
	}
}

func TestExtractMarkdownSymbols_InvalidHeadings(t *testing.T) {
	src := []byte(`####### Not a heading (7 hashes)

#NoSpaceAfterHash

#

##

Some text
`)
	result := ExtractSymbols("test.md", src)
	// Only "##" should NOT be a valid heading (empty text after trim)
	// "#" is also invalid (empty text after trim)
	// "#NoSpaceAfterHash" is invalid (no space after hashes)
	// "####### Not a heading" is invalid (7 hashes, max is 6)
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for invalid headings, got %d: %+v", len(result.Symbols), result.Symbols)
	}
}

func TestExtractSymbols_MarkdownDispatch(t *testing.T) {
	// Verify that ExtractSymbols dispatches to ExtractMarkdownSymbols for .md files
	src := []byte(`# Hello

## World
`)
	result := ExtractSymbols("test.md", src)
	if result.Lang != "markdown" {
		t.Errorf("expected lang=markdown, got %s", result.Lang)
	}
	if len(result.Symbols) != 2 {
		t.Errorf("expected 2 symbols, got %d", len(result.Symbols))
	}
	if result.Symbols[0].Kind != "heading" {
		t.Errorf("expected kind=heading, got %s", result.Symbols[0].Kind)
	}
}
