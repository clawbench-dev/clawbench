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

### 1.2 定位键（`locators`）

定位键是「跳回去」和「让 AI 找到来源」的唯一线索，因为它们无法从被引用的文本里反推。

**它是一个开放的键值袋 `locators: Record<string, string>`，不是一堆平铺字段。** 平铺的写法下，加一种来源要改约 14 处（Go 结构体、Go 分类器、Go 渲染器、Go 校验器、前端镜像、四个 TS 转换器、类型判定、跳转分支……），每一处都是「漏了就静默丢弃」——`mailId` 和 `sourceDir` 就是这么丢的：卡片照常显示、消息照常发出，只有跳转按钮消失了。

| 键 | 来源 | 跳转到 |
|---|---|---|
| `commit` | Git 提交 / CI 运行 | Git 历史对应提交 |
| `task` | 定时任务 | 任务详情 |
| `execution` | 一次任务执行 | 任务执行详情 |
| `session` | 聊天会话 | 该会话 |
| `message` | 聊天消息 | 会话内该条消息 |
| `meeting` | 会议记录 | 会议详情 |
| `mail` | 邮件 | 邮件阅读器 |
| `dir` | 会议（产物目录） | 不跳转，**给 AI 用**：项目相对路径，让它自己去读转写/纪要/录音 |

`url` 和 `filePath`（+ 行号）不在袋里，它们本来就是 `FileEntry` 的顶层字段，跳转时排在定位键之后。

**值一律是字符串**（`task: "12"`）：提示词是文本，数字与字符串在渲染上没有区别，统一成字符串省掉一层转换。

**加一种新来源不需要改后端任何代码**：客户端往袋里塞一个新键，后端原样透传、原样渲染进提示词（未知键排在已知键之后、按字典序，保证同一引用每次渲染的文本完全一致）。只有当你希望它能**跳转**时，才需要在 `utils/quoteJump.ts` 注册一个跳转器（见 §7）。

---

### 1.2.1 三张表，各管一件事

改这套东西时最容易踩的坑是「以为改了类型就够了」。实际有四张表，各自独立：

| 表 | 位置 | 管什么 | 加来源时要动吗 |
|---|---|---|---|
| 定位键袋 | `locators`（前后端同形） | 存什么 | 否，塞键即可 |
| 类型识别 | `resolveQuoteType`（`utils/quoteItem.ts`） | 卡片上的图标/标签 | 是（新类型时） |
| 跳转 | `utils/quoteJump.ts` 的注册表 | 点「跳转来源」去哪 | 是（要能跳时） |
| 拖拽补内容 | `utils/quoteHydrate.ts` 的注册表 | 落点要不要先请求内容 | 是（内容不在载荷里时） |

后两张都是**注册表**，不是 `if` 链。它们以前各是一条手写的 `if` 链，其中拖拽补内容那条还被抄了两份（聊天列 + 会话行），只有一份写在文档里——于是「拖到聊天列能用、拖到会话行没反应」这种缺陷反复出现。

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
| 邮件正文（iframe） | 同上，但经 `identity` 额外给出 `sourceKind: 'mail'` + `locators.mail`（正文不是文件，`filePath` 只是「主题 · 发件人」标签） |
| 终端 | `TerminalPanelContent.vue`（`sourceKind: 'terminal'`） |
| 会议中的转写 / 会议聊天 | 白名单里的 `.transcript` / `.live-chat`，身份来自会议面板的 `data-quote-*` |
| 议题 / PR 详情、CI 详情、Git diff、任务执行详情、会议正文 | 标 `data-quote-*` 属性，由 `quoteQuestionUtils.getQuoteSource` 统一读取 |
| **任意插件面板** | 容器标 `data-quotable`（+ `data-quote-*` 给身份），见下 |

`data-quote-*` 是给「一段可选中区域」标注身份的方式：

```html
<div
  data-quotable                    <!-- 让这块区域可被划词引用 -->
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

`data-quotable` 是**开关**，`data-quote-*` 是**身份**，两件事：

- 白名单（`.markdown-body` / `.raw-content-pre` / `.transcript` …）认的是内置预览的类名；新面板的容器不在里面，**只标 `data-quote-*` 不会弹条**，必须再加 `data-quotable`。属性存在即开启，但 `data-quotable="false"` 例外——Vue 的 `:data-quotable="flag"` 关掉时会渲染成 `"false"` 而不是删属性，所以这个值按关闭处理。
- 只标 `data-quotable` 也能引用，但卡片没有标签、跳不回来源；实际使用时应同时给 `data-quote-source` 和需要的定位键。


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
- **只带身份的载荷**（会议记录、邮件）在 `dragstart` 里无法内联内容（不能 await），由落点补一次请求再落卡片。**补内容的逻辑在 `utils/quoteHydrate.ts` 的注册表里，两个落点都调它**（`hydrateDraggedQuote`）——以前这里是两条手写 `if` 链，只有 App.vue 那条写在文档里，于是邮件拖到会话行上静默无反应。加一种需要补内容的来源，只需在 `utils/quoteHydrators.ts` 注册一次。
- **正在进行的会议是个例外**：它还没有产物可拉，所以转写文本在 `dragstart` 时就被塞进载荷（`text` 非空），补内容那一层看到 `text` 非空就直接放行、不再请求。
- 拖拽预览图是共享的（`buildAttachDragImage`），`dragend` 必须调 `cleanupDragGhost()`。

### 3.4 拖拽源与附件构造器：优先用 `v-chat-drag`

新面板不要手写 `draggable` + `dragstart` + `dragend` 三件套。指令把宽屏门控、载荷、拖影、清理都包了：

```html
<!-- 引用卡片：payload 可以是对象，也可以是 getter（v-for 行读当前值） -->
<div v-chat-drag="{ kind: 'quote', payload: forgeItemDragPayload(item) }">

<!-- 文件附件：opts 与 startAttachDrag 相同 -->
<div v-chat-drag="{ kind: 'attach', path: file.path, name: file.name }">
```

- 窄屏默认自动禁用；确实有同屏落点时才加 `allowNarrow: true`。
- 指令**只能放在行/卡片上**，不能放在正文容器上（`draggable` 会让文字选不中）。
- 仍然需要 `data-quotable` 的面板，把两者放在不同层级：外层卡片拖拽，内层正文划词。

底层构造器仍然可用（`utils/attachDrag.ts` 的 `startAttachDrag`），适用于指令表达不了的场景：

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

### 3.5 插件面板：`useChatBridge`

投递这一段以前是「知道哪个模块管哪一步」才能用：暂存在 `useChatContext`、浮条在 `useQuoteQuestion`、选目标在 `useConversationTarget`，而「引用后跳到聊天」只有 App.vue 能切页签——知识库面板为此单独造了一个模块级 ref 让 App 去捡。现在这三件事收进一个门面：

```ts
import { useChatBridge } from '@/composables/useChatBridge'

// setup() 里取一次
const chat = useChatBridge()

chat.quote(payload)                          // 直接进当前会话（含跳转 + toast）
chat.quote(payload, { note: '为什么？' })     // 带批注
chat.comment(payload, { anchorEl: btn })     // 开浮条，让用户先写批注
chat.attach('src/a.ts')                      // 附件：弹会话选择器（文件管理器行为）
chat.attach(['a.ts', 'b.ts'], { to: 'active' }) // 直接进当前会话
```

- `quote` 要求载荷**已经完整**；内容要现拉的来源（会议、邮件）用 `comment` 或拖拽（落点会 `hydrateDraggedQuote`）。
- `comment` 只是打开浮条，用户提交才进草稿——关掉浮条不会污染输入框。
- 门面**没有**挂到 `DockPanelHost` 上：宿主接口是给「面板拿不到的东西」用的，而这些都是纯前端状态，直接 import 更短，也不会让每个插件都看见别人的能力。

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
- 定位键是 `commit:` / `task:` / `session:` / `message:` / `execution:` / `meeting:` / `mail:` / `dir:`，外加客户端塞进来的任意新键。**新键不需要改这里**：渲染器遍历整袋，未知键排在已知键之后、按字典序（顺序必须稳定，否则同一引用两次渲染的文本不同，提示词测试与会话标题推导都会变成随机失败）。
- `dir:` 是唯一不用于跳转的定位键：它给 AI 一个**项目相对**目录（会议引用就是 `.clawbench/notes/<场次>`），让它可以打开转写、纪要、录音这些没有内联进提示词的材料。用相对路径是因为智能体的工作目录就是项目根，绝对路径对它没有意义。
- **引用不计入「有附件」**（`HasAttachmentEntries` 跳过 quote）：媒体处理规则不该因为一条引用被注入。
- **会话自动命名也读引用**（`service.userMessageTitleText`）：引用文本 + 标签跟着用户原话一起进标题模型。一句「修了」+ 引用的消息，主题全在引用里；只喂正文会让模型起出「简短对话片段」这类空标题。本地标题（不调模型的那层）仍只用正文，正文非空时不会退到引用。

批注在发送后仍可修改：`PATCH` 按 entry 的 `id` 定位（不是数组下标，下标会随增删漂移）。

---

## 7. 新增一个引用来源：检查清单

假设要给「XXX 面板」加「引用到对话」。这套体系的设计目标就是**让这个清单尽量短**——只有你真要那项能力时才需要动对应的地方。

### 7.1 采集端（每次都要）

1. 构造载荷，优先复用 `quoteItemFromTarget` / `buildMessageQuote` / `*DragPayload`；不要手拼。
2. `language` 给一个类型标记（如 `'mail'`），`sourceKind` 给来源类别；二者至少一个能让 `resolveQuoteType` 认出它。
3. 投递走 `useChatBridge()`：`quote(payload)`（一键）/ `comment(payload, { anchorEl })`（要批注）/ `attach(path, { to })`（附件）。
4. 拖拽源用 `v-chat-drag`（宽屏门控、拖影、清理都已内置），不要手写三件套。
5. 面板里的文字要能被划词引用，就给容器加 `data-quotable`；身份仍用 `data-quote-*`。
6. 要能跳回源头，就往 `locators` 里塞一个键——**前后端都不用改**。

### 7.2 加定位键：零改动（除非要跳转）

往 `locators` 里塞一个键，例如 `locators: { deploy: 'd-42' }`：

| 环节 | 需要改吗 | 为什么 |
|---|---|---|
| `internal/model/chat.go` | 否 | `Locators map[string]string`，开放袋 |
| `internal/handler/chat.go` `validatedQuoteEntry` | 否 | 整结构体拷贝，只覆盖 `Kind` |
| `internal/model/attachment_prompt.go` | 否 | 分类与渲染都遍历袋子；未知键排在已知键之后、按字典序 |
| `web/src/utils/fileAttachmentUtils.ts` | 否 | 袋子整体复制 |
| `web/src/utils/quoteItem.ts` 的转换函数 | 否 | 同上 |
| **跳转** | **是** | 在 `utils/quoteJump.ts` 注册一个跳转器 |
| **拖拽补内容** | **是**（内容不在载荷里时） | 在 `utils/quoteHydrators.ts` 注册一个补内容器 |
| **类型图标/标签** | **是**（新类型时） | `quoteSourceMeta.ts` + `QuoteDisplayType` + i18n `quoteBar.typeXxx` |

注意「渲染顺序」：已知键按 `locatorKeyOrder` 的固定顺序（commit → task → session → message → execution → meeting → mail → dir），未知键排在后面按字典序。**顺序必须稳定**——同一引用渲染两次必须得到逐字节相同的文本，否则提示词测试和会话标题推导都会变成随机失败。

### 7.3 注册跳转（`utils/quoteJump.ts`）

```ts
register('deploy', '部署详情', (q, ctx) => {
  ctx.dispatch('clawbench-open-deploy', { id: locatorOf(q, 'deploy') })
  return true   // true = 已处理，链条停止
})
```

- 顺序即优先级，**最具体的在前**（`commit` 排在文件之前：选了某个提交里的 hunk，用户指的是那个提交）。
- 返回 `true` 表示「我处理了（即使我拒绝了——比如功能被关掉）」，链条不会继续往下掉到「把标签当路径打开」。
- `canJumpQuoteToSource`（按钮显示与否）与 `jumpToQuoteSource`（去哪）**读的是同一张表**，所以「按钮出现但点了没反应」不再可表达。以前这是两条独立的 `if` 链，靠一个比对两份文件文本的测试维持同步。
- 跨面板跳转用 window 事件（`clawbench-open-task` / `-meeting` / `-mail` / `navigate-to-commit`），**App 负责切页签、面板负责切视图**。目标记录可能跨项目（会议 / 任务），App 要先切项目再重发事件；全局数据（邮件）不需要切项目。

### 7.4 注册拖拽补内容（`utils/quoteHydrators.ts`）

```ts
registerQuoteHydrator('deploy', async (id, payload) => {
  if (payload.text) return payload        // 内容已经在载荷里了
  const detail = await fetchDeploy(id)
  return detail ? buildDeployQuote(detail) : null   // null = 这次没内容可引用
})
```

**两个落点（聊天列 / 会话行）都调 `hydrateDraggedQuote`，不要再在落点里写 `if`。** 这一处曾经是两份手写分支，只有一份写在文档里，于是「拖到聊天列能用、拖到会话行没反应」反复出现。

### 7.5 测试（至少五条）

1. `utils/__tests__/quoteItem.test.ts` —— 载荷往返（`toFileEntry` → `fromFileEntry`）与类型识别。
2. `internal/handler/chat_test.go` —— 用**裸 map** 发请求，断言定位键穿透 decode → validate → persist（用结构体会只证明结构体有字段，证明不了线上 JSON 名对得上）。断言里带一个**本版本不认识的键**，钉住「开放袋」这件事。
3. `internal/model/attachment_prompt_test.go` —— 断言提示词里有 `(mail: 77)` 这样的定位键，且未知键也能渲染、顺序稳定。
4. `components/chat/__tests__/quoteJumpWiring.test.ts` —— **行为测试**：驱动真实的跳转注册表，用假 context 断言每种定位键产生的副作用；同时断言「按钮显示」与「真能跳」对同一批输入一致。
5. `utils/__tests__/fileEntryParity.test.ts` —— 从 Go 的 `FileEntry` 结构体**读出字段清单**，逐个断言 `normalizeFileEntry` 保留（不需要人工维护，加字段自动生效）。
6. `utils/__tests__/quoteHydrate.test.ts` —— 补内容注册表的契约：无注册者原样返回、`null` 丢拖拽、多个定位键只跑第一个、空值不算命中。

---

## 8. 已知取舍

- **拖拽不产生批注**：拖拽已经表达意图，再弹输入框是摩擦；需要批注就点开卡片补。
- **窄屏没有拖拽**：布局决定拖拽源和落点不可能同屏。手机上的等价入口是「引用」按钮 + 会话选择器。
- **引用文本有上限**：会议与邮件正文在采集时裁剪（数千字符），避免一条引用吃掉整个上下文预算。
- **邮件附件是重上传**：聊天附件必须位于项目目录内，而邮件附件在数据目录下，所以「添加到对话」是把字节重新上传进项目 `uploads/`，不是引用原路径。
- **分享页的引用只读**：卡片正常渲染，但点击不跳转（匿名读者没有那些面板）。
