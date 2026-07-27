# 自动补充文档

> Task ID: 1 | Agent: codebuddy

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

### 6. 在独立 Worktree 中提交改动

**所有文档修改必须在独立 worktree 中进行，不能直接在主工作区操作。改动仅在本地提交，不推送到远程，由人工审查后决定是否合并。**

#### 6a. 确保获取最新 main

```bash
git fetch origin main
```

#### 6b. 创建 Worktree 和分支

分支命名规范：`ai/{类型}/{简要描述}-{日期}`，其中类型为 `docs`/`fix`/`feat`，日期格式 `YYYYMMDD`。

```bash
BRANCH=ai/docs/update-$(date +%Y%m%d)
git worktree add .worktrees/doc-update -b "$BRANCH" origin/main
cd .worktrees/doc-update
```

#### 6c. 应用改动

将之前步骤中准备好的文档改动应用到 worktree 中。

#### 6d. 提交

```bash
git add -A
git status
git commit -m "docs: 更新文档 — $(date +%Y-%m-%d)"
```

**不要 push。** 改动仅在本地，等待人工审查后决定是否合并。

#### 6e. 清理 Worktree

```bash
cd {项目根目录}
git worktree remove .worktrees/doc-update --force
```

**无论修改成功还是失败，都必须清理 worktree。分支保留在本地供审查。**

**如果没有文档需要更新，跳过步骤 6，直接输出报告。**

### 7. 输出报告

- 检查了多少提交
- 更新了哪些文档
- 每个文档的具体修改内容（一句话概括）
- 本地分支名（供审查合并）
- 如果没有更新，说明原因
