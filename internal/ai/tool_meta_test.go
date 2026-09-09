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

		// PermissionApproval special case
		{
			name:     "PermissionApproval command",
			toolName: "PermissionApproval",
			input: map[string]any{
				"toolName":  "Bash",
				"toolInput": `{"command":"rm -rf /tmp/cache"}`,
			},
			want: "rm -rf /tmp/cache",
		},
		{
			name:     "PermissionApproval file_path basename",
			toolName: "PermissionApproval",
			input: map[string]any{
				"toolName":  "Edit",
				"toolInput": `{"file_path":"/home/user/project/main.go"}`,
			},
			want: "main.go",
		},
		{
			name:     "PermissionApproval falls back to toolName",
			toolName: "PermissionApproval",
			input: map[string]any{
				"toolName":  "Bash",
				"toolInput": "not-json",
			},
			want: "Bash",
		},
		{
			name:     "PermissionApproval no toolName",
			toolName: "PermissionApproval",
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
			name:     "agent description wins over prompt",
			toolName: "Agent",
			input: map[string]any{
				"description": "Minimal reply task",
				"prompt":      "Reply with exactly the single word DONE and nothing else.",
			},
			want: "Minimal reply task",
		},
		{
			name:     "codex subagent lifecycle activity summary",
			toolName: "Agent",
			input: map[string]any{
				"activityKind":  "started",
				"agentPath":     "/root/codebase_research",
				"agentThreadId": "thr-01",
			},
			want: "started codebase_research",
		},
		{
			name:     "codex subagent complete activity summary",
			toolName: "Agent",
			input: map[string]any{
				"activityKind":  "completed",
				"agentPath":     "/root/codebase_research",
				"agentThreadId": "thr-01",
			},
			want: "completed codebase_research",
		},
		{
			name:     "codex subagent lifecycle without agentPath",
			toolName: "Agent",
			input:    map[string]any{"activityKind": "interrupted", "agentThreadId": "thr-01"},
			want:     "interrupted ",
		},
		{
			name:     "wait collab empty summary",
			toolName: "wait",
			input: map[string]any{
				"agentsStates":      map[string]any{},
				"senderThreadId":    "root-1",
				"receiverThreadIds": []any{},
				"status":            "inProgress",
			},
			want: "",
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
			name:     "taskupdate default input shows id and status",
			toolName: "TaskUpdate",
			input:    map[string]any{"status": "in_progress", "taskId": "3"},
			want:     "#3 · in_progress",
		},
		{
			name:     "taskupdate completed",
			toolName: "TaskUpdate",
			input:    map[string]any{"status": "completed", "taskId": "14"},
			want:     "#14 · completed",
		},
		{
			name:     "taskupdate deleted",
			toolName: "TaskUpdate",
			input:    map[string]any{"status": "deleted", "taskId": "4"},
			want:     "#4 · deleted",
		},
		{
			name:     "taskupdate taskId only when status missing",
			toolName: "TaskUpdate",
			input:    map[string]any{"taskId": "7"},
			want:     "#7",
		},
		{
			name:     "taskupdate no taskId defers to chain",
			toolName: "TaskUpdate",
			input:    map[string]any{"foo": "bar"},
			want:     "bar",
		},
		{
			name:     "taskupdate subject override wins",
			toolName: "TaskUpdate",
			input: map[string]any{
				"subject": "Fix auth bug",
				"status":  "in_progress",
				"taskId":  "3",
			},
			want: "Fix auth bug",
		},
		{
			name:     "taskupdate description override wins",
			toolName: "TaskUpdate",
			input: map[string]any{
				"description": "Roll back config",
				"status":      "completed",
				"taskId":      "9",
			},
			want: "Roll back config",
		},
		{
			name:     "taskupdate case-insensitive special case",
			toolName: "taskupdate",
			input:    map[string]any{"status": "in_progress", "taskId": "3"},
			want:     "#3 · in_progress",
		},
		{
			name:     "taskupdate composed label truncates to 200 runes",
			toolName: "TaskUpdate",
			input:    map[string]any{"status": repeatStr("s", 250), "taskId": "3"},
			want:     "#3 · " + repeatStr("s", 250)[:195], // 5 ("#3 · ") + 195 = 200
		},
		{
			name:     "deterministic fallback picks lexicographically first key",
			toolName: "Unknown",
			input:    map[string]any{"zeta": "last", "alpha": "first"},
			want:     "first",
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
			name:     "codex Agent lifecycle frame display name from agentPath basename",
			toolName: "Agent",
			input: map[string]any{
				"activityKind":  "started",
				"agentPath":     "/root/codebase_research",
				"agentThreadId": "thr-01",
			},
			want: "codebase_research",
		},
		{
			name:     "codex Agent lifecycle frame display name falls back to activity",
			toolName: "Agent",
			input: map[string]any{
				"activityKind":  "interrupted",
				"agentThreadId": "thr-01",
			},
			want: "interrupted",
		},
		{
			name:     "non-Agent tool ignored",
			toolName: "Read",
			input:    map[string]any{"subagent_type": "Explore"},
			want:     "",
		},
		{
			name:     "DeepThink with subagent_type",
			toolName: "DeepThink",
			input:    map[string]any{"subagent_type": "explore", "description": "Explore project"},
			want:     "explore",
		},
		{
			name:     "DeepThink without subagent_type",
			toolName: "DeepThink",
			input:    map[string]any{"description": "Explore project"},
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
			toolName: "Read",
			input:    map[string]any{"path": "/src/main.go"},
			want:     "/src/main.go",
		},
		{
			name:     "LS directory path is not a file identity",
			toolName: "LS",
			input:    map[string]any{"path": "/src"},
			want:     "",
		},
		{
			name:     "Glob pattern path is not a file identity",
			toolName: "Glob",
			input:    map[string]any{"path": "src/**/*.ts"},
			want:     "",
		},
		{
			name:     "Bash filename field is not promoted",
			toolName: "Bash",
			input:    map[string]any{"command": "curl -o /tmp/a.go", "filename": "/tmp/a.go"},
			want:     "",
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
		{
			name:     "new_file_path preferred over old_file_path",
			toolName: "Edit",
			input:    map[string]any{"old_file_path": "/src/old.go", "new_file_path": "/src/new.go"},
			want:     "/src/new.go",
		},
		{
			name:     "file_path still beats old_file_path",
			toolName: "Edit",
			input:    map[string]any{"file_path": "/src/a.go", "old_file_path": "/src/b.go"},
			want:     "/src/a.go",
		},
		{
			name:     "filename fallback",
			toolName: "Write",
			input:    map[string]any{"filename": "out.txt"},
			want:     "out.txt",
		},
		{
			name:     "camelCase filePath fallback",
			toolName: "Edit",
			input:    map[string]any{"filePath": "/src/camel.go"},
			want:     "/src/camel.go",
		},
		{
			name:     "file_paths array takes first string",
			toolName: "Edit",
			input:    map[string]any{"file_paths": []any{"/src/a.go", "/src/b.go"}},
			want:     "/src/a.go",
		},
		{
			name:     "locations array takes first object path",
			toolName: "Edit",
			input:    map[string]any{"locations": []any{map[string]any{"path": "/src/loc.go"}}},
			want:     "/src/loc.go",
		},
		{
			name:     "single location object with file_path",
			toolName: "Edit",
			input:    map[string]any{"location": map[string]any{"file_path": "/src/nest.go"}},
			want:     "/src/nest.go",
		},
		{
			name:     "empty array yields empty",
			toolName: "Edit",
			input:    map[string]any{"file_paths": []any{}},
			want:     "",
		},
		{
			name:     "non-path command string is not captured",
			toolName: "Bash",
			input:    map[string]any{"command": "node script.js x.ts"},
			want:     "",
		},
		{
			name:     "location without path yields empty",
			toolName: "Edit",
			input:    map[string]any{"location": map[string]any{"range": "1-5"}},
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

func TestExtractAskUserQuestionSummary(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name: "header takes priority over question",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": "Approach", "question": "Which approach?"},
				},
			},
			want: "Approach",
		},
		{
			name: "question when no header",
			input: map[string]any{
				"questions": []any{
					map[string]any{"question": "Which approach?"},
				},
			},
			want: "Which approach?",
		},
		{
			name: "no questions key",
			input: map[string]any{
				"other": "value",
			},
			want: "",
		},
		{
			name:  "empty input",
			input: map[string]any{},
			want:  "",
		},
		{
			name: "questions is not a slice",
			input: map[string]any{
				"questions": "not a slice",
			},
			want: "",
		},
		{
			name: "questions is empty slice",
			input: map[string]any{
				"questions": []any{},
			},
			want: "",
		},
		{
			name: "first question is not a map",
			input: map[string]any{
				"questions": []any{"just a string"},
			},
			want: "",
		},
		{
			name: "header is empty string, falls to question",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": "", "question": "Fallback question"},
				},
			},
			want: "Fallback question",
		},
		{
			name: "both header and question empty",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": "", "question": ""},
				},
			},
			want: "",
		},
		{
			name: "header is non-string type",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": float64(42), "question": "Numeric header"},
				},
			},
			want: "Numeric header",
		},
		{
			name: "truncation of long header",
			input: map[string]any{
				"questions": []any{
					map[string]any{"header": repeatStr("x", 300)},
				},
			},
			want: repeatStr("x", 200),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAskUserQuestionSummary(tt.input)
			if got != tt.want {
				t.Errorf("extractAskUserQuestionSummary(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSummaryPriorityFields(t *testing.T) {
	// Verify the priority order and format functions in summaryPriorityFields.
	// This ensures the table-driven approach matches the documented priority chain:
	// description > file_path > command > pattern > query > url > skill > path
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "description field",
			input: map[string]any{"description": "do something"},
			want:  "do something",
		},
		{
			name:  "file_path field uses basename",
			input: map[string]any{"file_path": "/home/user/project/main.go"},
			want:  "main.go",
		},
		{
			name:  "command field",
			input: map[string]any{"command": "npm test"},
			want:  "npm test",
		},
		{
			name:  "pattern field",
			input: map[string]any{"pattern": "func Foo"},
			want:  "func Foo",
		},
		{
			name:  "query field",
			input: map[string]any{"query": "search term"},
			want:  "search term",
		},
		{
			name:  "url field",
			input: map[string]any{"url": "https://example.com"},
			want:  "https://example.com",
		},
		{
			name:  "skill field",
			input: map[string]any{"skill": "commit"},
			want:  "commit",
		},
		{
			name:  "path field uses basename",
			input: map[string]any{"path": "/home/user/project/src"},
			want:  "src",
		},
		{
			name:  "description over file_path",
			input: map[string]any{"description": "read config", "file_path": "/src/config.yaml"},
			want:  "read config",
		},
		{
			name:  "file_path over command",
			input: map[string]any{"file_path": "/src/main.go", "command": "cat main.go"},
			want:  "main.go",
		},
		{
			name:  "empty string field is skipped",
			input: map[string]any{"description": "", "file_path": "/src/main.go"},
			want:  "main.go",
		},
		{
			name:  "non-string field value is skipped",
			input: map[string]any{"description": float64(42), "file_path": "/src/main.go"},
			want:  "main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSummary("AnyTool", tt.input)
			if got != tt.want {
				t.Errorf("ExtractSummary(%v) = %q, want %q", tt.input, got, tt.want)
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
