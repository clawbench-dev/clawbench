package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/kb"
	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// TestBuildChatRequest_InjectsKBContext pins the wiring that puts retrieved
// knowledge base passages in front of the user's question, and the two
// exemptions: the option must be explicitly requested (internal turns do not
// get it), and slash commands must never be prefixed.
func TestBuildChatRequest_InjectsKBContext(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type)
		VALUES ('s-kb', 1, 'codebuddy', 'KB', 'codebuddy', 'chat')`)
	require.NoError(t, err)

	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	oldCfg := model.ConfigInstance.KB
	model.ConfigInstance.KB = model.KBConfig{
		Enabled:             true,
		AutoInject:          true,
		InjectTopN:          2,
		InjectMaxChars:      500,
		InjectMinSimilarity: 0.35,
		MaxFileMB:           50,
		MaxTextMB:           20,
		PollInterval:        "20ms",
		EmbedBatchSize:      4,
	}
	t.Cleanup(func() {
		kb.Shutdown()
		model.DataDir = oldDir
		model.ConfigInstance.KB = oldCfg
	})

	require.NoError(t, kb.Init(model.ConfigInstance.KB, model.RAGConfig{
		VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64,
	}))
	kb.StartIndexer()

	col, err := kb.CreateCollection("制度", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "handbook.md")
	require.NoError(t, os.WriteFile(src, []byte("报销流程：先提交申请，再等财务审批。"), 0o644))
	doc, err := kb.AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, err := kb.GetDocument(doc.ID)
		require.NoError(t, err)
		if d.Status == kb.StatusReady {
			break
		}
		if d.Status == kb.StatusFailed {
			t.Fatalf("index failed: %s", d.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}

	withKB := BuildChatRequest("报销流程是什么", "s-kb", "/proj", "codebuddy", "codebuddy", "", "", "", "/proj", false, WithKBInjection())
	require.Contains(t, withKB.Prompt, "[知识库参考]")
	require.Contains(t, withKB.Prompt, "handbook.md")
	require.Contains(t, withKB.Prompt, "财务审批")
	// The same passages ride the request so the assistant message can carry the
	// citation marker after a reload.
	require.NotEmpty(t, withKB.KBRefs)
	require.Equal(t, "handbook.md", withKB.KBRefs[0].DocumentTitle)
	require.Equal(t, "制度", withKB.KBRefs[0].CollectionName)
	require.Contains(t, withKB.KBRefs[0].Text, "财务审批")

	// An explicit query retrieves with the user's question even when the prompt
	// itself is a long group timeline.
	longPrompt := "群聊历史：大家讨论了很多与知识库无关的内容。现在轮到成员发言。"
	withQuery := BuildChatRequest(longPrompt, "s-kb", "/proj", "codebuddy", "codebuddy", "", "", "", "/proj", false, WithKBQuery("报销流程是什么"))
	require.Contains(t, withQuery.Prompt, "[知识库参考]")
	require.Contains(t, withQuery.Prompt, "财务审批")
	require.NotEmpty(t, withQuery.KBRefs)

	without := BuildChatRequest("报销流程是什么", "s-kb", "/proj", "codebuddy", "codebuddy", "", "", "", "/proj", false)
	require.NotContains(t, without.Prompt, "[知识库参考]")
	require.Empty(t, without.KBRefs)

	slash := BuildChatRequest("/compact", "s-kb", "/proj", "codebuddy", "codebuddy", "", "", "", "/proj", false, WithKBInjection())
	require.NotContains(t, slash.Prompt, "[知识库参考]")
	require.Empty(t, slash.KBRefs)

	// Scheduled tasks use the same retrieval through a separate prompt path.
	taskPrompt, taskRefs := withKnowledgeBaseContext("报销流程是什么", "/proj", "s-task")
	require.Contains(t, taskPrompt, "[知识库参考]")
	require.Contains(t, taskPrompt, "handbook.md")
	require.NotEmpty(t, taskRefs)
	require.Equal(t, "handbook.md", taskRefs[0].DocumentTitle)
	skippedPrompt, skippedRefs := withKnowledgeBaseContext("/compact", "/proj", "s-task")
	require.Equal(t, "/compact", skippedPrompt)
	require.Empty(t, skippedRefs)
}

func TestGroupOrchestratorKBInjectionOnce(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g-kb', 1, 'codebuddy', 'G', 'host', 'group', '{"host_member_id":"host"}')`)
	require.NoError(t, err)

	o := NewGroupOrchestrator("g-kb")
	// The host only routes speakers; it never carries retrieved passages.
	require.Nil(t, o.kbOptions("host"))
	// Exactly one member turn per user round gets the option.
	require.Len(t, o.kbOptions("m1"), 1)
	require.Nil(t, o.kbOptions("m2"))
}
