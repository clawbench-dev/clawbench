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
   执行历史里且可取消；界面需有说明备注解释这一语义

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
   CancelFunc = ctx 的 cancel、StartedAt、TriggerType），emit `running`
2. 执行脚本，工作目录 = projectPath，超时 = ScriptTimeout
   - **exit 0 且 stdout+stderr 皆空** → 写 execution（status `skipped`、session_id `''`）、
     UpdateTaskStats + 重算 next_run_at、移除合成条目、**不 emit 任何事件**、return
   - **ctx 被取消** → 写 execution（status `cancelled`、session_id `''`）、同样记账、
     移除合成条目、emit `cancelled`、return
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
- stdout / stderr 分开捕获（`bytes.Buffer`），不合并
- `SysProcAttr` 设置进程组，`cmd.Cancel` 杀整个进程组，`cmd.WaitDelay = 5s`
  （避免孙进程持有管道导致挂死）
- 用 `context.WithTimeout` 实现超时
- 不使用 `model.RunCommandContext`（`internal/model/exec.go:22`）：它只杀直接子进程、
  不杀进程组，且无 WaitDelay

## Prompt 注入

在 `renderedPrompt`（`scheduler.go:871`）上追加定界块，位于事件上下文之后、
`task.Prompt` 之前。stdout 与 stderr 各限 64 KiB 防撑爆。该变量同时供
`AddChatMessage`（`:885`）与 `ai.ChatRequest.Prompt`（`:912`），因此会话历史可见。

## 前端

- `web/src/components/task/TaskFormPage.vue`：新增 cron-only 的脚本 textarea 与超时
  输入，附说明备注（见下）
- `web/src/composables/useTaskForm.ts`：`form`（:33-46）、`init`（:51-80）、`submit`
  payload（:98-109）加 `script` / `script_timeout`（snake_case）
- `web/src/composables/useTaskHistory.ts`：RunningExecution 加 `phase`；
  `TaskHistoryTab.vue` 的独立 `prevRunningCount`（:210，基于
  `runningExecutions.length`）只数 ai
- `web/src/components/task/TaskHistoryTab.vue`：新增脚本阶段行、`skipped` 徽章与 CSS
- `web/src/components/task/TaskExecDetail.vue`：新增 `skipped` 提示；`showContinueBtn`
  （:187-191）排除 `skipped`（空 sessionId 会让「继续对话」报错，见
  `continue_conversation.go:121-122`）
- i18n `web/src/i18n/locales/zh.ts` 与 `en.ts` 的 `task.exec` 块加 `statusSkipped` 等键；
  `task.form` 块加脚本字段与说明备注文案
- `internal/api/openapi.yaml` 同步（POST `/api/tasks` 请求体 ~3246-3275，PUT ~3313）

### 界面说明备注文案（要点，zh/en 双份）

1. 脚本在调用 AI 之前执行，工作目录为任务的项目路径
2. 脚本运行期间不会在任务列表显示为「运行中」——它不是一次 AI 执行；但会出现在执行
   历史里，可以取消
3. 退出码 0 且无输出时会跳过 AI 且不发送任何通知；有输出或非 0 / 超时会照常调用 AI，
   内容注入 prompt

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

脚本阶段取消时 `emitTaskEvent` 传入空 sessionID，`scheduler.go:728-741` 会去查会话标题
与预览，需确认其容错；若不安全则加守卫。
