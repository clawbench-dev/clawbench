package service

import (
	"context"
	"database/sql"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- upsertToolCallToDB happy path coverage ---

func TestSessionExecutor_UpsertToolCallToDB_HappyPath(t *testing.T) {
	setupExecutorDB(t)
	agentID := "tc-db-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, agentID)
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     agentID,
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Set StreamingMessageID so upsertToolCallToDB will actually run
	msgID := GetStreamingMessageID(sid)
	require.True(t, msgID > 0, "expected valid StreamingMessageID, got %d", msgID)
	executor.cfg.StreamingMessageID = msgID

	// Add a tool_use block to accumulated blocks
	executor.blocks = append(executor.blocks, model.ContentBlock{
		Type:   "tool_use",
		ID:     "toolu_happy_001",
		Name:   "Read",
		Input:  map[string]any{"file_path": "/src/main.go"},
		Done:   true,
		Output: "file contents",
		Status: "success",
	})

	// Trigger upsert via a tool_result event matching the block ID
	event := ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{
			ID:     "toolu_happy_001",
			Name:   "Read",
			Done:   true,
			Output: "file contents",
			Status: "success",
		},
	}
	executor.upsertToolCallToDB(event)

	// Verify tool call was persisted
	record, err := GetToolCall("toolu_happy_001", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "expected tool call record to exist")
	assert.Equal(t, "Read", record.Name)
	assert.True(t, record.Done)
}

// --- ToolCalls error path coverage ---

func TestGetToolCall_ClosedDBError(t *testing.T) {
	setupExecutorDB(t)
	agentID := "tc-err-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, agentID)
	msgID := GetStreamingMessageID(sid)
	require.True(t, msgID > 0)

	// Use a closed DB to force a query error
	origDBRead := DBRead
	closedDB, _ := initClosedDB()
	DBRead = closedDB
	t.Cleanup(func() {
		DBRead = origDBRead
	})

	_, err := GetToolCall("toolu_err", msgID)
	assert.Error(t, err)
}

// initClosedDB creates and immediately closes a SQLite DB to produce query errors.
func initClosedDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.Close()
	return db, nil
}

// --- buildContentJSON cancel reason coverage ---

func TestSessionExecutor_BuildContentJSON_WithUserCancel(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "cancel-session",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{CancelReason: "user"}
	meta := &ai.Metadata{}
	contentJSON, blocks := executor.buildContentJSON(nil, result, meta)
	assert.Contains(t, contentJSON, "User cancelled")
	assert.Contains(t, contentJSON, `"cancelled":true`)
	_ = blocks
}

func TestSessionExecutor_BuildContentJSON_WithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "ctx-cancel-session",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)
	cancel() // cancel the context

	result := RunResult{}
	meta := &ai.Metadata{}
	contentJSON, _ := executor.buildContentJSON(nil, result, meta)
	assert.Contains(t, contentJSON, "AI response cancelled")
	assert.Contains(t, contentJSON, `"cancelled":true`)
}
