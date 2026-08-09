# 代码 Review

> Task ID: 4 | Agent: codebuddy

你是 ClawBench 项目的代码审查专家。请严格按照以下流程执行自动化代码 Review。

**Review 的核心目标：逆熵。** 除了解决 bug 之外，Review 更重要的作用是提升整个项目的质量、防止项目持续腐化。请始终站在提升整体质量的角度审视代码，而不只是挑出报错点。

**关于"可感知"的标准：** 只有用户可感知、可测试的行为问题才需提供复现方法；架构/设计层的优化直接给建议即可（详见「复现要求」）。

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## Review 维度与优先级

| 优先级 | 维度 | 关注点 |
|--------|------|--------|
| **P0** | 流程正确性 | 数据流完整性、错误处理缺口、边界条件 |
| **P0** | 安全性 | 命令注入、路径遍历、认证覆盖、密码处理 |
| **P1** | 架构合理性 | 分层清晰、职责单一、依赖方向 |
| **P1** | 并发安全 | 竞态条件、goroutine 泄露、死锁 |
| **P1** | 错误处理与资源泄露 | 错误传播、资源清理、僵尸进程 |
| **P2** | 死代码 | 未使用的函数/变量/导入、废弃分支 |
| **P2** | API 契约一致性 | SSE 事件格式对齐、请求/响应结构同步 |
| **P3** | 代码复用 | 重复逻辑提取、composable/util 复用 |
| **P3** | 硬编码值与魔法数字 | 分散的配置、硬编码端口/超时/阈值 |
| **P3** | 可观测性 | 关键路径日志、错误可追溯性 |

**执行限制**：P0 维度必须全部完成。P1/P2/P3 按优先级覆盖，超时则截断。

## 复现要求

- **仅限用户可感知、可测试的行为问题**（报错、异常输出、数据错误、性能卡顿、崩溃等）才必须提供复现步骤：
  - **人工可复现**：用户能按步骤手动触发，不依赖 AI 推断或"看起来可能有问题"
  - 复现描述格式：`**Reproduce**: <从触发场景到观察到异常现象的可执行步骤>`
  - **禁止**无感知复现路径的"纯静态"发现项下 Critical 结论
- **架构/设计层面的优化**（分层、职责、复用、可观测性等，不可被用户直接感知）**不需要**复现步骤，直接给出问题描述与改进建议即可。

## 排除项

不审查以下目录/文件：
- `.worktrees/`
- `vendor/`
- `*_test.go`
- `__tests__/`
- `public/`

## Step 1 — 确定模式

- **距上次全量扫描超过 7 天** → 全量扫描：枚举所有非排除的 `.go` / `.vue` / `.ts` 文件
- **否则** → 增量扫描：找到最新报告中的 `Baseline Commit`，用 `git diff {baseline-commit}..HEAD` 获取变更文件
- 如果没有任何历史报告（首次运行），按全量扫描执行

```bash
# 从最新报告中获取上次 review 的 commit id
# 检查上次全量扫描距今天数
LATEST_REPORT=$(ls -t .clawbench/reviews/*/report.md 2>/dev/null | head -1)
if [ -n "$LATEST_REPORT" ]; then
  BASELINE_COMMIT=$(grep -oP 'Baseline Commit: `\K[^`]+' "$LATEST_REPORT" 2>/dev/null || true)
  # 检查是否有全量扫描标记文件，以及距今天数
  LATEST_FULL=$(ls -t .clawbench/reviews/*/full-scan.marker 2>/dev/null | head -1)
  if [ -n "$LATEST_FULL" ]; then
    FULL_DATE=$(basename $(dirname "$LATEST_FULL"))
    DAYS_SINCE_FULL=$(( ($(date +%s) - $(date -d "$FULL_DATE" +%s 2>/dev/null || echo 0)) / 86400 ))
  else
    DAYS_SINCE_FULL=999
  fi
else
  BASELINE_COMMIT=""
  DAYS_SINCE_FULL=999
fi
echo "Baseline commit: ${BASELINE_COMMIT:-N/A}, Days since full scan: ${DAYS_SINCE_FULL}"

if [ "$DAYS_SINCE_FULL" -ge 7 ] || [ -z "$BASELINE_COMMIT" ]; then
  echo "MODE: full"
  mkdir -p .clawbench/reviews/$(date +%Y-%m-%d)
  touch .clawbench/reviews/$(date +%Y-%m-%d)/full-scan.marker
else
  echo "MODE: incremental"
  CHANGED_FILES=$(git diff --name-only $BASELINE_COMMIT..HEAD -- '*.go' '*.vue' '*.ts' | grep -v '_test.go' | grep -v '__tests__' | grep -v 'public/' | grep -v '.worktrees/' | grep -v 'vendor/')
  echo "变更文件:"
  echo "$CHANGED_FILES"
fi
```

## Step 2 — 流程追踪（增量模式）

对于增量模式，从变更文件出发：

1. 阅读每个变更文件，分析其 import 链和调用关系
2. 推导每个变更文件属于哪些数据流（flow）
3. 将上下游相关文件纳入审查范围（可跨前后端）

## Step 3 — 生成 Review 计划

输出到 `.clawbench/reviews/{date}/plan.md`，包含：

- 有序的 review block 列表，每个 block 包含：
  - Block 编号
  - 文件范围（每个 block ≤ 500 行）
  - 流程名称（如 "Chat Data Flow"、"SSH Tunnel"、"Scheduled Task"）
  - 该 block 的维度焦点
  - 优先级级别
- Block 按优先级排序（P0 优先），同优先级内按流程分组

## Step 4 — 逐 Block 执行 Review

对每个 block：

1. 用 Read 工具逐行读取 block 范围内的代码
2. 按维度焦点进行审查
3. 输出发现项，严重度分为：**Critical** / **Warning** / **Info**
4. 将结果写入 `.clawbench/reviews/{date}/block-{n}.md`
5. **Critical 发现项** → 同时写入 `.clawbench/issues/ISS-{nnn}.md`

### Block 文件格式

```markdown
# Review Block {n}: {flow name}

**Files**: {file list}
**Lines**: {start}-{end} ({count} lines)
**Dimension Focus**: {dimension name} (P{level})

## Findings

### Critical
- [CRIT-001] {description} ({file}:{line})
  - **Impact**: {why this is critical}
  - **Reproduce**: {可执行、可感知的人工复现步骤}
  - **Suggestion**: {how to fix}

### Warning
- [WARN-001] {description} ({file}:{line})
  - **Reproduce**: {可执行、可感知的人工复现步骤}
  - **Suggestion**: {improvement idea}

### Info
- [INFO-001] {description}
  - **Reproduce**: {可执行、可感知的人工复现步骤（若适用）}
```

### Issue 文件格式（仅 Critical）

```markdown
---
id: ISS-{nnn}
status: open
severity: critical
dimension: {dimension name}
created: {date}
files: [{file list}]
---

## Description
{problem description}

## Impact
{why this matters}

## Reproduce
{可执行、可感知的人工复现步骤 —— 从触发场景到观察到异常现象}

## Suggestion
{how to fix}

## History
- {date}: Created by review {review-date}
```

Issue 编号递增：检查 `.clawbench/issues/` 目录下已有的 `ISS-*.md` 文件，取最大编号 +1。

## Step 5 — 检查已有 Issue 的疑似解决状态

对于 `.clawbench/issues/` 下所有 `status: open` 的 Issue：

1. 检查 Issue 涉及的文件是否在本次变更范围内
2. 如果涉及文件的代码已变更，在 Issue 的 History 中追加：`- {date}: Suspected resolved (code changed in {file})`
3. 在报告中标记这些 Issue 为 "Suspected Resolved"

## Step 6 — 生成汇总报告

输出到 `.clawbench/reviews/{date}/report.md`。报告末尾的 `Baseline Commit` 字段记录当前 HEAD commit id，作为下次增量 review 的 diff 基准（Step 1 会从最新报告中读取该字段）。

### 报告格式

```markdown
# Code Review Report — {date}

**Mode**: {Full Scan | Incremental}
**Baseline Commit**: `{commit-id}` (from last report or "N/A (first run)")
**Blocks Executed**: {n}/{total}
**Truncation**: {None | "P2 and below truncated after block {n}"}

## Summary Statistics

| Dimension | Critical | Warning | Info |
|-----------|----------|---------|------|
| P0 - Flow Correctness | {n} | {n} | {n} |
| P0 - Security | {n} | {n} | {n} |
| P1 - Architecture | {n} | {n} | {n} |
| P1 - Concurrency | {n} | {n} | {n} |
| P1 - Error Handling | {n} | {n} | {n} |
| P2 - Dead Code | {n} | {n} | {n} |
| P2 - API Contract | {n} | {n} | {n} |
| P3 - Code Reuse | {n} | {n} | {n} |
| P3 - Hardcoded Values | {n} | {n} | {n} |
| P3 - Observability | {n} | {n} | {n} |

## Issues by Flow

### {flow name}
- [CRIT-001] {description} → ISS-{nnn}
- [WARN-001] {description}

## Open Issues from Previous Reviews
{list of unresolved issues with status, including Suspected Resolved ones}

## Next Steps
- [ ] Review findings and confirm/reject
- [ ] Address Critical issues

**Baseline Commit**: `{current HEAD commit id}` — 下次增量 review 将基于此 commit 计算 diff
```

## 目录结构

```
.clawbench/
├── reviews/
│   └── {date}/
│       ├── plan.md          # Review 计划
│       ├── block-01.md      # Block 1 审查结果
│       ├── block-02.md      # Block 2 审查结果
│       ├── ...
│       └── report.md        # 汇总报告
└── issues/
    ├── ISS-001.md           # Critical Issue
    ├── ISS-002.md
    └── ...
```

## 重要提醒

- 用 `date +%Y-%m-%d` 生成当天日期用于目录和文件命名
- 用 Read 工具逐行阅读代码，不要跳过任何文件
- Critical 发现项必须同时创建 Issue 文件
- 不要修改任何源代码文件，只生成 review 输出文件
- 不要打 git tag
