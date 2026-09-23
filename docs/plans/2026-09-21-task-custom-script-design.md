# 定时任务自定义脚本

日期：2026-09-21
状态：已确认，待实施

## 背景与目标

定时任务（cron）新增可选的「自定义脚本」，在调用 AI 之前执行。当脚本退出码为 0 且
stdout 与 stderr 均为空时，跳过 AI 调用，并且不发送任何成功通知。脚本属于任务的
前置条件，不是任务执行本身。

## 已确认的需求决策

1. 脚本有输出（退出码 0）→ 输出注入 prompt，照常调用 AI
2. 脚本非 0 或超时 → 错误信息注入 prompt，照常调用 AI（不阻塞）
3. 跳过时：记录一条 execution（新状态 `skipped`），计入 `run_count` 并推进
   `next_run_at`，但不发任何通知
4. 仅 cron 任务支持（事件触发任务不支持）
5. 手动触发（`triggerType="manual"`）也执行脚本
6. 脚本超时默认 300 秒，可在任务表单自定义（每任务一个字段）
7. 脚本输出注入后会话历史可见（因复用 `renderedPrompt`）
8. 脚本执行期间用户点取消 → 立即中止脚本，任务标记 cancelled
9. 脚本运行期间不作为任务「运行中」显示（列表 `runningCount` 不计数），但会出现在
   执行历史里且可取消；该语义由脚本输入框的 placeholder 说明（见下「界面文案」），
   不再使用独立的长备注段落

## 数据模型

- `model.ScheduledTask`（`internal/model/scheduler.go:9-39`）新增 `Script string`
  （json `script`）、`ScriptTimeout int`（json `script_timeout`，秒；0 视为默认 300）
- `model.RunningExecutionView`（`internal/model/scheduler.go:69-73`）新增 `Phase string`
  （json `phase`，取值 `script` | `ai`）
- SQLite `scheduled_tasks`（`internal/service/database.go:493-510`）新增
  `script TEXT NOT NULL DEFAULT ''`、`script_timeout INTEGER NOT NULL DEFAULT 0`，
  走 `database.go:912-923` 的幂等 `ALTER TABLE` 循环
- 同步 `GetTasks` SELECT+Scan（`scheduler.go:1203-1230`）、`GetTaskByID`
  （`scheduler.go:1252-1260`）、`insertTask`（`scheduler.go:1279-1281`）、
  `updateTask`（`scheduler.go:1297-1298`）——位置化参数，最易出错

## 执行流程

入口：`Scheduler.executeTask`（`internal/service/scheduler.go:776`）。

将 `ctx, cancel := context.WithCancel(context.Background())` 从 `scheduler.go:925`
上提到函数顶部（`:777`）并加 `defer cancel()`；删除原 `:955-958` defer 中的
`cancel()` 调用（保留 `runningExecutions.Delete`）。

仅当 `!task.IsEventTriggered() && task.Script != ""` 时执行脚本阶段：

1. 在 `runningExecutions` 登记合成条目（ID `script-<taskID>`、TaskID、Phase `script`、
   CancelFunc = ctx 的 cancel、StartedAt、TriggerType），**不 emit `running`**
   （理由见下方「实现偏差」第 1 条；`emit` 会在实现中造成通知噪音）
2. 执行脚本，工作目录 = projectPath，超时 = ScriptTimeout
   - **exit 0 且 stdout+stderr 皆空** → 写 execution（status `skipped`、session_id `''`）、
     `advanceTaskAfterRun`（记账 + 推进 `next_run_at` + 按 repeat_mode 结算）、移除合成
     条目、**不 emit 任何事件**、return
   - **ctx 被取消** → 写 execution（status `cancelled`、session_id `''`）、
     `UpdateTaskStats`（只记 `last_run_at`/`run_count`，**不推进** `next_run_at`、不改状态
     ——见「实现偏差」第 6 条）、移除合成条目、emit `cancelled`、return
   - **其他情况**（有输出 / 非 0 / 超时）→ 移除合成条目，把输出或错误注入
     `renderedPrompt`，继续原有流程
3. 继续原有流程：建 session → 写 execution 行 → 在 `runningExecutions` 登记 Phase `ai`
   条目 → `runTurnStart`

注意 `AddTaskExecution`（`scheduler.go:1303-1314`）硬编码 status `'running'`，且
`UpdateExecutionStatus`（`scheduler.go:1316-1323`）按 `session_id` 更新——跳过路径没有
session，需要新增按 execution id 定位的插入/更新能力。

## RunningCount 语义（关键设计点）

- `GetRunningCounts`（`scheduler.go:88-102`）只统计 Phase `ai` 的条目
- `GetRunningExecutions`（`scheduler.go:66-86`）返回全部 phase
- `internal/handler/scheduler.go:214-217` 的 `RunningCount` 同样只数 ai，与列表一致

理由：前端误报成功通知来自四条通道——`useTaskTab.loadTasks` 轮询（`runningCount`
1→0）、`App.vue:1267` WS popover、`useGlobalEvents.ts:570` 声音/浏览器通知、后端
钉钉/飞书推送。其中三条按 status 字符串门控（共享定义 `IsNotifiableEvent`，
`internal/service/pending_events.go:62-69`），只有轮询那条用 `runningCount` 启发式。
让脚本阶段不计入 `runningCount`，即可完全不改动这四条通道中的任何一条。

## 脚本执行器

新文件 `internal/service/task_script.go`（外加 build-tagged 进程组辅助文件，
unix/windows 各一）：

- shell 解析用 `platform.ResolveLoginShell()`，Windows 兜底 `cmd /C`
  （参照 `internal/ai/acp_terminal.go:73-79`）
- stdout / stderr 分开捕获（**cappedBuffer**，捕获时即按 `scriptOutputCap` 截断，不合并；
  见「实现偏差」第 3 条）
- `SysProcAttr` 设置进程组，`cmd.Cancel` 杀整个进程组，`cmd.WaitDelay = 5s`
  （避免孙进程持有管道导致挂死）
- 用 `context.WithTimeout` 实现超时
- 不使用 `model.RunCommandContext`（`internal/model/exec.go:22`）：它只杀直接子进程、
  不杀进程组，且无 WaitDelay

## Prompt 注入

在 `renderedPrompt`（`scheduler.go:871`）上追加定界块，位于事件上下文之后、
`task.Prompt` 之前。stdout 与 stderr 各限 `scriptOutputCap`（64 KiB）防撑爆；该上限在
**捕获时**由 `cappedBuffer` 施加（见「实现偏差」第 3 条），`truncateScriptOutput` 只做
二次收尾。该变量同时供 `AddChatMessage`（`:885`）与 `ai.ChatRequest.Prompt`（`:912`），
因此会话历史可见。

## 前端

- `web/src/components/task/TaskFormPage.vue`：新增 cron-only 的脚本 textarea 与超时
  输入；超时输入默认留空、以 placeholder `300` 呈现后端默认值（不再渲染字面 `0`），
  不提供独立的长备注段落（见下「界面文案」）
- `web/src/composables/useTaskForm.ts`：`form`（:33-46）、`init`（:51-80）、`submit`
  payload（:98-109）加 `script` / `script_timeout`（snake_case）；`scriptTimeout` 初值为
  `''`（空串），`submit()` 时 `Number(...) || 0` 强制为数字 `0`（后端「用默认值」哨兵），
  与 `maxRuns` 的空/零处理一致
- `web/src/composables/useTaskHistory.ts`：RunningExecution 加 `phase`；
  `TaskHistoryTab.vue` 的独立 `prevRunningCount`（:210，基于
  `runningExecutions.length`）只数 ai
- `web/src/components/task/TaskHistoryTab.vue`：新增脚本阶段行、`skipped` 徽章与 CSS
- `web/src/components/task/TaskExecDetail.vue`：新增 `skipped` 提示；`showContinueBtn`
  （:187-191）排除 `skipped`（空 sessionId 会让「继续对话」报错，见
  `continue_conversation.go:121-122`）
- i18n `web/src/i18n/locales/zh.ts` 与 `en.ts` 的 `task.exec` 块加 `statusSkipped` 等键；
  `task.form` 块加脚本字段文案（见下）
- `internal/api/openapi.yaml` 同步（POST `/api/tasks` 请求体 ~3246-3275，PUT ~3313）

### 界面文案（要点，zh/en 双份）

跳过语义（退出码 0 且无输出 → 跳过 AI）写在脚本 textarea 的 placeholder 里，**不再**
使用独立的 `.form-hint` 长段落（见「实现偏差」第 5 条）。原始长备注曾覆盖三点：

1. 脚本在调用 AI 之前执行，工作目录为任务的项目路径
2. 脚本运行期间不会在任务列表显示为「运行中」——它不是一次 AI 执行；但会出现在执行
   历史里，可以取消
3. 退出码 0 且无输出时会跳过 AI 且不发送任何通知；有输出或非 0 / 超时会照常调用 AI，
   内容注入 prompt

其中第 3 点保留在 placeholder 中；第 1、2 点不再在表单里显式说明（执行历史本身已可见
脚本阶段行）。超时字段的 300 秒默认值以 placeholder 呈现，空值提交为 `0`。

## 未读统计

把 `skipped` 排除出未读统计：`scheduler.go:1207-1208`、`1215-1216`、`1256-1257`、
`1472-1474`，以及 `internal/handler/scheduler.go:552` 的 `IsUnread`。否则无内容的跳过
会顶起未读角标。

## 测试

- 脚本执行器单测：有输出 / 无输出 / 非 0 / 超时 / 取消
- `executeTask` 跳过路径断言：不建 session、列表 runningCount 为 0、无任何 emit、
  run_count 递增、next_run_at 推进
- 脚本阶段取消路径
- 4 处重复 schema 补列：`scheduler_test.go:54-73`、`scheduler_executor_test.go:53-72`、
  `handler/testutil_test.go:129-161`（及其它含 `scheduled_tasks` DDL 的测试文件）
- 前端：`useTaskForm.test.ts` payload 断言 + 新字段渲染测试

## 待实现期验证

- **已确认安全**：脚本阶段取消时 `emitTaskEvent` 传入空 sessionID，`scheduler.go:728-741`
  对会话标题与预览的查询都以 `if sessionID != ""` 守卫（标题另有 `if taskName != ""`），
  空 sessionID 不会触发无效查询。无需额外守卫。

## 实现偏差

实施后与本文档原始设计的差异，以下为**已发布行为**，文档以本节为准：

1. **脚本阶段不 emit `running`。** 原设计第 1 步要求登记合成条目并 emit `running`，
   但 `running` 的 task_update 是用户可见通知（浏览器通知、钉钉/飞书的「任务已启动」，
   经 `IsNotifiableEvent` 与前端 `useGlobalEvents` 门控）。脚本阶段的目的是安静跳过，
   而「任务已启动」之后再无结果正是本功能要消除的噪音。因此实现只在
   `runningExecutions` 登记合成条目（保留可取消性与执行历史可见性），**不发任何
   task_update**；脚本行通过 `GetRunningExecutions`（`GET /api/tasks/{id}`）供 UI 标注。
   见 `internal/service/scheduler.go:836-842` 的注释。
2. **渲染体积预算 7000 → 8000。** `TestRenderCommand_SizeBudget`（`internal/api/render_test.go`）
   的 `CommandTask` 预算从 7000 上调至 8000：原基线已是 6885/7000，任何新增的已文档化
   字段都会顶破预算，故放宽而非删除校验。
3. **输出上限改在捕获时施加。** 原设计的 `bytes.Buffer` 只在 `cmd.Run()` 返回后才由
   `truncateScriptOutput` 截断；一个输出 GB 级的脚本（`yes`、`find /`）会在超时触发前
   就把服务端堆撑爆并 OOM。实现改用 `cappedBuffer`，写入时即按 `scriptOutputCap`
   （64 KiB，与 `truncateScriptOutput` 共用同一常量）截断并丢弃多余部分，仍返回完整
   写入长度以免脚本收到 broken pipe 而改变退出码/结论。见
   `internal/service/task_script.go` 的 `cappedBuffer`。
4. **前端轮询以 `runCount` 变化触发历史刷新。** 仅靠 runningCount 增减会漏掉两次 5s
   轮询之间「刚启动即结束」的快速跳过（`git diff --quiet`、`test -f`），其 `skipped` 行
   永不出现。`useTaskHistory.loadRunningStatus` 额外记录上次的 `runCount`（
   `GET /api/tasks/{id}` 已返回），变化即 reload；runningCount 启发式保留用于 AI 阶段
   的完成动画。
5. **长备注段落移除，跳过语义并入 placeholder；超时默认值改为 placeholder。**
   本文档原始「界面说明备注」要求一段独立的长备注解释工作目录 / 不显示为运行中 /
   跳过并注入三点，实施后用户反馈过于啰嗦，予以撤销：
   - `TaskFormPage.vue` 删除超时字段下的 `.form-hint` 元素，i18n 键 `task.form.scriptHint`
     从 `zh.ts` 与 `en.ts` **双双删除**；
   - 跳过语义保留在脚本 textarea 的 placeholder（`task.form.scriptPlaceholder`）中，
     在原句后追加「脚本以 0 退出且无输出时会跳过 AI。」（en 为对应英文）；
   - 超时输入默认留空、以 `placeholder="300"` 呈现后端默认值。原先渲染字面 `0` 会被
     读成「无超时」；`useTaskForm` 的 `scriptTimeout` 初值改为 `''`，`submit()` 里
     `Number(...) || 0` 保证提交的是数字 `0`（后端「用默认值」哨兵）而非空串，与
     `maxRuns` 的空/零处理一致。显式输入（如 `60`）按原样提交；非负整数校验不变。
   其余脚本相关键（`script`、`scriptPlaceholder`、`scriptTimeout`、`scriptTimeoutInvalid`
   及 `task.exec.*`）保留；`task.exec.skippedHint` 是执行历史行的**另一个**键，不受影响。
6. **脚本阶段取消不推进调度（不消耗运行次数）。** 原设计把 `skipped` 与 `cancelled`
   都走 `advanceTaskAfterRun`（记账 + 推进 `next_run_at` + 按 repeat_mode 结算状态）。
   但取消是**中止**一次尚未产生结果的运行，不是「完成一次决策」：对 `once` 任务会直接
   置 `completed` 且 `next_run_at=NULL`，而 AI 从未执行，等于静默吞掉用户唯一的一次运行。
   现改为：取消路径走 `UpdateTaskStats`（只记 `last_run_at`/`run_count`，不动状态与
   `next_run_at`），与 AI 阶段取消路径（ISS-013）一致；`skipped` 仍按原设计推进调度
   ——跳过是「无事可做」的完成决策，`once` 任务此时结算为 `completed` 是合理的。
7. **`script` 更新改为部分语义（指针字段）。** 原实现无条件 `task.Script = req.Script`，
   注释理由是「更新载荷就是整个表单」。但该端点（以及 `/cb-task` 注入给 AI 的文档）
   是部分更新：省略字段即保持不变，与 `name`/`prompt`/`event_types` 一致。无条件赋值
   会让任何只改 prompt 的调用（AI 经 `/cb-task` 是典型）静默清空脚本。现改为
   `*string`：`nil` = 保持不变，显式空串 = 清除；`script_timeout` 原本就是指针，语义对齐。
8. **`cmd.WaitDelay` 触发的 `exec.ErrWaitDelay` 不再算脚本失败。** 脚本若把子进程放后台
   （`foo &`），子进程继承管道并持有它，`cmd.Wait()` 会在 `WaitDelay`（5s）后报
   `ErrWaitDelay`。这是管道收尾的产物，不是脚本失败：脚本本身已正常退出。原实现落入
   `runErr != nil` 分支 → `ScriptFailed` + `ExitCode=-1`，导致 `sleep 20 & exit 0`
   这种「静默成功」被读成失败（本该跳过），并把假的失败信息注入 prompt，反转了本功能的
   契约。现按 `cmd.ProcessState` 的退出码分类。`os/exec` 仅在进程自身 wait 错误为 nil
   时才替换为 `ErrWaitDelay`，故非 0 退出仍以 `*ExitError` 呈现、不受影响。
9. **截断标记改由 `cappedBuffer.truncated` 驱动。** 原 `truncateScriptOutput` 用
   `len(s) >= scriptOutputCap` 反推是否被截断，无法区分「被截断」与「恰好写满上限」，
   对后者会误标 `output truncated`。现由捕获期的 `cappedBuffer.truncated` 记录并透出到
   `ScriptResult.StdoutTruncated`/`StderrTruncated`，`truncateScriptOutput` 接收该标志。
10. **`script_timeout` 溢出饱和。** `time.Duration(v) * time.Second` 对超大秒数（如
    `MaxInt64`）会溢出为负，随后被 `timeout <= 0` 读成「未设置」并静默回落到 300s 默认。
    新增 `scriptTimeoutDuration` 在超过 `math.MaxInt64 / time.Second` 时饱和到 Duration
    上限，使超大超时表现为「实际不限」而非「变成默认值」。
11. **脚本阶段取消后不显示「继续对话」。** `skipped` 与 `cancelled`（脚本阶段）都带空
    `sessionId`，继续对话必然服务端报 `source session not found`。原实现只排除了
    `skipped`，漏了 `cancelled`。现改为按 `sessionId` 是否为空门控（而非枚举 status），
    AI 阶段取消（有 session）仍可继续。
