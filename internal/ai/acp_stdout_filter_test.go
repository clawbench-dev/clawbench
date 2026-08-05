package ai

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
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
