# `/btw` — 由 ClawBench 接管的旁路问答

日期：2026-09-29
状态：设计已确认，待实现

## 目标

新增内置斜杠命令 `/btw <问题>`。用户在当前会话里问一个「顺便问一下」的问题，
由 **ClawBench 自己**用 AI 摘要模型回答——不经过当前会话的 CLI agent、不占用会话、
不写入历史。答案在一个抽屉里用**与聊天完全一致的消息渲染管线**展示。

## 为什么这样设计

当前会话的 agent 可能正在长任务里，或者用户只是想要一个快速的旁路问答，不想让它
打断主对话、污染上下文、在会话里留下两条消息。摘要模型已经是配置好的第二个模型，
把它当"第二个大脑"来问最自然。

复用会话分叉的压缩逻辑是关键：分叉已经把「把长会话塞进一个提示」这件事解决了
（L0 工具输入裁剪 + L1 优先级尾部窗口 + 省略提示），`/btw` 直接复用同一条路径，
不新造一套截断逻辑。

## 已确认的决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 抽屉内容 | **仅助手回答** | 问题在输入框里刚打过，不需要重复；抽屉是一段"补充说明" |
| 持久化 | **完全临时** | 不写 `chat_history`，不写 `chat_sessions`；关掉即消失 |
| 呈现节奏 | **等待完成后一次性弹出** | 先显示输入框加载态，拿到完整答案再弹抽屉、一次性渲染 |
| 上下文范围 | **整个当前会话，走分叉压缩** | 复用 `BuildForkContextWithOptions` |
| 预算 | **固定常量，100K 上下文的 80%** | 见下 |
| 回复长度 | **统一 8192 max_tokens** | 见下 |

## 数据流

```
用户输入 "/btw 为什么 X？"
  → ChatInputBar 拦截（不 emit('send')、不进队列、不进 CLI）
  → ChatPanelContent.handleBtw(question)
  → POST /api/ai/session/btw  { sessionId, question }
       ├─ service.BuildForkContextWithOptions(sessionId, 预算=btwContextBudgetChars)
       │     复用会话分叉的压缩（含省略提示）
       ├─ handler.GetSummarizer()   ← AI 摘要模型
       └─ 一次性问答：system=btwSystemPrompt, stable=压缩历史, rolling=问题
  → 返回 { ok: true, answer }（不落库）
  → 弹出抽屉，<ChatMessageItem> 渲染一条合成助手消息（streaming:false）
```

## 后端

### 新端点 `POST /api/ai/session/btw`

请求体 `{ sessionId: string, question: string }`。
- `sessionId` 缺省回退到 cookie/query（与 `ServeForkSession` 同一套解析）。
- `question` 为空 → 400。
- 摘要模型未配置（`GetSummarizer()` 为 nil，或底层不支持一次性问答）→ 400/503。
- 成功 → `{ ok: true, answer: string }`。

不落库、不发 WS 事件、不改任何会话状态。

### 一次性问答原语

现有 `recommendPassProvider.DoRecommendPass(ctx, system, stable, rolling)` 已具备
「system + 稳定前缀 + 滚动尾部」的形状，正是需要的。唯一缺口是 `max_tokens` 在
openai.go/anthropic.go 里硬编码 1024。

**实现方式（最小、零既有测试改动）**：不修改 `DoRecommendPass` 的签名（它有 20+ 处
测试调用），而是新增并行的 `askPassProvider.DoAskPass(ctx, system, stable, rolling, maxTokens)`，
由 `DoRecommendPass` 委托给它并传 `recommendMaxTokens`(1024)。两个后端共用同一份 HTTP
请求实现，避免复制。

新增导出的便捷函数 `summarize.AskAboutContext(ctx, s Summarizer, system, context, question string, maxTokens int) (string, error)`，
内部类型断言 `askPassProvider` 后调用；`maxTokens <= 0` 回退到 1024。不支持时返回
哨兵错误 `summarize.ErrOneShotUnsupported`。

**注**：`ErrOneShotUnsupported` 经核实**不可达** —— `NewAISummarizer` 只返回 `nil`
或真实的 LLM 摘要器，两者都实现了 `DoAskPass`。因此 service 层不做映射，handler
层也没有 `BtwSummarizerUnsupported` 分支（原本计划有，实现时按证据删除）。

Anthropic 的 `max_tokens` 是必填字段，8192 是显式大值；OpenAI 兼容端点同样显式传
8192（用户选择"统一给一个大值"）。

### 预算常量

用户要求「100K 上下文，取 80%」。ClawBench 拿不到模型的真实上下文长度（无模型元数据、
无 tokenizer，只有 RAG 里一个启发式）。按最保守的 **1.5 字符/token**（CJK）折算：

```
80,000 tokens × 1.5 字符/token = 120,000 字符
```

落地为命名常量（不加配置键，用户已明确选固定常量）：

```go
// internal/service/btw.go
const btwContextBudgetChars = 120000
```

### 提示词

`btwSystemPrompt` 要点：你是旁路问答助手，用户在主对话之外顺便问了一个问题；
基于给出的会话历史作答；直接回答、不要复述历史、不要假装在执行任务、不要调用工具；
用用户问题的语言作答。

## 前端

### 命令注册与拦截

- `ChatInputBar.vue` 的 `clawbenchCommands` 增加 `/btw` 条目（描述 i18n）。
- 发送路径拦截：抽出单一 `dispatchSend(text)` 作为唯一出口，`/btw` 前缀命中时
  **不** `emit('send')`，改 `emit('btw', question)`。三个发送入口
  （`handleSendClick` / `onTextareaKeydown` 的 Enter / `handleQuickSendClick`）
  全部改走 `dispatchSend`，避免三处各写一遍判断而漂移。
- 裸 `/btw`（无问题）**不触发请求**，文本留在输入框让用户继续输入。
- 加载态：新增 `btwLoading`，发送键转圈 + 禁用输入框；通过 `defineExpose({ setBtwLoading })`
  让父组件控制。**不**触碰会话的 `loading`（会话可能正在跑任务，不能被 `/btw` 影响）。
- **`contentBlocks.ts` 的 `CLAWBENCH_COMMAND_RE` 不改**：该正则匹配的是 `/cb-*`，
  而 `/btw` 在 `emit('send')` 之前就被拦截，永远不会进入消息气泡，因此没有徽章需要渲染。
  把 `/btw` 塞进一个 `/cb-` 专用正则反而是错的。

### 抽屉

新增 `web/src/components/chat/BtwAnswerDrawer.vue`：
- 用 `useTabDrawer('chat', { autoRestore: false })`（与 `MessageClustersDrawer` 同模式），
  `BottomSheet` 承载，标题为「顺便问一句」。
- 内容：单个 `<ChatMessageItem :msg="syntheticMsg" :active="false" read-only hide-session-actions />`，
  上方显示问题原文（次级样式）。
- **用 `ChatMessageItem` 而非 `ChatMessageList`**：列表组件持有聊天滚动容器、
  懒加载、滚动 FAB，且硬编码 `id="aiChatMessages"`，都不属于抽屉。`SessionShareView`
  与 `TaskExecDetail` 是"单条消息独立宿主"的现成先例。
- **复用注入**：抽屉挂在 `ChatPanelContent` 内，直接继承其 `provide('chatRender'/'chatSession'/'chatUI'/'autoSpeech')`
  （`ChatPanelContent.vue:872-883`），不重复构造渲染链。
- 渲染映射（`expandedTools`/`blockTasks`/`blockAskQuestions`/`staticBlockCache`）由
  `ChatPanelContent` 以 props 传入，确保用的是同一个 `useChatRender` 实例的映射。
- 合成消息形状（对齐 `TaskExecDetail` 的合成消息）：
  ```js
  { id: 'btw-<ts>', role: 'assistant', content: '', blocks: [{ type: 'text', text: answer }],
    metadata: null, createdAt: new Date().toISOString(), streaming: false, cancelled: false }
  ```
- 用 `streaming:false` 走完整（非流式）渲染分支：KaTeX、DOMPurify、路径/commit 标注、
  代码块头、表格包裹、ask-question/task 检测全部生效——与聊天消息同一条管线。

### 交互细节

- 错误（未配置摘要模型、空问题、请求失败）→ toast，不弹抽屉，输入恢复。
- 答案为空 → toast，不弹抽屉。
- 抽屉打开时清理输入框（问题已被消费）。
- 关闭后清理合成消息状态。

## 测试

**后端**
- `service`：`btwContextBudgetChars` 传给 `BuildForkContextWithOptions` 的预算正确；
  空会话历史时仍能问答（上下文为空字符串）。
- `handler`：`/api/ai/session/btw` 的 400（空问题、无 sessionId）、成功返回 answer、
  摘要器未配置的错误路径；不落库断言（调用前后 `chat_history` 行数不变）。
- `summarize`：`DoRecommendPass` 的 maxTokens=0 → 1024、>0 → 传入值（用 httptest 校验
  请求体里的 `max_tokens`）；`AskAboutContext` 对不支持的后端返回错误。
- `openapi_drift_test` 覆盖新路由；`openapi.yaml` 同步。

**前端**
- `ChatInputBar`：`/btw xxx` 走 `emit('btw')` 而非 `emit('send')`；三个发送入口都覆盖；
  裸 `/btw`（无问题）的处理。
- `ChatPanelContent`：`handleBtw` 调 POST、成功弹抽屉、失败 toast 且不弹。
- `BtwAnswerDrawer`：给定 answer 渲染出 `ChatMessageItem`；断言 `streaming:false`
  且 blocks 形状正确。

## 不做的事（YAGNI）

- 不新增配置键（预算固定常量、max_tokens 固定 8192）。
- 不写历史、不做"旁支"标记。
- 不做流式渲染（用户明确选一次性弹出）。
- 不改摘要模型的推荐/摘要现有路径的行为（maxTokens=0 保持 1024）。
- 不引入 tokenizer 或模型上下文元数据（用户选固定常量）。
