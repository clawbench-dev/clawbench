package ai

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACPStdoutFilter_PassesValidJSON(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}
{"jsonrpc":"2.0","id":2,"result":{"data":"hello"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	_, err := io.Copy(&buf, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"id":1`) {
		t.Errorf("expected output to contain id:1, got: %q", output)
	}
	if !strings.Contains(output, `"id":2`) {
		t.Errorf("expected output to contain id:2, got: %q", output)
	}
}

func TestACPStdoutFilter_FixesStringNumericID(t *testing.T) {
	// CodeWhale returns "id":"1" (string) when the request sent "id":1 (number)
	input := `{"jsonrpc":"2.0","id":"1","result":{"status":"ok"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	_, err := io.Copy(&buf, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"id":1`) {
		t.Errorf("expected string ID to be fixed to numeric, got: %q", output)
	}
	if strings.Contains(output, `"id":"1"`) {
		t.Errorf("string ID should have been converted to numeric, got: %q", output)
	}
}

func TestACPStdoutFilter_StripsNonJSONLines(t *testing.T) {
	input := "\x1b[?1004l\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\nsome noise\n{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n"
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	_, err := io.Copy(&buf, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "noise") {
		t.Errorf("expected non-JSON lines to be stripped, got: %q", output)
	}
	if strings.Contains(output, "\x1b") {
		t.Errorf("expected escape sequences to be stripped, got: %q", output)
	}
	// Should have 2 JSON lines
	lines := strings.Count(output, "\n")
	if lines != 2 {
		t.Errorf("expected 2 JSON lines, got %d lines: %q", lines, output)
	}
}

func TestACPStdoutFilter_CloseUnblocksRead(t *testing.T) {
	// Create a reader that never produces data (simulates a process whose stdout
	// pipe hasn't been closed yet after the process is killed)
	r, w := io.Pipe()
	f := newACPStdoutFilter(r)

	// Start a read that should block
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		f.Read(buf) // should unblock when Close is called
	}()

	// Give the goroutine time to start reading
	time.Sleep(50 * time.Millisecond)

	// Close the filter — this should unblock the Read
	f.Close()

	// Also close the pipe writer to clean up the pump goroutine
	w.Close()

	select {
	case <-done:
		// Success — Read was unblocked
	case <-time.After(2 * time.Second):
		t.Fatal("Read was not unblocked by Close within 2 seconds")
	}
}

func TestACPStdoutFilter_CloseIdempotent(t *testing.T) {
	f := newACPStdoutFilter(strings.NewReader(""))
	// Calling Close multiple times should not panic
	f.Close()
	f.Close()
	f.Close()
}

func TestFixStringNumericID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "numeric ID unchanged",
			input:    `{"jsonrpc":"2.0","id":1,"result":{}}`,
			expected: `{"jsonrpc":"2.0","id":1,"result":{}}`,
		},
		{
			name:     "string numeric ID fixed",
			input:    `{"jsonrpc":"2.0","id":"1","result":{}}`,
			expected: `{"jsonrpc":"2.0","id":1,"result":{}}`,
		},
		{
			name:     "string non-numeric ID unchanged",
			input:    `{"jsonrpc":"2.0","id":"abc","result":{}}`,
			expected: `{"jsonrpc":"2.0","id":"abc","result":{}}`,
		},
		{
			name:     "no id field unchanged",
			input:    `{"jsonrpc":"2.0","method":"notify","params":{}}`,
			expected: `{"jsonrpc":"2.0","method":"notify","params":{}}`,
		},
		{
			name:     "multi-digit string ID fixed",
			input:    `{"jsonrpc":"2.0","id":"42","result":{}}`,
			expected: `{"jsonrpc":"2.0","id":42,"result":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fixStringNumericID([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("fixStringNumericID(%q) = %q, want %q", tt.input, string(result), tt.expected)
			}
		})
	}
}

func TestACPStdoutFilter_ExtractsModelsFromSessionNewResponse(t *testing.T) {
	// Simulates a kimi ACP session/new response with SessionModelState
	input := `{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[{"model_id":"kimi-code/k3","name":"Kimi K3"},{"model_id":"kimi-code/k3,thinking","name":"Kimi K3 (thinking)"},{"model_id":"kimi-code/kimi-for-coding","name":"Kimi K2.7 Code"}],"currentModelId":"kimi-code/k3"},"modes":{"availableModes":[{"id":"default","name":"Default"}],"currentModeId":"default"},"sessionId":"test-123"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	// Read all output to ensure pump processes the line
	var buf bytes.Buffer
	io.Copy(&buf, f)

	// Check that models were cached
	cached := f.GetAndClearCachedModels()
	if cached == nil {
		t.Fatal("expected cached models to be non-nil")
	}
	if cached.CurrentModelID != "kimi-code/k3" {
		t.Errorf("expected currentModelId 'kimi-code/k3', got %q", cached.CurrentModelID)
	}
	if len(cached.Models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(cached.Models))
	}
	if cached.Models[0].ID != "kimi-code/k3" {
		t.Errorf("expected first model ID 'kimi-code/k3', got %q", cached.Models[0].ID)
	}
	if cached.Models[0].Name != "Kimi K3" {
		t.Errorf("expected first model name 'Kimi K3', got %q", cached.Models[0].Name)
	}
	if cached.Models[2].Name != "Kimi K2.7 Code" {
		t.Errorf("expected third model name 'Kimi K2.7 Code', got %q", cached.Models[2].Name)
	}

	// Verify the JSON was still passed through to the reader (not consumed)
	output := buf.String()
	if !strings.Contains(output, "availableModels") {
		t.Errorf("expected output to still contain the original JSON, got: %q", output)
	}
}

func TestACPStdoutFilter_ExtractsModelsEmptyList(t *testing.T) {
	// kimi ACP returns empty availableModels when not logged in.
	// Empty models with empty currentModelId should not be cached (nil),
	// consistent with buildModelListStateFromSelect behavior.
	input := `{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[],"currentModelId":""},"sessionId":"test-456"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	io.Copy(&buf, f)

	cached := f.GetAndClearCachedModels()
	if cached != nil {
		t.Fatal("expected nil cached models when availableModels is empty and currentModelId is empty")
	}
}

func TestACPStdoutFilter_GetAndClearCachedModels_ClearsAfterRead(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[{"model_id":"m1","name":"Model 1"}],"currentModelId":"m1"},"sessionId":"test"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	io.Copy(&buf, f)

	// First read should return the cached models
	cached := f.GetAndClearCachedModels()
	if cached == nil {
		t.Fatal("expected cached models to be non-nil on first read")
	}

	// Second read should return nil (cleared)
	cached2 := f.GetAndClearCachedModels()
	if cached2 != nil {
		t.Fatal("expected cached models to be nil after clearing")
	}
}

func TestACPStdoutFilter_NoModelsInResponse(t *testing.T) {
	// Response without models field should not cache anything
	input := `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}
{"jsonrpc":"2.0","id":2,"result":{"modes":{"availableModes":[{"id":"default","name":"Default"}],"currentModeId":"default"},"sessionId":"test"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	io.Copy(&buf, f)

	cached := f.GetAndClearCachedModels()
	if cached != nil {
		t.Fatal("expected no cached models when response has no models field")
	}
}

func TestFixStringNumericID_InvalidJSON(t *testing.T) {
	// When line contains "id" but is not valid JSON, return as-is
	input := `{"id":"1" this is not valid json`
	result := fixStringNumericID([]byte(input))
	assert.Equal(t, input, string(result), "invalid JSON should be returned as-is")
}

func TestFixStringNumericID_NoIdField(t *testing.T) {
	// When JSON has no "id" field at all, return as-is
	input := `{"jsonrpc":"2.0","result":{"status":"ok"}}`
	result := fixStringNumericID([]byte(input))
	assert.Equal(t, input, string(result))
}

func TestFixStringNumericID_IdNotString(t *testing.T) {
	// When "id" is already a number, return as-is
	input := `{"jsonrpc":"2.0","id":42,"result":{}}`
	result := fixStringNumericID([]byte(input))
	assert.Equal(t, input, string(result))
}

func TestFixStringNumericID_IdStringNotNumeric(t *testing.T) {
	// When "id" is a string but not numeric, return as-is
	input := `{"jsonrpc":"2.0","id":"abc","result":{}}`
	result := fixStringNumericID([]byte(input))
	assert.Equal(t, input, string(result))
}

func TestFixStringNumericID_RegexMatchButNotNumeric(t *testing.T) {
	// When regex matches "id":"<digits>" but the actual JSON id field is not
	// a string (e.g., edge case where another field has "id" in it)
	input := `{"jsonrpc":"2.0","method":"someMethod","params":{"id":"123"},"result":{}}`
	result := fixStringNumericID([]byte(input))
	// The top-level "id" field doesn't exist, so the check at msg["id"] fails
	assert.Equal(t, input, string(result))
}

func TestIsDigits(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"12a", false},
		{" 1", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDigits([]byte(tt.input)))
		})
	}
}

func TestMinInt(t *testing.T) {
	assert.Equal(t, 1, minInt(1, 2))
	assert.Equal(t, 3, minInt(5, 3))
	assert.Equal(t, 0, minInt(0, 0))
}

func TestACPStdoutFilter_CacheModelsFromResponse_InvalidJSON(t *testing.T) {
	// When the line is not valid JSON, cacheModelsFromResponse should return without caching
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	// Directly call cacheModelsFromResponse with invalid JSON
	f.cacheModelsFromResponse([]byte(`not valid json at all`))

	cached := f.GetAndClearCachedModels()
	assert.Nil(t, cached, "invalid JSON should not cache models")
}

func TestACPStdoutFilter_CacheModelsFromResponse_NoResultField(t *testing.T) {
	// When JSON has no "result" field, should not cache
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	f.cacheModelsFromResponse([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error"}}`))

	cached := f.GetAndClearCachedModels()
	assert.Nil(t, cached, "no result field should not cache models")
}

func TestACPStdoutFilter_CacheModelsFromResponse_NoModelsField(t *testing.T) {
	// When result has no "models" field, should not cache
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	f.cacheModelsFromResponse([]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"test-123"}}`))

	cached := f.GetAndClearCachedModels()
	assert.Nil(t, cached, "no models field should not cache models")
}

func TestACPStdoutFilter_CacheModelsFromResponse_InvalidModelsJSON(t *testing.T) {
	// When "models" field is not valid SessionModelState, should not cache
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	f.cacheModelsFromResponse([]byte(`{"jsonrpc":"2.0","id":2,"result":{"models":"not an object"}}`))

	cached := f.GetAndClearCachedModels()
	assert.Nil(t, cached, "invalid models JSON should not cache models")
}

func TestACPStdoutFilter_CacheModelsFromResponse_EmptyModelsNotCached(t *testing.T) {
	// When availableModels is empty and currentModelId is empty, should not cache
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	f.cacheModelsFromResponse([]byte(`{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[],"currentModelId":""}}}`))

	cached := f.GetAndClearCachedModels()
	assert.Nil(t, cached, "empty models should not be cached")
}

func TestACPStdoutFilter_CacheModelsFromResponse_CurrentModelIDOnly(t *testing.T) {
	// When there's a currentModelId but no availableModels, should still cache
	f := newACPStdoutFilter(strings.NewReader(""))
	defer f.Close()

	f.cacheModelsFromResponse([]byte(`{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[],"currentModelId":"kimi-code/k3"}}}`))

	cached := f.GetAndClearCachedModels()
	require.NotNil(t, cached, "currentModelId with empty availableModels should be cached")
	assert.Equal(t, "kimi-code/k3", cached.CurrentModelID)
	assert.Empty(t, cached.Models)
}

func TestACPStdoutFilter_PumpSourceEOF(t *testing.T) {
	// When source reaches EOF, the pipe writer should be closed
	// so the reader side gets EOF too
	input := `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	n, err := io.Copy(&buf, f)
	assert.NoError(t, err)
	assert.Greater(t, n, int64(0))
	assert.Contains(t, buf.String(), `"status":"ok"`)
}

func TestACPStdoutFilter_EmptyLinesStripped(t *testing.T) {
	// Empty lines should be stripped
	input := "\n\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	io.Copy(&buf, f)

	lines := strings.Count(buf.String(), "\n")
	assert.Equal(t, 1, lines, "only one JSON line should remain")
}

func TestACPStdoutFilter_MultipleLinesWithModels(t *testing.T) {
	// Multiple lines where one contains availableModels
	input := `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}
{"jsonrpc":"2.0","id":2,"result":{"models":{"availableModels":[{"model_id":"m1","name":"Model 1"}],"currentModelId":"m1"},"sessionId":"s1"}}
`
	f := newACPStdoutFilter(strings.NewReader(input))
	defer f.Close()

	var buf bytes.Buffer
	io.Copy(&buf, f)

	// Both lines should be passed through
	assert.Contains(t, buf.String(), `"status":"ok"`)
	assert.Contains(t, buf.String(), "availableModels")

	// Models should be cached
	cached := f.GetAndClearCachedModels()
	require.NotNil(t, cached)
	assert.Equal(t, "m1", cached.CurrentModelID)
}
