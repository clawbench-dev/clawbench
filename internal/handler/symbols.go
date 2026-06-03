package handler

import (
	"net/http"
	"os"
	"path/filepath"

	"clawbench/internal/model"
	"clawbench/internal/symbol"
)

// ServeFileSymbols extracts code symbols (functions, classes, etc.) from a source file
// using tree-sitter AST parsing.
// GET /api/file/symbols?path=<path>
func ServeFileSymbols(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	_, ok := requireProject(w, r)
	if !ok {
		return
	}

	pathStr := r.URL.Query().Get("path")
	if pathStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	absPath, ok := resolveAbsPath(w, r, pathStr)
	if !ok {
		return
	}

	// Validate file exists and is not a directory
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFoundShort")
			return
		}
		model.WriteError(w, model.Internal(err))
		return
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileNotFoundShort")
		return
	}

	// Check if text file
	if !model.IsTextFile(info.Name()) {
		writeJSON(w, http.StatusOK, symbol.SymbolResult{Lang: "", Symbols: nil})
		return
	}

	// Skip files larger than 1MB
	const readLimit = 1 << 20
	if info.Size() > readLimit {
		writeJSON(w, http.StatusOK, symbol.SymbolResult{Lang: "", Symbols: nil})
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	// Use filename (with extension) for language detection
	filename := filepath.Base(absPath)
	result := symbol.ExtractSymbols(filename, content)
	writeJSON(w, http.StatusOK, result)
}
