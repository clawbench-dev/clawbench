package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sync"

	"clawbench/internal/model"
)

// acpStdoutFilter wraps an io.Reader (agent stdout) and produces a filtered
// io.Reader that fixes common ACP protocol violations. Currently handles:
//
//  1. String-number ID mismatch: Some agents (e.g., CodeWhale/codewhale) return
//     JSON-RPC response IDs as strings ("1") when the request sent numeric IDs (1).
//     The ACP SDK uses strict canonical ID matching, so "1" != 1, causing responses
//     to be silently dropped. This filter converts string-number IDs back to numbers.
//
//  2. Non-JSON lines: Some agents emit terminal escape sequences (e.g., "\x1b[?1004l")
//     on stdout during ACP stdio mode. These are silently stripped.
//
//  3. SessionModelState extraction: Some agents (e.g., kimi) return a "models" field
//     in NewSessionResponse/ResumeSessionResponse that is not part of the ACP Go SDK
//     v0.13.5 schema. The SDK's json.Unmarshal silently drops this field. This filter
//     intercepts the raw JSON and caches the models for later retrieval.
//
// The filter runs a background goroutine that reads from the underlying source and
// writes filtered output to an io.Pipe. Closing the filter (via Close) unblocks any
// pending reads, preventing the ACP connection cleanup from hanging when the agent
// process is killed but the stdout pipe hasn't been closed by the OS yet.
type acpStdoutFilter struct {
	pr     *io.PipeReader
	pw     *io.PipeWriter
	closed bool
	mu     sync.Mutex

	// cachedModels holds the models extracted from the SessionModelState extension
	// in a session/new or session/resume response. The ACP Go SDK v0.13.5 does not
	// include the "models" field in NewSessionResponse/ResumeSessionResponse, so we
	// extract it from the raw JSON here. Thread-safe via modelsMu.
	modelsMu     sync.Mutex
	cachedModels *ModelListState
}

// newACPStdoutFilter creates a new filtered reader that fixes protocol violations.
// The caller must call Close when done to release resources and unblock pending reads.
func newACPStdoutFilter(r io.Reader) *acpStdoutFilter {
	pr, pw := io.Pipe()
	f := &acpStdoutFilter{
		pr: pr,
		pw: pw,
	}
	go f.pump(r)
	return f
}

// pump reads lines from src, filters them, and writes to the pipe writer.
func (f *acpStdoutFilter) pump(src io.Reader) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Skip non-JSON lines (terminal escape sequences, etc.)
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		// Fix string-number ID mismatch
		fixed := fixStringNumericID(line)

		// Extract SessionModelState from session/new or session/resume responses.
		// Some agents (e.g., kimi) return a "models" field that the ACP Go SDK
		// v0.13.5 does not include in its NewSessionResponse type, so it gets
		// silently dropped by json.Unmarshal. We intercept it here.
		if bytes.Contains(fixed, []byte("availableModels")) {
			f.cacheModelsFromResponse(fixed)
		}

		if _, err := f.pw.Write(fixed); err != nil {
			return // pipe closed (Close() called or reader side gone)
		}
		if _, err := f.pw.Write([]byte{'\n'}); err != nil {
			return
		}
	}

	// Source reached EOF — close the write end so the read side gets EOF too
	f.pw.CloseWithError(scanner.Err())
}

// Read implements io.Reader. It reads filtered JSON lines from the pipe.
func (f *acpStdoutFilter) Read(p []byte) (int, error) {
	return f.pr.Read(p)
}

// Close releases the filter resources. It closes the pipe writer to unblock any
// pending reads on the pipe reader side. Safe to call multiple times.
func (f *acpStdoutFilter) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	// Close the write end to unblock the pump goroutine and the read side
	f.pw.CloseWithError(io.EOF)
}

// stringNumericIDRe matches "id":"<digits>" in JSON-RPC responses where the ID
// should be a number (matching a numeric request ID) but was returned as a string.
var stringNumericIDRe = regexp.MustCompile(`"id"\s*:\s*"(\d+)"`)

// fixStringNumericID converts string-number IDs to numeric IDs in a JSON-RPC message.
// E.g., {"id":"1",...} → {"id":1,...}
// This fixes the CodeWhale/codewhale bug where numeric request IDs are returned as strings.
func fixStringNumericID(line []byte) []byte {
	if !bytes.Contains(line, []byte(`"id"`)) {
		return line
	}

	// Quick check: if there's no string-quoted numeric ID, return as-is
	if !stringNumericIDRe.Match(line) {
		return line
	}

	// Parse the line as generic JSON to verify it's a valid message
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return line // not valid JSON, return as-is
	}

	// Check if "id" field is a string that looks like a number
	idRaw, ok := msg["id"]
	if !ok {
		return line
	}

	// Trim whitespace and check if it's a quoted numeric string
	trimmed := bytes.TrimSpace(idRaw)
	if len(trimmed) < 3 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
		return line
	}

	// Extract the string content
	inner := trimmed[1 : len(trimmed)-1]
	if !isDigits(inner) {
		return line
	}

	// Replace "id":"<number>" with "id":<number>
	result := stringNumericIDRe.ReplaceAllFunc(line, func(match []byte) []byte {
		submatches := stringNumericIDRe.FindSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		return []byte(fmt.Sprintf(`"id":%s`, submatches[1]))
	})

	slog.Debug("acp stdout filter: fixed string-numeric ID", "original", string(trimmed), "fixed", string(result[:minInt(len(result), 100)]))
	return result
}

// isDigits returns true if all bytes in s are ASCII digits.
func isDigits(s []byte) bool {
	for _, b := range s {
		if b < '0' || b > '9' {
			return false
		}
	}
	return len(s) > 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetAndClearCachedModels returns the cached SessionModelState and clears it.
// Returns nil if no models were cached. Thread-safe.
func (f *acpStdoutFilter) GetAndClearCachedModels() *ModelListState {
	f.modelsMu.Lock()
	defer f.modelsMu.Unlock()
	ml := f.cachedModels
	f.cachedModels = nil
	return ml
}

// cacheModelsFromResponse extracts the "models" field from a JSON-RPC response
// and caches it. The response is expected to have a "result.models" structure
// matching the ACP SessionModelState extension used by kimi and similar agents.
func (f *acpStdoutFilter) cacheModelsFromResponse(line []byte) {
	// Parse as generic JSON-RPC to get the "result" field
	var msg struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &msg); err != nil || len(msg.Result) == 0 {
		return
	}

	// Extract the "models" field from the result
	var result struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(msg.Result, &result); err != nil || len(result.Models) == 0 {
		return
	}

	// Parse the SessionModelState
	var state acpSessionModelState
	if err := json.Unmarshal(result.Models, &state); err != nil {
		return
	}

	// Convert to ModelListState. Return nil for empty state, consistent
	// with buildModelListStateFromSelect in acp_state_extract.go.
	if len(state.AvailableModels) == 0 && state.CurrentModelID == "" {
		return
	}

	ml := &ModelListState{
		CurrentModelID: state.CurrentModelID,
	}
	for _, m := range state.AvailableModels {
		ml.Models = append(ml.Models, model.AgentModel{
			ID:   m.ModelID,
			Name: m.Name,
		})
	}

	f.modelsMu.Lock()
	f.cachedModels = ml
	f.modelsMu.Unlock()

	slog.Info("acp stdout filter: cached SessionModelState extension", "current", state.CurrentModelID, "available", len(state.AvailableModels))
}

// acpSessionModelState mirrors the ACP SessionModelState extension that some
// agents (e.g., kimi) return in NewSessionResponse / ResumeSessionResponse.
// The ACP Go SDK v0.13.5 does not include this field, so we extract it from
// raw JSON in the stdout filter.
type acpSessionModelState struct {
	AvailableModels []acpModelInfo `json:"availableModels"`
	CurrentModelID  string         `json:"currentModelId"`
}

type acpModelInfo struct {
	ModelID string `json:"model_id"`
	Name    string `json:"name"`
}
