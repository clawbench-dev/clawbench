# GitHub PR 审查与合并

> Task ID: 17 | Agent: codebuddy

你已在定时执行中，直接执行以下步骤，不要创建新的定时任务。

**项目根目录：** 运行 `cd` 到 Git 仓库根目录（即本文件所在仓库的根目录），后续所有命令均基于该目录执行。

## 总则

- 一次只处理一个 PR，处理完一个再处理下一个。
- 如果正在修复自己的 PR（author=xulongzhe）的 CI 问题，则暂不评审其他人的 PR，优先把自己的 PR 修好。
- 本仓库 owner 是 xulongzhe。
- **对其他人的 PR，不要对已有 review 评论的 PR 重复评论**（见下方去重逻辑）。
- **自己的 PR 合并策略**：用 `gh pr merge --auto --squash` 启用 GitHub 原生 auto-merge，CI 通过后由 GitHub 自动合并。定时任务不直接执行 merge。
- **其他人的 PR 合并策略**：定时任务 review + approve 后，直接 `gh pr merge --squash --delete-branch`。

## 步骤

1. 运行 `gh pr list --state open --json number,title,author,statusCheckRollup` 获取所有 open PR。
2. 如果没有 open PR，直接结束。

3. **分类处理**：将 PR 分为两组：
   - **自己的 PR**（author.login == "xulongzhe"）
   - **其他人的 PR**（author.login != "xulongzhe"）

4. **优先处理自己的 PR**（按 PR number 升序，一个一个来）：

   对每个自己的 PR：
   a. 检查 CI 状态：查看 statusCheckRollup 中所有 required checks 的 conclusion。
      必须通过的 7 项 CI checks：
      - Test (ubuntu-latest)
      - Test (macos-latest)
      - Test (windows-latest)
      - Coverage Gate (Go)
      - Coverage Gate (Frontend)
      - Build Frontend
      - Build Android APK

   b. **如果所有 7 项 CI 都通过（conclusion == SUCCESS）且无冲突（mergeable == "MERGEABLE"）**：
      - 直接合并：`gh pr merge <编号> --squash --delete-branch`
      - 继续处理下一个自己的 PR。

   c. **如果有任何 CI 不通过 或 有合并冲突**：
      - 先检查合并冲突：运行 `gh pr view <编号> --json mergeable,mergeStateStatus`
      - 查看失败的 CI 详情（如有）：`gh run view --log-failed` 或访问 failure 的 detailsUrl。
      - **创建独立 worktree 和修复分支**（不要直接 checkout PR 分支，避免污染主工作区）：

   分支命名规范：`ai/{类型}/{简要描述}-{日期}`，其中类型为 `docs`/`fix`/`feat`，日期格式 `YYYYMMDD`。

        ```
        git fetch origin <headRefName>
        BRANCH=ai/fix/pr<编号>-ci-$(date +%Y%m%d)
        git worktree add .worktrees/prfix-<编号> -b "$BRANCH" <headRefName>
        cd .worktrees/prfix-<编号>
        ```
      - 如果有合并冲突，先 rebase 解决冲突：
        ```
        git fetch origin main
        git rebase origin/main
        # 解决冲突后
        git add -A
        git rebase --continue
        ```
      - 如果 CI 不通过，分析失败原因，修改代码使 CI 能通过。**CI 有覆盖率门禁（Coverage Gate），如果是因为覆盖率下降导致失败，必须补充对应的测试用例**：
        - Go 代码变更：在 `*_test.go` 中添加测试覆盖新增/修改的逻辑
        - 前端代码变更：在 `__tests__/` 中添加测试覆盖新增/修改的逻辑
        - 修复 bug 的变更必须附带验证该 bug 的回归测试
        - 纯文档/配置变更无需补充测试
      - 修改完成后 commit 并 push 回 PR 分支（rebase 后用 --force-with-lease）：
        ```
        cd .worktrees/prfix-<编号>
        git add -A
        git commit -m "fix: resolve CI failures and merge conflicts"  # 仅 CI 问题时改为 "fix: resolve CI failures"
        git push origin <headRefName> --force-with-lease  # rebase 过就用 --force-with-lease
        ```
      - **清理 worktree**：push 成功后立即清理，释放工作区：
        ```
        git worktree remove .worktrees/prfix-<编号> --force
        ```
      - **轮询 CI 直到通过或失败**：
        ```bash
        MAX_POLLS=40
        POLL_INTERVAL=30
        for i in $(seq 1 $MAX_POLLS); do
          STATUS=$(gh pr view <编号> --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.status == "in_progress" or .status == "queued" or .conclusion == null)] | length' 2>/dev/null)
          if [ "$STATUS" = "0" ] 2>/dev/null; then
            FAILED=$(gh pr view <编号> --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.conclusion == "failure")] | length' 2>/dev/null)
            if [ "$FAILED" = "0" ] 2>/dev/null; then
              echo "✅ CI passed"
              break
            else
              echo "❌ CI still failing"
              break
            fi
          fi
          sleep $POLL_INTERVAL
        done
        ```
      - CI 通过后确认 auto-merge 或手动合并：
        ```bash
        gh pr merge <编号> --auto --squash
        # 确认合并
        for i in $(seq 1 10); do
          STATE=$(gh pr view <编号> --json state --jq '.state' 2>/dev/null)
          if [ "$STATE" = "MERGED" ]; then
            echo "✅ PR merged"
            break
          fi
          sleep 15
        done
        ```
      - 修复完当前这个 PR 后，回到步骤 1 重新获取 PR 列表（因为状态可能变化），然后继续处理下一个自己的 PR。

5. **处理其他人的 PR**（仅在自己的 PR 全部处理完毕后）：

   对每个其他人的 PR（按 PR number 升序）：
   a. 检查 CI 状态（同上的 7 项）。
   b. **检查合并冲突**：运行 `gh pr view <编号> --json mergeable,mergeStateStatus`
      - 如果 `mergeable == "CONFLICTING"` 或 `mergeStateStatus == "DIRTY"`：说明存在合并冲突。
      - 留评论（先检查去重）：`gh pr comment <编号> --body "⚠️ 该 PR 与主分支存在合并冲突，请先 rebase 或 resolve conflicts。"`
      - **跳过此 PR，不合并也不做深度审查**，继续下一个。
   c. 运行 `gh pr view <编号> --json title,body,baseRefName,headRefName,author,additions,deletions,changedFiles` 获取详情。
   d. 运行 `gh pr diff <编号>` 获取代码变更。
   e. 审查代码：检查逻辑正确性、边界情况、安全性、代码风格。
   f. **审查单元测试覆盖**：
      - 修复 bug 的 PR 必须包含验证该 bug 的测试用例。
      - 新功能的 PR 必须包含对应功能的单元测试。
      - 纯配置/文档/样式变更可豁免。
      - 没有针对问题的单元测试 = 严重问题，必须驳回。
   g. 如果 CI 全部通过且无冲突且审查无严重问题：
      - `gh pr review <编号> --approve --body "<审查意见>"`（先检查去重）
      - `gh pr merge <编号> --squash --delete-branch`
   h. 如果 CI 不通过或有严重审查问题：
      - `gh pr review <编号> --request-changes --body "<具体问题描述和修改建议>"`（先检查去重）
      - 不合并。

6. 汇总报告本次处理的 PR 数量、每个 PR 的处理结果。

## 去重逻辑：不要重复评论（仅针对其他人的 PR）

在对其他人的 PR 执行 `gh pr review` 或 `gh pr comment` 之前，必须先检查该 PR 是否已有评论：
- 运行 `gh api repos/xulongzhe/clawbench/pulls/<编号>/reviews --jq '.[].user.login'` 获取已有 review 的用户列表。
- 运行 `gh api repos/xulongzhe/clawbench/issues/<编号>/comments --jq '.[].user.login'` 获取已有 issue 评论的用户列表。
- 如果列表中已包含 `xulongzhe[bot]` 或当前 GitHub 用户名，说明已经评论/审查过，**跳过本次评论**，避免重复通知 PR 作者。
- 冲突提醒同理：如果已经提醒过冲突，不要重复提醒。

## 注意事项

- 审查标准要严格，但不要过度挑剔 Minor 样式问题。
- 合并前必须确认 CI 通过 **且** 无合并冲突。
- **自己的 PR**：CI 不过或冲突，一律拉 worktree 修。修复并 push 后轮询 CI 直到通过，确认合并后才算完成。**如果 CI 因覆盖率门禁失败，必须补充测试用例后再推送**。
- **其他人的 PR**：冲突的不要自己解决，一律评论通知作者解决；CI 不过的 request changes，明确指出缺少测试用例的具体位置。审查通过后用 --squash 合并。
- 修复自己 PR 时必须使用独立 worktree：创建 `.worktrees/prfix-<编号>` 目录和 `ai/fix/pr<编号>-ci-<日期>` 分支，修复完 push 后立即清理 worktree。绝不直接 checkout PR 分支到主工作区。
- 一次只修一个 PR，修完 push 并清理 worktree 后轮询 CI，不要并行处理多个。
