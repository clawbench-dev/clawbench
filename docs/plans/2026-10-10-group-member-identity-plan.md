# 群成员身份：内置英文名池 + 按名生成头像 + 真实智能体名披露

> **变更说明（2026-10-11）**：命名规则**已回到本计划原始设计**——**首个实例保留智能体本名**（`Claude`）；**重复实例（第 2 个及以后）取名池名**（`Alice`/`Bob`…）；池耗尽回退 `本名 序号`（从 2 起）。以本节与 `docs/spec/features/group-chat.md`、`internal/service/group_store.go` 的 `assignMemberNames` 为准。（中途曾短暂改为「多实例时全部取名池名」，现已回退。）
>
> **头像规则**：只有**同一智能体被添加多次**时才生成成员头像——**第 2 个及以后**的实例各生成一个（用于区分副本），**第 1 个/群内唯一实例不生成**，保留智能体自身头像。下文 §头像 与决策表中「所有新成员都按名生成、忽略并覆盖智能体原头像」为**过时描述**，以本节与 `docs/spec/features/group-chat.md`、`web/src/composables/useGroupChat.ts` 的 `generateMemberAvatars` 为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让群聊里同一个智能体的多个实例各自拥有唯一昵称（内置英文名池）与各自生成的头像（按名做种子、落库），并在消息里显示该成员底层的真实智能体名。

**Architecture:**
- **命名**：`assignMemberNames` 改为——第一个实例保留智能体本名；重复实例（第 2 个及以后）从**代码内置的英文名池**里取第一个未占用的名字；池耗尽则回退现行 `本名 + 序号`。名池不做配置、无设置 UI。
- **头像**：`chat_sessions` 新增 `agent_avatar` 列（仅群成员行有意义）。前端用 DiceBear（随机风格 + 成员名种子）生成 SVG，经新端点写回。后端 `speakerFromMember` **优先**成员自己的头像，缺失时回退智能体头像。**存量群不回填**——只在建群/加成员时生成。
- **真实名**：`SpeakerIdentity` 增加 `agentName`（底层智能体名），前端在消息的「头像 + 昵称」之后、当昵称 ≠ 真实名时，以弱化样式显示真实名。分享/导出走同一 `SpeakerIdentity`，自动一致。

**Tech Stack:** Go（`internal/service`、`internal/handler`、`internal/store`、SQLite）、Vue 3 + TS（`web/src`）、DiceBear（`@dicebear/core`）、Vitest、Playwright。

---

## 已锁定的设计决策

| 项 | 决策 |
|---|---|
| 名池来源 | **代码内置英文名列表**，不可配置、无设置 UI |
| 名池适用范围 | **只喂重复实例**（第 2 个及以后）；第一个实例保留智能体本名 |
| 池耗尽回退 | `本名 + 序号`（如 `Claude 2`） |
| 名池唯一性 | 组内全局唯一，且避开用户昵称 / 已有成员名 |
| 头像来源 | **所有新成员**按成员名生成；**忽略并覆盖**智能体原头像 |
| 头像风格 | **随机**，建成员时定一次并**冻结进 SVG**（落库） |
| 头像落库 | 新增 `chat_sessions.agent_avatar` 列；分享/导出读同一份 |
| 存量群 | **不回填**（旧群成员保持旧头像；旧群昵称不变） |
| 真实名显示 | **仅当昵称 ≠ 真实名**时显示；位置在昵称之后 |
| 判定口径 | 昵称与真实名比较（天然覆盖池名与序号名两种情况） |

---

## File Structure

**后端**
- Create: `internal/service/group_name_pool.go` — 内置名池 + 取名辅助
- Modify: `internal/service/group_store.go` — `assignMemberNames`、`GroupMember`、`ListGroupMembers`、`GroupMembersForGroups`、`SetMemberAvatar`
- Modify: `internal/service/speaker_identity.go` — `SpeakerIdentity.AgentName`、`speakerFromMember` 头像优先级
- Modify: `internal/service/database.go` — `agent_avatar` 列迁移
- Modify: `internal/model/chat.go` — `GroupMemberPreview.Avatar`
- Modify: `internal/handler/group.go` — 成员列表响应加 `avatar`/`agentName`、新端点 `ServeGroupMemberAvatars`
- Modify: `internal/handler/handler.go` — 注册 `/api/group/member-avatars`
- Modify: `internal/api/openapi.yaml` — 新端点 + 成员响应字段
- Test: `internal/service/group_instances_test.go`、`internal/service/group_name_pool_test.go`、`internal/service/agent_avatar_migration_test.go`、`internal/service/speaker_identity_test.go`、`internal/handler/group_member_avatar_test.go`

**前端**
- Modify: `web/src/composables/useGroupChat.ts` — `GroupMemberInfo` 加字段、`setMemberAvatars`、`generateMemberAvatars`
- Modify: `web/src/composables/useGroupMembers.ts` — `resolveSpeaker`/`resolveByName` 返回 `avatar`/`agentName`
- Modify: `web/src/utils/lazyAvatar.ts` — `generateAvatarSvg`
- Modify: `web/src/App.vue`、`web/src/components/chat/GroupSettingsSheet.vue` — 建群/加人后触发生成
- Modify: `web/src/components/chat/ChatMessageItem.vue` — 显示真实名
- Modify: `web/src/components/chat/GroupAvatarStack.vue`、`web/src/components/session/GroupMemberStack.vue`、`web/src/components/chat/GroupSettingsSheet.vue` — 用成员头像
- Modify: `web/src/share/SessionShareView.vue`、`web/src/components/session/SessionShareDialog.vue` — `agentName` 透传
- Test: `web/src/components/chat/__tests__/ChatMessageItem.*`、`web/src/composables/__tests__/useGroupMembers.*`、`web/src/utils/__tests__/lazyAvatar.*`
- E2E: `e2e/specs/group-member-identity.spec.ts`

---

## Phase A — 内置英文名池（后端命名）

### Task A1: 名池 + `assignMemberNames` 改造

**Files:**
- Create: `internal/service/group_name_pool.go`
- Modify: `internal/service/group_store.go:126-154`
- Test: `internal/service/group_name_pool_test.go`、`internal/service/group_instances_test.go`

- [ ] **Step 1: 写失败测试** — 新建 `internal/service/group_name_pool_test.go`

```go
package service

import "testing"

// 重复实例取池名，第一个实例保留本名；池按顺序取未占用者。
func TestAssignMemberNames_DuplicatesUsePool(t *testing.T) {
	specs := []GroupMemberSpec{
		{AgentID: "a"}, {AgentID: "a"}, {AgentID: "a"},
	}
	assignMemberNames(specs, nil, nil)
	want := []string{"", groupNamePool[0], groupNamePool[1]}
	// 第一个实例的 base 由 GetAgentDisplayName 决定（测试环境无注册表 → 空）
	// 这里只断言重复实例名来自池。
	if specs[1].DisplayName != want[1] || specs[2].DisplayName != want[2] {
		t.Fatalf("dup names = %q,%q want %q,%q", specs[1].DisplayName, specs[2].DisplayName, want[1], want[2])
	}
}

// 池被占用时跳到下一个未占用名字。
func TestAssignMemberNames_PoolSkipsTaken(t *testing.T) {
	taken := map[string]bool{groupNamePool[0]: true}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	if specs[1].DisplayName != groupNamePool[1] {
		t.Fatalf("dup name = %q want %q", specs[1].DisplayName, groupNamePool[1])
	}
}

// 池里所有名字都被占用 → 回退本名 + 序号。
func TestAssignMemberNames_PoolExhaustedFallsBack(t *testing.T) {
	taken := map[string]bool{}
	for _, n := range groupNamePool {
		taken[n] = true
	}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	if specs[1].DisplayName != " 2" {
		t.Fatalf("fallback = %q want %q", specs[1].DisplayName, " 2")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestAssignMemberNames_ -count=1`
Expected: 编译失败（`groupNamePool` 未定义）。

- [ ] **Step 3: 建名池** — 新建 `internal/service/group_name_pool.go`

```go
package service

// groupNamePool is the built-in pool of English display names used to
// disambiguate DUPLICATE instances of one agent inside a group. The FIRST
// instance keeps the agent's own name; the 2nd, 3rd, … draw the next unused
// pool name. When the pool is exhausted, naming falls back to
// "<agent name> <ordinal>" (the pre-pool behavior).
//
// It is deliberately NOT configurable (no config field, no settings UI): the
// names are intentionally language-neutral English tokens so they never collide
// with the group routing protocol's reserved characters and read the same in
// every UI locale. Drawn names are unique within a group and never reused.
var groupNamePool = []string{
	"Alice", "Bob", "Carol", "Dave", "Erin", "Frank", "Grace", "Heidi",
	"Ivan", "Judy", "Karl", "Laura", "Mallory", "Nina", "Olivia", "Peggy",
	"Quinn", "Rupert", "Sybil", "Trent", "Uma", "Victor", "Wendy", "Xena",
	"Yara", "Zane", "Aaron", "Bella", "Cody", "Diana", "Ethan", "Fiona",
	"Gavin", "Hazel", "Ian", "Julia", "Kevin", "Luna", "Milo", "Nora",
}

// nextPoolName returns the first pool name not present in used, or "" when the
// whole pool is taken.
func nextPoolName(used map[string]bool) string {
	for _, n := range groupNamePool {
		if !used[n] {
			return n
		}
	}
	return ""
}
```

- [ ] **Step 4: 改 `assignMemberNames`** — 替换 `internal/service/group_store.go:126-154` 整个函数

```go
// assignMemberNames gives every spec a UNIQUE display name, in the order the
// specs appear.
//
// Naming rule (design §4, updated for the built-in pool): the FIRST instance of
// an agent keeps its bare name ("Claude"); later instances draw the first
// unused name from groupNamePool ("Alice", "Bob", …). When the pool is
// exhausted the name falls back to the pre-pool rule ("<base> <ordinal>").
//
// The starting ordinal is (existing member rows for that agent in this group) +
// (ordinal within this batch); because member rows are only ever archived
// (never deleted) and a re-add always creates a NEW instance, the ordinal is
// monotonic and a historical nickname always refers to the same member. A
// collision (with another member's name, a pool name, or the reserved user
// nickname) advances until unique, so routing by name stays unambiguous.
//
// existingCounts maps agent id → number of member rows already in the group
// (archived included). It may be nil (no existing members). taken holds names
// already in use in the group, including the reserved user nickname.
//
// A spec's DisplayName, when non-empty, is used as the BASE name (callers such
// as CreateGroup may pass an explicit name); otherwise the base is derived from
// the agent's display name.
func assignMemberNames(specs []GroupMemberSpec, existingCounts map[string]int, taken map[string]bool) {
	used := make(map[string]bool, len(taken)+len(specs))
	for n := range taken {
		used[n] = true
	}
	used[groupUserTarget()] = true
	nextOrdinal := make(map[string]int, len(specs))
	for i := range specs {
		s := &specs[i]
		base := strings.TrimSpace(s.DisplayName)
		if base == "" {
			base = GetAgentDisplayName(s.AgentID)
		}
		// Instance index: existing rows + already-named in this batch, then +1.
		k := existingCounts[s.AgentID] + nextOrdinal[s.AgentID] + 1
		candidate := ""
		if k == 1 {
			// First instance keeps the agent's own name.
			candidate = base
		} else {
			// Duplicate instance: prefer an unused pool name.
			candidate = nextPoolName(used)
		}
		if candidate == "" || used[candidate] {
			// Pool exhausted, or the first instance's bare name collides:
			// fall back to "<base> <ordinal>", advancing until unique.
			candidate = base
			if k > 1 {
				candidate = fmt.Sprintf("%s %d", base, k)
			}
			for used[candidate] {
				k++
				candidate = fmt.Sprintf("%s %d", base, k)
			}
		}
		nextOrdinal[s.AgentID]++
		used[candidate] = true
		s.DisplayName = candidate
	}
}
```

- [ ] **Step 5: 更新既有断言** — `internal/service/group_instances_test.go`

三处断言随池名更新（其余逻辑不变）：

```go
// TestAddGroupMembers_AssignsUniqueNames（第 47、57 行）
	assertNames(t, names, "Claude", groupNamePool[0], groupNamePool[1])
	// …
	assertNames(t, names, "Claude", groupNamePool[0], groupNamePool[1], groupNamePool[2])

// TestAddGroupMembers_OrdinalNotReusedAfterRemoval（第 97 行）
	// 删除的是第 2 个实例（池名 groupNamePool[0]）。再加一个不得复用该池名，
	// 取下一个未占用池名（被删实例仍在 taken 中，故跳到 groupNamePool[2]）。
	assertNames(t, active, "Claude", groupNamePool[1], groupNamePool[2])
```

同时把 `TestAddGroupMembers_AssignsUniqueNames` 的注释里 `" 2", " 3"` 说法改为“池名”。`TestAddGroupMembers_NameCollisionWithUserNickname` **不变**（k=1 撞用户昵称 → 回退 `Claude 2`，仍成立）。

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/service/ -run 'TestAssignMemberNames_|TestAddGroupMembers_|TestCreateGroupWithMembers_' -count=1`
Expected: PASS（全部）。

- [ ] **Step 7: 提交**

```bash
git add internal/service/group_name_pool.go internal/service/group_name_pool_test.go internal/service/group_store.go internal/service/group_instances_test.go
git commit -m "feat(group): 重复实例改用内置英文名池命名"
```

---

## Phase B — 成员头像（后端）

### Task B1: `agent_avatar` 列 + 迁移

**Files:**
- Modify: `internal/service/database.go:1201-1223`（紧接 `agent_name` 迁移之后）
- Test: `internal/service/agent_avatar_migration_test.go`

- [ ] **Step 1: 写失败测试** — 新建 `internal/service/agent_avatar_migration_test.go`

```go
package service

import (
	"testing"

	"clawbench/internal/store"
)

// InitDB 必须为 chat_sessions 加上 agent_avatar 列（幂等）。
func TestInitDB_AddsAgentAvatarColumn(t *testing.T) {
	setupGroupDB(t)
	var cnt int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='agent_avatar'",
	).Scan(&cnt); err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("agent_avatar column count = %d, want 1", cnt)
	}
}

// 重复调用 InitDB 不报错（守卫式迁移）。
func TestInitDB_AgentAvatarMigrationIdempotent(t *testing.T) {
	setupGroupDB(t)
	if err := InitDB(); err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestInitDB_Add.*Avatar -count=1`
Expected: FAIL（列为 0）。

- [ ] **Step 3: 加迁移** — 在 `internal/service/database.go` 的 `agent_name` 迁移块（约 1223 行 `}` 之后）追加

```go
	// Migrate: add agent_avatar column — the per-MEMBER avatar SVG snapshot of a
	// group member row. Unlike agent_name (a name snapshot), the avatar is
	// generated CLIENT-side (DiceBear) from the member's own name and frozen
	// here so the public share/export snapshot (built server-side) can render
	// the exact same icon without a JS runtime. Non-member rows leave it ''.
	// Guarded by column existence so it is a no-op on re-run.
	var hasAgentAvatar int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='agent_avatar'").Scan(&hasAgentAvatar)
	if hasAgentAvatar == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN agent_avatar TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add agent_avatar column: %w", err)
		}
	}
```

（确认 `store` 已被 `database.go` import；`fmt` 亦然。）

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/service/ -run TestInitDB_Add.*Avatar -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/database.go internal/service/agent_avatar_migration_test.go
git commit -m "feat(group): chat_sessions 增加 agent_avatar 列与迁移"
```

### Task B2: `GroupMember.Avatar` + 列表查询 + 预览

**Files:**
- Modify: `internal/service/group_store.go:66-73`（`GroupMember`）、`688-711`（`ListGroupMembers`）、`766-790`（`GroupMembersForGroups`）
- Modify: `internal/model/chat.go:317-326`（`GroupMemberPreview`）
- Test: `internal/service/group_store_test.go`（追加）

- [ ] **Step 1: 写失败测试** — 追加到 `internal/service/group_store_test.go`

```go
// ListGroupMembers 读回成员自身的头像列；GroupMembersForGroups 预览也带头像。
func TestListGroupMembers_ReturnsAvatar(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-avatar-list"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := SetMemberAvatar(groupID, hostID, "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"); err != nil {
		t.Fatalf("SetMemberAvatar: %v", err)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if members[0].Avatar == "" {
		t.Fatalf("member avatar not read back")
	}
	previews, err := GroupMembersForGroups([]string{groupID})
	if err != nil {
		t.Fatalf("GroupMembersForGroups: %v", err)
	}
	if previews[groupID][0].Avatar == "" {
		t.Fatalf("preview avatar not read back")
	}
}
```

（`SetMemberAvatar` 在 Task B4 实现；若先跑此测试会编译失败——按 TDD 顺序先写测试、Task B4 实现后转绿。若希望本任务独立可跑，可临时用 `store.WriteExec` 直写；下方步骤 3 采用前者，最终在 B4 后统一验证。）

- [ ] **Step 2: `GroupMember` 加字段** — `internal/service/group_store.go:66`

```go
type GroupMember struct {
	ID      string // member session row id (also the speaker id in chat_history.agent_id)
	AgentID string // real agent id
	Name    string // display name (member row agent_name)
	Backend string
	Avatar  string // per-member avatar SVG snapshot (may be empty → fall back to agent avatar)
	Left    bool   // archived=1: removed from the group but its speech is kept
}
```

- [ ] **Step 3: `ListGroupMembers` 读 `agent_avatar`** — 替换查询与 Scan（`group_store.go:688-711`）

```go
	rows, err := store.ReadDB().Query(
		`SELECT id, agent_id, agent_name, backend, agent_avatar, archived FROM chat_sessions
		 WHERE group_id = ? AND session_type = ?
		 ORDER BY created_at ASC, rowid ASC`,
		groupID, groupMemberSessionType,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	members := make([]GroupMember, 0)
	for rows.Next() {
		var m GroupMember
		var archived int
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Name, &m.Backend, &m.Avatar, &archived); err != nil {
			return nil, err
		}
		m.Left = archived != 0
		members = append(members, m)
	}
	return members, rows.Err()
```

- [ ] **Step 4: `GroupMemberPreview` 加 `Avatar`** — `internal/model/chat.go:317`

```go
type GroupMemberPreview struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	Name    string `json:"name"`
	Backend string `json:"backend"`
	// Avatar is the member's own frozen avatar SVG (agent_avatar); empty for
	// members created before the per-member avatar feature.
	Avatar string `json:"avatar,omitempty"`
	IsHost bool   `json:"isHost,omitempty"`
}
```

- [ ] **Step 5: `GroupMembersForGroups` SQL + Scan** — `group_store.go:766-790`

```go
	query := fmt.Sprintf(`
		SELECT m.group_id, m.id, m.agent_id, m.agent_name, m.backend, m.agent_avatar,
		       (m.id = COALESCE(json_extract(g.context_state, '$.host_member_id'), '')) AS is_host
		FROM chat_sessions m
		LEFT JOIN chat_sessions g ON g.id = m.group_id
		WHERE m.group_id IN (%s) AND m.session_type = ? AND m.archived = 0
		ORDER BY m.group_id, m.created_at ASC, m.id ASC`, placeholders)
	// …
		if err := rows.Scan(&groupID, &p.ID, &p.AgentID, &p.Name, &p.Backend, &p.Avatar, &isHost); err != nil {
			return nil, err
		}
```

- [ ] **Step 6: 运行测试**

Run: `go test ./internal/service/ -run 'TestListGroupMembers_ReturnsAvatar' -count=1`
Expected: 编译失败（`SetMemberAvatar` 未实现）——在 Task B4 实现后转绿。先运行 `go build ./internal/service/` 确认除 `SetMemberAvatar` 外无错。

- [ ] **Step 7: 提交**

```bash
git add internal/service/group_store.go internal/model/chat.go internal/service/group_store_test.go
git commit -m "feat(group): 成员行读写 agent_avatar，预览携带头像"
```

### Task B3: `SpeakerIdentity.AgentName` + 成员头像优先

**Files:**
- Modify: `internal/service/speaker_identity.go`
- Test: `internal/service/speaker_identity_test.go`

- [ ] **Step 1: 写失败测试** — 新建 `internal/service/speaker_identity_test.go`

```go
package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

func TestSpeakerFromMember_PrefersMemberAvatarAndCarriesAgentName(t *testing.T) {
	setupGroupDB(t)
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		"claude": {ID: "claude", Name: "Claude", Backend: "claude", Avatar: "<svg>agent</svg>"},
	}
	t.Cleanup(func() { model.Agents = orig })

	project := "/tmp/speaker-avatar"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "claude", "claude", "Claude")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := SetMemberAvatar(groupID, hostID, "<svg>member</svg>"); err != nil {
		t.Fatalf("SetMemberAvatar: %v", err)
	}
	members, _ := ListGroupMembers(groupID)
	s := speakerFromMember(members[0], true)
	if s.Avatar != "<svg>member</svg>" {
		t.Fatalf("avatar = %q, want member avatar", s.Avatar)
	}
	if s.AgentName != "Claude" {
		t.Fatalf("agentName = %q, want Claude", s.AgentName)
	}
}

func TestSpeakerFromMember_FallsBackToAgentAvatar(t *testing.T) {
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		"claude": {ID: "claude", Name: "Claude", Backend: "claude", Avatar: "<svg>agent</svg>"},
	}
	t.Cleanup(func() { model.Agents = orig })
	s := speakerFromMember(GroupMember{AgentID: "claude", Name: "Alice", Backend: "claude"}, true)
	if s.Avatar != "<svg>agent</svg>" {
		t.Fatalf("avatar = %q, want agent fallback", s.Avatar)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestSpeakerFromMember_ -count=1`
Expected: FAIL（`AgentName` 字段不存在 / 头像未优先）。

- [ ] **Step 3: 改 `SpeakerIdentity` 与 `speakerFromMember`** — `internal/service/speaker_identity.go`

```go
type SpeakerIdentity struct {
	Name    string `json:"name,omitempty"`
	Backend string `json:"backend"`
	Avatar  string `json:"avatar,omitempty"`
	// AgentName is the underlying REAL agent's display name. For a member whose
	// display name was auto-derived (a built-in pool name, or "<base> <n>"), it
	// differs from Name, and the UI shows it after the nickname so the reader
	// knows which agent is speaking. Empty when it equals Name (no disclosure
	// needed) or the agent is gone.
	AgentName string `json:"agentName,omitempty"`
}

func speakerFromMember(m GroupMember, includeAvatar bool) SpeakerIdentity {
	s := SpeakerIdentity{Backend: m.Backend, Name: m.Name}
	if a := model.GetAgent(m.AgentID); a != nil {
		if a.Backend != "" {
			s.Backend = a.Backend
		}
		if a.Name != "" && a.Name != m.Name {
			s.AgentName = a.Name
		}
		if includeAvatar {
			// The member's OWN avatar snapshot wins; the agent avatar is only a
			// fallback for members created before per-member avatars existed.
			if m.Avatar != "" {
				s.Avatar = m.Avatar
			} else {
				s.Avatar = a.Avatar
			}
		}
	}
	return s
}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/service/ -run TestSpeakerFromMember_ -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/speaker_identity.go internal/service/speaker_identity_test.go
git commit -m "feat(group): 发言人身份携带真实智能体名，成员头像优先于智能体头像"
```

### Task B4: `SetMemberAvatar` + 新端点 + 成员列表响应

**Files:**
- Modify: `internal/service/group_store.go`（新增 `SetMemberAvatar`）
- Modify: `internal/handler/group.go`（成员列表加字段、新 handler）
- Modify: `internal/handler/handler.go:428` 附近
- Modify: `internal/api/openapi.yaml`
- Test: `internal/handler/group_member_avatar_test.go`

- [ ] **Step 1: 写失败测试** — 新建 `internal/handler/group_member_avatar_test.go`（沿用 `internal/handler` 现有群测试 fixture；参照 `group_test.go` 的 setup 写法）

```go
package handler

// 设置成员头像：合法 SVG 存库，非法 SVG（含 <script>）被拒。
func TestServeGroupMemberAvatars_StoresAndRejectsUnsafe(t *testing.T) {
	// … 建立带一个成员行的群（复用 group_test.go 的 helper）…
	// 合法：
	//   POST /api/group/member-avatars {groupId, avatars:[{memberId, avatar:"<svg .../>"}]} → 200
	//   再 GET 成员列表，该成员 avatar 非空。
	// 非法：
	//   avatar 含 "<script>" → 400，且库中该成员 avatar 仍为空。
}
```

（写测试时对齐 `internal/handler/group_test.go` 现有的 server fixture 与 helper 名，确保能真实发请求、读回列表。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/handler/ -run TestServeGroupMemberAvatars_ -count=1`
Expected: FAIL（路由未注册）。

- [ ] **Step 3: 服务层 `SetMemberAvatar`** — 追加到 `internal/service/group_store.go`

```go
// SetMemberAvatar stores a member's own avatar SVG (agent_avatar). The WHERE
// clause pins both the member row AND its group, so a caller cannot write an
// avatar onto a member of a different group even if it learns the row id; only
// group_member rows are touched (a plain session has no agent_avatar meaning).
func SetMemberAvatar(groupID, memberID, avatar string) error {
	_, err := store.WriteExec(
		`UPDATE chat_sessions SET agent_avatar = ? WHERE id = ? AND group_id = ? AND session_type = ?`,
		avatar, memberID, groupID, groupMemberSessionType,
	)
	return err
}
```

- [ ] **Step 4: 成员列表响应加 `avatar`/`agentName`** — `internal/handler/group.go` `serveGroupMembersList` 的循环体

```go
	for _, m := range members {
		agentName := ""
		if a := model.GetAgent(m.AgentID); a != nil {
			agentName = a.Name
		}
		out = append(out, map[string]any{
			"id":        m.ID,
			jsonAgentID: m.AgentID,
			jsonName:    m.Name,
			jsonBackend: m.Backend,
			"avatar":    m.Avatar,
			"agentName": agentName,
			"left":      m.Left,
			"isHost":    m.ID == hostMemberID,
		})
	}
```

- [ ] **Step 5: 新 handler** — 追加到 `internal/handler/group.go`

```go
// ServeGroupMemberAvatars persists per-member avatar SVGs generated client-side
// (DiceBear). Batch so a freshly created/expanded group writes all its new
// members in ONE round trip.
//
//	POST /api/group/member-avatars {groupId, avatars:[{memberId, avatar}]} -> {ok, saved:int}
//
// Each avatar must pass the same SVG safety gate as agent avatars
// (avatarSVGLooksSafe): this is a stored-XSS sink reachable by any authenticated
// caller, and the stored string is later replayed into the public share page.
func ServeGroupMemberAvatars(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req struct {
		GroupID string `json:"groupId"`
		Avatars []struct {
			MemberID string `json:"memberId"`
			Avatar   string `json:"avatar"`
		} `json:"avatars"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || len(req.Avatars) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
		return
	}
	saved := 0
	for _, a := range req.Avatars {
		if a.MemberID == "" || !avatarSVGLooksSafe([]byte(a.Avatar)) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		if err := service.SetMemberAvatar(req.GroupID, a.MemberID, a.Avatar); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		saved++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}
```

- [ ] **Step 6: 注册路由** — `internal/handler/handler.go:428` 之后

```go
	register("/api/group/member-avatars", ServeGroupMemberAvatars)
```

- [ ] **Step 7: openapi** — `internal/api/openapi.yaml` 在 `/api/group/members` 之后加 `/api/group/member-avatars`（POST，无 query，body `{groupId, avatars:[{memberId, avatar}]}`，200 `{ok, saved}`，400/401/500；鉴权同其它 `/api/group/*`）。同时给 `/api/group/members` 的 GET 200 成员对象补 `avatar`、`agentName` 字段。

- [ ] **Step 8: 运行测试**

Run: `go test ./internal/handler/ -run 'TestServeGroupMemberAvatars_|TestOpenAPIDrift' -count=1 && go test ./internal/service/ -run 'TestListGroupMembers_ReturnsAvatar|TestSpeakerFromMember_' -count=1`
Expected: PASS。

- [ ] **Step 9: 提交**

```bash
git add internal/service/group_store.go internal/handler/group.go internal/handler/handler.go internal/api/openapi.yaml internal/handler/group_member_avatar_test.go
git commit -m "feat(group): 成员头像写回端点 + 列表响应 avatar/agentName"
```

---

## Phase C — 成员头像（前端）

### Task C1: 类型 + API 客户端 + 发言人解析

**Files:**
- Modify: `web/src/composables/useGroupChat.ts`
- Modify: `web/src/composables/useGroupMembers.ts`
- Test: `web/src/composables/__tests__/useGroupMembers.test.ts`

- [ ] **Step 1: 写失败测试** — 追加/新建 `web/src/composables/__tests__/useGroupMembers.test.ts`

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

// resolveSpeaker 优先返回成员自己的 avatar，并透出真实 agentName。
describe('useGroupMembers.resolveSpeaker', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('prefers member avatar and exposes agentName', async () => {
    vi.mock('@/composables/useGroupChat', async () => ({
      listGroupMembers: vi.fn(async () => ({
        members: [{ id: 'm1', agentId: 'claude', name: 'Alice', backend: 'claude', avatar: '<svg>m</svg>', agentName: 'Claude', left: false, isHost: false }],
        mode: 'host', parallelDefault: false,
      })),
    }))
    // … mount composable with a ref session id, await refresh, assert resolveSpeaker('m1')
    //   → { name:'Alice', backend:'claude', avatar:'<svg>m</svg>', agentName:'Claude' }
  })
})
```

（按仓库 vitest 既有模式构造 composable 调用；不必真的 mount 组件。）

- [ ] **Step 2: 运行确认失败**

Run: `npx vitest run web/src/composables/__tests__/useGroupMembers.test.ts`
Expected: FAIL。

- [ ] **Step 3: `GroupMemberInfo` 加字段 + 客户端函数** — `web/src/composables/useGroupChat.ts`

```ts
export interface GroupMemberInfo {
  id: string
  agentId: string
  name: string
  backend: string
  /** 成员自身的头像 SVG（agent_avatar）；空 = 回退到智能体头像。 */
  avatar: string
  /** 底层真实智能体名；与 name 相同时也带回，供 UI 判定是否披露。 */
  agentName: string
  left: boolean
  isHost: boolean
}

/** setMemberAvatars 批量写回成员头像（DiceBear 生成，落库以对齐分享/导出）。 */
export async function setMemberAvatars(groupId: string, avatars: { memberId: string; avatar: string }[]): Promise<void> {
  await postJSON('/api/group/member-avatars', { groupId, avatars })
}
```

- [ ] **Step 4: `resolveSpeaker`/`resolveByName` 返回头avatar+agentName** — `web/src/composables/useGroupMembers.ts`

```ts
  function resolveSpeaker(memberRowId: string): { name: string; backend: string; avatar: string; agentName: string } | null {
    const m = byId.value.get(memberRowId)
    if (!m) return null
    // 成员自己的头像快照优先；旧成员无快照时回退智能体头像。
    return { name: m.name, backend: m.backend, avatar: m.avatar || getAgentAvatar(m.agentId) || '', agentName: m.agentName }
  }

  function resolveByName(name: string): { name: string; backend: string; avatar: string; agentName: string } | null {
    const target = name.trim()
    if (!target) return null
    const m = members.value.find(x => x.name.trim() === target)
    if (!m) return null
    return { name: m.name, backend: m.backend, avatar: m.avatar || getAgentAvatar(m.agentId) || '', agentName: m.agentName }
  }
```

- [ ] **Step 5: 运行测试**

Run: `npx vitest run web/src/composables/__tests__/useGroupMembers.test.ts && npm run typecheck`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web/src/composables/useGroupChat.ts web/src/composables/useGroupMembers.ts web/src/composables/__tests__/useGroupMembers.test.ts
git commit -m "feat(group): 前端成员类型/客户端携带 avatar+agentName"
```

### Task C2: `generateAvatarSvg` + 建群/加人后落库

**Files:**
- Modify: `web/src/utils/lazyAvatar.ts`
- Modify: `web/src/composables/useGroupChat.ts`（`generateMemberAvatars`）
- Modify: `web/src/App.vue:1955-1962`
- Modify: `web/src/components/chat/GroupSettingsSheet.vue:162`
- Test: `web/src/utils/__tests__/lazyAvatar.test.ts`

- [ ] **Step 1: 写失败测试** — 追加到 `web/src/utils/__tests__/lazyAvatar.test.ts`

```ts
import { describe, it, expect } from 'vitest'
import { generateAvatarSvg } from '@/utils/lazyAvatar'

describe('generateAvatarSvg', () => {
  it('returns a non-empty SVG string deterministically for a seed', async () => {
    const svg = await generateAvatarSvg('Alice')
    expect(svg).toContain('<svg')
  }, 20000)
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npx vitest run web/src/utils/__tests__/lazyAvatar.test.ts`
Expected: FAIL（`generateAvatarSvg` 未导出）。

- [ ] **Step 3: 实现 `generateAvatarSvg`** — 追加到 `web/src/utils/lazyAvatar.ts`

```ts
/**
 * Generate ONE DiceBear avatar SVG seeded by `seed`, using a RANDOM style from
 * the curated set. Loads only the single style chunk it picks (never the whole
 * kit), so it is cheap enough to call once per new group member. The chosen
 * style is baked into the returned SVG, so the stored string renders identically
 * on the public share page (which has no DiceBear runtime).
 */
export async function generateAvatarSvg(seed: string): Promise<string> {
    const core = await getAvatarLib()
    const styleName = AVATAR_STYLES[Math.floor(Math.random() * AVATAR_STYLES.length)]
    const mod = await retryableImport(STYLE_LOADERS[styleName])
    const style = new core.Style(mod.default as StyleDefinition)
    return new core.Avatar(style, { seed }).toString()
}
```

- [ ] **Step 4: `generateMemberAvatars`** — 追加到 `web/src/composables/useGroupChat.ts`

```ts
/**
 * Generate + persist avatars for group members that lack one. Called right
 * after a create/add (so only NEW members are backfilled — old groups are
 * deliberately left alone). Idempotent: members that already have an avatar are
 * skipped, so a partial failure can be retried by a later add.
 */
export async function generateMemberAvatars(groupId: string, onlyMemberIds?: string[]): Promise<void> {
  const { members } = await listGroupMembers(groupId)
  const targets = members.filter(m => !m.avatar && (!onlyMemberIds || onlyMemberIds.includes(m.id)))
  if (!targets.length) return
  const { generateAvatarSvg } = await import('@/utils/lazyAvatar')
  const avatars = await Promise.all(targets.map(async m => ({ memberId: m.id, avatar: await generateAvatarSvg(m.name) })))
  await setMemberAvatars(groupId, avatars)
}
```

- [ ] **Step 5: 建群后触发** — `web/src/App.vue:1955-1962`

在 `const { groupId } = await createGroup(...)` 之后追加：

```ts
    const { generateMemberAvatars } = await import('@/composables/useGroupChat')
    void generateMemberAvatars(groupId).catch(() => {})
```

（建群是全新群，全员无头像 → 传 `groupId` 即可。）

- [ ] **Step 6: 加人后触发** — `web/src/components/chat/GroupSettingsSheet.vue:162`

```ts
      const newIds = await addGroupMembers(props.groupId, list)
      void generateMemberAvatars(props.groupId, newIds).catch(() => {})
```

（`addGroupMembers` 返回 `string[]`；`generateMemberAvatars` 已在上方 import 列表补入。）

- [ ] **Step 7: 运行测试**

Run: `npx vitest run web/src/utils/__tests__/lazyAvatar.test.ts && npm run typecheck`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
git add web/src/utils/lazyAvatar.ts web/src/composables/useGroupChat.ts web/src/App.vue web/src/components/chat/GroupSettingsSheet.vue web/src/utils/__tests__/lazyAvatar.test.ts
git commit -m "feat(group): 新成员按名生成头像并落库"
```

### Task C3: 头像渲染统一走成员快照

**Files:**
- Modify: `web/src/components/chat/GroupAvatarStack.vue:61-69`
- Modify: `web/src/components/session/GroupMemberStack.vue:18-25`
- Modify: `web/src/components/chat/GroupSettingsSheet.vue:29`

- [ ] **Step 1: `GroupAvatarStack` 传 avatar** — 映射体加一个字段

```ts
const stackMembers = computed(() =>
  props.members.map(m => ({
    id: m.id,
    agentId: m.agentId,
    name: m.name,
    backend: m.backend,
    avatar: m.avatar,
    isHost: m.isHost,
  })),
)
```

- [ ] **Step 2: `GroupMemberStack` 类型加 avatar** — `GroupMemberPreview` 接口

```ts
  /** 成员自身头像 SVG（agent_avatar）；缺失时 AvatarStack 回退智能体头像。 */
  avatar?: string
```

（`AvatarStack` 已支持 `m.avatar || getAgentAvatar(m.agentId)`，无需改 `AvatarStack`。）

- [ ] **Step 3: `GroupSettingsSheet` 成员行头像** — 第 29 行

```html
            <AgentIcon :backend="m.backend" :name="m.name" :avatar="m.avatar || getAgentAvatar(m.agentId)" size="lg" />
```

- [ ] **Step 4: 运行相关测试 + typecheck**

Run: `npm run typecheck && npx vitest run web/src/components/common/__tests__/ web/src/components/chat/__tests__/`
Expected: PASS（如既有快照含成员行，更新快照以纳入 avatar）。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/chat/GroupAvatarStack.vue web/src/components/session/GroupMemberStack.vue web/src/components/chat/GroupSettingsSheet.vue
git commit -m "feat(group): 头像堆叠与成员行改用成员自身头像"
```

---

## Phase D — 消息显示真实智能体名

### Task D1: `agentName` 透传 + 消息渲染 + i18n

**Files:**
- Modify: `web/src/share/SessionShareView.vue`、`web/src/components/session/SessionShareDialog.vue`（类型 + resolver）
- Modify: `web/src/components/chat/ChatMessageItem.vue`（模板 + 样式）
- Modify: `web/src/i18n/locales/zh.ts`、`en.ts`
- Test: `web/src/components/chat/__tests__/ChatMessageItem.test.ts`（或既有对应用例）

- [ ] **Step 1: 写失败测试** — 追加到 ChatMessageItem 测试

```ts
it('shows the real agent name after the nickname when they differ', async () => {
  // speaker = { name:'Alice', backend:'claude', avatar:'', agentName:'Claude' }
  // 断言渲染出 "Alice" 与 "Claude" 两段（真实名以弱化样式出现在昵称之后）。
})
it('hides the real agent name when it equals the nickname', async () => {
  // speaker = { name:'Claude', backend:'claude', avatar:'', agentName:'Claude' }
  // 断言只出现一次 "Claude"。
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npx vitest run web/src/components/chat/__tests__/ChatMessageItem.test.ts`
Expected: FAIL。

- [ ] **Step 3: 模板加真实名** — `web/src/components/chat/ChatMessageItem.vue` 的 `.msg-speaker` 块内，`<span class="msg-speaker-name">` 之后、host tag 之前插入

```html
      <span class="msg-speaker-name">{{ speaker.name }}</span>
      <span
        v-if="speaker.agentName && speaker.agentName !== speaker.name"
        class="msg-speaker-real"
        :title="t('group.realAgent')"
      >{{ speaker.agentName }}</span>
```

样式（弱化小标签，与 host tag 一致的语言）：

```css
.msg-speaker-real {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  padding: 0 var(--space-1);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
}
```

- [ ] **Step 4: i18n** — `zh.ts` 的 `group:` 段加 `realAgent: '真实智能体'`；`en.ts` 加 `realAgent: 'Agent'`（若无该段则置于 `group` 下）。

- [ ] **Step 5: resolver 透传 `agentName`** — app 侧 `useGroupMembers` 已在 C1 返回；share 侧 `SessionShareView.vue` / `SessionShareDialog.vue`：

给 `SpeakerIdentity` 前端类型加 `agentName?: string`，并让 `makeSpeakerResolver` 产出的对象带上 `agentName`（`speakers[id].agentName`）。这样分享页与 App 内同源渲染。

- [ ] **Step 6: 运行测试 + typecheck**

Run: `npx vitest run web/src/components/chat/ && npm run typecheck`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add web/src/components/chat/ChatMessageItem.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts web/src/share/SessionShareView.vue web/src/components/session/SessionShareDialog.vue web/src/components/chat/__tests__/ChatMessageItem.test.ts
git commit -m "feat(group): 消息在昵称后显示真实智能体名"
```

---

## Phase E — E2E + 收尾

### Task E1: E2E 覆盖「同 agent 两次 → 池名 + 头像 + 真实名」

**Files:**
- Create: `e2e/specs/group-member-identity.spec.ts`

- [ ] **Step 1: 前置二进制**

Run: `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`

- [ ] **Step 2: 写 spec** — 用稳定 hook（`data-tab`/`data-session-id`/`data-action`）：

流程：建群选 Claude ×2 → 断言成员花名册出现两个不同名（含 pool 名）→ 打开会话，断言两条成员发言分别是 `Claude`（无真实名尾标）与 `Alice · Claude`（有尾标）→ 断言两成员头像 `src` 不同。
约定：若改动 mode，在 `afterAll` 调 `restoreNonBlockingMode()`；用 `expect.poll`/`toBeVisible` 等待，不用固定 `waitForTimeout`。

- [ ] **Step 3: 单跑本 spec（chromium）**

Run: `npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/group-member-identity.spec.ts`
Expected: 3 passed（或按 spec 用例数）。

- [ ] **Step 4: 提交**

```bash
git add e2e/specs/group-member-identity.spec.ts
git commit -m "test(group): e2e 覆盖池名/成员头像/真实名披露"
```

### Task E2: 文档同步 + 全量自检

- [ ] **Step 1: 更新设计文档** — 在 `docs/plans/2026-10-04-ai-group-chat-design.md` 的命名/成员章节，把 `本名 + 序号` 规则更新为「内置英文名池喂重复实例，池尽回退序号」，并补 `agent_avatar` 列与真实名披露的说明。

- [ ] **Step 2: 后端全量自检**

Run: `go build ./... && go test ./internal/service/ ./internal/handler/ ./internal/model/ -count=1 && golangci-lint run ./internal/service/ ./internal/handler/`
Expected: 全绿，0 issues。

- [ ] **Step 3: 前端自检**

Run: `npm run typecheck && npx vitest run web/src/composables/ web/src/utils/ web/src/components/chat/ web/src/components/common/ web/src/components/session/`
Expected: PASS。

- [ ] **Step 4: 构建前端（供立即验证）**

Run: `npm run build`
（纯前端即时生效；后端列/端点需 `./build.sh --restart`，按红线**需用户确认**后再执行。）

- [ ] **Step 5: 提交**

```bash
git add docs/plans/2026-10-04-ai-group-chat-design.md
git commit -m "docs(group): 同步成员命名/头像/真实名披露"
```

---

## 边界与风险清单（实现时勿丢）

1. **名池是全局共享的组内顺序取用**——同一群里 Claude×2 + GPT×2 得到 `Claude, Alice, GPT, Bob`（池被所有重复实例共用，组内唯一）。
2. **池耗尽回退序号**——回退用实例序号 `k`（`本名 k`），保持单调唯一；池被删名占用时旧名不回收（`archived` 行仍计入 `taken`）。
3. **存量群不回填**——`generateMemberAvatars` 仅在 create/add 后调用，不做「加载即补」。
4. **头像安全**——新端点必须过 `avatarSVGLooksSafe`（存储型 XSS 汇，且回放到公开分享页）。
5. **分享一致性**——头像落库后，`ResolveSessionSpeakers(true)` 的 `speakerFromMember` 读 `m.Avatar`，公开分享页无需 DiceBear。
6. **AI 路由按名**——池名是纯英文 token，安全且组内唯一，群提示词（`BuildHostSystemPrompt`/`BuildMemberSystemPrompt`）自动带上新名，无需改动。
7. **改智能体名**——`speakerFromMember` 的名字取成员快照（`agent_name`），改智能体名不影响历史；但 `agentName` 是**实时**智能体名，改名后真实名尾标会变（可接受）。
