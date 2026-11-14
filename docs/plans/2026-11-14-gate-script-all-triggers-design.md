# 门控脚本扩展到邮件与 GitLab/事件触发

日期：2026-11-14
状态：**已实施**（实施偏差见文末「实施记录」）
相关文档：`docs/plans/2026-09-21-task-custom-script-design.md`（门控脚本的由来与语义）、
`docs/spec/features/scheduled-tasks.md`、`docs/spec/features/mail.md`、
`docs/spec/features/forge-integration.md`

## 一句话

现在「门控脚本」只有 cron 任务能用；邮件任务只能靠「发件人/主题/正文包含某个词」这种字符串匹配，
事件任务（GitHub / GitLab）连内容过滤都没有，只能按事件类型订阅。本方案让三种触发方式都能挂
同一段门控脚本：脚本退出 0 才唤起 AI，否则这次触发直接记一条 `skipped`，不建会话、不发通知。

## 现状（已核对代码）

| 触发方式 | 现在的门控能力 | 脚本能不能用 |
|---|---|---|
| cron | 无前置条件 | 能用（`scheduler.go:959` 判断 `!IsEventTriggered() && Script != ""`） |
| email | 发件人 / 主题 / 正文子串（不区分大小写，AND）+ 必须带附件（`mail_task_trigger.go:231`） | 后端其实会跑（email 不是 event），但前端在提交时把脚本清空了（`useTaskForm.ts:160`），表单里也看不到脚本区 |
| event（GitHub / GitLab） | 只有事件类型订阅 + 项目绑定的仓库（`forge_task_trigger.go:291`） | 后端显式跳过（`!task.IsEventTriggered()`），前端也清空 |

GitLab 不需要单独改：`internal/forge` 已经把两个平台归一成同一套 `Item` / `Change`，
事件任务的匹配与执行只有一份代码（`forge_task_trigger.go`），改这里 GitHub 与 GitLab 同时生效。

## 关键决策

### 1. 字符串条件保留，脚本是「再加一道」

不删 `mailFrom` / `mailSubject` / `mailBody` / `mailHasAttachment`，理由是：

- 删掉会让存量邮件任务**静默变成「每封邮件都触发」**——条件字段空了，按现在的语义就是「不限制」。
- 简单条件用输入框两秒就能填，不该逼用户写脚本。

新的判定顺序是「便宜的先做」：账号/文件夹 → 三个子串 + 附件 → 去重占位 → **门控脚本** → AI。
三者是 AND：任何一道不过就不触发。想只靠脚本判断，把三个输入框留空即可。

### 2. 脚本拿到的上下文：短字段走环境变量，长正文走 stdin

脚本还是用 `sh -c` 跑，但多了两样东西：

- **环境变量**：这次触发的结构化字段（见下表）。
- **stdin**：这次触发的主文本（邮件正文；事件是评论正文或条目描述），原始文本，不做 JSON 包装。

为什么不全塞环境变量：Windows 的进程环境块总上限约 32K 字符，一封中文邮件正文 4000 字就吃掉一大半，
再叠加继承来的 PATH 等变量，`os/exec` 会直接报「环境块过长」，表现为脚本启动失败 → 门控永远关闭 →
任务看起来「再也不跑了」。正文走 stdin 没有这个上限，也不受 NUL 字节限制。

为什么不做 JSON：写 shell 判断的人不该为了取一个主题去装 `jq`。环境变量是 shell 原生能力，
`[ "$CLAWBENCH_MAIL_SUBJECT" = "周报" ]` 就能用。

环境变量一律加 `CLAWBENCH_` 前缀，避免和用户自己的变量撞名。取值统一做两件事：
**去掉 NUL 字节**（否则 `os/exec` 直接报错）、**单个值截到 8 KiB**（防御性上限，正常字段远达不到）。

#### 通用（三种触发方式都有）

| 变量 | 含义 |
|---|---|
| `CLAWBENCH_TRIGGER` | `cron` / `email` / `event` |
| `CLAWBENCH_TASK_ID` | 任务 id |
| `CLAWBENCH_TASK_NAME` | 任务名 |
| `CLAWBENCH_PROJECT_PATH` | 脚本的工作目录（也是任务的项目路径） |

#### 邮件

| 变量 | 含义 |
|---|---|
| `CLAWBENCH_MAIL_ACCOUNT` | 邮箱账号显示名 |
| `CLAWBENCH_MAIL_FOLDER` | 文件夹显示名（如「收件箱」） |
| `CLAWBENCH_MAIL_FROM` | 发件人，`名字 <地址>` 形式 |
| `CLAWBENCH_MAIL_TO` | 收件人，逗号分隔 |
| `CLAWBENCH_MAIL_SUBJECT` | 主题 |
| `CLAWBENCH_MAIL_DATE` | 时间，`2006-01-02 15:04` |
| `CLAWBENCH_MAIL_HAS_ATTACHMENT` | `1` / `0` |
| `CLAWBENCH_MAIL_MESSAGE_ID` | 本地缓存里的邮件行 id（排查/去重用） |

stdin：正文（和 prompt 看到的是同一份，已按 4000 字截断并带截断标记）。

#### 事件（GitHub / GitLab 同一套）

| 变量 | 含义 |
|---|---|
| `CLAWBENCH_EVENT_TYPE` | `opened` / `closed` / `merged` / `reopened` / `commented` / `pipeline_done` |
| `CLAWBENCH_REPO` | `owner/repo` |
| `CLAWBENCH_ITEM_TYPE` | `issue` / `pr` / `pipeline` |
| `CLAWBENCH_ITEM_NUMBER` | issue / PR 编号，流水线事件为空 |
| `CLAWBENCH_TITLE` | 标题 |
| `CLAWBENCH_URL` | 链接 |
| `CLAWBENCH_AUTHOR` | 触发这次事件的用户（评论人 / 操作人 / 作者兜底） |
| `CLAWBENCH_STATE` / `CLAWBENCH_PREV_STATE` | 当前状态 / 前一状态 |
| `CLAWBENCH_LABELS` / `CLAWBENCH_ASSIGNEES` | 逗号分隔 |
| `CLAWBENCH_SOURCE_BRANCH` | PR 的源分支 |
| `CLAWBENCH_DRAFT` | `1` / `0` |
| `CLAWBENCH_ACTOR_IS_SELF` | 是不是凭据自己的账号干的（`1` / `0`） |
| `CLAWBENCH_PIPELINE_STATUS` / `_URL` / `_NUMBER` / `_REF` / `_SHA` | 仅流水线事件有值 |

stdin：`commented` 事件是评论正文，其余是条目描述（都已按 4000 字截断）。
两个正文不会同时给——脚本要判断的是「这次事件带来的那段话」，条目描述在评论事件里不是本次变化。

> 注意：这是**门控脚本**的输入，和 prompt 里的 `{{...}}` 模板变量是两套东西。
> prompt 那套（`{{mailSubject}}`、`{{TITLE}}` 等）不变，脚本这套是新增的。

### 3. 脚本什么时候跑

```
cron ：到点 → 脚本 → AI
email：新邮件 → 账号/文件夹 → 子串/附件 → 占位去重 → 脚本 → AI
event：事件到达 → 事件类型/仓库绑定/防自触发/去抖 → 排队 → 脚本 → AI
```

脚本统一在 `executeTask` 里跑（`scheduler.go:948`），三种方式共用同一段代码，
所以「退出码非 0 / 超时 → 记 `skipped`、不建会话、不发通知、可取消、结果落库」这些语义天然一致，
不需要各写一套。

**邮件必须先占位再去重**：占位（`mail_task_triggers`）发生在脚本之前，所以脚本关闸的邮件
不会被下一次同步重新评估一遍。这是有意的——同一封邮件只判断一次，否则每次同步都会跑一遍脚本。

**事件任务在 drain 循环里同步跑脚本**：慢脚本会挡住后面排队的事件，但脚本有超时上限（默认 300 秒），
而一次 AI 回合本来就以分钟计，这个代价可以接受。

## 顺带修一个既有缺陷：邮件任务跑完一次就被暂停

这个缺陷现在就有，只是没有门控脚本时不容易被发现，加上脚本后会被放大（跳过也走同一条记账路径）。

- 邮件任务的 `cron_expr` 是空串（前端 `useTaskForm.ts:150` 明确发送 `''`）。
- `advanceTaskAfterRun`（`scheduler.go:1255`）只对 `IsEventTriggered()` 走「只记运行次数、保持 active」的分支，
  邮件任务落到了 cron 分支。
- cron 分支里 `cron.ParseStandard("")` 必然报 `empty spec string`（已核对 robfig/cron v3 源码），
  于是 `newStatus = "paused"`，任务被写回 `paused`。

结果：**任何邮件任务成功跑完一次之后都会变成「已暂停」，再也不会触发。**
修法：把 `IsEventTriggered()` 改成 `HasEventSource()`（`model/scheduler.go:80` 已同时覆盖 event 与 email）。
`finishScriptOnlyRun`（`scheduler.go:1428`）复用它，所以脚本跳过路径一并修好。

存量已经被暂停的邮件任务不会自动恢复（那是用户的显式状态），需要在更新说明里提一句。

## 后端改动清单

| 位置 | 改动 |
|---|---|
| `internal/service/task_script.go` | `RunTaskScript` 增加输入参数（环境变量 + stdin）。签名改为 `RunTaskScript(ctx, script, workDir, timeout, ScriptInput)`，`ScriptInput{Env map[string]string, Stdin string}`。实现里 `cmd.Env = append(os.Environ(), kv...)`、`cmd.Stdin = strings.NewReader(in.Stdin)`；环境值去 NUL、截断。cron 调用点传空 `ScriptInput` |
| `internal/service/gate_script_env.go`（新增） | 变量注册表：`gateScriptEnvForTask(task, trigger, mailCtx, eventCtx) ScriptInput`，以及每个变量的取值函数。三种触发方式的差异集中在这一个文件，`executeTask` 只调用它 |
| `internal/service/scheduler.go:959` | 条件从 `!task.IsEventTriggered() && task.Script != ""` 改为 `task.Script != ""`；注释同步（不再写「仅 cron」） |
| `internal/service/scheduler.go:1255` | `IsEventTriggered()` → `HasEventSource()`，修上面那个暂停缺陷 |
| `internal/service/mail_task_trigger.go:190` | 正文抓取条件从 `MailBody != ""` 改为 `MailBody != "" \|\| Script != ""`（脚本需要正文）。抓取失败仍按现有语义「这一趟跳过这封，下趟重试」 |
| `internal/handler/scheduler.go:64,294` | 注释里「cron tasks only」改为「三种触发方式都支持」；逻辑不变（创建/更新本来就接受任意模式的 `script`） |
| `internal/model/scheduler.go:43` | `Script` 的注释同步（现在写的是「Only meaningful for cron tasks」） |
| `internal/api/openapi.yaml` | `script` / `script_timeout` 的描述改为「所有触发方式可用」，补一句脚本能读到环境变量与 stdin |
| `internal/api/render_test.go` | 若 `CommandTask` 体积预算被新增描述顶破，按先例（8000）适度放宽，不要删校验 |

没有数据库改动：`script` / `script_timeout` 两列早就在，`task_executions` 的脚本结果列也够用。

## 前端改动清单

| 位置 | 改动 |
|---|---|
| `TaskFormPage.vue:262-326` | 把「门控脚本」整块从 `v-if="triggerMode === 'cron'"` 里搬出来，放到触发配置之后、提示词之前，成为独立 `form-section`，三种模式都可见。文案改成「执行前检查」 |
| `TaskFormPage.vue` 的说明面板 | 现在只列 `{{code}}` / `{{stdout}}` 这类 prompt 变量；新增「脚本能读到的触发信息」，按当前触发方式展示环境变量清单（cron 只显示通用四个） |
| `useTaskForm.ts:128,160,162` | 去掉「event/email 清空脚本」「只对 cron 校验超时」三处分支，三种模式都提交并校验 |
| `web/src/utils/gateScriptVars.ts`（新增） | 环境变量清单的单一来源（变量名 + i18n 说明），表单说明面板与总览卡都从它派生；Go 侧注册表与它用对齐测试钉住（照 `forge_event_context_parity_test.go` 的做法，Go 测试读 TS 文件比对名字与顺序） |
| `TaskListPage.vue:214` / `TaskMailCard.vue` | 配了脚本的任务在总览卡加一个「脚本门控」标记（邮件卡现在只列子串条件，会漏掉脚本这道） |
| i18n `zh.ts` / `en.ts` | `task.form.scriptHint` 改为三种模式通用；新增 `task.form.scriptVars*` 一组键（中英成对） |

## 兼容性与存量数据

- 存量 cron 任务：行为完全不变（脚本照旧、模板变量照旧）。
- 存量邮件/事件任务：`script` 为空 → 行为不变。
- 存量邮件任务被暂停的（上面那个缺陷造成的）：不会自动恢复，需手动点「恢复」。
- 前端曾经会把 email/event 的脚本清空，所以即使有人通过 API 配过，重存一次表单就没了；
  这次修好后不会再清。

## 安全与成本

- **脚本是用户自己写的、以服务端权限运行**，和现在的 cron 门控脚本同一风险面，没有新增权限。
- **邮件正文是外部可控内容**。我们只把它作为环境变量 / stdin 的**值**传进去，不拼进脚本字符串，
  所以不存在注入；但如果用户自己在脚本里 `eval "$CLAWBENCH_MAIL_SUBJECT"`，那是用户自己的选择。
  说明面板里给一句提醒即可。
- **成本**：配了脚本的邮件任务会对每封通过初筛的新邮件拉一次正文（原先只有填了正文条件才拉）。
  这是脚本能看到正文的必要代价，抓取失败按现有语义跳过该封、下趟重试。

## 测试

Go（`internal/service`）：

1. 邮件任务：脚本退出 0 → 建会话、正常跑 AI；退出 1 → 一条 `skipped` 执行记录、`session_id` 为空、
   无 `task_update` 事件、`run_count` +1、**任务仍是 active**（同时是暂停缺陷的回归测试）。
2. 邮件任务：脚本能读到 `CLAWBENCH_MAIL_SUBJECT` 等环境变量，stdin 是正文；环境值含 NUL 时脚本仍能启动。
3. 事件任务：脚本关闸 → `skipped`、不建会话、任务保持 active；开闸 → 正常执行。
4. 邮件触发侧：配了脚本但没填正文条件时，仍会抓正文（用现有假 IMAP 夹具断言 fetch 次数）。
5. `advanceTaskAfterRun`：email 任务跑完保持 active（单测直接调，不依赖脚本）。
6. 变量注册表与前端 `gateScriptVars.ts` 的名字/顺序对齐（Go 读 TS 文件的源码守卫）。

前端（Vitest）：

7. `useTaskForm`：email/event 模式下 `script` / `script_timeout` 原样提交；超时非法值在三种模式下都报错。
8. `TaskFormPage`：email 与 event 模式下渲染脚本区与对应变量清单；cron 模式不显示邮件/事件变量。

E2E：cron 的脚本跳过链路已有 `e2e/specs/task-script-skip.spec.ts`，本次不改它；
邮件/事件需要真实 IMAP / forge 环境，不新增 e2e，靠上面的单测覆盖。

## 实施顺序

1. 先修 `advanceTaskAfterRun` 的 email 分支（独立可验证，和脚本无关）。
2. `RunTaskScript` 加输入通道 + 变量注册表（含单测）。
3. `executeTask` 放开条件、接上注册表；邮件侧补正文抓取条件。
4. 前端表单搬位置、放开提交限制、变量清单与对齐测试。
5. 文档（spec / 用户手册）、OpenAPI、i18n。
6. 全量 `go test ./internal/service/...`、`npx vitest run` 相关文件、`npm run build`。

## 明确不做

- 不做「脚本决定把哪段文字注入 prompt」的额外通道——`{{stdout}}` 已经够了。
- 不做脚本的图形化编辑器 / 预设模板。
- 不给事件任务加「标题包含某词」这类字符串条件：脚本能做的事不必再开一套 UI（用户原话是
  「不仅仅是字符串匹配」，不是「再加一种字符串匹配」）。
- 不动 GitLab adapter：平台差异在 `internal/forge` 内部，上层只有一套事件模型。

## 实施记录（2026-11-14）

按上面的方案落地，与设计稿的差异：

1. **`RunTaskScript` 的输入参数做成可变参数**（`inputs ...ScriptInput`），不是必填形参。
   现有二十多个只跑裸脚本的测试调用点因此一行不用改；生产路径永远只传一个。
2. **事件任务的 stdin 只有一个正文**：`commented` 事件给评论正文，其余给条目描述。
   设计稿里已经这么定，实施时确认了 `RenderEventContext` 本来就分别渲染 `BODY` 与
   `COMMENT_BODY`，脚本侧不重复给两份——要判断的是"这次事件带来的那段话"。
3. **空值不导出**：除三个布尔（`HAS_ATTACHMENT` / `DRAFT` / `ACTOR_IS_SELF`）恒为 `1`/`0`
   外，空字段直接不设环境变量，脚本用 `[ -n "$X" ]` 判断即可，不会看到一串空串。
4. **邮件正文抓取条件**改为 `MailBody != "" || Script != ""`；抓不到仍按现有语义跳过这封、
   下次同步重试（不拿空正文去跑门控，避免脚本基于缺失信息做错误判断）。
5. **既有缺陷一并修复**：`advanceTaskAfterRun` 改用 `HasEventSource()`。邮件任务的
   `cron_expr` 是空串，原先落到 cron 分支后 `ParseStandard("")` 报错 → 任务被写成
   `paused`，即"邮件任务跑完一次就再也不触发"。存量已被暂停的邮件任务不会自动恢复，
   需要手动点「恢复」。
6. **前端位置**：脚本区从「日程」段里搬出来，成为独立的「执行前检查」段，位于触发配置
   与提示词之间。变量清单默认收起（事件任务有 19 项，全展开会把表单拉得很长）。
7. **列表卡**：三种触发方式的任务卡摘要行统一加「脚本门控」标记；邮件总览卡的条件 chip
   也补上这一项，否则卡片会声称任务只按子串条件触发。
8. **对齐测试**：`gate_script_env_parity_test.go` 读 `web/src/utils/gateScriptVars.ts`，
   钉住变量名、i18n 键、顺序与作用域，并校验中英文案都存在——两边各写一份清单，
   漂移会让表单承诺一个后端根本不设的变量。
