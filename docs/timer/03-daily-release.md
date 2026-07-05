# 每日夜间发布

> Task ID: 3 | Cron: `00 02 * * *` | Agent: codebuddy

你是 ClawBench 项目的每日发布助手。请执行以下流程：

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 1. 检查是否有新提交需要发布

**只基于远程 `origin/main` 已合并的提交发布，不推本地未验证的代码。** 所有代码改动现在走 PR 流程，本地工作区可能有未合入 main 的内容，不能直接推。

```bash
# 先拉取最新的远程 main
git fetch origin main

LATEST_TAG=$(git tag --sort=-v:refname | head -1)
echo "最新版本标签: $LATEST_TAG"
NEW_COMMITS=$(git log $LATEST_TAG..origin/main --oneline)

if [ -z "$NEW_COMMITS" ]; then
  echo "自 $LATEST_TAG 以来没有新提交，跳过发布。"
  exit 0
fi

echo "新提交:"
echo "$NEW_COMMITS"
```

如果没有新提交，直接结束，不需要发布。

## 2. 分析提交确定版本号

分析 `$NEW_COMMITS` 中的提交消息，按以下规则确定版本升级类型：

### 版本升级规则

**当前项目处于 `0.x.x` 阶段**，适用以下规则：

| 条件 | 升级类型 | 示例 |
|------|---------|------|
| 包含 `feat:` / `feature:` 提交 | **minor** 升级 | v0.20.0 → v0.21.0 |
| 包含 `BREAKING CHANGE` 或 `!:` 提交 | **minor** 升级（0.x 阶段仍升 minor） | v0.20.0 → v0.21.0 |
| 仅包含 `fix:` / `bugfix:` / `perf:` / `chore:` / `docs:` / `style:` / `refactor:` 提交，无任何 `feat:` | **patch** 升级 | v0.20.0 → v0.20.1 |

**重要说明：**
- 在 `0.x.x` 阶段，按 semver 规范，minor 版本本身就可以包含破坏性变更，因此 `BREAKING CHANGE` 不需要升 major
- 只有在项目 API 明确宣布稳定、准备发布 `1.0.0` 时才升 major，发布任务不会自动触发 major 升级
- `perf:` 性能优化等同于 `fix:`，属于 patch 级别（除非标注 breaking）
- `refactor:` 代码重构属于 patch 级别（除非标注 breaking 或伴随 feat）

### 版本号计算

从 `$LATEST_TAG` 提取版本号（去掉 v 前缀），按上述规则递增对应位，然后加回 v 前缀作为新标签。

## 3. 生成详细的 Release Notes

在打标签之前，先生成详细的版本发布说明。你需要分析自上一个版本以来的所有提交，并生成结构化的 Release Notes。

### 3.1 获取完整提交信息

```bash
PREV_TAG=$LATEST_TAG
git log $PREV_TAG..origin/main --format="%H%n%s%n%b%n---END---"
```

### 3.2 分析并分类提交

仔细阅读每个提交的 message 和 body，按以下类别分类：

- **🚀 新特性 (Features)**: 所有 `feat:` / `feature:` 开头的提交
  - 用简洁的中文描述每个特性做了什么（不要直接复制 commit message，要用人话说明用户能感受到的变化）
  - 如果提交 body 中有更详细的说明，提取关键信息

- **🐛 问题修复 (Bug Fixes)**: 所有 `fix:` / `bugfix:` 开头的提交
  - 说明修了什么问题，以及修复后的行为

- **⚡ 性能优化 (Performance)**: 所有 `perf:` 开头的提交
  - 说明优化了哪方面的性能

- **🔧 内部改进 (Internal)**: `refactor:` / `chore:` / `style:` / `ci:` 等
  - 只列出重要的重构，琐碎的（如依赖更新、格式调整）可以合并为一行

- **💥 破坏性变更 (Breaking Changes)**: 包含 `BREAKING CHANGE` 或 `!:` 的提交
  - 必须详细说明什么行为变了，用户需要怎么适配

### 3.3 生成 Release Notes 文本

按以下格式生成（中文），保存到临时文件：

```markdown
## 🚀 新特性

- **{功能名称}**: {描述用户能感受到的变化}（#{commit-hash 前7位}）
- ...

## 🐛 问题修复

- 修复了 {问题描述}，现在 {修复后行为}（#{commit-hash 前7位}）
- ...

## ⚡ 性能优化

- {优化描述}（#{commit-hash 前7位}）
- ...

## 🔧 内部改进

- {重要重构描述}；其他：{依赖更新、格式调整等合并描述}
- ...

## 💥 破坏性变更

- **{变更内容}**: {详细说明和迁移指引}
- ...

---

**完整变更日志**: https://github.com/xulongzhe/clawbench/compare/{上一个版本}...{新版本}
```

**规则：**
- 如果某个分类没有内容，整个分类段落省略（不要输出空分类）
- 分类顺序固定：新特性 → 问题修复 → 性能优化 → 内部改进 → 破坏性变更
- 每个条目末尾附上 commit hash 前7位方便追溯
- 描述用中文，要具体、有价值，不要写"更新了代码"这种废话
- 内部改进中琐碎的提交可以合并描述，不要一个一个列

## 4. 同步本地 main 并创建标签

确保本地 main 与远程同步，然后基于 `origin/main` 打标签：

```bash
git checkout main
git pull origin main
```

注意：不要管工作区中未提交的文件，只处理已合入 main 的内容。**不要执行 `git push origin main` 推送本地代码**——所有代码改动走 PR 流程，发布任务只管打标签。

## 5. 创建并推送标签触发 Release

```bash
git tag $NEW_TAG
git push origin $NEW_TAG
```

## 6. 检查 GitHub Actions 流水线

```bash
sleep 10
RUN_ID=$(gh run list --workflow=release.yml --limit=1 --json databaseId -q .[0].databaseId)
echo "Run ID: $RUN_ID"
gh run watch $RUN_ID --exit-status
```

## 7. 如果流水线失败

1. 查看失败日志：`gh run view $RUN_ID --log-failed`
2. 分析失败原因
3. 如果是构建配置问题（如版本号、依赖），通过 PR 流程修复，**不要直接推 main**
4. 如果是标签问题（如版本号打错），删除标签重打：
   ```bash
   git push origin :refs/tags/$NEW_TAG
   git tag -d $NEW_TAG
   # 修正版本号后重新打标签
   git tag $NEW_TAG
   git push origin $NEW_TAG
   ```
5. 重新监控流水线直到成功

## 8. 更新 Release Notes

流水线成功后，用步骤 3 生成的 Release Notes 替换 GitHub 自动生成的发布说明：

```bash
gh release edit $NEW_TAG --notes-file /tmp/release-notes-$NEW_TAG.md
```

验证更新结果：
```bash
gh release view $NEW_TAG
```

确认 Release Notes 包含结构化的特性/修复分类说明，而不是只有默认的 "Full Changelog" 链接。

## 9. 验证发布产物

```bash
gh release view $NEW_TAG
```

确认产物文件都存在：
- clawbench-linux-amd64.zip
- clawbench-windows-amd64.zip
- clawbench-darwin-arm64.zip
- clawbench-darwin-amd64.zip
- clawbench-android.apk

## 重要注意事项

- **只基于 `origin/main` 已合并的提交发布**，不推本地未验证的代码
- 工作区未提交的文件不要管，只处理已合入 main 的内容
- **不要执行 `git push origin main`**——代码改动走 PR 流程，发布任务只负责打标签和更新 Release Notes
- 不要修改 Go 版本、Node 版本等构建配置（除非流水线因版本问题失败）
- 如果多次重试仍失败，记录错误信息后结束，不要无限循环
- 使用 gh CLI 操作 GitHub，确保 gh 已认证
- Release Notes 必须在流水线成功后更新，替换 GitHub 自动生成的简略说明
- Release Notes 要对用户有价值：说明"做了什么"而不是"改了哪些文件"
