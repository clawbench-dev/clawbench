package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"clawbench/internal/model"
)

const (
	acpStdoutInitialBuffer = 64 * 1024
	acpStdoutMaxMessage    = 32 * 1024 * 1024
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
//     in NewSessionResponse/ResumeSessionResponse that the ACP schema does not
//     model. The SDK's json.Unmarshal silently drops this field. This filter
//     intercepts the raw JSON and caches the models for later retrieval.
//
// Messages the SDK cannot use are not handled here: the connection reports them
// through SetUnhandledHandler (see acp_ext.go), so this filter only scrubs the
// bytes the SDK would choke on.
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

	// src is the underlying agent stdout. The pump is NOT started in the
	// constructor — it is armed on the first Read (see startOnce). Starting it
	// eagerly raced the sink installation: with an immediately-readable source
	// the pump could drain the first line and dispatch it before the notification
	// sink was installed, so the sink silently missed it (macOS scheduling
	// exposed this; a strings.Reader in tests reproduces it deterministically).
	// Deferring the start means the sink is installed before any line can be
	// dispatched, since the caller installs it before the reader is consumed.
	src       io.Reader
	startOnce sync.Once

	// cachedModels holds the models extracted from the SessionModelState extension
	// in a session/new or session/resume response. The ACP schema does not model a
	// "models" field on NewSessionResponse/ResumeSessionResponse, so we
	// extract it from the raw JSON here. Thread-safe via modelsMu.
	modelsMu     sync.Mutex
	cachedModels *ModelListState
}

// newACPStdoutFilter creates a new filtered reader that fixes protocol violations.
// The caller must call Close when done to release resources and unblock pending reads.
//
// The pump is armed lazily on the first Read rather than started here, so sinks
// installed between construction and the first read (SetNotificationSink) are
// guaranteed to see every line — see the src field.
func newACPStdoutFilter(r io.Reader) *acpStdoutFilter {
	pr, pw := io.Pipe()
	return &acpStdoutFilter{
		pr:  pr,
		pw:  pw,
		src: r,
	}
}

// start arms the pump exactly once. Called from Read; a no-op afterwards.
func (f *acpStdoutFilter) start() {
	f.startOnce.Do(func() {
		go f.pump(f.src)
	})
}

// pump reads lines from src, filters them, and writes to the pipe writer.
func (f *acpStdoutFilter) pump(src io.Reader) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, acpStdoutInitialBuffer), acpStdoutMaxMessage)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Skip non-JSON lines (terminal escape sequences, etc.)
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		// Fix string-number ID mismatch
		fixed := fixStringNumericID(line)

		// Let the SDK decode elicitation schemas whose `type` is implied by
		// enum/oneOf/items (JSON Schema allows omitting it; the SDK's union
		// does not). See acp_elicitation_wire.go.
		fixed = normalizeElicitationSchema(fixed)

		// Extract SessionModelState from session/new or session/resume responses.
		// Some agents (e.g., kimi) return a "models" field that the ACP Go SDK
		// schema does not model in its NewSessionResponse type, so it gets
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
//
// The pump is armed here (not in the constructor) so the caller can finish
// wiring the connection before the first line is processed.
func (f *acpStdoutFilter) Read(p []byte) (int, error) {
	f.start()
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
			ID:      m.ModelID,
			Name:    m.Name,
			Credits: m.credits(),
		})
	}

	f.modelsMu.Lock()
	f.cachedModels = ml
	f.modelsMu.Unlock()

	slog.Info("acp stdout filter: cached SessionModelState extension", "current", state.CurrentModelID, "available", len(state.AvailableModels))
}

// acpSessionModelState mirrors the ACP SessionModelState extension that agents
// (kimi, hermes, …) return in NewSessionResponse / ResumeSessionResponse.
// The ACP schema does not model this field, so we extract it from
// raw JSON in the stdout filter.
type acpSessionModelState struct {
	AvailableModels []acpModelInfo `json:"availableModels"`
	CurrentModelID  string         `json:"currentModelId"`
}

// acpModelInfo mirrors acp.schema.ModelInfo. Its wire alias is camelCase
// "modelId" (the official Python schema declares model_id with
// Field(alias="modelId")), so the JSON tag MUST be modelId — a snake_case tag
// silently decodes every entry to an empty ID.
type acpModelInfo struct {
	ModelID string `json:"modelId"`
	Name    string `json:"name"`
	// Meta is the extension bag. CodeBuddy puts the billing multiplier under
	// `credits` ("x0.06 credits") here; no other agent reports one today.
	Meta map[string]any `json:"_meta"`
}

// credits returns the model's billing multiplier from the extension bag, or ""
// when the agent publishes none. Only a string is accepted: a numeric or
// structured value is not a multiplier we know how to render, and guessing one
// would put a made-up number in front of the user.
func (m acpModelInfo) credits() string {
	if m.Meta == nil {
		return ""
	}
	s, ok := m.Meta["credits"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
