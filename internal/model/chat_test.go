package model

import (
	"encoding/json"
	"testing"
)

func TestContentBlockToolUseMarshalSlim(t *testing.T) {
	// tool_use blocks should serialize without input/output
	block := ContentBlock{
		Type:     "tool_use",
		Name:     "Read",
		ID:       "t1",
		Input:    map[string]any{"file_path": "/a.go", "content": "very long content..."},
		Output:   "file contents here",
		Status:   "success",
		Done:     true,
		Summary:  "a.go",
		FilePath: "/a.go",
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Should have slim fields
	if parsed["type"] != "tool_use" {
		t.Errorf("expected type=tool_use, got %v", parsed["type"])
	}
	if parsed["name"] != "Read" {
		t.Errorf("expected name=Read, got %v", parsed["name"])
	}
	if parsed["id"] != "t1" {
		t.Errorf("expected id=t1, got %v", parsed["id"])
	}
	if parsed["summary"] != "a.go" {
		t.Errorf("expected summary=a.go, got %v", parsed["summary"])
	}

	// Should NOT have input/output
	if _, ok := parsed["input"]; ok {
		t.Error("input should not be present in slim serialization")
	}
	if _, ok := parsed["output"]; ok {
		t.Error("output should not be present in slim serialization")
	}
}

func TestContentBlockTextMarshalFull(t *testing.T) {
	// text blocks should serialize normally (full content)
	block := ContentBlock{
		Type: "text",
		Text: "Hello world",
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if parsed["type"] != "text" {
		t.Errorf("expected type=text, got %v", parsed["type"])
	}
	if parsed["text"] != "Hello world" {
		t.Errorf("expected text='Hello world', got %v", parsed["text"])
	}
}

func TestContentBlockUnmarshalOldFormat(t *testing.T) {
	// Old format with input/output should still deserialize correctly
	raw := `{"type":"tool_use","name":"Read","id":"t1","input":{"file_path":"/a.go"},"output":"contents","status":"success","done":true}`

	var block ContentBlock
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if block.Type != "tool_use" {
		t.Errorf("expected type=tool_use, got %s", block.Type)
	}
	if block.Name != "Read" {
		t.Errorf("expected name=Read, got %s", block.Name)
	}
	if block.Input["file_path"] != "/a.go" {
		t.Errorf("expected input file_path=/a.go, got %v", block.Input["file_path"])
	}
	if block.Output != "contents" {
		t.Errorf("expected output=contents, got %s", block.Output)
	}
}

func TestContentBlockSlimUnmarshal(t *testing.T) {
	// Slim format (no input/output) should deserialize with nil input
	raw := `{"type":"tool_use","name":"Bash","id":"t2","status":"success","done":true,"summary":"ls -la"}`

	var block ContentBlock
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if block.Name != "Bash" {
		t.Errorf("expected name=Bash, got %s", block.Name)
	}
	if block.Summary != "ls -la" {
		t.Errorf("expected summary=ls -la, got %s", block.Summary)
	}
	if block.Input != nil {
		t.Errorf("expected nil input for slim format, got %v", block.Input)
	}
}

func TestContentBlockInteractiveToolMarshalWithInput(t *testing.T) {
	// AskUserQuestion blocks should serialize WITH input for frontend rendering
	block := ContentBlock{
		Type:  "tool_use",
		Name:  "AskUserQuestion",
		ID:    "ask-123",
		Input: map[string]any{"questions": []map[string]any{{"question": "Which approach?", "options": []map[string]any{{"label": "A"}}}}},
		Done:  true,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Should include input
	if _, ok := parsed["input"]; !ok {
		t.Error("AskUserQuestion should include input in serialization")
	}
	input, _ := parsed["input"].(map[string]any)
	if input == nil {
		t.Fatal("input should not be nil")
	}
	questions, _ := input["questions"].([]any)
	if len(questions) != 1 {
		t.Errorf("expected 1 question, got %d", len(questions))
	}
}

func TestContentBlockPermissionApprovalMarshalWithInput(t *testing.T) {
	// PermissionApproval blocks should serialize WITH input
	block := ContentBlock{
		Type:  "tool_use",
		Name:  "PermissionApproval",
		ID:    "perm-456",
		Input: map[string]any{"tool_name": "Bash", "command": "rm -rf /tmp"},
		Done:  true,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Should include input
	if _, ok := parsed["input"]; !ok {
		t.Error("PermissionApproval should include input in serialization")
	}
}

func TestContentBlockRegularToolStillSlim(t *testing.T) {
	// Regular tool_use blocks (not interactive) should still use slim serialization
	block := ContentBlock{
		Type:   "tool_use",
		Name:   "Read",
		ID:     "t3",
		Input:  map[string]any{"file_path": "/test.go"},
		Output: "contents",
		Done:   true,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Should NOT have input/output
	if _, ok := parsed["input"]; ok {
		t.Error("Read tool should not include input in slim serialization")
	}
	if _, ok := parsed["output"]; ok {
		t.Error("Read tool should not include output in slim serialization")
	}
}
