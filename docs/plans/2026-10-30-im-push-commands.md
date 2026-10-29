# IM 推送新增 /show /stop /new 命令 — 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让钉钉与飞书机器人支持 `/show`（回看最后一条 AI 回复）、`/stop`（停止会话并清空队列）、`/new`（按序号或完整路径新建空会话）三条命令。

**Architecture:** 命令解析全部下沉到叶子包 `internal/push/common`（`ClassifyIncoming` 扩展为「`@id` → 命令词 → 粘性」三段式），钉钉/飞书两个 backend 只负责渲染回复。跨包能力（读最后回复、停会话、列项目、建会话）通过 `common.SessionMessenger` 接口新增方法暴露，实现落在 `internal/service/push_commands.go`，桥接在 `cmd/server/main.go`，从而保持 `service → push` 单向依赖不成环。

**Tech Stack:** Go 1.x、SQLite（`internal/store`）、go-i18n（`internal/i18n` 的 yaml 双语表）、DingTalk Stream SDK、Lark SDK、testify。

---

## 已确认的产品决策

| # | 决策 | 取值 |
|---|------|------|
| D1 | `/cat` 语义 | 改名为 `/show`，只回**最后一条 AI 回复**（不列历史、不读文件） |
| D2 | `/new` 目标项目 | **必须显式指定**：序号（来自 `/new` 自己的全量项目列表）或**完整路径** |
| D3 | `/new` 目录名匹配 | **不做**（已明确删除，避免歧义与同名冲突） |
| D4 | `/new` 的 agent | **默认智能体**（`resolveDefaultAgentForProject` 的 scope-aware 解析） |
| D5 | `/new` 是否带首条消息 | **只建空会话**，忽略命令后的其他文本 |
| D6 | `/stop` 语义 | **停会话并清空队列**（`CancelSession` + `ClearQueuedMessages` + `queue_cancel`） |
| D7 | `/show` 内容边界 | **仅最后一条 AI 回复**，不附元信息 |
| D8 | 长回复 | **复用现有 4000 字上限**（`truncateForDingTalk` / `truncateForFeishu`） |
| D9 | 平台范围 | **钉钉 + 飞书一起做**，命令集保持一致 |
| D10 | `/new` 成功后粘性目标 | **切换到新会话** |

## 命令语义（最终）

| 输入 | 行为 |
|---|---|
| `/ls` | 现有行为不变：最近 10 个会话 |
| `/show` | 粘性会话的最后一条已完成 assistant 回复 |
| `/show <id>` | 指定会话（8+ 位 hex 前缀）的最后一条已完成 assistant 回复 |
| `/stop` | 停止粘性会话 + 清空其队列 |
| `/stop <id>` | 停止指定会话 + 清空其队列 |
| `/new` | 回全量项目列表（1..N）+ 用法 |
| `/new <序号>` | 在列表第 N 个项目建空会话 |
| `/new <完整路径>` | 在该路径建空会话（必须是已注册项目） |
| `@<id> /new` | **逃逸口**：`@id` 优先级最高，`/new` 作为普通文本发给该会话 |

---

## Task 1: 命令常量与 `Route` 结构扩展

**Files:**
- Modify: `internal/push/common/session_command.go:26-77`
- Test: `internal/push/common/session_command_test.go`

**Step 1: 写失败的测试**

在 `internal/push/common/session_command_test.go` 末尾追加：

```go
func TestClassifyIncoming_NewCommands(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		sticky    string
		wantKind  RouteKind
		wantArg   string
		wantShort string
		wantMsg   string
	}{
		{
			name: "show without arg", text: "/show", sticky: "s1",
			wantKind: RouteShowLastReply,
		},
		{
			name: "show with id", text: "/show a1b2c3d4", sticky: "s1",
			wantKind: RouteShowLastReply, wantArg: "a1b2c3d4",
		},
		{
			name: "show with padded id", text: "  /show   a1b2c3d4  ", sticky: "",
			wantKind: RouteShowLastReply, wantArg: "a1b2c3d4",
		},
		{
			name: "stop without arg", text: "/stop", sticky: "s1",
			wantKind: RouteStopSession,
		},
		{
			name: "stop with id", text: "/stop a1b2c3d4", sticky: "",
			wantKind: RouteStopSession, wantArg: "a1b2c3d4",
		},
		{
			name: "new without arg lists projects", text: "/new", sticky: "s1",
			wantKind: RouteNewSession, wantArg: "",
		},
		{
			name: "new with index", text: "/new 2", sticky: "s1",
			wantKind: RouteNewSession, wantArg: "2",
		},
		{
			// A path may contain spaces; the arg must be the whole remainder,
			// not the first whitespace-delimited token.
			name: "new with path containing spaces", text: "/new /home/u/my project", sticky: "",
			wantKind: RouteNewSession, wantArg: "/home/u/my project",
		},
		{
			// "@id" wins over any command word: this is the escape hatch that
			// sends the literal text "/new" to a session.
			name: "explicit target beats command", text: "@a1b2c3d4 /new", sticky: "",
			wantKind: RouteToSession, wantShort: "a1b2c3d4", wantMsg: "/new",
		},
		{
			// A command word embedded mid-sentence is ordinary text.
			name: "command word embedded is plain text", text: "please /stop it", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "please /stop it",
		},
		{
			// Case sensitivity: commands are lowercase only.
			name: "uppercase is not a command", text: "/SHOW", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "/SHOW",
		},
		{
			// Unknown slash commands fall through to the session, so a user can
			// still type them to the AI.
			name: "unknown slash command goes to session", text: "/help me", sticky: "s1",
			wantKind: RouteToSession, wantMsg: "/help me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyIncoming(tt.text, tt.sticky)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Arg != tt.wantArg {
				t.Errorf("Arg = %q, want %q", got.Arg, tt.wantArg)
			}
			if got.ShortID != tt.wantShort {
				t.Errorf("ShortID = %q, want %q", got.ShortID, tt.wantShort)
			}
			if got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}
```

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/im-push-commands
/usr/local/go/bin/go test ./internal/push/common/ -run TestClassifyIncoming_NewCommands -v
```

Expected: 编译失败，`undefined: RouteShowLastReply` / `got.Arg undefined`。

**Step 3: 最小实现**

把 `internal/push/common/session_command.go` 的 26-77 行替换为：

```go
// Session list / reply / stop / new commands. Only these exact words are
// commands; everything else is ordinary text for the AI.
const (
	// SessionListCommand lists recent sessions.
	SessionListCommand = "/ls"
	// SessionShowCommand resends the target session's last AI reply.
	SessionShowCommand = "/show"
	// SessionStopCommand stops the target session and clears its queue.
	SessionStopCommand = "/stop"
	// SessionNewCommand creates a session, or lists projects when bare.
	SessionNewCommand = "/new"
)

// RouteKind identifies how an inbound IM message should be handled.
type RouteKind int

const (
	// RouteToSession targets a specific session. ShortID is set when the user
	// named one with "@{shortID}"; when empty the caller uses the sticky
	// session instead. Message is the text to send (may be empty for a bare
	// file/image).
	RouteToSession RouteKind = iota
	// RouteListSessions asks for the recent-session list ("/ls").
	RouteListSessions
	// RouteShowLastReply asks for the target session's last AI reply ("/show").
	// Arg carries an optional "@{shortID}"-style prefix; empty means sticky.
	RouteShowLastReply
	// RouteStopSession asks to stop the target session and clear its queue
	// ("/stop"). Arg carries an optional session prefix; empty means sticky.
	RouteStopSession
	// RouteNewSession creates a session ("/new"). Arg is the project selector
	// (a 1-based index into the project list, or a full path); empty means the
	// user wants the project list.
	RouteNewSession
	// RouteNoTarget means the user sent something routable but has no sticky
	// session yet. The caller must reply with the "/ls" hint.
	RouteNoTarget
)

// Route is the outcome of classifying an inbound IM message.
type Route struct {
	Kind    RouteKind
	ShortID string // set only for an explicit "@{shortID}" target
	Message string // text payload; empty for a bare file/image
	Arg     string // command argument (session prefix or project selector)
}

// ClassifyIncoming routes an inbound text message from an IM user.
//
// Precedence: an explicit "@{shortID}" always wins — which doubles as the
// escape hatch for sending a literal command word to a session; then a leading
// command word (/ls, /show, /stop, /new); then any other text goes to the
// sticky session (shortID left empty for the caller to resolve). An
// empty/whitespace-only text with no sticky session is RouteNoTarget — the same
// hint a bare file gets when nothing was ever chosen.
//
// stickySessionID is the caller's already-resolved sticky target ("" when
// none). It is passed in rather than looked up so this stays pure and testable.
func ClassifyIncoming(text, stickySessionID string) Route {
	if shortID, msg, ok := ParseSessionCommand(text); ok {
		return Route{Kind: RouteToSession, ShortID: shortID, Message: msg}
	}

	trimmed := strings.TrimSpace(text)
	if cmd, arg := splitCommand(trimmed); cmd != "" {
		switch cmd {
		case SessionListCommand:
			// "/ls" takes no argument; a trailing argument means the user
			// meant something else, so fall through to the session path.
			if arg == "" {
				return Route{Kind: RouteListSessions}
			}
		case SessionShowCommand:
			return Route{Kind: RouteShowLastReply, Arg: arg}
		case SessionStopCommand:
			return Route{Kind: RouteStopSession, Arg: arg}
		case SessionNewCommand:
			return Route{Kind: RouteNewSession, Arg: arg}
		}
	}

	if stickySessionID == "" {
		return Route{Kind: RouteNoTarget}
	}
	return Route{Kind: RouteToSession, Message: trimmed}
}

// splitCommand splits a trimmed message into its leading command word and the
// remaining argument, preserving the argument verbatim (a project path may
// contain spaces). Returns ("", "") when the message does not start with a
// known command word followed by end-of-text or whitespace.
func splitCommand(trimmed string) (cmd, arg string) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", ""
	}
	idx := strings.IndexAny(trimmed, " \t\n")
	if idx < 0 {
		return trimmed, ""
	}
	return trimmed[:idx], strings.TrimSpace(trimmed[idx+1:])
}
```

**Step 4: 跑测试确认通过**

```bash
/usr/local/go/bin/go test ./internal/push/common/ -run 'TestClassifyIncoming' -v
```

Expected: 全部 PASS，包括既有的 `TestClassifyIncoming`（`/ls` 行为不变）。

**Step 5: 提交**

```bash
git add internal/push/common/session_command.go internal/push/common/session_command_test.go
git commit -m "feat(push): add /show /stop /new command parsing to ClassifyIncoming"
```

---

## Task 2: 接口新增四个方法 + `ProjectRef`

**Files:**
- Modify: `internal/push/common/interfaces.go:41-62`
- Test: `internal/push/common/session_command_test.go`（mock 补齐）

**Step 1: 写失败的测试**

`SessionMessenger` 加方法会让三个既有 mock 编译失败——这本身就是第一步的红灯。先跑：

```bash
/usr/local/go/bin/go build ./internal/push/... 2>&1 | head
```

Expected: 无输出（接口还没改，先确认基线绿）。

**Step 2: 改接口**

在 `internal/push/common/interfaces.go` 的 `SessionMessenger` 接口末尾（`SendMessageToSession` 之后）追加：

```go
	// LastAssistantReply returns the target session's most recent FINALIZED
	// assistant reply as plain text (protocol tags already stripped for group
	// sessions). ok is false when the session has no finished reply yet — the
	// caller must say so rather than send an empty message. A reply still
	// streaming in is not returned.
	LastAssistantReply(sessionID string) (text string, ok bool, err error)
	// StopAndClearSession stops the session's in-flight turn (idempotent) and
	// drops its queued messages. wasRunning reports whether a turn was actually
	// cancelled; cleared is the number of queued messages removed.
	StopAndClearSession(sessionID string) (wasRunning bool, cleared int, err error)
	// ListProjects returns every registered project, index starting at 1, in
	// the order /new renders them.
	ListProjects() ([]ProjectRef, error)
	// CreateSessionInProject creates an empty chat session in projectPath using
	// the default agent visible in that project. The path must already be a
	// registered project; an unknown path is an error rather than an implicit
	// registration.
	CreateSessionInProject(projectPath string) (sessionID, title string, err error)
```

并在同文件 `SessionInfo` 之后新增：

```go
// ProjectRef is one row of the /new project list. Index is 1-based and is what
// the user types back; Path is the canonical registered path.
type ProjectRef struct {
	Index        int
	Path         string
	Exists       bool // directory still present on disk
	SessionCount int
}
```

**Step 3: 补齐测试 mock**

三个 mock 都要实现新方法。在 `internal/push/common/session_command_test.go` 的 `mockMessenger` 末尾追加：

```go
func (m *mockMessenger) LastAssistantReply(string) (string, bool, error) {
	return m.lastReply, m.lastReplyOK, nil
}

func (m *mockMessenger) StopAndClearSession(string) (bool, int, error) {
	return m.wasRunning, m.cleared, nil
}

func (m *mockMessenger) ListProjects() ([]ProjectRef, error) {
	return m.projects, nil
}

func (m *mockMessenger) CreateSessionInProject(path string) (string, string, error) {
	return m.newSessionID, m.newSessionTitle, nil
}
```

并把 `mockMessenger` 结构体扩展为：

```go
type mockMessenger struct {
	sessions []SessionInfo
	running  map[string]bool

	lastReply      string
	lastReplyOK    bool
	wasRunning     bool
	cleared        int
	projects       []ProjectRef
	newSessionID   string
	newSessionTitle string
}
```

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go build ./internal/push/... && /usr/local/go/bin/go test ./internal/push/common/
```

Expected: 编译通过，common 包测试全绿。

**Step 5: 提交**

```bash
git add internal/push/common/interfaces.go internal/push/common/session_command_test.go
git commit -m "feat(push): extend SessionMessenger with show/stop/new capabilities"
```

---

## Task 3: 会话前缀解析辅助函数

**Files:**
- Modify: `internal/push/common/session_command.go`（末尾追加）
- Test: `internal/push/common/session_command_test.go`

**Step 1: 写失败的测试**

```go
func TestResolveCommandTarget(t *testing.T) {
	m := &mockMessenger{
		sessions: []SessionInfo{
			{ID: "a1b2c3d4-1111", Title: "T1"},
			{ID: "b2c3d4e5-2222", Title: "T2"},
		},
		running: map[string]bool{"b2c3d4e5-2222": true},
	}

	// No arg → the sticky session, read back for its title.
	id, title, err := ResolveCommandTarget(m, "", "a1b2c3d4-1111")
	if err != nil || id != "a1b2c3d4-1111" || title != "T1" {
		t.Fatalf("sticky resolve = %q %q %v", id, title, err)
	}

	// Explicit prefix wins over the sticky target.
	id, title, err = ResolveCommandTarget(m, "b2c3d4e5", "a1b2c3d4-1111")
	if err != nil || id != "b2c3d4e5-2222" || title != "T2" {
		t.Fatalf("prefix resolve = %q %q %v", id, title, err)
	}

	// No arg and no sticky target is an error, not a silent empty session.
	if _, _, err := ResolveCommandTarget(m, "", ""); err == nil {
		t.Fatal("expected error when neither arg nor sticky target is set")
	}

	// Unknown prefix propagates the not-found error.
	if _, _, err := ResolveCommandTarget(m, "ffffffff", "a1b2c3d4-1111"); err == nil {
		t.Fatal("expected error for unknown prefix")
	}
}
```

**Step 2: 跑测试确认失败**

```bash
/usr/local/go/bin/go test ./internal/push/common/ -run TestResolveCommandTarget -v
```

Expected: `undefined: ResolveCommandTarget`。

**Step 3: 实现**

`internal/push/common/session_command.go` 末尾追加：

```go
// ResolveCommandTarget resolves the target of a /show or /stop command.
//
// An explicit prefix (the command's argument) resolves like "@{shortID}" does.
// An empty argument means the sticky session, whose title is read back so the
// reply can name it. Returns an error when there is no sticky target at all —
// the caller turns that into the "/ls" hint rather than acting on a guess.
func ResolveCommandTarget(messenger SessionMessenger, arg, stickySessionID string) (sessionID, title string, err error) {
	if arg != "" {
		return ResolveShortSessionID(messenger, arg)
	}
	return ResolveTarget(messenger, Route{Kind: RouteToSession}, stickySessionID)
}
```

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/push/common/ -v
```

Expected: 全绿。

**Step 5: 提交**

```bash
git add internal/push/common/session_command.go internal/push/common/session_command_test.go
git commit -m "feat(push): add ResolveCommandTarget for /show and /stop targets"
```

---

## Task 4: service 层四个能力 + 项目选择器

**Files:**
- Create: `internal/service/push_commands.go`
- Create: `internal/service/push_commands_test.go`

**Step 1: 写失败的测试**

```go
package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"
)

func setupTestDBForPushCommands(t *testing.T) func() {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, _ = db.Exec(store.ProjectsDDL)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			agent_source TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			external_session_id TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			title_source TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			files TEXT DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			agent_id TEXT NOT NULL DEFAULT '',
			injected INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	cleanup := store.SetDBForTest(db, db)
	return func() {
		cleanup()
		_ = db.Close()
	}
}

func TestGetLastAssistantReply_SkipsStreamingAndEmpty(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	projectID, err := store.ProjectIDForPath("/tmp/push-proj")
	if err != nil {
		t.Fatalf("project id: %v", err)
	}
	const sid = "a1b2c3d4-0000-0000-0000-000000000000"

	_, _ = store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, title) VALUES (?, ?, ?)", sid, projectID, "T")
	// An older finished reply, then a newer streaming one that must be ignored.
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 0)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"old answer"}]}`)
	_, _ = store.WriteExec(
		`INSERT INTO chat_history (project_id, session_id, role, content, streaming)
		 VALUES (?, ?, 'assistant', ?, 1)`, projectID, sid,
		`{"blocks":[{"type":"text","text":"still streaming"}]}`)

	text, ok := GetLastAssistantReply(sid)
	if !ok {
		t.Fatal("expected a reply")
	}
	if text != "old answer" {
		t.Errorf("text = %q, want %q", text, "old answer")
	}
}

func TestGetLastAssistantReply_NoReply(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	text, ok := GetLastAssistantReply("ffffffff-0000-0000-0000-000000000000")
	if ok {
		t.Errorf("expected ok=false, got text %q", text)
	}
}

func TestResolveProjectSelector_IndexAndPath(t *testing.T) {
	teardown := setupTestDBForPushCommands(t)
	defer teardown()

	_, err := store.ProjectIDForPath("/tmp/proj-a")
	if err != nil {
		t.Fatalf("register a: %v", err)
	}
	_, err = store.ProjectIDForPath("/tmp/proj-b")
	if err != nil {
		t.Fatalf("register b: %v", err)
	}

	refs, err := ListProjectsForPush()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d projects, want 2", len(refs))
	}
	if refs[0].Index != 1 || refs[1].Index != 2 {
		t.Errorf("indices not 1-based: %+v", refs)
	}

	// Index selector resolves to the same path the list showed.
	path, err := ResolveProjectSelector("1", refs)
	if err != nil || path != refs[0].Path {
		t.Fatalf("index selector = %q %v", path, err)
	}

	// Full path selector is exact-match against the registry.
	path, err = ResolveProjectSelector("/tmp/proj-b", refs)
	if err != nil || path != refs[1].Path {
		t.Fatalf("path selector = %q %v", path, err)
	}

	// Out-of-range index names the total so the user can retry.
	if _, err := ResolveProjectSelector("9", refs); err == nil {
		t.Fatal("expected out-of-range error")
	}

	// Unknown path is refused rather than implicitly registered.
	if _, err := ResolveProjectSelector("/tmp/never-registered", refs); err == nil {
		t.Fatal("expected unknown-path error")
	}
}
```

**Step 2: 跑测试确认失败**

```bash
/usr/local/go/bin/go test ./internal/service/ -run 'TestGetLastAssistantReply|TestResolveProjectSelector' -v
```

Expected: `undefined: GetLastAssistantReply` 等编译错误。

**Step 3: 实现**

创建 `internal/service/push_commands.go`：

```go
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// GetLastAssistantReply returns the session's most recent FINALIZED assistant
// reply as plain text. ok is false when there is no finished reply yet.
//
// Reuses GetAssistantRawContents + extractPreviewFromBlocksRaw so the text is
// byte-identical to what the completion push showed (the group-chat protocol
// tags are stripped there too). It deliberately does NOT read the streaming
// row: a half-written answer is worse than "no reply yet".
func GetLastAssistantReply(sessionID string) (string, bool) {
	contents, err := GetAssistantRawContents(sessionID)
	if err != nil {
		slog.Warn("push: read assistant replies failed",
			"session_id", sessionID, "error", err)
		return "", false
	}
	for _, raw := range contents {
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			continue
		}
		if text := extractPreviewFromBlocksRaw(sessionID, content.Blocks); text != "" {
			return text, true
		}
	}
	return "", false
}

// StopAndClearSession stops the session's in-flight turn and drops its queue.
//
// CancelSession is idempotent (it returns true for an already-idle session), so
// wasRunning is derived from IsSessionRunning taken BEFORE the cancel. The
// queue is cleared after cancelling: CancelSession's drain-loop branch also
// collects queue IDs and emits queue_cancel, but that runs on the agent
// goroutine and may lose the race with this call — clearing here and emitting
// again is harmless (the frontend treats an empty queueIds array as a no-op)
// and guarantees nothing is left stuck when no turn was running at all.
func StopAndClearSession(sessionID string) (wasRunning bool, cleared int, err error) {
	wasRunning = IsSessionRunningForSweep(sessionID)

	CancelSession(sessionID)

	queueIDs, listErr := GetQueuedQueueIDs(sessionID)
	if listErr != nil {
		return wasRunning, 0, fmt.Errorf("list queued messages: %w", listErr)
	}
	if err := ClearQueuedMessages(sessionID); err != nil {
		return wasRunning, 0, fmt.Errorf("clear queued messages: %w", err)
	}
	cleared = len(queueIDs)

	// Emit even when queueIDs is empty: a cross-device _remote bubble must not
	// linger (the frontend treats an empty array as a no-op).
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "queue_cancel",
		QueueEvent: &ai.QueueEventData{
			SessionID: sessionID,
			QueueIDs:  queueIDs,
		},
	})
	return wasRunning, cleared, nil
}

// ProjectRef mirrors common.ProjectRef without importing the push package (the
// dependency direction is push → service, never the reverse).
type ProjectRef struct {
	Index        int
	Path         string
	Exists       bool
	SessionCount int
}

// ListProjectsForPush returns every registered project in the order /new
// renders them, with 1-based indices the user types back.
func ListProjectsForPush() ([]ProjectRef, error) {
	items, err := ListAllProjects()
	if err != nil {
		return nil, err
	}
	refs := make([]ProjectRef, 0, len(items))
	for i, item := range items {
		refs = append(refs, ProjectRef{
			Index:        i + 1,
			Path:         item.Path,
			Exists:       item.Exists,
			SessionCount: item.SessionCount,
		})
	}
	return refs, nil
}

// ResolveProjectSelector resolves a /new argument against the project list.
//
// Only two forms are accepted: a 1-based index from that same list, or a full
// path that exactly matches a registered project. Basename matching is
// deliberately absent — two projects named "web" would be indistinguishable,
// and silently picking one would create the session in the wrong repo.
//
// A path that is not registered is refused rather than registered on the fly:
// store.ProjectIDForPath would happily insert a row for any string, which would
// let a typo permanently pollute the project list.
func ResolveProjectSelector(arg string, refs []ProjectRef) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("empty project selector")
	}

	if n, err := strconv.Atoi(arg); err == nil {
		for _, r := range refs {
			if r.Index == n {
				return r.Path, nil
			}
		}
		return "", fmt.Errorf("project index %d out of range (1-%d)", n, len(refs))
	}

	canon := store.NormalizeProjectPath(arg)
	for _, r := range refs {
		if store.NormalizeProjectPath(r.Path) == canon {
			return r.Path, nil
		}
	}
	return "", fmt.Errorf("project not registered: %s", arg)
}

// CreatePushSession creates an empty chat session in projectPath using the
// default agent visible in that project.
//
// Mirrors the handler's create path (scope-aware default agent, numbered
// placeholder title, auto-approve snapshot) so a session created from IM is
// indistinguishable from one created in the UI.
func CreatePushSession(projectPath string) (sessionID, title string, err error) {
	projectID, ok, err := store.ProjectIDByPath(projectPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve project: %w", err)
	}
	if !ok {
		return "", "", fmt.Errorf("project not registered: %s", projectPath)
	}

	agentID := defaultAgentForProject(projectPath, projectID)
	if agentID == "" {
		return "", "", fmt.Errorf("no agents available")
	}
	agent := model.GetAgent(agentID)
	if agent == nil {
		return "", "", fmt.Errorf("no agents available")
	}

	baseTitle := i18nT("NewSession")
	n, err := NextSessionNumber(projectPath, baseTitle)
	if err != nil {
		title = baseTitle
	} else {
		title = i18nT("NewSessionN", map[string]any{"N": n})
	}

	sessionID, err = CreateSession(projectPath, agent.Backend, title, agentID, "", "default", "chat")
	if err != nil {
		return "", "", err
	}
	return sessionID, title, nil
}

// defaultAgentForProject resolves the default agent scoped to projectPath.
// A project-scoped agent is only usable in its own project; a global one is
// usable everywhere.
func defaultAgentForProject(projectPath string, projectID int64) string {
	visible := func(a *model.Agent) bool {
		if a == nil {
			return false
		}
		if a.ProjectID == nil {
			return true
		}
		return *a.ProjectID == projectID
	}
	if def := model.GetDefaultAgentID(); visible(model.GetAgent(def)) {
		return def
	}
	for _, a := range model.GetAgentList() {
		if visible(a) {
			return a.ID
		}
	}
	return ""
}
```

其中 `i18nT` 是本地小包装（service 包已有 `i18n.LocalizerForLocale(model.Language)` 用法），在文件顶部加：

```go
// i18nT localizes a key for the push path, which runs without an HTTP request.
// Matches the pattern already used by group_store.go / group_orchestrator.go.
func i18nT(key string, data ...map[string]any) string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), key, data...)
}
```

并把 `"clawbench/internal/i18n"` 加入 import 块。

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/service/ -run 'TestGetLastAssistantReply|TestResolveProjectSelector' -v
```

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/service/push_commands.go internal/service/push_commands_test.go
git commit -m "feat(service): add IM push command capabilities (show/stop/new)"
```

---

## Task 5: `cmd/server` 桥接

**Files:**
- Modify: `cmd/server/main.go`（`dingtalkSessionMessenger` 与 `feishuSessionMessenger` 的方法集）

**Step 1: 跑 build 确认失败**

```bash
/usr/local/go/bin/go build ./... 2>&1 | head -20
```

Expected: `cannot use dingtalkSessionMessenger{} ... missing method LastAssistantReply` 等。

**Step 2: 实现**

在 `cmd/server/main.go` 里，为两个 adapter 各补四个方法（内容相同，只是挂在不同的空 struct 上）：

```go
func (dingtalkSessionMessenger) LastAssistantReply(sessionID string) (string, bool, error) {
	text, ok := service.GetLastAssistantReply(sessionID)
	return text, ok, nil
}

func (dingtalkSessionMessenger) StopAndClearSession(sessionID string) (bool, int, error) {
	return service.StopAndClearSession(sessionID)
}

func (dingtalkSessionMessenger) ListProjects() ([]common.ProjectRef, error) {
	refs, err := service.ListProjectsForPush()
	if err != nil {
		return nil, err
	}
	out := make([]common.ProjectRef, len(refs))
	for i, r := range refs {
		out[i] = common.ProjectRef{
			Index:        r.Index,
			Path:         r.Path,
			Exists:       r.Exists,
			SessionCount: r.SessionCount,
		}
	}
	return out, nil
}

func (dingtalkSessionMessenger) CreateSessionInProject(projectPath string) (string, string, error) {
	return service.CreatePushSession(projectPath)
}
```

飞书侧同样四个，接收者换成 `feishuSessionMessenger`。**建议**把这段抽成一个共用 struct 嵌入两个 adapter，避免 40 行重复；若嵌入，注意两个 adapter 都是空 struct 且已注册在 `RegisterSessionMessenger` 处，改动时不要动注册代码。

**Step 3: 跑 build**

```bash
/usr/local/go/bin/go build ./...
```

Expected: 通过。

**Step 4: 提交**

```bash
git add cmd/server/main.go
git commit -m "feat(push): bridge show/stop/new capabilities into IM session messengers"
```

---

## Task 6: i18n 键（中英双语）

**Files:**
- Modify: `internal/i18n/locales/active.zh.yaml`（`PushRecentSessionUnavailable` 之后）
- Modify: `internal/i18n/locales/active.en.yaml`（同位置）
- Modify: `internal/i18n/i18n_test.go:97-215`（键清单）与 `:282-300`（占位符清单）

**Step 1: 写失败的测试**

在 `TestT_AllKeysPresentInBothLanguages` 的 `keys` 切片末尾（`"ForgeEventViewDetail",` 之后）追加：

```go
		"PushShowTitle",
		"PushShowEmpty",
		"PushShowSessionLabel",
		"PushStoppedTitle",
		"PushStoppedRunning",
		"PushStoppedIdle",
		"PushNewTitle",
		"PushNewCreated",
		"PushNewListIntro",
		"PushNewListUsage",
		"PushNewProjectNotFound",
		"PushNewProjectOutOfRange",
		"PushNewProjectMissingDir",
		"PushNewNoAgent",
```

在 `TestPushTemplatesKeepPlaceholders` 的 `withData` map 里追加：

```go
		"PushShowSessionLabel":      {"Label": "L"},
		"PushStoppedRunning":        {"Label": "L", "Count": 2},
		"PushStoppedIdle":           {"Label": "L", "Count": 0},
		"PushNewCreated":            {"Label": "L", "ID": "abc"},
		"PushNewProjectOutOfRange":  {"Count": 3},
```

**Step 2: 跑测试确认失败**

```bash
/usr/local/go/bin/go test ./internal/i18n/ -run 'TestT_AllKeysPresentInBothLanguages|TestPushTemplatesKeepPlaceholders' -v
```

Expected: FAIL，`English translation missing for key: PushShowTitle`。

**Step 3: 加词条**

`active.zh.yaml`（`PushRecentSessionUnavailable` 之后）：

```yaml
PushShowTitle: 最近回复
PushShowEmpty: 该会话还没有已完成的回复。
PushShowSessionLabel: 会话 **{{.Label}}**
PushStoppedTitle: 已停止
PushStoppedRunning: 已停止会话 **{{.Label}}**，清空 {{.Count}} 条排队消息。
PushStoppedIdle: 会话 **{{.Label}}** 未在运行，清空 {{.Count}} 条排队消息。
PushNewTitle: 新建会话
PushNewCreated: 已在 **{{.Label}}** 创建会话，发送 `@{{.ID}} <消息>` 或直接发消息继续。
PushNewListIntro: 发送 **/new 序号** 或 **/new 完整路径** 在指定项目下新建会话：
PushNewListUsage: '用法：/new 1'
PushNewProjectNotFound: 未找到该项目（只支持完整路径或列表序号）。发送 /new 查看项目列表。
PushNewProjectOutOfRange: 项目序号超出范围（共 {{.Count}} 个），发送 /new 查看列表。
PushNewProjectMissingDir: '⚠️ 该项目的目录已不存在，会话已创建但可能无法执行。'
PushNewNoAgent: 该项目下没有可用的智能体。
```

`active.en.yaml`（同位置）：

```yaml
PushShowTitle: Last reply
PushShowEmpty: This session has no completed reply yet.
PushShowSessionLabel: 'Session **{{.Label}}**'
PushStoppedTitle: Stopped
PushStoppedRunning: Stopped session **{{.Label}}** and cleared {{.Count}} queued messages.
PushStoppedIdle: Session **{{.Label}}** was not running; cleared {{.Count}} queued messages.
PushNewTitle: New session
PushNewCreated: 'Created a session in **{{.Label}}**. Send `@{{.ID}} <message>` or just type to continue.'
PushNewListIntro: 'Send **/new <index>** or **/new <full path>** to create a session in a project:'
PushNewListUsage: 'Usage: /new 1'
PushNewProjectNotFound: 'Project not found (full path or list index only). Send /new to see the project list.'
PushNewProjectOutOfRange: 'Project index out of range ({{.Count}} projects). Send /new to see the list.'
PushNewProjectMissingDir: '⚠️ The project directory no longer exists; the session was created but may not run.'
PushNewNoAgent: No agents available in this project.
```

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/i18n/ -v
```

Expected: 全绿。

**Step 5: 提交**

```bash
git add internal/i18n/locales/active.zh.yaml internal/i18n/locales/active.en.yaml internal/i18n/i18n_test.go
git commit -m "feat(i18n): add IM push command strings (zh/en)"
```

---

## Task 7: 钉钉渲染与路由

**Files:**
- Modify: `internal/push/dingtalk/stream.go:145-157`（`handleIncomingText` 的 switch）
- Modify: `internal/push/dingtalk/stream.go`（新增三个 handler，放在 `handleSessionList` 之后）
- Modify: `internal/push/dingtalk/stream_test.go`（mock 补新方法 + 新测试）

**Step 1: 写失败的测试**

先在 `stream_test.go` 的 `mockSessionMessenger`（`session_messenger_test.go:277`）与 `mockSessionMessengerWithErr`（`:222`）上补四个新方法（返回零值即可），否则包编译不过。

再追加：

```go
func TestOnChatBotMessage_ShowLastReply(t *testing.T) {
	origDB, origMsg := db, sessionMessenger
	defer func() { db, sessionMessenger = origDB, origMsg }()
	db = &mockDBWithCallback{
		upsertFn:  func(_, _, _, _ string) error { return nil },
		getLastFn: func(string) (string, error) { return "a1b2c3d4-1111", nil },
	}
	m := &mockSessionMessenger{
		allSessions: []common.SessionInfo{{ID: "a1b2c3d4-1111", Title: "T"}},
	}
	m.lastReply, m.lastReplyOK = "the full answer", true
	sessionMessenger = m

	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1", SenderStaffId: "s1", ConversationId: "c1",
		SessionWebhook: server.URL,
		Text:           chatbot.BotCallbackDataTextModel{Content: "/show"},
	}
	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(body), "the full answer") {
		t.Errorf("expected last reply in response, got %s", body)
	}
}

func TestOnChatBotMessage_ShowNoReply(t *testing.T) {
	// lastReplyOK=false → the empty-reply hint, not a blank message.
	// (same fixture shape as above)
}

func TestOnChatBotMessage_StopClearsQueue(t *testing.T) {
	// messenger reports wasRunning=true, cleared=2 → reply mentions both.
}

func TestOnChatBotMessage_NewListsProjects(t *testing.T) {
	// "/new" with 2 projects → reply contains "1." and both paths.
}

func TestOnChatBotMessage_NewCreatesSession(t *testing.T) {
	// "/new 1" → CreateSessionInProject called with the first path,
	// reply contains the new short id, and SetLastSessionID recorded it.
}

func TestOnChatBotMessage_NewOutOfRange(t *testing.T) {
	// "/new 9" → error reply naming the total; no session created.
}
```

（每个用例的具体断言在实现时按上一条的 fixture 形状补全；关键是让每个断言在实现前是红的。）

**Step 2: 跑测试确认失败**

```bash
/usr/local/go/bin/go test ./internal/push/dingtalk/ -run 'TestOnChatBotMessage_(Show|Stop|New)' -v
```

Expected: 回复里没有预期内容 → FAIL（`/show` 会走粘性发送路径而不是回看）。

**Step 3: 实现**

`handleIncomingText` 的 switch 增加三个分支：

```go
	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, data)
	case common.RouteShowLastReply:
		m.handleShowLastReply(ctx, data, staffID, route.Arg)
	case common.RouteStopSession:
		m.handleStopSession(ctx, data, staffID, route.Arg)
	case common.RouteNewSession:
		m.handleNewSession(ctx, data, staffID, route.Arg)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, data)
	case common.RouteToSession:
		m.deliverToSession(ctx, data, staffID, route)
	}
```

在 `handleSessionList` 之后新增：

```go
// handleShowLastReply resends the target session's last finalized AI reply.
// The target is the command's argument or the sticky session; when neither
// exists the user gets the same "/ls" hint as any other untargeted message.
func (m *Manager) handleShowLastReply(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, arg string) {
	replier := chatbot.NewChatbotReplier()
	if sessionMessenger == nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(common.T("PushSessionServiceUnavailable")))
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(staffID))
	if err != nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	text, ok, err := sessionMessenger.LastAssistantReply(sessionID)
	if err != nil {
		slog.Warn("dingtalk: read last reply failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(common.T("PushSessionUnavailable")))
		return
	}
	if !ok {
		_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
			[]byte(common.T("PushShowTitle")),
			[]byte("### "+common.T("PushShowTitle")+"\n"+
				common.T("PushShowSessionLabel", map[string]any{"Label": common.FormatSessionLabel(sessionID, title)})+"\n\n"+
				common.T("PushShowEmpty")))
		return
	}

	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte(common.T("PushShowTitle")),
		[]byte("### "+common.T("PushShowTitle")+"\n"+
			common.T("PushShowSessionLabel", map[string]any{"Label": common.FormatSessionLabel(sessionID, title)})+"\n\n"+
			truncateForDingTalk(text)))
}

// handleStopSession stops the target session and drops its queue. The reply
// distinguishes "was running" from "already idle" because a silent success on
// an idle session looks identical to a failed cancel.
func (m *Manager) handleStopSession(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, arg string) {
	replier := chatbot.NewChatbotReplier()
	if sessionMessenger == nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(common.T("PushSessionServiceUnavailable")))
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(staffID))
	if err != nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	wasRunning, cleared, err := sessionMessenger.StopAndClearSession(sessionID)
	if err != nil {
		slog.Warn("dingtalk: stop session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	label := common.FormatSessionLabel(sessionID, title)
	key := "PushStoppedIdle"
	if wasRunning {
		key = "PushStoppedRunning"
	}
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte(common.T("PushStoppedTitle")),
		[]byte("### "+common.T("PushStoppedTitle")+"\n"+
			common.T(key, map[string]any{"Label": label, "Count": cleared})))
}

// handleNewSession renders the project list for a bare "/new", or creates an
// empty session in the selected project and makes it the sticky target.
func (m *Manager) handleNewSession(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, arg string) {
	replier := chatbot.NewChatbotReplier()
	if sessionMessenger == nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(common.T("PushSessionServiceUnavailable")))
		return
	}

	projects, err := sessionMessenger.ListProjects()
	if err != nil {
		slog.Warn("dingtalk: list projects failed", "error", err)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(common.T("PushSubscribedListFailedDingtalk")))
		return
	}

	if strings.TrimSpace(arg) == "" {
		_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
			[]byte(common.T("PushNewTitle")), []byte(renderProjectListMarkdown(projects)))
		return
	}

	path, ref, err := common.SelectProject(projects, arg)
	if err != nil {
		slog.Warn("dingtalk: project selector failed", "error", err, "arg", arg)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(projectSelectErrorText(arg, projects)))
		return
	}

	sessionID, title, err := sessionMessenger.CreateSessionInProject(path)
	if err != nil {
		slog.Warn("dingtalk: create session failed", "error", err, "path", path)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}
	m.rememberSession(staffID, sessionID)

	body := "### " + common.T("PushNewTitle") + "\n" +
		common.T("PushNewCreated", map[string]any{
			"Label": common.FormatSessionLabel(sessionID, title),
			"ID":    common.ShortSessionID(sessionID),
		})
	if !ref.Exists {
		body += "\n\n" + common.T("PushNewProjectMissingDir")
	}
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte(common.T("PushNewTitle")), []byte(body))
}
```

`renderProjectListMarkdown` 与 `projectSelectErrorText` 是**共享**逻辑（钉钉与飞书都要用），放 `internal/push/common`：

```go
// renderProjectListMarkdown renders the /new project list. Shared by both
// backends so the index a user reads is the index they type back.
func renderProjectListMarkdown(refs []ProjectRef) string {
	var sb strings.Builder
	sb.WriteString("### " + T("PushNewTitle") + "\n" + T("PushNewListIntro") + "\n\n")
	for _, r := range refs {
		mark := ""
		if !r.Exists {
			mark = " ⚠️"
		}
		fmt.Fprintf(&sb, "%d. `%s`%s\n", r.Index, r.Path, mark)
	}
	if len(refs) == 0 {
		sb.WriteString(T("PushNewNoProjects") + "\n")
	}
	sb.WriteString("\n" + T("PushNewListUsage"))
	return sb.String()
}

// SelectProject resolves a /new selector and returns the matched ref so the
// caller can warn about a missing directory.
func SelectProject(refs []ProjectRef, arg string) (string, ProjectRef, error) {
	arg = strings.TrimSpace(arg)
	if n, err := strconv.Atoi(arg); err == nil {
		for _, r := range refs {
			if r.Index == n {
				return r.Path, r, nil
			}
		}
		return "", ProjectRef{}, fmt.Errorf(T("PushNewProjectOutOfRange", map[string]any{"Count": len(refs)}))
	}
	for _, r := range refs {
		if r.Path == arg {
			return r.Path, r, nil
		}
	}
	return "", ProjectRef{}, fmt.Errorf(T("PushNewProjectNotFound"))
}
```

> **注意**：`SelectProject` 是**渲染层**的选择器，只做索引/精确路径匹配；service 层的 `ResolveProjectSelector` 是**权威**版本（含 `NormalizeProjectPath`）。两处都保留是因为 push 包不能 import service，但 service 才是最终校验（`CreatePushSession` 里再走一次 `ProjectIDByPath`，未注册路径直接报错）。`SelectProject` 的路径比较用原样字符串即可，因为 `refs[].Path` 已经是规范化路径。

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/push/... -v
```

Expected: 全绿。

**Step 5: 提交**

```bash
git add internal/push/
git commit -m "feat(dingtalk): handle /show /stop /new commands"
```

---

## Task 8: 飞书渲染与路由

**Files:**
- Modify: `internal/push/feishu/stream.go:187-202`（`handleIncomingText` 的 switch）
- Modify: `internal/push/feishu/stream.go`（新增三个 handler）
- Modify: `internal/push/feishu/stream_test.go`（mock 补方法 + 新测试）

**Step 1: 写失败的测试**

先给 `mockSessionMessenger`（`stream_test.go:245`）与 `mockSessionMessengerListError`（`:348`）补四个新方法。再仿照 Task 7 写五个用例：

- `/show` 返回最后回复
- `/show` 无回复 → 空提示
- `/stop` 运行中 → 提到「已停止」
- `/new` 无参 → 项目列表含序号
- `/new 1` → 建会话并更新粘性

**Step 2: 跑测试确认失败**

```bash
/usr/local/go/bin/go test ./internal/push/feishu/ -run 'TestOnMessageReceive_(Show|Stop|New)' -v
```

Expected: FAIL。

**Step 3: 实现**

switch 与钉钉同形。飞书用 `m.SendPostMessage(ctx, openID, title, content)`，且 `truncateForFeishu` 取代 `truncateForDingTalk`：

```go
func (m *Manager) handleShowLastReply(ctx context.Context, openID, arg string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionServiceUnavailable"))
		return
	}
	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(openID))
	if err != nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}
	text, ok, err := sessionMessenger.LastAssistantReply(sessionID)
	if err != nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionUnavailable"))
		return
	}
	head := common.T("PushShowSessionLabel", map[string]any{
		"Label": common.FormatSessionLabel(sessionID, title),
	})
	if !ok {
		_ = m.SendPostMessage(ctx, openID, common.T("PushShowTitle"), head+"\n\n"+common.T("PushShowEmpty"))
		return
	}
	_ = m.SendPostMessage(ctx, openID, common.T("PushShowTitle"), head+"\n\n"+truncateForFeishu(text))
}
```

`handleStopSession` / `handleNewSession` 与钉钉同构，仅发送 API 不同，共用 `common.SelectProject` 与 `common.renderProjectListMarkdown`（后者需导出，见下）。

**Step 4: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/push/... -v
```

Expected: 全绿。

**Step 5: 提交**

```bash
git add internal/push/feishu/
git commit -m "feat(feishu): handle /show /stop /new commands"
```

---

## Task 9: 文档

**Files:**
- Modify: `docs/user-guide/DINGTALK_PUSH.md:73-116`
- Modify: `docs/user-guide/FEISHU_PUSH.md:78-118`

**Step 1: 改文档**

在两份文档的「使用说明」里，`查看会话列表` 之后插入：

```markdown
### 回看最后一条回复

发送 `/show` 让机器人把**当前会话**最后一条 AI 回复完整发回来（推送通知里的预览是截断版）。
也可以点名：`/show a1b2c3d4`。

### 停止会话并清空队列

`/stop` 停止当前会话正在执行的回合，并清空该会话的排队消息；`/stop a1b2c3d4` 指定会话。

### 新建会话

`/new` 会返回全部项目及其序号：

```
1. /path/to/project-a
2. /path/to/project-b

用法：/new 1
```

`/new 1` 在第 1 个项目下用默认智能体新建一个空会话，并自动切换为你的当前会话——
之后直接发消息就会进入这个新会话。也可以用完整路径：`/new /path/to/project-a`。

> 只支持**序号**和**完整路径**；不支持只写目录名（同名目录无法区分）。
```

并在命令优先级处补一句：`@{会话ID} /new` 会把 `/new` 当普通文本发给该会话（`@` 优先级最高）。

**Step 2: 提交**

```bash
git add docs/user-guide/DINGTALK_PUSH.md docs/user-guide/FEISHU_PUSH.md
git commit -m "docs: document /show /stop /new IM commands"
```

---

## Task 10: 全量验证

**Step 1: 相关包测试**

```bash
/usr/local/go/bin/go test ./internal/push/... ./internal/service/ ./internal/i18n/ 2>&1 | tail -20
```

Expected: 全绿。

**Step 2: lint**

```bash
/usr/local/go/bin/go vet ./internal/push/... ./internal/service/ ./cmd/server
```

Expected: 无输出。

**Step 3: 构建**

```bash
/usr/local/go/bin/go build ./...
```

Expected: 通过。

**Step 4: 覆盖率自查**

新增文件的行覆盖率需 ≥ 80%：

```bash
/usr/local/go/bin/go test ./internal/push/common/ ./internal/service/ -coverprofile=/tmp/cov.out
/usr/local/go/bin/go tool cover -func=/tmp/cov.out | grep -E "push_commands|session_command"
```

**Step 5: 提交（若有修补）**

```bash
git add -A
git commit -m "test(push): close coverage gaps for IM commands"
```

---

## 风险与已知取舍

1. **`SelectProject` 与 `ResolveProjectSelector` 重复**：push 包不能 import service（会成环）。`SelectProject` 只用于渲染层选择，真正的校验在 `CreatePushSession` 的 `ProjectIDByPath`，未注册路径会被拒。两处的索引语义必须一致——由 Task 4 与 Task 7 的测试各自钉住。
2. **`/show` 的 4000 字上限**：长回复会被截断。这是刻意取舍（D8），与推送预览一致；若要全文，后续可改成分段发多条。
3. **`/stop` 与 drain loop 的 queue_cancel 竞态**：`CancelSession` 的 drain 分支也会 emit 一次。重复 emit 是幂等的（前端对空 `queueIds` 是 no-op），但会产生两条事件——可接受，换来的是「未运行时也能清干净」的确定性。
4. **`/new` 建会话后不校验目录存在**：目录已删除时仍建会话，回执附警告。与 UI 行为一致（UI 也允许对已删除项目建会话）。
5. **群聊会话**：`/show` 对群会话走 `AssistantConclusion` 的 group 分支（剥协议标签），行为正确；`/stop` 对群会话会停掉整个编排（`CancelSession` 的 runner 覆盖所有成员 turn）。这是期望行为，但文档未单列，若用户反馈需要区分再补。
