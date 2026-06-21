package ai

import (
	"encoding/json"
	"testing"
)

func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]any
		want     string
	}{
		// AskUserQuestion special case
		{
			name:     "AskUserQuestion with header",
			toolName: "AskUserQuestion",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": "Approach", "question": "Which approach?"},
				},
			},
			want: "Approach",
		},
		{
			name:     "AskUserQuestion with question only",
			toolName: "AskUserQuestion",
			input: map[string]any{
				"questions": []any{
					map[string]any{"question": "Which approach?"},
				},
			},
			want: "Which approach?",
		},
		{
			name:     "AskUserQuestion with no questions",
			toolName: "AskUserQuestion",
			input:    map[string]any{},
			want:     "",
		},

		// Priority chain: description > file_path > command > ...
		{
			name:     "description takes priority",
			toolName: "Read",
			input: map[string]any{
				"description": "Read the config file",
				"file_path":   "/src/config.yaml",
			},
			want: "Read the config file",
		},
		{
			name:     "file_path with basename",
			toolName: "Read",
			input:    map[string]any{"file_path": "/home/user/project/main.go"},
			want:     "main.go",
		},
		{
			name:     "file_path simple filename",
			toolName: "Read",
			input:    map[string]any{"file_path": "config.yaml"},
			want:     "config.yaml",
		},
		{
			name:     "command",
			toolName: "Bash",
			input:    map[string]any{"command": "npm test"},
			want:     "npm test",
		},
		{
			name:     "pattern",
			toolName: "Grep",
			input:    map[string]any{"pattern": "func ExtractSummary"},
			want:     "func ExtractSummary",
		},
		{
			name:     "query",
			toolName: "WebSearch",
			input:    map[string]any{"query": "Go filepath.Base"},
			want:     "Go filepath.Base",
		},
		{
			name:     "url",
			toolName: "WebFetch",
			input:    map[string]any{"url": "https://example.com/api"},
			want:     "https://example.com/api",
		},
		{
			name:     "skill",
			toolName: "Skill",
			input:    map[string]any{"skill": "commit"},
			want:     "commit",
		},
		{
			name:     "prompt only for agent tool",
			toolName: "Agent",
			input:    map[string]any{"prompt": "Research the codebase"},
			want:     "Research the codebase",
		},
		{
			name:     "prompt ignored for non-agent tool",
			toolName: "Bash",
			input:    map[string]any{"prompt": "should be skipped", "command": "ls"},
			want:     "ls",
		},
		{
			name:     "path with basename",
			toolName: "LS",
			input:    map[string]any{"path": "/home/user/project/src"},
			want:     "src",
		},
		{
			name:     "src_path and dst_path",
			toolName: "Edit",
			input: map[string]any{
				"src_path": "/src/old.go",
				"dst_path": "/src/new.go",
			},
			want: "old.go → new.go",
		},
		{
			name:     "only src_path without dst_path falls through",
			toolName: "Edit",
			input:    map[string]any{"src_path": "/src/old.go"},
			want:     "/src/old.go", // falls to first string value
		},
		{
			name:     "first string value fallback",
			toolName: "Unknown",
			input:    map[string]any{"custom_field": "hello"},
			want:     "hello",
		},
		{
			name:     "empty input",
			toolName: "Read",
			input:    map[string]any{},
			want:     "",
		},
		{
			name:     "non-string first value skipped",
			toolName: "Unknown",
			input:    map[string]any{"count": float64(42)},
			want:     "",
		},
		{
			name:     "description over command",
			toolName: "Bash",
			input: map[string]any{
				"description": "Run tests",
				"command":     "npm test",
			},
			want: "Run tests",
		},
		{
			name:     "file_path over command",
			toolName: "Read",
			input: map[string]any{
				"file_path": "/src/main.go",
				"command":   "cat main.go",
			},
			want: "main.go",
		},
		{
			name:     "truncation at 200 chars",
			toolName: "Bash",
			input:    map[string]any{"command": repeatStr("a", 300)},
			want:     repeatStr("a", 200),
		},
		{
			name:     "nil input",
			toolName: "Read",
			input:    nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSummary(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("ExtractSummary(%q, %v) = %q, want %q", tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]any
		want     string
	}{
		{
			name:     "Agent with subagent_type",
			toolName: "Agent",
			input:    map[string]any{"subagent_type": "Explore", "prompt": "research"},
			want:     "Explore",
		},
		{
			name:     "Agent without subagent_type",
			toolName: "Agent",
			input:    map[string]any{"prompt": "research"},
			want:     "",
		},
		{
			name:     "non-Agent tool ignored",
			toolName: "Read",
			input:    map[string]any{"subagent_type": "Explore"},
			want:     "",
		},
		{
			name:     "nil input",
			toolName: "Agent",
			input:    nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDisplayName(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("ExtractDisplayName(%q, %v) = %q, want %q", tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]any
		want     string
	}{
		{
			name:     "file_path present",
			toolName: "Read",
			input:    map[string]any{"file_path": "/src/main.go"},
			want:     "/src/main.go",
		},
		{
			name:     "no file_path, path present",
			toolName: "LS",
			input:    map[string]any{"path": "/src"},
			want:     "/src",
		},
		{
			name:     "file_path takes priority over path",
			toolName: "Write",
			input:    map[string]any{"file_path": "/src/main.go", "path": "/src"},
			want:     "/src/main.go",
		},
		{
			name:     "neither present",
			toolName: "Bash",
			input:    map[string]any{"command": "ls"},
			want:     "",
		},
		{
			name:     "nil input",
			toolName: "Read",
			input:    nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFilePath(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("ExtractFilePath(%q, %v) = %q, want %q", tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractToolCallMeta(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		want  ToolCallMeta
	}{
		{
			name: "tool_use event with input",
			event: StreamEvent{
				Type: "tool_use",
				Tool: &ToolCall{
					Name:   "Read",
					ID:     "toolu_01",
					Input:  `{"file_path":"/src/main.go"}`,
					Output: "",
					Status: "",
					Done:   false,
				},
			},
			want: ToolCallMeta{
				ToolID:      "toolu_01",
				Summary:     "main.go",
				DisplayName: "",
				FilePath:    "/src/main.go",
			},
		},
		{
			name: "tool_result event",
			event: StreamEvent{
				Type: "tool_result",
				Tool: &ToolCall{
					Name:   "Bash",
					ID:     "toolu_02",
					Input:  `{"command":"npm test"}`,
					Output: "ok",
					Status: "success",
					Done:   true,
				},
			},
			want: ToolCallMeta{
				ToolID:      "toolu_02",
				Summary:     "npm test",
				DisplayName: "",
				FilePath:    "",
			},
		},
		{
			name: "Agent tool with subagent_type",
			event: StreamEvent{
				Type: "tool_use",
				Tool: &ToolCall{
					Name:  "Agent",
					ID:    "toolu_03",
					Input: `{"subagent_type":"Explore","prompt":"research"}`,
					Done:  false,
				},
			},
			want: ToolCallMeta{
				ToolID:      "toolu_03",
				Summary:     "research",
				DisplayName: "Explore",
				FilePath:    "",
			},
		},
		{
			name: "non-tool event returns empty meta",
			event: StreamEvent{
				Type:    "content",
				Content: "hello",
			},
			want: ToolCallMeta{},
		},
		{
			name: "tool_use with invalid JSON input",
			event: StreamEvent{
				Type: "tool_use",
				Tool: &ToolCall{
					Name:  "Read",
					ID:    "toolu_04",
					Input: `{invalid`,
					Done:  false,
				},
			},
			want: ToolCallMeta{
				ToolID:  "toolu_04",
				Summary: "",
			},
		},
		{
			name: "nil tool returns empty meta",
			event: StreamEvent{
				Type: "tool_use",
				Tool: nil,
			},
			want: ToolCallMeta{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolCallMeta(tt.event)
			if got != tt.want {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(tt.want)
				t.Errorf("ExtractToolCallMeta() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func repeatStr(s string, n int) string {
	result := make([]byte, 0, len(s)*n)
	for range n {
		result = append(result, s...)
	}
	return string(result)
}
