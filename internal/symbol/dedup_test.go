package symbol

import (
	"testing"
)

func TestExtractSymbols_GoDedup(t *testing.T) {
	// Go's inferred tags query produces duplicate definitions on the same line
	// for functions with return types (e.g., func foo() error → "foo" + "error")
	content := []byte(`package main

func levelFromKind(kind string) int {
	switch kind {
	case "class":
		return 1
	default:
		return 2
	}
}

func InitDB() error {
	return nil
}
`)

	result := ExtractSymbols("test.go", content)

	// Should NOT contain duplicate "int" or "error" symbols on the same lines as the functions
	for _, sym := range result.Symbols {
		if sym.Name == "int" || sym.Name == "error" {
			t.Errorf("found spurious symbol %q on line %d — should have been deduplicated", sym.Name, sym.Line)
		}
	}

	// Should contain the actual functions
	foundLevel := false
	foundInit := false
	for _, sym := range result.Symbols {
		if sym.Name == "levelFromKind" {
			foundLevel = true
			if sym.Line != 3 {
				t.Errorf("levelFromKind line = %d, want 3", sym.Line)
			}
		}
		if sym.Name == "InitDB" {
			foundInit = true
			if sym.Line != 12 {
				t.Errorf("InitDB line = %d, want 12", sym.Line)
			}
		}
	}
	if !foundLevel {
		t.Error("levelFromKind not found in symbols")
	}
	if !foundInit {
		t.Error("InitDB not found in symbols")
	}
}
