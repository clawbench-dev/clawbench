package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestCruiseRunTurnCarriesAbnormalReason is a SOURCE guard for a defect that no
// unit test can reach: the CruiseContinue hook is built inside the HTTP handler
// and the queue command, both of which need a live session to execute.
//
// The bug it prevents: those two closures map a turn's result into a DrainResult
// field by field, and they used to omit AbnormalReason. A confirmation round that
// then failed (quota exhausted mid-cruise) came back looking like a CLEAN FINISH,
// so the drain loop kept starting rounds against an account that could not
// answer — up to the speech cap, at ~85s per round. The identical omission had
// already been fixed once in the AutoContinue closure next to it, which is why
// this guard checks BOTH hooks in both files: DrainResult is a struct of zero
// values, so a missing field compiles silently and no type check can catch it.
//
// It parses the AST rather than scanning text. A text scan is defeated by three
// ordinary edits — commenting the field out, a comment that happens to mention
// "AbnormalReason:", or setting it to "" — all of which still contain the
// substring. The AST sees the actual key/value expression.
func TestCruiseRunTurnCarriesAbnormalReason(t *testing.T) {
	files := []string{
		"../handler/chat.go",
		"session_command.go",
	}

	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		checked := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || (key.Name != "CruiseContinue" && key.Name != "AutoContinue") {
					return true
				}
				checked++
				if !drainResultCarriesAbnormalReason(kv.Value) {
					t.Errorf("%s: the %s hook's RunTurn closure does not carry AbnormalReason — a failed confirmation round would look like a clean finish",
						path, key.Name)
				}
				return true
			})
		}
		if checked < 2 {
			t.Fatalf("%s: expected both AutoContinue and CruiseContinue hooks, found %d — the guard's anchor is stale", path, checked)
		}
	}
}

// drainResultCarriesAbnormalReason reports whether the hook value (a
// New*Runner(...) call) contains a RunTurn closure whose returned DrainResult
// literal sets AbnormalReason to a non-empty selector expression.
func drainResultCarriesAbnormalReason(hook ast.Expr) bool {
	found := false
	ast.Inspect(hook, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "AbnormalReason" {
			return true
		}
		// Must be `something.abnormalReason`, not "" and not an unrelated value.
		sel, ok := kv.Value.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "abnormalReason" {
			return true
		}
		found = true
		return false
	})
	return found
}
