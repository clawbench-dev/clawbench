# 启动耗时优化 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 把服务启动耗时从实测 ~25s 降到 ~7s，方法是：①模型发现移出启动关键路径（异步化）；②发现本身并行化 + antigravity 探针短超时；③数据转换类迁移改为版本化只跑一次。

**Architecture:** 三块互相独立，按 C → B → A 顺序做（每块可单独交付、单独验证）。
- **C（迁移版本化）**：新增 `schema_migrations` 台账表 + `runOnce(name, fn)` helper，把 4 个「每次启动都重扫全表」的数据转换迁移改成记录后跳过。**只动数据迁移，不动 51 个加列/建索引迁移**（后者已由列探针天然只跑一次）。
- **B（发现提速）**：`discoverAndPersistModels` 串行 → 有界并行（信号量 4）；antigravity 探针超时 10s → 5s；修正 `isAgyStatusLine` 的大小写敏感。
- **A（异步化）**：`RefreshAgents` 启动时以 `SkipDiscovery: true` 同步跑（只做 CLI 探测 + YAML + 内存加载，~0.1s），模型探测丢到后台 goroutine，完成后重载内存并经 `ws.BroadcastEvent` 发 `agents_updated`，前端收到后 `loadAgents(true)`。

**Tech Stack:** Go 1.2x（modernc.org/sqlite、log/slog）、Vue 3 + TypeScript + Vitest。

---

## 实测依据（2026-09-29，本机 10 次重启一致）

`server starting` → `starting with TLS` 共 **24–31s**：

| 阶段 | 耗时 | 证据（`~/.clawbench/logs/clawbench-2026-09-29.log`，19:34 那次） |
|---|---|---|
| `model.RefreshAgents` | **~18.2s** | 15.896 → 34.085 |
| `service.InitDB` | **~5.5s** | 02.813 → 14.2（含一次性项目迁移 5.9s，8 天仅 1 次） |
| `rag.Init` | ~1.6s | 14.2 → 15.82（`LoadDictEmbed("zh")` + 第二个连接） |
| 其余（SSH/FRP/DingTalk/terminal…） | ~2.3s | 34.09 → 36.43 |

发现阶段串行明细（本次）：antigravity 卡满 10s 超时 → claude/codebuddy/codex 插件 ~3s → deepseek ~1s → pi ~0.9s → vecli ~0.05s → grok ~0.4s → opencode ~1.7s → qoder ~1.0s。

关键测量：
- `agy models` 未登录时实测 **16.0 / 16.5 / 16.0 秒**（联网+鉴权重试），被 10s 超时杀掉 → 走 `AntigravityCatalog` 静态 fallback。**花 10s 换来一个本来就有默认值的静态列表。**
- `MigrateToolCallsFromContent` 每次启动都报 `rows=443`（9/29 的 10 次重启全部 443），耗时 ~1.2s，**永不收敛**：候选行的 tool_use 块 `input` 是 `null`（slim 格式），`migrateToolCallsForRow` 直接返回 nil，而守卫 `NOT EXISTS (SELECT 1 FROM chat_tool_calls WHERE message_id=h.id)` 永远不满足。`MigrateMetadataFromContent` 同理（`rows=25 / migrated=0 / needed=25`，~1.25s）。
- DB：主库 3.1GB、WAL 435MB、`chat_history` 35,693 行 / `chat_tool_calls` 440,949 行 / `chat_thinking` 337,393 行。

---

## 范围与非目标

**做：** 上述 A/B/C 三块。

**明确不做（避免过度设计）：**
- 不把 51 个加列/建索引迁移改成编号版本（它们已由 `pragma_table_info` 探针天然只跑一次，改造收益为零、测试改动巨大）。
- 不动 `internal/rag` 的独立 schema 初始化（它是同一 DB 文件上的第二个 owner，但初始化实测仅 1.6s，且不重复跑数据迁移）。
- 不给迁移加「整体事务」（现有迁移用 `WriteExec`/`WriteBegin`，各自持 `writeMu`，包大事务会自死锁）。
- 不引入 `golang.org/x/sync/errgroup`（用 `sync.WaitGroup` + 缓冲 channel 信号量）。

**已知取舍（需在评审时确认）：**
1. 异步化后，启动横幅（`startup.PrintBanner`，`cmd/server/main.go:1335`）打印的 `Models: len(a.Models)` 会是 0（内存里模型还没到）。横幅纯装饰，可接受；若不接受，见 Task 8 的可选步骤。
2. 迁移改为 run-once 后，**若用户之后从旧备份恢复出一个含旧格式数据的 DB，这些数据不会被自动转换**。这符合「版本化迁移」语义，但需在 commit message 里写清。

---

# 阶段 C：数据迁移版本化

## Task 1: 新增 `schema_migrations` 台账表与 `runOnce` helper

**Files:**
- Modify: `internal/service/database.go`（在 `InitDB` 之前的 helper 区，约 line 200 附近；DDL 常量与 `WriteExec` 已存在）
- Test: `internal/service/schema_migrations_test.go`（新建）

**Step 1: 写失败测试**

新建 `internal/service/schema_migrations_test.go`：

```go
package service

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForMigrations 建一个内存库，装上 schema_migrations 表，并把
// 包级 db/dbRead 指过去（runOnce 读 dbRead、写 db）。
func setupTestDBForMigrations(t *testing.T) {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// 内存库是 per-connection 的，必须限制为单连接。
	d.SetMaxOpenConns(1)
	restore := SetDBForTest(d, d)
	t.Cleanup(func() {
		restore()
		_ = d.Close()
	})
	require.NoError(t, ensureSchemaMigrationsTable())
}

func TestRunOnce_ExecutesOnlyOnce(t *testing.T) {
	setupTestDBForMigrations(t)

	runs := 0
	fn := func() bool { runs++; return true }

	runOnce("test_migration_a", fn)
	runOnce("test_migration_a", fn)

	assert.Equal(t, 1, runs, "第二次调用必须被台账拦住")
	assert.True(t, isMigrationApplied("test_migration_a"))
}

func TestRunOnce_FailedMigrationIsRetried(t *testing.T) {
	setupTestDBForMigrations(t)

	runs := 0
	runOnce("test_migration_b", func() bool { runs++; return false })

	assert.Equal(t, 1, runs)
	assert.False(t, isMigrationApplied("test_migration_b"),
		"未完成的迁移不得记账，下次启动必须重试")
}

func TestRunOnce_DistinctNamesAreIndependent(t *testing.T) {
	setupTestDBForMigrations(t)

	runOnce("test_migration_c", func() bool { return true })
	assert.True(t, isMigrationApplied("test_migration_c"))
	assert.False(t, isMigrationApplied("test_migration_d"))
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run 'TestRunOnce' -v`
Expected: 编译失败，`undefined: runOnce` / `undefined: isMigrationApplied` / `undefined: ensureSchemaMigrationsTable`

**Step 3: 写最小实现**

在 `internal/service/database.go` 中、`InitDB` 之前加入：

```go
// schemaMigrationsDDL 是「已应用迁移」的台账。数据转换类迁移无法用列探针
// 判断是否已完成（它们的守卫是 LIKE 扫描 + NOT EXISTS，而某些行按设计永远
// 无法转换），所以改为按名字记账：跑完一次就不再重扫。
//
// 加列 / 建索引类迁移不在这里记账——它们已由 pragma_table_info / sqlite_master
// 探针天然只跑一次，且探针比台账更能反映真实 schema（例如用户手工 DROP 过列）。
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// ensureSchemaMigrationsTable 幂等建台账表。必须在任何 runOnce 之前调用。
func ensureSchemaMigrationsTable() error {
	_, err := WriteExec(schemaMigrationsDDL)
	return err
}

// isMigrationApplied 报告某个具名迁移是否已成功记账。
func isMigrationApplied(name string) bool {
	var n int
	if err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name,
	).Scan(&n); err != nil {
		// 读失败按「未应用」处理：宁可重跑一次（幂等迁移无副作用），
		// 也不要因为一次读错误永久跳过迁移。
		slog.Warn("schema_migrations: probe failed", "name", name, "err", err)
		return false
	}
	return n > 0
}

// markMigrationApplied 记账。INSERT OR IGNORE 使并发/重复调用安全。
func markMigrationApplied(name string) {
	if _, err := WriteExec(
		"INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)", name,
	); err != nil {
		slog.Warn("schema_migrations: mark failed", "name", name, "err", err)
	}
}

// runOnce 只在 name 未记账时执行 fn。fn 返回 true 表示「本轮已完成」——包括
// 「没有需要转换的行」和「扫描循环正常跑完但留下了按设计无法转换的行」两种
// 情况。返回 false 表示硬失败（查询/事务错误），此时不记账，下次启动重试。
func runOnce(name string, fn func() bool) {
	if isMigrationApplied(name) {
		return
	}
	if fn() {
		markMigrationApplied(name)
		return
	}
	slog.Warn("migration did not complete; will retry on next start", "name", name)
}

// ResetSchemaMigrationsForTest 清空台账，让测试能重新走迁移路径。
func ResetSchemaMigrationsForTest() {
	_, _ = WriteExec("DELETE FROM schema_migrations")
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/service/ -run 'TestRunOnce' -v`
Expected: 3 个 PASS

**Step 5: Commit**

```bash
git add internal/service/database.go internal/service/schema_migrations_test.go
git commit -m "feat(db): add schema_migrations ledger with runOnce helper"
```

---

## Task 2: 让 4 个数据迁移返回「是否完成」

**Files:**
- Modify: `internal/service/database.go:2084`（`MigrateMetadataFromContent`）、`:2197`（`MigrateTaskExecutionSummaries`）、`:2405`（`MigrateToolCallsFromContent`）
- Modify: `internal/service/thinking_migrate.go:14`（`MigrateThinkingFromContent`）
- Test: 复用既有直调测试（它们以语句形式调用，忽略返回值，**无需改动**）

> Go 允许把有返回值的函数当表达式语句调用并丢弃结果，所以 `MigrateMetadataFromContent()` 这种既有调用点不会因为加了返回值而编译失败。Step 2 会实测确认这一点。

**Step 1: 先确认既有直调点不会被破坏**

Run:
```bash
grep -rn "MigrateMetadataFromContent()\|MigrateTaskExecutionSummaries()\|MigrateToolCallsFromContent()\|MigrateThinkingFromContent()" internal/ --include=*.go | grep -v "func "
```
Expected: 只在 `database.go` / `thinking_migrate.go` 的 `InitDB` 调用点，以及测试里以语句形式出现（`MigrateXxxFromContent()`）。若有任何 `:= Migrate...` 或作为实参传递的用法，停下来重新评估。

**Step 2: 逐个改返回类型**

对 4 个函数统一套用这个形状（以 `MigrateMetadataFromContent` 为例，其余同构）：

```go
// 返回 true 表示本轮已完成（含「无待转换行」与「扫描跑完但留下无法转换的行」）；
// 返回 false 表示硬失败，调用方（runOnce）会留待下次启动重试。
func MigrateMetadataFromContent() bool {
	var needed int
	if err := dbRead.QueryRow(`...原查询...`).Scan(&needed); err != nil {
		slog.Error("metadata migration: count failed", slog.String("err", err.Error()))
		return false
	}
	if needed == 0 {
		return true
	}
	slog.Info("migrating metadata from chat_history to chat_metadata", slog.Int("rows", needed))
	...
	for {
		batch, err := migrateMetadataBatch(batchSize, offset)
		if err != nil {
			slog.Error("metadata migration: query failed", slog.String("err", err.Error()))
			return false          // ← 原为 return
		}
		...
	}
	slog.Info("metadata migration complete", ...)
	return true
}
```

四个函数的具体落点：
- `MigrateMetadataFromContent`（`database.go:2084`）：`needed` 查询改为检查 err（原为 `_ =`）；`needed==0 → return true`；`migrateMetadataBatch` 出错 → `return false`；函数末尾 `return true`。
- `MigrateTaskExecutionSummaries`（`database.go:2197`）：读该函数体，同样的三处改动（count 查询 err 检查 / 无待转换 → true / 硬失败 → false / 末尾 true）。
- `MigrateToolCallsFromContent`（`database.go:2405`）：`needed==0 → return true`；首个 query 出错 → `return false`（原为 `return`）；末尾 `return true`。
- `MigrateThinkingFromContent`（`thinking_migrate.go:14`）：`needed==0 → return true`；`rows.Err()` 分支 → `return false`；query 出错 → `return false`；末尾 `return true`。

**Step 3: 编译并跑既有直调测试**

Run:
```bash
go build ./... && \
go test ./internal/service/ -run 'TestMigrateMetadataFromContent|TestMigrateTaskExecutionSummaries|TestMigrateToolCallsFromContent|TestMigrateThinkingFromContent' -v
```
Expected: 编译通过；全部 PASS（证明加返回值没破坏既有调用）。

**Step 4: 变异验证（确认测试真的在测行为）**

临时把 `MigrateToolCallsFromContent` 末尾的 `return true` 改成 `return false`，重跑 Step 3。
Expected: 若既有测试全绿，说明它们不覆盖返回值——这是**预期的**（它们测转换结果，不测返回值）；把改动改回来。返回值语义由 Task 1 的 `TestRunOnce_FailedMigrationIsRetried` 与 Task 3 覆盖。

**Step 5: Commit**

```bash
git add internal/service/database.go internal/service/thinking_migrate.go
git commit -m "refactor(db): make data-conversion migrations report completion"
```

---

## Task 3: 在 InitDB 调用点用 runOnce 包住这 4 个迁移

**Files:**
- Modify: `internal/service/database.go:1782-1806`（4 个调用点）
- Modify: `internal/service/database.go:282`（`InitDB` 开头调用 `ensureSchemaMigrationsTable`）
- Test: `internal/service/schema_migrations_test.go`（追加）

**Step 1: 写失败测试**

追加到 `internal/service/schema_migrations_test.go`。这个测试走**真实 InitDB 路径**：造一个含旧格式数据的库 → 第一次 InitDB 应转换 → 清台账 → 第二次应再转换（证明「已应用则跳过」的正是台账）。

```go
// TestInitDB_DataMigrationsAreGatedByLedger 走真实 InitDB：
// 第一次应转换旧格式数据并记账；清掉台账后第二次应重跑。
func TestInitDB_DataMigrationsAreGatedByLedger(t *testing.T) {
	dir := t.TempDir()
	model.DataDir = dir

	// 第一次启动：空库，4 个迁移无待转换行 → 全部记账。
	require.NoError(t, InitDB(true))
	for _, name := range dataMigrationNames {
		assert.True(t, isMigrationApplied(name), "空库首启也应记账: %s", name)
	}

	// 清台账 = 模拟「尚未应用」。
	ResetSchemaMigrationsForTest()
	for _, name := range dataMigrationNames {
		assert.False(t, isMigrationApplied(name))
	}

	// 第二次启动：应重跑（空库仍是 no-op，但必须再次记账）。
	require.NoError(t, InitDB(true))
	for _, name := range dataMigrationNames {
		assert.True(t, isMigrationApplied(name), "清台账后应重新记账: %s", name)
	}
}
```

> `dataMigrationNames` 是 Task 3 Step 3 引入的具名常量切片，测试与调用点共用，避免名字漂移。

**Step 2: 跑测试确认失败**

Run: `go test ./internal/service/ -run 'TestInitDB_DataMigrationsAreGatedByLedger' -v`
Expected: 编译失败 `undefined: dataMigrationNames`（台账表也还没建）

**Step 3: 引入迁移名常量并在调用点套 runOnce**

在 `internal/service/database.go` 的 `schemaMigrationsDDL` 附近加入：

```go
// 数据转换类迁移的台账名。名字里带日期是为了可读；一旦发布就不得再改，
// 改了等于让所有已升级的库重跑一次全表扫描。
const (
	migMetadataFromContent     = "2026-09-29_migrate_metadata_from_content"
	migTaskExecSummaries       = "2026-09-29_migrate_task_execution_summaries"
	migToolCallsFromContent    = "2026-09-29_migrate_tool_calls_from_content"
	migThinkingFromContent     = "2026-09-29_migrate_thinking_from_content"
)

// dataMigrationNames 是上表的名字集合，供测试遍历。
var dataMigrationNames = []string{
	migMetadataFromContent,
	migTaskExecSummaries,
	migToolCallsFromContent,
	migThinkingFromContent,
}
```

在 `InitDB` 中，**早于**第一个 `runOnce` 调用点（建议放在 `PRAGMA busy_timeout` 之后、line 324 附近）插入：

```go
	// 迁移台账：数据转换类迁移按名字记账，只跑一次。
	if err := ensureSchemaMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}
```

然后把 4 个调用点（`database.go:1785 / 1791 / 1795 / 1806`）替换为：

```go
	runOnce(migMetadataFromContent, MigrateMetadataFromContent)
	runOnce(migTaskExecSummaries, MigrateTaskExecutionSummaries)
	runOnce(migToolCallsFromContent, MigrateToolCallsFromContent)
	// ...（migrateChatThinkingSeq 保持不变，它是列探针守卫的重建）...
	runOnce(migThinkingFromContent, MigrateThinkingFromContent)
```

> **顺序约束**：`migrateChatThinkingSeq`（`database.go:1800`）必须仍在 `MigrateThinkingFromContent` **之前**——后者的插入要与前者的 `seq` 约束匹配。保持原相对顺序。

**Step 4: 跑测试确认通过**

Run:
```bash
go test ./internal/service/ -run 'TestRunOnce|TestInitDB_DataMigrationsAreGatedByLedger' -v
```
Expected: 全部 PASS

**Step 5: 跑完整 service 包（迁移测试集中在这里）**

Run: `go test ./internal/service/ 2>&1 | tail -20`
Expected: 全绿。若有失败，逐条判断归属（研究已确认：**没有任何测试**通过 InitDB 断言这 4 个数据迁移，所以预期零失败）。

**Step 6: 变异验证**

把 `runOnce(migToolCallsFromContent, MigrateToolCallsFromContent)` 改回直接调用 `MigrateToolCallsFromContent()`，重跑 Step 4。
Expected: `TestRunOnce` 仍绿，但 `TestInitDB_DataMigrationsAreGatedByLedger` 必须失败（台账不再被记账）。改回来。

**Step 7: Commit**

```bash
git add internal/service/database.go internal/service/schema_migrations_test.go
git commit -m "perf(db): gate data-conversion migrations behind schema_migrations ledger

The metadata/tool_use/thinking conversions use LIKE scans whose guard can
never become false for rows whose content merely contains the literal text
(e.g. a tool_use block whose input is already slim/null). They re-scanned
the whole chat_history table on every boot: tool_use reported rows=443 on
all 10 restarts on 2026-09-29. Now they run once and are recorded.

Note: restoring an old backup that still contains legacy-format rows will
no longer auto-convert them."
```

---

# 阶段 B：发现提速

## Task 4: antigravity 探针独立短超时

**Files:**
- Modify: `internal/ai/backends/antigravity/discovery.go:10-15`
- Test: `internal/ai/backends/antigravity/discovery_test.go`（若不存在则新建）

**Step 1: 写失败测试**

```go
package antigravity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"clawbench/internal/model"
)

// agy 未登录时会联网重试 ~16s 才报错。默认 10s 超时会在每次启动白等 10s，
// 而结果永远是 AntigravityCatalog。给它一个更短的上限。
func TestAntigravitySourceHasShortTimeout(t *testing.T) {
	src, ok := model.LookupModelSource("antigravity")
	assert.True(t, ok, "antigravity 必须注册了模型源")
	assert.LessOrEqual(t, src.Timeout(), 5*time.Second,
		"antigravity 探针超时必须 <=5s（agy 未登录时挂 ~16s）")
}
```

> 若 `ModelSource` 接口没有 `Timeout()`，见 Step 3 的接口补充。

**Step 2: 跑测试确认失败**

Run: `go test ./internal/ai/backends/antigravity/ -run TestAntigravitySourceHasShortTimeout -v`
Expected: FAIL（当前是默认 10s）

**Step 3: 实现**

先确认 `ModelSource` 是否暴露超时。Run:
```bash
grep -n "type ModelSource interface" -A 15 internal/model/modelcatalog.go
```
- 若已有 `Timeout() time.Duration`：直接进入下面的改动。
- 若没有：给 `ModelSource` 接口加 `Timeout() time.Duration`，并在 `cliSource` 上实现 `func (s *cliSource) Timeout() time.Duration { return s.opts.Timeout }`；`pluginSource` / `staticSource` 返回 0（不适用）。然后跑 `go build ./...` 找出所有未实现该方法的类型并补齐。

改 `internal/ai/backends/antigravity/discovery.go`：

```go
func init() {
	model.RegisterModelSource(model.NewCLISource("antigravity", model.CLIOptions{
		Command: "agy",
		Args:    []string{"models"},
		Parse:   parseAgyModels,
		// agy 未登录时会联网重试 ~16s 才输出错误（实测 16.0/16.5/16.0s），
		// 而结果永远是 Fallback 里的 AntigravityCatalog。默认 10s 会在每次
		// 启动白等 10s，这里收紧到 5s：登录用户的一次真实拉取足够，
		// 未登录用户少等一半。
		Timeout:  5 * time.Second,
		Fallback: model.AntigravityCatalog,
	}))
}
```
（记得 `import "time"`。）

**Step 4: 跑测试确认通过**

Run: `go test ./internal/ai/backends/antigravity/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ai/backends/antigravity/ internal/model/modelcatalog.go
git commit -m "perf(discovery): cap antigravity probe at 5s (agy hangs ~16s when logged out)"
```

---

## Task 5: `discoverAndPersistModels` 并行化

**Files:**
- Modify: `internal/model/refresh.go:326-356`
- Test: `internal/model/discovery_db_test.go`（追加）

**Step 1: 写失败测试**

```go
// TestDiscoverAndPersistModels_RunsProbesConcurrently 断言各 backend 的探针
// 是并发跑的：3 个各 sleep 200ms 的探针，串行需 >=600ms，并发应 <400ms。
func TestDiscoverAndPersistModels_RunsProbesConcurrently(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	const n = 3
	const delay = 200 * time.Millisecond
	for i := range n {
		backend := fmt.Sprintf("slow-probe-%d", i)
		// 先插一个空 models、flag=0 的 agent，让 discovery 有行可写。
		_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
			VALUES (?, ?, ?, '[]', 0)`, backend, backend, backend)
		require.NoError(t, err)
		RegisterModelSource(PluginSource(backend, func() ([]AgentModel, string) {
			time.Sleep(delay)
			return []AgentModel{{ID: "m-" + backend, Name: "M"}}, ""
		}))
	}

	start := time.Now()
	discoverAndPersistModels(db)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 3*delay,
		"探针应并发执行（串行为 %v，并发应明显更快）", 3*delay)
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/model/ -run TestDiscoverAndPersistModels_RunsProbesConcurrently -v`
Expected: FAIL（elapsed ≈ 600ms+）

**Step 3: 实现**

替换 `internal/model/refresh.go:326-356` 的循环：

```go
// maxConcurrentProbes 限制同时运行的探针数。每个 CLI 探针会 fork 一个进程
// （部分会拉 Node），14 个同时起会打爆启动期的 CPU/内存，所以有界并行。
const maxConcurrentProbes = 4

func discoverAndPersistModels(db dbutil.Writer) (map[string][]AgentModel, []string) {
	backends := RegisteredModelSources()
	type probeResult struct {
		backend string
		models  []AgentModel
	}

	results := make([]probeResult, len(backends))
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup
	for i, backend := range backends {
		wg.Add(1)
		go func(i int, backend string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// DiscoverModels 内部有 per-backend 探针锁 + TTL 缓存，
			// 并发调用不会重复探测同一 backend。
			results[i] = probeResult{backend: backend, models: DiscoverModels(backend)}
		}(i, backend)
	}
	wg.Wait()

	// 落库必须串行化顺序无关，但结果集合必须确定（测试要断言）：
	// results 按 backends 下标写入，天然有序。
	discovered := make(map[string][]AgentModel)
	var updated []string
	for _, r := range results {
		if len(r.models) == 0 {
			continue
		}
		discovered[r.backend] = r.models

		modelsJSON, err := json.Marshal(r.models)
		if err != nil {
			slog.Warn("failed to marshal discovered models", "backend", r.backend, "error", err)
			continue
		}
		res, err := db.Exec(
			`UPDATE agents SET models = ?, models_auto_detected = 1
			 WHERE backend = ? AND (models_auto_detected = 1 OR models IS NULL OR models = '[]' OR models = 'null')`,
			string(modelsJSON), r.backend,
		)
		if err != nil {
			slog.Warn("failed to persist discovered models", "backend", r.backend, "error", err)
			continue
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			updated = append(updated, r.backend)
		}
	}
	return discovered, updated
}
```

> 注意：写入走 `db.Exec`（`service.WriteDB()` 的 `WriteExec` 已由 `writeMu` 串行化），所以并发只发生在**探测**阶段，落库仍在主 goroutine 顺序执行——避免并发写同一个 `*sql.DB`。

**Step 4: 跑测试确认通过**

Run: `go test ./internal/model/ -run 'TestDiscoverAndPersistModels|TestRefreshAgents' -v`
Expected: 全部 PASS（含既有的 `TestDiscoverAndPersistModels_SkipsBackendWithNoModels`）

**Step 5: 变异验证**

把 `maxConcurrentProbes` 改成 `1`，重跑 Step 4。
Expected: `TestDiscoverAndPersistModels_RunsProbesConcurrently` 必须失败。改回 `4`。

**Step 6: Commit**

```bash
git add internal/model/refresh.go internal/model/discovery_db_test.go
git commit -m "perf(discovery): probe model sources concurrently (bounded at 4)"
```

---

## Task 6: 修正 `isAgyStatusLine` 的大小写敏感

**Files:**
- Modify: `internal/ai/backends/antigravity/discovery.go:37-53`
- Test: `internal/ai/backends/antigravity/discovery_test.go`

**背景（实测）：** `agy models` 的 `Error: Please sign in...` 与 spinner 帧都写到 **stderr**，而探针默认 `CombineStderr: false` 只读 stdout，所以当前不会泄漏成假模型。但 `isAgyStatusLine` 用 `strings.HasPrefix(line, "error ")`（小写、带空格）匹配，与实际 `Error:` 不一致——一旦将来打开 `CombineStderr` 或上游改写输出流，错误行就会被 `ParsePlainLines` 当成 model id。这是低风险防御性修正。

**Step 1: 写失败测试**

```go
func TestIsAgyStatusLine_DropsErrorLines(t *testing.T) {
	for _, line := range []string{
		"Error: Please sign in to view available models.",
		"error something went wrong",
		"Failed to fetch models",
		"You are not logged into Antigravity",
		"Fetching available models...",
	} {
		assert.True(t, isAgyStatusLine(line), "应被当作诊断行丢弃: %q", line)
	}
}

func TestIsAgyStatusLine_KeepsModelIDs(t *testing.T) {
	for _, line := range []string{"gemini-3-pro", "gemini-2.5-flash"} {
		assert.False(t, isAgyStatusLine(line), "是模型 id，不得丢弃: %q", line)
	}
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/ai/backends/antigravity/ -run TestIsAgyStatusLine -v`
Expected: `TestIsAgyStatusLine_DropsErrorLines` FAIL（`"Error: ..."` 未被识别）

**Step 3: 实现**

```go
func isAgyStatusLine(line string) bool {
	lower := strings.ToLower(line)
	switch {
	case line == "Fetching available models...":
		return true
	case line == "You are not logged into Antigravity":
		return true
	// agy 实际输出是 "Error: ..."（首字母大写、带冒号），旧代码只匹配
	// 小写 "error "，两者都对不上。按不区分大小写的前缀匹配，且同时接受
	// 有无冒号两种形态。
	case strings.HasPrefix(lower, "error"):
		return true
	case strings.Contains(line, "Failed to"):
		return true
	case agyLogPrefixRE.MatchString(line):
		return true
	}
	return false
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/ai/backends/antigravity/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ai/backends/antigravity/discovery.go internal/ai/backends/antigravity/discovery_test.go
git commit -m "fix(discovery): match agy error lines case-insensitively"
```

---

# 阶段 A：模型发现异步化

## Task 7: 新增异步发现入口

**Files:**
- Modify: `internal/model/refresh.go`（在 `RefreshAgents` 之后追加）
- Test: `internal/model/discovery_db_test.go`

**设计要点：**
- `RefreshAgents` **签名与行为完全不变**，既有 20+ 测试原样通过。
- 新增 `StartModelDiscoveryAsync(db, onComplete)`：立刻返回，后台探测 → 落库 → **重载内存** → 调 `onComplete`。
- `internal/model` **不能** import `internal/ws`（`ws` 已 import `model`，会成环），所以广播回调由调用方注入。
- 必须重载内存：否则 `model.AgentList[i].Models` 仍是空的，模型选择器拿不到。

**Step 1: 写失败测试**

```go
// TestStartModelDiscoveryAsync_PersistsAndReloads 断言后台发现完成时：
// 模型已落库、内存已重载、回调被调用。
func TestStartModelDiscoveryAsync_PersistsAndReloads(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	const backend = "async-probe"
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
		VALUES (?, ?, ?, '[]', 0)`, backend, backend, backend)
	require.NoError(t, err)
	RegisterModelSource(StaticSource(backend, backend, []AgentModel{{ID: "m1", Name: "M1"}}))

	done := make(chan struct{})
	StartModelDiscoveryAsync(db, func() { close(done) })

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("异步发现未在 5s 内完成")
	}

	var modelsJSON string
	require.NoError(t, db.QueryRow(
		`SELECT models FROM agents WHERE backend = ?`, backend).Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "m1", "发现的模型必须已落库")
	assert.Contains(t, modelIDsFor(backend), "m1", "内存必须已重载")
}
```

> `modelIDsFor(backend)` 是小 helper，从 `Agents` map 里取该 backend 的 model id 集合；实现时按 `model.Agents` 的实际结构写（可能是按 agent id 索引，用 `GetAgent` 取）。

**Step 2: 跑测试确认失败**

Run: `go test ./internal/model/ -run TestStartModelDiscoveryAsync -v`
Expected: 编译失败 `undefined: StartModelDiscoveryAsync`

**Step 3: 实现**

追加到 `internal/model/refresh.go`：

```go
// StartModelDiscoveryAsync 在后台探测每个 backend 的模型列表，落库后重载
// 内存，然后调用 onComplete（可为 nil）。它立刻返回，调用方不得假设模型在
// 返回时已可用。
//
// 存在的理由：探测会 fork 各家的 CLI，其中 antigravity 未登录时会挂满超时，
// 串行合计约 18s。把它移出启动关键路径后，服务可立即开始接受请求，模型列表
// 稍后到达。
//
// 调用方负责通知前端（本包不能 import internal/ws——ws 已 import 本包）。
// 必须在 RefreshAgents(SkipDiscovery: true) 之后调用，否则 CLI 探测与 agent
// 插入尚未完成，后台发现会写不到行。
func StartModelDiscoveryAsync(db dbutil.Writer, onComplete func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("async model discovery panicked", "panic", r)
			}
		}()

		discovered, updated := discoverAndPersistModels(db)
		slog.Info("async model discovery complete",
			"backends", len(discovered), "rows_updated", len(updated))

		// 重载内存，让模型选择器拿到新列表。LoadAgentsIntoMemoryFromDB 先建新
		// map 再原子替换，并发读者不会看到空集合（ISS-302）。
		if err := LoadAgentsIntoMemoryFromDB(db); err != nil {
			slog.Error("async model discovery: memory reload failed", "error", err)
		}

		if onComplete != nil {
			onComplete()
		}
	}()
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/model/ -run 'TestStartModelDiscoveryAsync|TestRefreshAgents' -v`
Expected: 全部 PASS

**Step 5: Commit**

```bash
git add internal/model/refresh.go internal/model/discovery_db_test.go
git commit -m "feat(model): add async model discovery entry point"
```

---

## Task 8: 接线 `cmd/server/main.go`

**Files:**
- Modify: `cmd/server/main.go:906-917`（同步 refresh 加 `SkipDiscovery`）
- Modify: `cmd/server/main.go:1184-1196` 附近（`ws.InitManager` 之后启动异步发现并广播）

**Step 1: 同步 refresh 改为跳过发现**

`cmd/server/main.go:906-917` 改为：

```go
	// 1. 同步：探测已安装的 CLI、插入 agent、加载 YAML、重载内存。
	// 跳过模型发现——那一步会 fork 各家 CLI（串行约 18s），已移到下面的
	// 后台 goroutine，否则会拖住启动。
	//
	// 必须同步完成的部分：model.Agents / AgentList 要在默认 agent 选择
	// （下方）与 scheduler.LoadTasksFromDB（其会校验 task.AgentID 是否存在，
	// 否则静默跳过注册）之前就绪。
	if _, err := model.RefreshAgents(service.WriteDB(), model.RefreshOptions{
		ConfigDir:     filepath.Dir(configPath),
		SkipDiscovery: true,
	}); err != nil {
		slog.Error("failed to refresh agents", "error", err)
	}
```

**Step 2: 在 ws.InitManager 之后启动异步发现**

`cmd/server/main.go:1184-1186` 的 `ws.InitManager(...)` 之后插入：

```go
	// 模型发现放到后台：探测会 fork 各家 CLI（antigravity 未登录时挂满超时），
	// 串行约 18s。这里必须晚于 ws.InitManager，否则广播时 Manager 还没建好。
	model.StartModelDiscoveryAsync(service.WriteDB(), func() {
		ws.GetManager().BroadcastEvent(ws.ServerMessage{
			Type:  ws.MessageTypeEvent,
			ID:    ws.GenerateEventID(),
			Event: "agents_updated",
		})
	})
```

**Step 3: 编译**

Run: `go build ./...`
Expected: 通过

**Step 4: 实测启动耗时（关键验收）**

```bash
./build.sh --restart-skip-build 2>/dev/null || true   # 若已在跑；否则用 ./build.sh --restart
```
**⚠️ 不要擅自重启服务**——重启前先问用户。改为先只构建、由用户决定何时重启：
```bash
go build -o /tmp/clawbench-startup-check ./cmd/server
```
Expected: 编译通过。

用户重启后，用与基线同样的口径测量：
```bash
cd ~/.clawbench/logs && awk '/server starting|starting with TLS|agents refreshed|async model discovery complete/' clawbench-$(date +%F).log | tail -20
```
Expected:
- `server starting` → `starting with TLS` **<= 10s**（基线 24–31s）
- `agents refreshed` 紧跟在 `server starting` 之后 ~1s 内出现
- `async model discovery complete` 在其后若干秒出现，且 `starting with TLS` 不等待它

**Step 5: 可选（若用户不接受横幅显示 0 个模型）**

把 `cmd/server/main.go:1334-1341` 构造 `AgentInfo` 时的 `Models: len(a.Models)` 改为读一个「尚未发现」标志，在异步完成前打印 `Models: -1` 并在 `startup.PrintBanner` 里渲染成 `detecting…`。**默认不做**——先问用户。

**Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "perf(startup): move model discovery off the startup critical path

RefreshAgents now runs with SkipDiscovery, so agent presence + memory load
stay synchronous (the scheduler and default-agent selection depend on them)
while the ~18s of CLI model probes run in a background goroutine that
broadcasts agents_updated when done."
```

---

## Task 9: 前端响应 `agents_updated`

**Files:**
- Modify: `web/src/composables/useAgents.ts`（模块级注册 handler）
- Test: `web/src/composables/__tests__/useAgents.test.ts`

**设计要点：** 在 `useAgents.ts` 里注册（而不是在 `useGlobalEvents.ts` 的 dispatcher 里），因为方向必须是 `useAgents → useGlobalEvents`；反过来会让 `useGlobalEvents` 多一个 `useAgents` 依赖，风险更高。与 `useMessageClusters.ts:53-86` 的既有模式一致。

**Step 1: 写失败测试**

追加到 `useAgents.test.ts`：

```ts
it('agents_updated 事件触发强制重载', async () => {
  const { loadAgents } = useAgents()
  // 先加载一次，使 loadPromise 归位
  await loadAgents()
  const callsBefore = fetchMock.mock.calls.length

  // 触发全局事件分发
  emitGlobalEvent('agents_updated', {})

  await flushPromises()
  expect(fetchMock.mock.calls.length).toBeGreaterThan(callsBefore)
})
```

> 具体的事件分发调用名（`emitGlobalEvent` / 直接调 `useGlobalEvents` 内部 dispatcher）按 `useGlobalEvents.test.ts` 里既有的写法对齐——先读那个文件，照抄它的触发方式。

**Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useAgents.test.ts -t 'agents_updated'`
Expected: FAIL（无 handler，不会触发重载）

**Step 3: 实现**

在 `web/src/composables/useAgents.ts` 顶部 import 区加入：

```ts
import { useGlobalEvents } from '@/composables/useGlobalEvents'
```

在模块级（`loadAgents` 定义**之后**，因为 handler 引用它）加入：

```ts
// 模型发现在后台异步跑，完成时服务端广播 agents_updated。此时前端可能已经
// 用空模型列表渲染过选择器，需要强制重拉一次。与 useMessageClusters 一样
// 在模块级注册，全应用生命周期只注册一次。
//
// 方向是 useAgents → useGlobalEvents：反过来会让 useGlobalEvents 依赖
// useAgents，而它已被大量模块引用，风险更高。
useGlobalEvents().onEvent((event: string) => {
  if (event !== 'agents_updated') return
  void loadAgents(true)
})
```

**Step 4: 跑测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useAgents.test.ts`
Expected: PASS

**Step 5: 确认没有循环依赖**

Run:
```bash
grep -rn "useAgents" web/src/composables/useGlobalEvents.ts
```
Expected: **无输出**（`useGlobalEvents` 不得 import `useAgents`）。若有输出，说明方向反了，停下来重设计。

**Step 6: 前端构建**

Run: `npm run build`
Expected: 成功。若报循环依赖，改回在 `useGlobalEvents.ts` 的 dispatcher 里内联处理。

**Step 7: Commit**

```bash
git add web/src/composables/useAgents.ts web/src/composables/__tests__/useAgents.test.ts
git commit -m "feat(web): reload agents when the async model discovery completes"
```

---

## Task 10: 更新过期注释与规格文档

**Files:**
- Modify: `internal/model/refresh.go:1-22`（文件头注释）
- Modify: `internal/model/exec.go:17`（"runs synchronously in main"）
- Modify: `docs/spec/infra/config-and-discovery.md:20-25, 79, 97`
- Modify: `docs/spec/features/setup-wizard.md:32, 51, 66, 84`（若提到启动时同步发现）
- Modify: `docs/spec/README.md:51`（若索引描述需更新）

**Step 1: 改 `refresh.go` 文件头**

把 `refresh.go:1-22` 的历史叙述（`SyncDiscoverModels` / `AsyncRefreshModelCache`）替换为当前事实：

```go
// Package model — unified agent + model refresh.
//
// 两条路径：
//
//	RefreshAgents(db, opts)              → 同步：CLI 探测 + YAML + 内存重载
//	StartModelDiscoveryAsync(db, onDone) → 异步：模型探测 + 落库 + 内存重载
//
// 拆分的原因是启动耗时：模型探测会 fork 各家 CLI（antigravity 未登录时会挂满
// 超时），串行合计约 18s。启动只保留同步那一半（~0.1s，CLI 探测已并行），
// 否则默认 agent 选择与 scheduler.LoadTasksFromDB 拿不到 model.Agents。
//
// 历史上曾有过 AsyncRefreshModelCache，它因「同步+异步各探一遍且都不缓存」
// 被移除。现在 discoveryCache（TTL 5min）已去重，该反对理由不再成立。
```

**Step 2: 改 `exec.go:17`**

把 "This blocks server startup (model.RefreshAgents runs synchronously in main)" 改为说明文件捕获仍然必要（防止持有管道的孙进程把探针挂死），但不再是「阻塞启动」的理由。

**Step 3: 改规格文档**

- `config-and-discovery.md`：把「模型列表随启动时一次性发现」改为「启动同步加载 agent，模型列表由后台发现并广播 `agents_updated`」；补一节说明 `schema_migrations` 台账。
- `setup-wizard.md`：若描述「启动时同步扫描」，同步更正。

**Step 4: 确认无遗漏的过期引用**

Run:
```bash
grep -rn "SyncDiscoverModels\|AsyncRefreshModelCache\|runs synchronously in main" --include=*.go --include=*.md . | grep -v node_modules
```
Expected: 仅剩本 Task 刻意保留的历史说明（在 `refresh.go` 里）。

**Step 5: Commit**

```bash
git add internal/model/refresh.go internal/model/exec.go docs/spec/
git commit -m "docs: reflect split sync/async model discovery and migration ledger"
```

---

# 收尾验收

## Task 11: 全量检查与耗时对比

**Step 1: 确认没有并发 agent 在跑测试**

Run: `ps -eo pid,etime,cmd | grep -E "vitest|go test|npm run build" | grep -v grep`
Expected: 无输出。有则等待，**不要并发起跑**（会互相拖超时并产生假失败）。

**Step 2: 后端全量**

Run: `go test ./internal/service/ ./internal/model/ ./internal/ai/backends/... 2>&1 | tail -30`
Expected: 全绿。

**Step 3: 前端相关测试**

Run: `npx vitest run web/src/composables/__tests__/useAgents.test.ts web/src/composables/__tests__/useGlobalEvents.test.ts`
Expected: 全绿。

**Step 4: 推送前检查**

Run: `./scripts/pre-push-checks.sh`
Expected: 通过（Tier 1 失败若为 baseline 陈旧导致，按 AGENTS.md 判归属，不要靠补测试去"修"）。

**Step 5: 最终耗时对比**

重启后（**先征得用户同意**）用同一口径测量并记录：

| 指标 | 基线 | 目标 |
|---|---|---|
| `server starting` → `starting with TLS` | 24–31s | **<= 10s** |
| `server starting` → `agents refreshed` | ~24s | **<= 2s** |
| `tool_use migration progress` 是否再出现 | 每次启动都出现 | **首启后不再出现** |
| 前端模型选择器可用时间 | 启动即可（但列表来自 fallback） | 后台事件到达后自动刷新 |

---

## 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 异步发现期间用户开新会话 | 该回合可能不带模型（`DefaultModelID()` 返回空） | 前端收到 `agents_updated` 后刷新；用户也可手动 rescan。接受此短暂窗口 |
| 台账记账后迁移失败 | 旧数据不再自动转换 | `runOnce` 只在 `fn()==true` 时记账；硬失败会留待下次重试 |
| 从旧备份恢复 DB | 旧格式数据不再自动转换 | commit message 写明；提供 `ResetSchemaMigrationsForTest` 同款的手动重置途径（如需生产开关，另行讨论） |
| 并行探测打爆 CPU | 启动期卡顿 | 信号量限制为 4 |
| `useAgents → useGlobalEvents` 引入循环依赖 | 前端构建失败 | Task 9 Step 5/6 显式检查方向；失败则回退到 dispatcher 内联 |
| 横幅模型数显示 0 | 观感问题 | Task 8 Step 5 可选修复，默认不动 |

**回滚**：三块互相独立，可按 commit 粒度 revert。C 块回滚需同时保留 `schema_migrations` 表（无害的空表），或连带删除。
