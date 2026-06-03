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
