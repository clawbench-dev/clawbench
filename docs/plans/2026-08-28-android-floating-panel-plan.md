# Android 悬浮窗多会话列表面板 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Android 悬浮状态窗升级为"迷你胶囊 ↔ 展开列表面板"双形态，面板展示所有项目的运行中/待审批/未读会话，按项目名分组，点击会话项打开对应会话。

**Architecture:** 后端新增 `/api/ai/sessions/overview` 跨项目端点（复用 `GetRunningSessionIDs` + `GetPendingApprovalSessionIDs` 批查，新增跨 project 的 unread 查询），返回按项目分组的会话列表。Android 原生侧 `FloatingStatusController` 增加展开/收起状态机，胶囊点击决策（单会话直接进/多会话展开），展开时拉取 overview 渲染原生 View 分组列表。

**Tech Stack:** Go (backend, net/http + sqlite)、Java (Android, WindowManager + View)、Robolectric + JUnit 4（Android 测试）、Go 标准测试

---

### Task 1: 后端 service 层跨 project 会话查询

**Files:**
- Modify: `internal/service/chat.go`（`GetSessions` 附近，约 L928）
- Test: `internal/service/chat_test.go`

**Step 1: 写失败测试**

在 `internal/service/chat_test.go` 添加测试（参照现有测试的连接方式，确认用 `dbRead`/测试 DB）：

```go
func TestGetOverviewSessions_crossProjectUnread(t *testing.T) {
    // 需要测试 DB。参照 chat_test.go 现有用例如何 setup。
    // 场景：projectA 和 projectB 各有一个会话，B 有未读 assistant 消息
    // 断言：返回两个会话，B 的 UnreadCount > 0，且各自 ProjectPath 正确
}
```

先看 `internal/service/chat_test.go` 现有 setup 方式再写。

**Step 2: 运行确认失败**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel && go test ./internal/service/ -run TestGetOverviewSessions -v`
Expected: FAIL（函数不存在，编译错误）

**Step 3: 实现 `GetOverviewSessions`**

在 `internal/service/chat.go` 新增：

```go
// GetOverviewSessions returns all non-archived chat sessions across every
// project (for the floating window overview panel), including per-session
// unread counts. unread is computed per-project (joined by project_path).
func GetOverviewSessions() ([]model.ChatSession, error) {
    sessions := []model.ChatSession{}
    query := `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.created_at, s.updated_at, s.last_read_at, s.project_path,
        COALESCE(unread.cnt, 0) AS unread_count
        FROM chat_sessions s
        LEFT JOIN (
            SELECT h.session_id, h.project_path, COUNT(*) AS cnt
            FROM chat_history h
            JOIN chat_sessions s2 ON s2.id = h.session_id AND s2.project_path = h.project_path
            WHERE h.role = 'assistant' AND h.streaming = 0
              AND (s2.last_read_at IS NULL OR h.created_at > s2.last_read_at)
            GROUP BY h.session_id, h.project_path
        ) unread ON unread.session_id = s.id AND unread.project_path = s.project_path
        WHERE s.archived = 0 AND s.session_type = 'chat'
        ORDER BY s.updated_at DESC, s.id DESC`
    // 扫描 + 填充 LastReadAt/SourceSessionID，同 GetSessions
    return sessions, rows.Err()
}
```

**注意**：`model.ChatSession` 没有 `ProjectPath` JSON 字段——需要确认。若没有，需在 `ChatSession` 模型加 `ProjectPath string json:"projectPath,omitempty"`（用于分组），并在扫描时填充。

**Step 4: 运行确认通过**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel && go test ./internal/service/ -run TestGetOverviewSessions -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/chat.go internal/model/chat.go internal/service/chat_test.go
git commit -m "feat(service): add cross-project overview sessions query with unread"
```

---

### Task 2: 后端 overview handler + 路由

**Files:**
- Modify: `internal/handler/chat_session.go`（新增 `ServeSessionsOverview`）
- Modify: `internal/handler/handler.go`（注册路由）
- Test: `internal/handler/session_overview_test.go`（新）

**Step 1: 写失败测试**

```go
// 用 httptest + 测试 DB 造数据：
// projectA: 运行中会话1（Running=true）、已读已完成会话2
// projectB: 待审批会话3（PendingApproval=true）、有未读的会话4
// 断言响应：
// 1. projects 数组按 name 分组，含 A 和 B
// 2. 会话1 在 A 组且 running=true
// 3. 会话3 在 B 组且 pendingApproval=true
// 4. 会话4 在 B 组且 unreadCount>0
// 5. 已读已完成会话2 被过滤（不出现）
// 6. total = 3
```

参照 `internal/handler/chat_session_new_test.go` 的 handler 测试 setup（httptest + auth）。

**Step 2: 运行确认失败**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel && go test ./internal/handler/ -run TestServeSessionsOverview -v`
Expected: FAIL（函数/路由不存在）

**Step 3: 实现 handler**

```go
// ServeSessionsOverview handles GET /api/ai/sessions/overview.
// Returns sessions across ALL projects that are running, pending approval,
// or have unread messages, grouped by project. Requires auth (session cookie).
func ServeSessionsOverview(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
        return
    }
    sessions, err := service.GetOverviewSessions()
    if err != nil {
        model.WriteError(w, model.Internal(fmt.Errorf("failed to load overview sessions")))
        return
    }
    runningIDs := service.GetRunningSessionIDs()
    runningSet := make(map[string]bool, len(runningIDs))
    for _, id := range runningIDs { runningSet[id] = true }
    pendingSet := ai.GetACPConnManager().GetPendingApprovalSessionIDs()

    type overviewSession struct {
        ID              string    `json:"id"`
        Title           string    `json:"title"`
        Running         bool      `json:"running"`
        PendingApproval bool      `json:"pendingApproval"`
        UnreadCount     int       `json:"unreadCount"`
        UpdatedAt       time.Time `json:"updatedAt"`
    }
    type projectGroup struct {
        Name     string            `json:"name"`
        Sessions []overviewSession `json:"sessions"`
    }
    groups := []*projectGroup{}
    groupByName := map[string]*projectGroup{}
    total := 0
    for _, s := range sessions {
        running := runningSet[s.ID]
        pending := pendingSet[s.ID]
        if !running && !pending && s.UnreadCount <= 0 {
            continue // 不值得关注
        }
        g, ok := groupByName[s.ProjectPath]
        if !ok {
            g = &projectGroup{Name: s.ProjectPath}
            groupByName[s.ProjectPath] = g
            groups = append(groups, g)
        }
        g.Sessions = append(g.Sessions, overviewSession{
            ID: s.ID, Title: s.Title, Running: running,
            PendingApproval: pending, UnreadCount: s.UnreadCount,
            UpdatedAt: s.UpdatedAt,
        })
        total++
    }
    writeJSON(w, http.StatusOK, map[string]any{"projects": groups, "total": total})
}
```

路由注册（handler.go 的 register 区，L243 `/api/ai/sessions` 附近）：
```go
register("/api/ai/sessions/overview", middleware.Auth(ServeSessionsOverview))
```

**Step 4: 运行确认通过**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel && go test ./internal/handler/ -run TestServeSessionsOverview -v`
Expected: PASS。再跑 `go test ./internal/handler/ ./internal/service/` 确认无回归。

**Step 5: Commit**

```bash
git add internal/handler/chat_session.go internal/handler/handler.go internal/handler/session_overview_test.go
git commit -m "feat(handler): add /api/ai/sessions/overview cross-project grouped endpoint"
```

---

### Task 3: Android 悬浮窗控制逻辑扩展（状态机 + 点击决策）

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/FloatingStatusController.java`
- Test: `android/app/src/test/java/com/clawbench/app/FloatingStatusControllerTest.java`

**Step 1: 写失败测试**

在 FloatingStatusControllerTest 添加纯函数测试：

```java
// 决策纯函数：给定运行中会话数，决定点击胶囊行为
@Test public void decideCapsuleClick_singleRunningSession_opensSession() {
    // decideCapsuleClick(1) → OPEN_SESSION
}
@Test public void decideCapsuleClick_multipleRunningSessions_expands() {
    // decideCapsuleClick(2) → EXPAND_PANEL
}
// JSON 解析：overview 响应 → 扁平会话列表
@Test public void parseOverview_noRunning_completedOnly_returnsEmpty() { ... }
@Test public void parseOverview_mixed_returnsGrouped() { ... }
```

设计纯函数签名：
- `public static int decideCapsuleClick(int activeSessionCount)` 返回 `CLICK_OPEN_SESSION` / `CLICK_EXPAND_PANEL` 常量
- `public static JSONObject parseOverview(JSONObject overview)` 或分组解析为可测结构

**Step 2: 运行确认失败**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusControllerTest"`
Expected: FAIL

**Step 3: 实现**

- 新增常量 `CLICK_OPEN_SESSION = 0` / `CLICK_EXPAND_PANEL = 1`
- `decideCapsuleClick(int activeCount)` 纯函数：`count == 1 ? OPEN_SESSION : EXPAND_PANEL`
- 维护 `runningSessionCount`（`Set<String> runningSessions`），事件驱动更新：
  - `isActiveStatus` true → 加入
  - terminal（completed/cancelled）→ 移出
- 新增 `int getRunningSessionCount()` 供胶囊点击决策
- `setExpanded(boolean expanded)` 状态：展开时拉 overview（通过回调/接口），收起回胶囊
- 新增 `onOverviewLoaded(JSONObject overview)` 解析并渲染列表（Task 4 的 View）
- 胶囊点击处理：`if (onTap != null)` 改为判断 `decideCapsuleClick(count)`——单会话走 onTap（进会话），多会话展开

**Step 4: 运行确认通过**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusControllerTest"`
Expected: PASS

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/FloatingStatusController.java android/app/src/test/java/com/clawbench/app/FloatingStatusControllerTest.java
git commit -m "feat(android): capsule click decision and running session tracking in controller"
```

---

### Task 4: Android 列表面板 View（按项目分组）

**Files:**
- Create: `android/app/src/main/java/com/clawbench/app/FloatingStatusPanelView.java`
- Test: `android/app/src/test/java/com/clawbench/app/FloatingStatusPanelViewTest.java`

**Step 1: 写失败测试**

```java
// 纯逻辑：overview JSON → 分组模型列表（可测）
@Test public void buildGroups_fromOverview_groupsByProject() { ... }
@Test public void buildGroups_runningSession_markedRunning() { ... }
@Test public void buildGroups_unreadSession_hasBadge() { ... }
```

设计纯函数：`public static List<ProjectGroup> buildGroups(JSONObject overview)`（ProjectGroup 含 name + 会话列表，会话含 id/title/running/pendingApproval/unreadCount）

**Step 2: 运行确认失败**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusPanelViewTest"`
Expected: FAIL

**Step 3: 实现**

`FloatingStatusPanelView extends FrameLayout`：
- 代码构建 UI（无 XML）：垂直 LinearLayout/RecyclerView
- 顶部 header（项目名）行 + 会话项行（状态指示 + 标题 + 未读徽标）
- 状态指示：running=绿点/旋转、pendingApproval=黄点、unreadCount>0=数字角标
- 会话项点击回调：`setOnSessionClickListener(Consumer<String> sessionId)`
- 面板最大高度（如 400dp），内容超出滚动
- 复用胶囊的圆角半透明背景风格

**Step 4: 运行确认通过**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusPanelViewTest"`
Expected: PASS

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/FloatingStatusPanelView.java android/app/src/test/java/com/clawbench/app/FloatingStatusPanelViewTest.java
git commit -m "feat(android): grouped session list panel view for floating window"
```

---

### Task 5: Android 集成（overview 拉取 + 面板切换 + 事件刷新）

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`（fetchRunningSessions 升级为 fetchOverview、事件驱动刷新）
- Modify: `android/app/src/main/java/com/clawbench/app/FloatingStatusController.java`（面板显示/收起、拉取回调）
- Modify: `android/app/src/test/java/com/clawbench/app/BackgroundServiceFloatingTest.java`

**Step 1: 写失败测试**

BackgroundServiceFloatingTest 添加：
- `fetchOverview_parsesProjects`：验证 overview 拉取后 controller 收到分组数据（mock 或真实 HTTP）
- `handleEvent_running_addsToSet`：running 事件后 running 集合 +1
- `handleEvent_completed_removesFromSet`：completed 后集合 -1，空则隐藏

**Step 2: 运行确认失败**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest --tests "com.clawbench.app.BackgroundServiceFloatingTest"`
Expected: FAIL

**Step 3: 实现**

- BackgroundService：
  - 将 `fetchRunningSessions` 改为 `fetchOverviewSessions`（拉 `/api/ai/sessions/overview`，完整 cookie）
  - WS onOpen 后拉一次；收到事件时若面板展开也拉
  - 把 overview JSON 传给 `floatingController.onOverviewLoaded(...)`
  - 事件处理：维护 running 集合（或依赖 controller）
- FloatingStatusController：
  - 胶囊点击 → `decideCapsuleClick(getRunningSessionCount())` → 单会话进/多会话展开
  - 展开 → `onExpandRequested` 回调（由 BackgroundService 拉 overview）→ `onOverviewLoaded` 渲染面板
  - 面板头部/空白点击 → 收起（setExpanded(false)）
  - 面板会话项点击 → `launchFromFloatingWindow(sessionId)`（复用）
  - `notifyRunningSession` 保留（overview 拉取兜底）

**Step 4: 运行确认通过**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest`
Expected: 全量 PASS

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/main/java/com/clawbench/app/FloatingStatusController.java android/app/src/test/java/com/clawbench/app/BackgroundServiceFloatingTest.java
git commit -m "feat(android): wire overview fetch and panel expand/collapse into service"
```

---

### Task 6: 全量验证 + 收尾

**Step 1: 后端全量测试**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel && go test ./...`
Expected: 全部 PASS

**Step 2: Android 全量测试 + 编译**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/floating-panel/android && ./gradlew :app:testDebugUnitTest && ./gradlew :app:assembleDebug`
Expected: BUILD SUCCESSFUL

**Step 3: 代码风格检查**

- Go: `gofmt -l internal/`（应无输出）
- Android: 新文件无 `android.util.Log`（用 AppLog）

**Step 4: 提交收尾**

```bash
git status
git add -A
git commit -m "feat(android): finalize floating window multi-session panel" --no-verify
```

---

## 手动验证清单（真机）

1. 两个不同项目各发一个消息 → 切后台 → 悬浮窗胶囊出现（有会话）
2. 点胶囊 → 展开面板，看到两个项目分组，各自会话状态正确
3. 完成一个会话 → 面板刷新（该会话移出或标完成）
4. 点某会话项 → App 打开并进入对应会话
5. 单会话运行 → 点胶囊直接进入该会话（不展开）
6. 全部完成 → 悬浮窗淡出隐藏
