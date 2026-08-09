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

func TestContentBlockToolUseMarshalIncludesDurationMs(t *testing.T) {
	// duration_ms should survive slim serialization so the frontend can display it
	block := ContentBlock{
		Type:       "tool_use",
		Name:       "Bash",
		ID:         "t-dur",
		Done:       true,
		Summary:    "ls -la",
		DurationMs: 5200,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed["duration_ms"] != float64(5200) {
		t.Errorf("expected duration_ms=5200, got %v", parsed["duration_ms"])
	}

	// Zero duration is omitted
	zero := ContentBlock{Type: "tool_use", Name: "Read", ID: "t-z", Done: true}
	zeroData, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var zeroParsed map[string]any
	if err := json.Unmarshal(zeroData, &zeroParsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := zeroParsed["duration_ms"]; ok {
		t.Error("duration_ms should be omitted when 0")
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

func TestChatMessageUnmarshalOldFilesFormat(t *testing.T) {
	raw := `{"role":"user","content":"hello","files":["/a.go","/b.ts"]}`
	var msg ChatMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}
	if len(msg.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(msg.Files))
	}
	if msg.Files[0].Path != "/a.go" || msg.Files[0].IsDir != false {
		t.Errorf("file[0] = %+v, want {Path:/a.go, IsDir:false}", msg.Files[0])
	}
	if msg.Files[1].Path != "/b.ts" || msg.Files[1].IsDir != false {
		t.Errorf("file[1] = %+v, want {Path:/b.ts, IsDir:false}", msg.Files[1])
	}
}

func TestChatMessageUnmarshalNewFilesFormat(t *testing.T) {
	raw := `{"role":"user","content":"hello","files":[{"path":"/src","isDir":true},{"path":"/main.go","isDir":false}]}`
	var msg ChatMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal new format: %v", err)
	}
	if len(msg.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(msg.Files))
	}
	if msg.Files[0].Path != "/src" || !msg.Files[0].IsDir {
		t.Errorf("file[0] = %+v, want {Path:/src, IsDir:true}", msg.Files[0])
	}
	if msg.Files[1].Path != "/main.go" || msg.Files[1].IsDir {
		t.Errorf("file[1] = %+v, want {Path:/main.go, IsDir:false}", msg.Files[1])
	}
}

func TestChatMessageUnmarshalNoFiles(t *testing.T) {
	raw := `{"role":"user","content":"hello"}`
	var msg ChatMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal no files: %v", err)
	}
	if msg.Files != nil {
		t.Errorf("expected nil files, got %v", msg.Files)
	}
}

func TestFileEntriesFromPaths(t *testing.T) {
	entries := FileEntriesFromPaths([]string{"/a.go", "/b.ts"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Path != "/a.go" || entries[0].IsDir != false {
		t.Errorf("entry[0] = %+v, want {Path:/a.go, IsDir:false}", entries[0])
	}
	if FileEntriesFromPaths(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if FileEntriesFromPaths([]string{}) != nil {
		t.Error("expected nil for empty input")
	}
}

func TestPathsFromFileEntries(t *testing.T) {
	paths := PathsFromFileEntries([]FileEntry{{Path: "/a.go"}, {Path: "/src", IsDir: true}})
	if len(paths) != 2 || paths[0] != "/a.go" || paths[1] != "/src" {
		t.Errorf("expected [/a.go /src], got %v", paths)
	}
	if PathsFromFileEntries(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestSummaryCardsRoundTrip(t *testing.T) {
	cards := SummaryCards{
		Tools: []SummaryTool{{
			Name:   "Bash",
			ID:     "tool-1",
			Input:  map[string]any{"command": "ls"},
			Done:   true,
			Status: "error",
			Output: "Cancelled",
		}},
		TaskIDs: []int64{42},
		CreatedFiles:  []string{"/src/new.go"},
		ModifiedFiles: []string{"/src/a.go"},
		AskQuestions: []AskQuestionCard{{
			Header:      "",
			MultiSelect: false,
			Question:    "Continue?",
			Options:     []AskQuestionOption{{Label: "Yes"}, {Label: "No"}},
		}},
	}
	raw, err := json.Marshal(cards)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SummaryCards
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Tools) != 1 || back.Tools[0].Name != "Bash" {
		t.Fatalf("tools mismatch: %+v", back.Tools)
	}
	if !back.Tools[0].Done || back.Tools[0].Status != "error" || back.Tools[0].Output != "Cancelled" {
		t.Fatalf("tools result state mismatch: %+v", back.Tools[0])
	}
	if len(back.TaskIDs) != 1 || back.TaskIDs[0] != 42 {
		t.Fatalf("taskIDs mismatch: %+v", back.TaskIDs)
	}
	if len(back.CreatedFiles) != 1 || back.CreatedFiles[0] != "/src/new.go" {
		t.Fatalf("createdFiles mismatch: %+v", back.CreatedFiles)
	}
	if len(back.ModifiedFiles) != 1 || back.ModifiedFiles[0] != "/src/a.go" {
		t.Fatalf("modifiedFiles mismatch: %+v", back.ModifiedFiles)
	}
	if len(back.AskQuestions) != 1 || back.AskQuestions[0].Question != "Continue?" {
		t.Fatalf("askQuestions mismatch: %+v", back.AskQuestions)
	}
}

func TestChatMessageSummaryCardsMarshal(t *testing.T) {
	m := ChatMessage{ID: 1, Role: "assistant", Summary: strPtr("hi"), SummaryCards: &SummaryCards{TaskIDs: []int64{1}}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["summary"] != "hi" {
		t.Fatalf("summary missing: %v", obj["summary"])
	}
	if obj["summaryCards"] == nil {
		t.Fatalf("summaryCards missing: %v", obj["summaryCards"])
	}
}

func strPtr(s string) *string { return &s }
