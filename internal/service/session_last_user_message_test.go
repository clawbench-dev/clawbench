package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetLastUserMessagePlain_ReturnsLatestUserMessage(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-1"
	insertMsg := func(role, content string) {
		_, err := WriteExec(
			"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', ?, ?, 'claude', 0, 0)",
			sessionID, role, content,
		)
		require.NoError(t, err)
	}

	insertMsg("user", "第一个问题")
	insertMsg("assistant", `{"blocks":[{"type":"text","text":"回答一"}]}`)
	insertMsg("user", "第二个问题")
	insertMsg("assistant", `{"blocks":[{"type":"text","text":"回答二"}]}`)

	got := GetLastUserMessagePlain(context.Background(), sessionID)
	require.Equal(t, "第二个问题", got)
}

func TestGetLastUserMessagePlain_ExtractsPlainTextFromBlocks(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-2"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 0)",
		sessionID, `{"blocks":[{"type":"text","text":"带格式的用户消息"}]}`,
	)
	require.NoError(t, err)

	got := GetLastUserMessagePlain(context.Background(), sessionID)
	require.Equal(t, "带格式的用户消息", got)
}

func TestGetLastUserMessagePlain_SkipsStreamingAndQueued(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-3"
	// 流式中/排队中的 user 消息应被跳过
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 1, 0)",
		sessionID, "流式中消息",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 1)",
		sessionID, "排队消息",
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 0)",
		sessionID, "最终消息",
	)
	require.NoError(t, err)

	got := GetLastUserMessagePlain(context.Background(), sessionID)
	require.Equal(t, "最终消息", got)
}

func TestGetLastUserMessagePlain_EmptyWhenNoUserMessage(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-4"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'assistant', ?, 'claude', 0, 0)",
		sessionID, `{"blocks":[{"type":"text","text":"只有助手消息"}]}`,
	)
	require.NoError(t, err)

	got := GetLastUserMessagePlain(context.Background(), sessionID)
	require.Equal(t, "", got)
}
