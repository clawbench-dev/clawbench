# 每晚自动补充文档

> Task ID: 1 | Cron: `0 1 * * *` | Agent: codebuddy

你是 ClawBench 项目的文档维护助手。请完成以下任务：

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 任务目标

检查最近24小时的 git 提交，根据新功能/变更补充或更新项目文档。

## 执行步骤

### 1. 获取最近提交

```bash
git log --oneline --since="24 hours ago"
```

### 2. 分析变更内容

对每个 feat/fix/refactor 提交，阅读 commit message 并判断是否影响文档：

- `feat:` 新功能 → 检查是否已在文档中描述
- `feat:` 修改已有功能 → 检查文档描述是否需要更新
- `fix:` 修复了面向用户的行为 → 检查是否需要更新文档中的行为描述
- `refactor:` 纯内部重构 → 通常不需要更新文档

### 3. 检查需要更新的文档

需要检查的文档列表：

- `README.md` — 用户面向的功能介绍、截图、功能详解
- `README.en.md` — 英文版 README
- `AGENTS.md` — AI Agent 项目指引（架构、组件、配置、模式）
- `docs/DEVELOPMENT.md` — 开发指南
- `docs/DEVELOPMENT.en.md` — 英文开发指南
- `docs/FAQ.md` — 常见问题
- `docs/FAQ.en.md` — 英文FAQ
- 其他 `docs/` 下的专题文档

### 4. 更新文档

对于每个需要更新的文档：

- 阅读当前文档内容
- 根据提交内容，在合适位置添加或更新相关描述
- 保持文档现有风格和格式一致
- 中文文档用中文，英文文档用英文
- 如果新功能有截图，在 README 截图区域添加（仅当截图文件存在时）

### 5. 特别注意

- **AGENTS.md** 的 Architecture 部分需要反映最新的组件、composable、handler 等
- **README.md** 的功能详解部分需要覆盖所有面向用户的功能
- 新增的 AI 后端需要在所有文档中同步添加
- 新增的配置项需要添加到 AGENTS.md 的 Configuration 表格中
- 如果没有检测到需要更新的内容，直接输出「无需更新文档」即可，不要强行修改

### 6. 通过 PR 流程提交并等待 CI 通过

**所有文档修改必须通过 PR 流程，不能直接推送到 main。任务在 CI 通过且 PR 合并后才算完成。**

**重要：在独立 worktree 中工作，不要直接在主工作区上切分支。**

#### 6a. 创建独立 Worktree

```bash
WORKTREE=.worktrees/docs-update-$(date +%Y-%m-%d)
git worktree add "$WORKTREE" -b docs/update-$(date +%Y-%m-%d) origin/main
cd "$WORKTREE"
```

#### 6b. 更新文档

在 worktree 中完成所有文档修改。

#### 6c. 提交改动

```bash
cd "$WORKTREE"
git add -A
git status
git commit -m "docs: 更新文档 — $(date +%Y-%m-%d)"
```

#### 6d. 推送分支并创建 PR

```bash
cd "$WORKTREE"
BRANCH=docs/update-$(date +%Y-%m-%d)
git push origin "$BRANCH"
PR_URL=$(gh pr create --base main --head "$BRANCH" --title "docs: 更新文档 $(date +%Y-%m-%d)" --body "自动文档更新：检查最近24小时提交，补充或更新项目文档。")
PR_NUMBER=$(echo "$PR_URL" | grep -oE '[0-9]+' | tail -1)
echo "PR #$PR_NUMBER created"
gh pr edit "$PR_NUMBER" --add-label auto-merge
```

如果分支名已存在，加后缀 `-2`（worktree 路径也对应加后缀）。

#### 6e. 轮询 CI 直到通过或失败

```bash
MAX_POLLS=40
POLL_INTERVAL=30

for i in $(seq 1 $MAX_POLLS); do
  echo "=== Poll $i/$MAX_POLLS ==="
  PENDING=$(gh pr view "$PR_NUMBER" --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.status == "in_progress" or .status == "queued" or .conclusion == null)] | length' 2>/dev/null)

  if [ "$PENDING" = "0" ] 2>/dev/null; then
    FAILED=$(gh pr view "$PR_NUMBER" --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.conclusion == "failure")] | length' 2>/dev/null)
    if [ "$FAILED" = "0" ] 2>/dev/null; then
      echo "✅ All CI checks passed!"
      break
    else
      echo "❌ Some CI checks failed"
      break
    fi
  fi

  echo "CI still running... waiting ${POLL_INTERVAL}s"
  sleep $POLL_INTERVAL
done
```

#### 6f. CI 失败时修复

1. 查看失败详情：`gh pr view "$PR_NUMBER" --json statusCheckRollup --jq '.statusCheckRollup[] | select(.conclusion == "failure")'`
2. 分析失败原因并在同一 worktree 中修复
3. 推送修复：
   ```bash
   cd "$WORKTREE"
   git add -A && git commit -m "docs: 修复 CI 失败"
   git push origin docs/update-$(date +%Y-%m-%d)
   ```
4. 回到步骤 6e 继续轮询
5. 最多修复 3 次，超过后记录失败信息并结束

#### 6g. 确认合并

CI 通过后，auto-merge workflow 会自动合并 PR。轮询确认：

```bash
for i in $(seq 1 10); do
  STATE=$(gh pr view "$PR_NUMBER" --json state --jq '.state' 2>/dev/null)
  if [ "$STATE" = "MERGED" ]; then
    echo "✅ PR #$PR_NUMBER 已合并到 main"
    break
  fi
  sleep 15
done
```

如果 auto-merge 未触发，手动合并：`gh pr merge "$PR_NUMBER" --squash --delete-branch`

#### 6h. 清理 Worktree

**无论成功还是失败，都必须清理 worktree。**

```bash
cd {项目根目录}
git worktree remove .worktrees/docs-update-$(date +%Y-%m-%d) --force
git branch -d docs/update-$(date +%Y-%m-%d) 2>/dev/null || true
git fetch origin --prune
```

**如果没有文档需要更新，跳过步骤 6，直接输出报告。**

### 7. 输出报告

- 检查了多少提交
- 更新了哪些文档
- 每个文档的具体修改内容（一句话概括）
- PR 号码和 CI 状态
- 如果没有更新，说明原因
