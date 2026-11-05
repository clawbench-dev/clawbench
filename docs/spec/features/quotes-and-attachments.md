# 引用与附件体系

把「界面上的一段内容 / 一个对象」带进对话的统一机制。**引用（quote）不是一种新消息类型，它就是 `files[]` 里一条 `kind: 'quote'` 的附件**——和本地文件、目录、外部链接共用同一条通道，因此不需要 schema 迁移，也不需要给聊天加分支。

本文是这个体系的操作手册：数据长什么样、有哪些入口、怎么落到会话、怎么进提示词、以及**新增一个来源时要改哪几处**。

相关：文件附件与系统分享见 [附件与系统分享](attachments-and-share.md)；消息队列见 [聊天流程](../core/chat-flow.md)。

---

## 1. 数据形状

### 1.1 一条附件

后端 `internal/model/chat.go` 的 `FileEntry`，前端 `web/src/utils/fileAttachmentUtils.ts` 的 `FileEntry`（逐字段镜像，改一侧必须改另一侧）。

| `kind` | 含义 | `path` 的含义 | 发送时是否走路径校验 |
|---|---|---|---|
| 空 / `file` | 本地文件或目录 | 真实路径 | **是**（`os.Stat`，越界 403 / 不存在 404） |
| `url` | 外部地址（议题 / PR / CI） | 人类可读标签 | 否（只校验 `http(s)` scheme） |
| `quote` | 引用 | **只是标签**，可以为空 | 否 |

`quote` 的载荷在 `text`（被引用内容）+ `note`（用户的批注）+ 一组定位字段。**标签不是路径**——把它当路径解析会 404 整条消息，这是历史上踩过的坑。

### 1.2 定位字段

定位字段是「跳回去」和「让 AI 找到来源」的唯一线索，因为它们无法从被引用的文本里反推：

| 字段 | 来源 | 跳转到 |
|---|---|---|
| `commitSha` | Git 提交 / CI 运行 | Git 历史对应提交 |
| `taskId` + `executionId` | 定时任务 / 一次执行 | 任务详情（可带执行） |
| `meetingToken` | 会议记录 | 会议详情 |
| `mailId` | 邮件 | 邮件阅读器 |
| `sessionId` + `messageId` | 聊天消息 | 会话并滚到该消息 |
| `url` | 议题 / PR / CI | 外部浏览器 |
| `filePath` + 行号 | 文件 / 代码片段 | 文件预览 |

**加一个定位字段要同时改 8 处**，见 §7 的清单。漏任何一处，症状都是「卡片正常、跳转静默无反应」。

### 1.3 前端的三份表示

| 表示 | 位置 | 用途 |
|---|---|---|
| `QuoteData` | `useChatContext.ts` | 采集端产出的裸载荷（无 id / note） |
| `StagedQuote` | `useChatContext.ts` | 已暂存进输入框（带 id + note） |
| `QuoteItem` | `utils/quoteItem.ts` | **归一化形状**，卡片 / 抽屉 / 拖拽都吃它 |

`QuoteData` 由 `QuoteItem` 派生（`Omit<...>`），不再手写字段。三个转换函数是唯一的进出口：

- `fromStagedQuote` —— 输入框 → `QuoteItem`
- `fromFileEntry` —— 已发送消息的附件 → `QuoteItem`
- `toFileEntry` —— `QuoteItem` → 发送载荷
- `materializeQuotes` —— 暂存列表 → 发送载荷（**直发和排队两条路共用**）

---

## 2. 全链路

```
采集（选区 / 按钮 / 拖拽 / 会话行）
  → 浮条 或 直接投递
  → requestTarget（选目标会话 / 直投当前）
  → 暂存卡片（addStagedQuote / stageQuoteIntoDraft）
  → 发送（materializeQuotes → files[]）
  → 落库（chat_history.files / queued_messages.files）
  → 提示词注入（validatedQuoteEntry → ClassifyAttachments → RenderQuoteBlock）
  → 展示（QuoteCard / QuoteDetailDrawer）
```

### 2.1 四种投递方式

| 方式 | 触发 | 落点 |
|---|---|---|
| 选区浮条 | 选中文字后浮条「引用 / 评论」 | 当前会话（无会话或失败回退选择器） |
| Composer 浮条 | 入口按钮（文件头、议题详情、会议详情、提交行、任务行、邮件） | 同上，可先写批注 |
| 直接投递 | 消息引用按钮、代码链接预览 | `requestTarget(..., { deliverTo: 'active' })` |
| 拖拽 | 提交 / 任务 / 议题 / PR / CI / 会议 / 邮件 | 聊天列（当前会话）或**会话行**（指定会话） |

**去向按意图决定，不按可见性决定**：评论类入口默认直投当前会话；要换会话用浮条上的长按（弹选择器），或把卡片拖到会话列表的某一行。

---

## 3. 采集入口清单

新增入口前先看这里——大多数场景已经有现成的入口形状可以照抄。

### 3.1 选区类（`useQuoteQuestion` 的全局 selection 处理）

| 界面 | 实现 |
|---|---|
| 聊天消息 | `useQuoteQuestion.deriveQuoteData` |
| Markdown 预览 | `MarkdownPreview.vue` → `showBar` |
| 代码预览（CodeMirror） | `CodeMirrorViewer.vue` → `showBar`（自带 anchor） |
| HTML / OpenAPI 预览（iframe） | `utils/iframeSelectionQuote.ts` 的 `makeIframeQuoteShowHandler` |
| 终端 | `TerminalPanelContent.vue`（`sourceKind: 'terminal'`） |
| 会议中的转写 / 会议聊天 | 白名单里的 `.transcript` / `.live-chat`，身份来自会议面板的 `data-quote-*` |
| 议题 / PR 详情、CI 详情、Git diff、任务执行详情、会议正文 | 标 `data-quote-*` 属性，由 `quoteQuestionUtils.getQuoteSource` 统一读取 |

`data-quote-*` 是给「一段可选中区域」标注身份的方式：

```html
<div
  data-quote-source="标签"        <!-- 卡片上显示的名字 -->
  data-quote-language="issue"     <!-- 类型标记，见 §4 -->
  data-quote-url="https://…"
  data-quote-commit="abc1234"
  data-quote-task-id="12"
  data-quote-session-id="sess-1"
  data-quote-message-id="42"
  data-quote-execution-id="exec-7"
  data-quote-meeting-token="tok-9"
>
```

### 3.2 按钮类（composer）

| 界面 | 实现 |
|---|---|
| 文件预览头 | `FileHeader.vue` → `App.vue` `handleFileQuoteInChat` |
| 议题 / PR 详情头 | `App.vue` `handleForgeQuote` |
| 会议记录详情头 | `App.vue` `handleMeetingQuoteButton` |
| 会议中（stage） | `MeetingStage.vue` `quoteLiveMeeting`（正文取当前转写） |
| Git 提交行 | `GitCommitList.vue` `quoteCommit` |
| 任务行 | `TaskListPage.vue` `quoteTask` |
| 邮件阅读器 | `MailReader.vue` `quoteToChat` |
| 聊天消息整条 | `ChatMessageItem.vue` → `ChatPanelContent.handleQuoteMessage` |
| 代码链接预览 | `CodeLinkPreview.vue` `handleQuoteToChat`（直投） |

构造载荷一律走 `utils/quoteItem.ts` 的三个构造器：

- `buildMessageQuote(text, session, messageId)` —— 消息
- `quoteItemFromTarget({ filePath, url, label, language, text, startLine, endLine, sourceKind, mailId })` —— 文件 / 议题 / 提交 / 任务 / 代码片段 / 邮件
- `quoteDrag.ts` 的 `commitDragPayload` / `taskDragPayload` / `forgeItemDragPayload` / `pipelineDragPayload` / `meetingDragPayload` / `forgeUnreadDragPayload` —— 拖拽

**不要手拼载荷。** 历史上手拼导致了「按钮路径漏 sessionId，引用打不开」和「代码预览自己造 id」两处漂移，都有守卫测试钉着。

### 3.3 拖拽类

两种私有 MIME，一个落点：

| MIME | 产物 | 源 |
|---|---|---|
| `application/x-clawbench-attach` | 文件 / 目录附件 | 文件管理器行、面包屑、文件名、Markdown 图片、mermaid 代码块、图片预览、会议资源 |
| `application/x-clawbench-quote` | 引用卡片 | 提交、任务、议题 / PR、CI、会议记录、**会议中**、邮件、**聊天消息的引用按钮** |

落点：
- **聊天列**（`App.vue` `onChatColDrop`）—— 落到当前会话，提示文案按载荷类型区分（「松开添加到会话」/「松开引用到会话」）
- **会话列表的行**（`SessionList.vue`）—— 落到该会话的草稿，行会高亮

约束：
- **所有拖拽源都按宽屏门控**（`:draggable="isWideScreen"`）。窄屏下列表栏和聊天栏互斥显示，拖拽源和落点永远不同屏，允许拖拽只会得到「能拖但没反应」。
- 消息气泡本身**不能**设 `draggable`——那会让文字选不中，而选中文字正是片段引用的入口。拖的是引用按钮。
- 拖拽产物**没有批注**（拖拽本身已经表达「放进聊天」，再弹输入框是多余步骤）。
- **只带身份的载荷**（会议记录、邮件）在 `dragstart` 里无法内联内容（不能 await），由落点处理器补一次请求再落卡片；这类载荷必须同时在 `App.vue` 的 `onChatColDrop` 里有分支，否则拖了等于没拖。
- **正在进行的会议是个例外**：它还没有产物可拉，所以转写文本在 `dragstart` 时就被塞进载荷（`text` 非空），落点直接落卡片、不再请求。
- 拖拽预览图是共享的（`buildAttachDragImage`），`dragend` 必须调 `cleanupDragGhost()`。

### 3.4 附件拖拽的构造器

统一用 `utils/attachDrag.ts` 的 `startAttachDrag(e, path, opts)`：

```ts
startAttachDrag(e, '/proj/a.md', {
  name: 'a.md',
  isDir: false,
  startLine: 10, endLine: 24,        // 行范围（mermaid 代码块）
  entries: [{ path, isDir }],        // 多选
  effectAllowed: 'move',             // 文件管理器浏览行可移动
})
```

它负责写载荷、设光标、装预览图。**不要再自己 `setData` + `setDragImage`**。

---

## 4. 类型识别（卡片上的图标和标签）

`resolveQuoteType` 按三级判定，顺序不能乱：

1. **显式类型标记**：`language` 是 `pipeline` / `task-exec` / `task` / `diff` / `pr` / `issue` / `meeting` / `mail` 之一（见 `TYPE_MARKER_LANGUAGE`）。这是来源方主动声明的，优先于任何推断。
2. **定位字段，从具体到宽泛**：`meetingToken` → `mailId` → `executionId` → `taskId` → `commitSha`（带 url 是 CI，不带是提交）→ `url`。
3. **`sourceKind`**：`terminal` / `message`，最后落到 `filePath` 或 `selection`。

图标与文案在 `utils/quoteSourceMeta.ts` 成对定义（`QUOTE_TYPE_ICON` + `QUOTE_TYPE_LABEL_KEY`），卡片和抽屉共用同一张表——**新增类型必须同时给图标和文案**，否则两处会不一致。

---

## 5. 展示

| 位置 | 组件 | 说明 |
|---|---|---|
| 输入框暂存 | `QuoteCard.vue` | `removable`，点击开抽屉 |
| 已发送消息 | `FileAttachmentList.vue` → `QuoteCard` | 点击开抽屉（只读） |
| 排队条 / 排队编辑 | `QueuedMessageBar.vue` / `QueuedMessageEditSheet.vue` | 同一张卡；编辑框额外 `removable` |
| 分享页 | `SessionShareView.vue` → `ChatMessageItem` → `FileAttachmentList` | 自动生效 |
| 详情抽屉 | `QuoteDetailDrawer.vue` + `useQuoteDetail`（单例，挂在 `ChatPanelContent`） | 暂存可改批注，已发送只读 |

**卡片不要自己画。** `QuoteCard` 的配色/描边在全局 `web/css/components.css`（`.chat-file-attachment.attachment-quote`），几何尺寸由各宿主的 `.chat-file-attachment` 提供；没有宿主规则的地方由同一文件里 `:where(...)` 的零特异性兜底填上。新增宿主时**什么都不用写**。

---

## 6. 提示词注入

`ApplyAttachmentPrefixes`（`internal/model/attachment_prompt.go`）把引用**追加**在用户原话之后：

````
用户的原话

[Quote 1/2] src/a.go:10-20
[Note] 为什么这样写？
```go:src/a.go:10-20
被引用的内容
```

[Quote 2/2] 产品评审会 · 纪要 (meeting: tok-9)
[Note] …
````

规则：

- 每条引用是 `[Quote i/n] 标签 (定位键)` 一行，**必须单行**——会话标题生成靠「剥首行」的规则，多一行会被当成用户说的话。
- 有批注才有 `[Note]`。
- **有内容才有围栏**：整文件 / 整议题引用不带内容（`text` 为空），只给标签和地址，让 AI 自己去读；渲染空围栏会把「引用这个文件」曲解成「这是它的空内容」。
- 定位键是 `commit:` / `task:` / `session:` / `message:` / `execution:` / `meeting:` / `mail:`。新增定位字段必须在这里也加一条，否则 AI 拿不到机器可读的句柄。
- **引用不计入「有附件」**（`HasAttachmentEntries` 跳过 quote）：媒体处理规则不该因为一条引用被注入。

批注在发送后仍可修改：`PATCH` 按 entry 的 `id` 定位（不是数组下标，下标会随增删漂移）。

---

## 7. 新增一个引用来源：检查清单

假设要给「XXX 面板」加「引用到对话」。按顺序做完这些，缺一条就会出现特定的坏味道。

### 7.1 采集端

1. 构造载荷，优先复用 `quoteItemFromTarget` / `buildMessageQuote` / `*DragPayload`；不要手拼。
2. `language` 给一个类型标记（如 `'mail'`），`sourceKind` 给来源类别；二者至少一个能让 `resolveQuoteType` 认出它。
3. 入口走 `quoteQuestion.openComposer({ quote, label })`（要批注）或 `requestTarget(..., { deliverTo: 'active' })`（一键）。
4. 拖拽源记得 `:draggable="isWideScreen"` + `@dragend="cleanupDragGhost()"`。

### 7.2 新增定位字段时（以 `mailId` 为例，共 8 处）

| # | 位置 | 作用 |
|---|---|---|
| 1 | `internal/model/chat.go` `FileEntry` | 加字段 + JSON tag |
| 2 | `internal/handler/chat.go` `validatedQuoteEntry` | 逐字段重建时**必须抄进去**（漏了就是静默丢弃） |
| 3 | `internal/model/attachment_prompt.go` `QuotePrompt` + `ClassifyAttachments` | 分类时带上 |
| 4 | 同文件 `quoteSourceKeys` | 渲染成 `mail: 77` |
| 5 | `web/src/utils/fileAttachmentUtils.ts` `FileEntry` | 前端镜像 |
| 6 | `web/src/utils/quoteItem.ts` `QuoteItem` / `StagedQuoteLike` / `fromStagedQuote` / `fromFileEntry` / `toFileEntry` | 三处转换都要带 |
| 7 | 同文件 `resolveQuoteType` + `canJumpToSource` | 类型识别 + 是否显示跳转按钮 |
| 8 | `ChatPanelContent.vue` `jumpToQuoteSource` | 实际跳转分支 |

另外：`quoteSourceMeta.ts` 加图标与文案（新类型时）、`QuoteDisplayType` 加成员、i18n 加 `quoteBar.typeXxx`。

### 7.3 跳转

- 跨面板的跳转用 window 事件（`clawbench-open-task` / `clawbench-open-meeting` / `clawbench-open-mail` / `navigate-to-commit`），**App 负责切页签、面板负责切视图**，两边各听同一个事件。
- 目标记录可能是跨项目的（会议 / 任务），App 要先切项目再重发事件。
- 全局数据（邮件）不需要切项目。

### 7.4 测试（至少四条）

1. `utils/__tests__/quoteItem.test.ts` —— 载荷往返（`toFileEntry` → `fromFileEntry`）与类型识别。
2. `internal/handler/chat_test.go` —— 用**裸 map** 发请求，断言定位字段穿透 decode → validate → persist（用结构体会只证明结构体有字段，证明不了线上 JSON 名对得上）。
3. `internal/model/attachment_prompt_test.go` —— 断言提示词里有 `(mail: 77)` 这样的定位键。
4. `components/chat/__tests__/quoteJumpWiring.test.ts` —— `canJumpToSource` 接受的每个定位字段，`jumpToQuoteSource` 都必须有分支（否则按钮出现但点了没反应）。

---

## 8. 已知取舍

- **拖拽不产生批注**：拖拽已经表达意图，再弹输入框是摩擦；需要批注就点开卡片补。
- **窄屏没有拖拽**：布局决定拖拽源和落点不可能同屏。手机上的等价入口是「引用」按钮 + 会话选择器。
- **引用文本有上限**：会议与邮件正文在采集时裁剪（数千字符），避免一条引用吃掉整个上下文预算。
- **邮件附件是重上传**：聊天附件必须位于项目目录内，而邮件附件在数据目录下，所以「添加到对话」是把字节重新上传进项目 `uploads/`，不是引用原路径。
- **分享页的引用只读**：卡片正常渲染，但点击不跳转（匿名读者没有那些面板）。
