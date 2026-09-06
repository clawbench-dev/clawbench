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

func TestGetLastUserMessageMeta_ReturnsPlainAndNoFiles(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m1"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, '', 'claude', 0, 0)",
		sessionID, "没有附件的消息",
	)
	require.NoError(t, err)

	plain, hasFiles := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "没有附件的消息", plain)
	require.False(t, hasFiles)
}

func TestGetLastUserMessageMeta_ReportsFilesWhenPresent(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m2"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, ?, 'claude', 0, 0)",
		sessionID, "带附件的问题", `[{"path":"/proj/src/main.go","isDir":false}]`,
	)
	require.NoError(t, err)

	plain, hasFiles := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "带附件的问题", plain)
	require.True(t, hasFiles)
}

func TestGetLastUserMessageMeta_AttachmentOnlyMessage(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m3"
	// 纯附件消息：content 为空、files 非空
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, ?, 'claude', 0, 0)",
		sessionID, "", `[{"path":"/proj/img/logo.png","isDir":false},{"path":"/proj/docs/a.md","isDir":false}]`,
	)
	require.NoError(t, err)

	plain, hasFiles := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "", plain)
	require.True(t, hasFiles)
}

func TestGetLastUserMessageMeta_EmptyFilesArrayMeansNoAttachments(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m4"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, '[]', 'claude', 0, 0)",
		sessionID, "空数组",
	)
	require.NoError(t, err)

	plain, hasFiles := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "空数组", plain)
	require.False(t, hasFiles)
}

func TestGetLastUserMessageMeta_SkipsStreamingAndQueued(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m5"
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, ?, 'claude', 1, 0)",
		sessionID, "流式中", `[{"path":"/proj/a.go","isDir":false}]`,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, files, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, ?, 'claude', 0, 1)",
		sessionID, "排队中", `[{"path":"/proj/b.go","isDir":false}]`,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 0)",
		sessionID, "最终消息",
	)
	require.NoError(t, err)

	plain, hasFiles := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "最终消息", plain)
	require.False(t, hasFiles)
}

func TestGetLastUserMessageMeta_CollapsesMultiBlockToSingleLine(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m6"
	// 用户消息实际是两段话（两个 text block），ExtractPlainText 以 \n\n 连接；
	// 通知引用块应折叠为单行流动文本（两个段落变"段一 段二"），而不是变成两行。
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 0)",
		sessionID, `{"blocks":[{"type":"text","text":"帮我看看这个报错"},{"type":"text","text":"以及怎么处理"}]}`,
	)
	require.NoError(t, err)

	plain, _ := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "帮我看看这个报错 以及怎么处理", plain)
	require.NotContains(t, plain, "\n")
}

func TestGetLastUserMessageMeta_FlattensEmbeddedNewlinesToSpaces(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-m7"
	// 纯文本消息内含换行/制表/连续空格——通知预览应折叠为单个空格流
	_, err := WriteExec(
		"INSERT INTO chat_history (session_id, project_path, role, content, backend, streaming, queued) VALUES (?, 'proj', 'user', ?, 'claude', 0, 0)",
		sessionID, "请修复这个bug\t如果方便\n\n谢谢",
	)
	require.NoError(t, err)

	plain, _ := GetLastUserMessageMeta(context.Background(), sessionID)
	require.Equal(t, "请修复这个bug 如果方便 谢谢", plain)
}
