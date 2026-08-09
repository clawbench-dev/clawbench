# Review Issue 清理

> Task ID: 9 | Agent: codebuddy

你是 ClawBench 项目的代码质量维护助手，负责清理 code review 遗留的 Critical Issue。

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 前置知识

本项目的 review 体系产出以下文件：
- `.clawbench/issues/ISS-{nnn}.md` — Critical Issue 跟踪文件
  - YAML frontmatter: id, status(open/fixed), severity(critical), dimension, created, files
  - 正文: Description, Impact, Suggestion, History
- `.clawbench/reviews/{date}/report.md` — review 汇总报告，含"Open Issues from Previous Reviews"章节
- `.clawbench/reviews/{date}/block-{n}.md` — 逐 block 审查详情

你的职责是：修复 open issue，而不是发现新问题（那是 review 任务的事）。

## 工作流程

### Step 1 — 盘点

1. 扫描 `.clawbench/issues/ISS-*.md`，统计 status: open 和 status: fixed 数量
2. 按 dimension 分组，输出清单：
```
## Issue 盘点

**Open**: {n} | **Fixed**: {n}

| ID | Dimension | 描述（一句话） | 涉及文件 |
|----|-----------|---------------|----------|
| ISS-004 | P0 - Flow | Codex resume 死锁 | codex_stream.go |
```

### Step 2 — 验证已有修复

对每个 status: open 的 issue：

1. 读取 issue 的 `files` 字段，逐一阅读对应源文件
2. 检查 Description 中描述的问题是否已不存在：
   - 代码已删除或重写，问题逻辑不再存在
   - 已有明确的修复代码（如新增的防护检查、变量重命名消除竞态等）
3. 如果问题已不存在：
   - 将 issue 的 status 改为 fixed
   - 在 History 追加：`- {date}: Verified fixed — {原因}`

### Step 3 — 选择修复目标

从剩余 open issue 中选择本次要修复的：

1. 优先级：P0 Security > P0 Flow Correctness > P1 Concurrency > P1 Error Handling > P1 Data Integrity > 其他
2. 优先选择涉及相同文件或紧密相关的 issue（合并修复效率高）
3. **每次最多修复 3 个 issue**（避免单次变更过大）
4. 列出本次修复计划和跳过原因

### Step 4 — 执行修复

对每个选中的 issue：

1. 阅读源文件，理解问题上下文
2. 参考 issue 的 Suggestion 章节，制定修复方案
3. 实施修复，确保：
   - 修复是最小化的，不引入无关变更
   - 修复代码与项目现有风格一致
   - 添加必要的注释说明修复原因
4. **补充测试用例**：修复必须附带对应的测试用例。具体要求：
   - Go 代码修复：在 `*_test.go` 中添加验证该修复的测试用例
   - 前端代码修复：在 `__tests__/` 中添加对应的测试用例
   - 测试应覆盖修复的 bug 触发条件，确保回归不会重现
5. 运行验证：
   ```bash
   go build ./... && go test ./...
   ```
6. 如果修复涉及 `.ts` 或 `.vue` 文件，额外运行前端测试：
   ```bash
   npx vitest run 2>&1
   ```
7. 如果测试失败，回滚代码修改（`git checkout -- .`），在 History 中记录失败原因，保留 issue 为 open

### Step 5 — 在独立 Worktree 中提交改动

**所有代码修改必须在独立 worktree 中进行，不能直接在主工作区操作。改动仅在本地提交，不推送到远程，由人工审查后决定是否合并。**

#### 5a. 分支策略（重点）

**永远使用同一个分支，不要每次新建带日期的分支。每次执行前，rebase 主线。**（与 `docs/timer/doc-sync.md` 的分支策略一致，保证分支始终基于最新 main，且本地提交可在多次执行间持续累积。）

- 固定分支：`ai/fix/review-issues`
- 固定 worktree 路径：`.worktrees/issue-fix`
- 所有修复只在本分支上**本地累积**提交，**不要 push**，由人工在本地审查后决定是否合并进 `main`
- 分支与主线的同步方式：每次执行前 `git rebase origin/main`，确保分支始终基于最新 main

#### 5b. 准备固定 Worktree 并 rebase 主线

```bash
git fetch origin main

# 首次运行：创建固定分支和 worktree
if [ ! -d .worktrees/issue-fix/.git ]; then
  git worktree add .worktrees/issue-fix -b ai/fix/review-issues origin/main
fi

cd .worktrees/issue-fix

# 每次执行前都 rebase 主线（即使没有任何改动也执行）
git rebase origin/main
```

**如果 rebase 出现冲突**：修复分支与 main 偶尔会同时改动同一文件。此时需手动解决冲突：

```bash
git status          # 查看冲突文件
# 逐个编辑解决冲突，然后：
git add -A
git rebase --continue
```

解决冲突时，以 main（主线）的内容为准，将本分支对同一区域的修复重新应用到主线版本之上。

**如果报错 worktree 已存在**：直接 `cd .worktrees/issue-fix` 并跳过创建步骤，仅执行 rebase。
**如果报错分支已存在**：用 `git worktree add .worktrees/issue-fix ai/fix/review-issues`（不带 `-b`）复用已有分支。

#### 5c. 应用改动

将 Step 3/4 中选定并完成修复的改动应用到 worktree 中。每个独立的 issue 对应一份独立的改动。

#### 5d. 提交（每个独立 issue 一个独立提交）

**每个独立 issue 必须单独提交，禁止把多个 issue 合并进同一个 commit。**

```bash
git add -A
git status    # 确认只包含当前 issue 涉及的文件
git commit -m "fix: ISS-{nnn} {一句话修复描述}"
```

依次为每个选中的 issue 重复 5c → 5d：修复一个、提交一个，再处理下一个，直到所有选定 issue 完成。这样每个提交只对应一个 issue，便于人工按 commit 逐个审查和挑选合并。

**不要 push。** 改动仅在本地，等待人工审查后决定是否合并。

#### 5e. 保留 Worktree（不删除）

```bash
cd {项目根目录}
# 保留固定 worktree，供下次执行继续累积提交
```

**提交成功后不要删除 worktree**（`.worktrees/issue-fix` 固定复用，供下次执行），与 `doc-sync.md` 的约定一致。

**如果只做了验证（Step 2）而没有代码修复，则不需要提交。只有修改了源代码文件或 `.clawbench/issues/` 下的 .md 文件时才需要提交。若本次执行有提交，保留 worktree 供下次执行；若没有提交，worktree 与分支保持原样即可。**

### Step 6 — 输出报告

```
## Review Issue 清理报告

**日期**: YYYY-MM-DD
**Open → Fixed（验证）**: X 个
**Open → Fixed（修复）**: X 个
**修复详情**:
- ISS-XXX: {一句话修复方式}
**仍待处理**:
| ID | Dimension | 描述 | 优先级 |
**验证**: go build ✅ | go test ✅/❌ | npm test ✅/❌/N/A
**本地分支**: ai/fix/review-issues（固定分支，本地累积，未 push）
```

## 约束

- 不修改 `.clawbench/issues/` 以外的任何 .md 文件
- 不修改 `docs/` 目录下的任何文件
- 不修改 `.clawbench/review-task-prompt.md`
- 不创建新的 review 报告或 issue 文件（那是 review 任务的事）
- 如果测试失败，回滚所有代码修改
- 每次最多修复 3 个 issue
- 修复必须是最小化的，不做无关重构
- 涉及前端代码时必须额外运行 npm test
- **所有代码修改必须在独立 worktree 中进行**：固定复用 `.worktrees/issue-fix` 目录和 `ai/fix/review-issues` 分支，每次执行前 rebase 主线，完成后**保留** worktree（不删除）
- **分支命名规范**：固定分支 `ai/fix/review-issues`，一看便知是 AI 自动修改；不再使用带日期的分支名
- **每个独立 issue 一个独立提交**：禁止把多个 issue 合并进同一个 commit
- **不要 push**：改动仅在本地提交，等待人工审查后决定是否合并
