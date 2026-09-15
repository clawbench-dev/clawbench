# AskUserQuestion 补充信息/选项/已提交状态被清空 — 根因分析与修复

> 状态：**已实施**
> 日期：2026-09-15
> 分支：`fix/ask-question-state-persistence`
> 关联：`docs/plans/2026-09-15-tool-detail-wrap-toggle-state.md`（同源排查中发现的另一处，性质不同，另修）

---

## 1. 现象

在 AskUserQuestion 卡片里：

1. 在「补充信息」输入框填了内容
2. 切到别的界面（切 tab / 切前后台 / 切会话），再切回来
3. **补充信息被清空**

同源问题（一并修复）：**选项勾选**、**已提交状态** 也会丢失。重载后已答过的卡片会重新变成可提交，用户可能重复回答同一问题。

## 2. 根因

**三者都只存在于 DOM，没有任何 Vue 状态承载。**

### 2.1 输入框是拼进 HTML 字符串的

`renderToolDetail.ts:382-385`：

```ts
html += '<div class="ask-question-supplementary">'
html += `<label class="ask-supplementary-label">${escapeHtml(gt('tool.askUser.supplementary'))}</label>`
html += `<input class="ask-supplementary-input" type="text" placeholder="..." />`
html += '</div>'
```

这个字符串经 `v-html` 注入（`ContentBlocks.vue:119,193-194,363-364`；`ToolDetailDrawer.vue:15`）。

### 2.2 状态从未被保存

| 状态 | 存储方式 | 写入点 |
|---|---|---|
| 补充信息文本 | `<input>.value` | **无**（只有 `updateAskSubmitState` 读它） |
| 选项勾选 | `.selected` class | `renderToolDetail.ts:1773-1797` `classList` |
| 已提交 | `.ask-submitted` class | `renderToolDetail.ts:1809, 1857` `classList` |
| 提交按钮 enabled | 从上面派生 | `renderToolDetail.ts:1752-1756` |

`@input` 处理器只切换按钮，**不保存文本**：

```
ContentBlocks.vue:1248  handleToolDetailInput → updateAskSubmitState(askView)
```

### 2.3 触发条件：渲染出的 HTML 字符串真的变了

**实测确认的一个反直觉点**：Vue 在 `patchProps` 里有 `if (next !== prev)` 守卫（`@vue/runtime-core` `runtime-dom.cjs.js:5840`），**字符串未变时根本不会碰 innerHTML**，节点与用户输入都保留。

所以普通 tab 切换（`skipIfUnchanged=true`，无新消息时直接 return）**不会**丢。会丢的是字符串真变了的场景：

1. **前后台切换 → 强制重载**（最贴合「切出去再切回来」）
   `visibilitychange` → `useAppForeground` → `onAppForeground` → `handleManualRefresh()`（`useChatSession.ts:788-795`）→ `force=true` → `skipIfUnchanged=false`，无条件重建：
   ```
   useChatSession.ts:176  Object.keys(blockAskQuestions).forEach(k => delete ...)
   useChatSession.ts:177  parseMessages(...)   ← 全新 message/block 对象
   useChatSession.ts:184  dispatch({ type: 'db_load' })
   chatStreamUtils.ts:1607-1608  rebuildFromDb(...)
   ```
2. **列表整体重挂载**：`:key="listKey"`（`ChatMessageList.vue:22,250-255`），后台期间来了新消息就整表 unmount/remount
3. **合并卡分支翻转**：第二张 ask 卡到达时锚点从 `block.input` 切到 `mergedAskQuestions`（`ContentBlocks.vue:701-747,172,193-194`）
4. **`block.input` 异步回填**（`ContentBlocks.vue:457`）
5. **抽屉重赋值 `inputHtml`**（`useToolDetailDrawer.ts:112,185`）

## 3. 为什么 PermissionApproval 不受影响

同区域排查确认 `PermissionApproval` **不是**同源 bug，因为它在渲染时**从后端数据派生**状态（`renderToolDetail.ts:730-742`）：

```ts
const hasRealResult = isDone && blockCtx?.output
if (hasRealResult) html += ' permission-responded'
if (isAutoApproved) html += ' permission-auto-approved'
```

重载后从 `blockCtx` 重建，天然自愈。这是正确做法，可作为参照。

## 4. 修复方案

### 4.1 新增模块级状态存储

`web/src/utils/askQuestionState.ts`，接口对齐已有的 `chatDraftStore.ts`：

```ts
export interface AskAnswerState {
  selected: Record<string, string[]>   // qi -> [选项 label...]
  supplementary: string
  submitted: boolean
}
getAskState(key) / patchAskState(key, patch) / clearAskState(key)
clearAskStatesByPrefix(prefix) / askCardKey(sessionId, kind, id) / askSessionPrefix(sessionId)
hasAskStatesForPrefix(prefix) / _resetAskStatesForTesting()
```

**为什么必须模块级**（直接沿用 `chatDraftStore.ts:3-8` 的理由）：组件级 Map 会随子树重建被 GC，静默丢掉用户输入。

### 4.2 key 设计

`askCardKey(sessionId, kind, id)`，`kind` 区分四处渲染点（它们承载的问题集不同，不可互换）：

| 渲染点 | kind | id |
|---|---|---|
| `ContentBlocks.vue:175` 工具卡 | `tool` | `block.id`（后端 `ask-<uuid>`，持久化） |
| `:58` 汇总工具卡 | `tool` | `tool.id` |
| `:193/363` 合并卡 | `msg` | `msgId` |
| `:354` 文本卡 | `text` | `blockTaskKey(bi)` |
| `:119` 汇总合并卡 | `summary` | `msgId` |

按 session 前缀作用域，使归档/销毁能批量清理。

### 4.3 写入 + 回填

- **写入**：选项点击、补充信息 `input`（新增 `handleAskSupplementaryInput`）、提交（`patchAskState({submitted:true})`）
- **回填**：`restoreAskStateFromStore(view)` / `restoreAskStatesInContainer(container)`，在 `ContentBlocks.vue` 与 `ToolDetailDrawer.vue` 的 `onUpdated` 调用
- 卡片携带 `data-ask-key`（由 `ToolBlockCtx.askKey` 经渲染器输出）

### 4.4 生命周期

| 时机 | 行为 |
|---|---|
| 输入 / 勾选 | 写状态 |
| 重载 / 重挂载 | **不动**（核心修复点） |
| 切回同会话 | 回填 DOM |
| 提交 | 记 `submitted:true`，**不删除** |
| **发送失败** | `revertAskSubmission(key)` 撤销 `submitted`，保留选择与备注 |
| 归档 / 销毁 | `clearAskStatesByPrefix` 批量清 |
| 页面刷新 | 允许丢（内存态） |

**两个关键设计决定：**

1. **提交后不删除条目。** `restoreAskStateFromStore` 在无状态时直接 return，若提交后删除，重载会把卡片恢复成**未答**——正好复现本 bug。已答状态必须持久。

2. **发送失败必须回滚。** 持久化 `submitted` 带来一个副作用：发送失败时卡片会永久卡在已提交、无法重试。因此 `sendMessage` 改为**返回布尔**（不抛异常，避免 fire-and-forget 调用点产生 unhandled rejection），`handleToolSendMessage(text, cardKey)` 据此调用 `revertAskSubmission`。

### 4.3 回填时机：更新 + 挂载，缺一不可

初版只在 `onUpdated` 回填，**单元测试全绿但真实浏览器里答案依然丢失**。原因是
`ChatMessageList` 绑定 `:key="listKey"`（`sessionId|msgs.length|first|last`），一条新消息到达
就会让整个列表**重新挂载** —— 正是「切出去期间 AI 回复了，切回来」这条用户路径。
而 `onUpdated` 在首次挂载时**不会触发**，`AskUserQuestion` 又是交互式工具、`input` 随块内联下发，
没有任何后续更新会补触发一次，于是回填钩子从未运行。

修复：`onMounted`（配合 `nextTick` 等 v-html 落地）与 `onUpdated` 都执行回填。
`ContentBlocks` 与 `ToolDetailDrawer` 两处同改（抽屉每次打开都是新挂载）。

**为什么单元测试没抓到**：它们只覆盖了「更新」路径（`setProps` 触发 `onUpdated`），
没有覆盖「重挂载」路径。已补 mount 用例，并验证删掉 `onMounted` 即变红。

### 4.4 已知边界：文本卡不跨 Finalize

前端**不做** `<ask-question>` → `tool_use` 转换，转换在后端 Finalize（`session_executor.go:574-577`、`block_helpers.go:86-170`）。文本卡在 Finalize 后 key 从 `text:*` 变 `tool:*`，无天然映射。

**决策：接受此边界**，理由：

- Finalize 发生在流结束瞬间，随后立即 `loadHistory`（`useChatSession.ts:1109,1121`），窗口极窄
- 用户报告的场景（切走再切回）不受影响——那时卡片早已是 `tool` 形态
- 强行迁移（按问题文本匹配）会引入脆弱的错配风险

已在测试中明确固化该边界，而非隐藏它。

## 5. 涉及文件

| 文件 | 改动 |
|---|---|
| `web/src/utils/askQuestionState.ts` | **新增** 状态存储 |
| `web/src/utils/renderToolDetail.ts` | `ToolBlockCtx.askKey`；写入；`restoreAskStateFromStore` / `restoreAskStatesInContainer` / `handleAskSupplementaryInput` / `revertAskSubmission` |
| `web/src/components/chat/ContentBlocks.vue` | 四张卡传 `askKey`；`onUpdated` 回填；输入 handler |
| `web/src/components/chat/ToolDetailDrawer.vue` | `onUpdated` 回填；输入 handler |
| `web/src/composables/useToolDetailDrawer.ts` | 两处 `formatToolInput` 传 `askKey` |
| `web/src/composables/useSessionManager.ts` | 归档/销毁时批量清 |
| `web/src/components/chat/ChatPanelContent.vue` | `sendMessage` 返回布尔；`handleToolSendMessage` 失败回滚 |
| `ChatMessageItem.vue` / `ChatMessageList.vue` | 转发 `cardKey` |

## 6. 测试

| 文件 | 内容 |
|---|---|
| `askQuestionState.test.ts` | **新增** 21 用例：读写/合并/空态删除/key 构造/前缀批量清 |
| `renderToolDetail.test.ts` | 新增约 31 用例：写入三类状态、DOM 重建后回填、失败回滚、`data-ask-key` 输出与转义、容器入口、submit 携带 cardKey |
| `ContentBlocks.test.ts` | 更新 1 处既有断言；新增 10 用例：四张卡的 askKey（tool/msg/text/summary 四种 kind + 无 id 兜底）、更新回填钩子、**挂载回填钩子**、输入路由 |
| `ToolDetailDrawer.test.ts` | **新增文件**（此前无）：抽屉的回填钩子（更新 + 挂载）、输入路由 |
| `useToolDetailDrawer.test.ts` | 新增 3 用例：抽屉与列表共用同一 askKey |
| `useSessionManager.test.ts` | 新增归档/销毁的批量清用例 |
| `ChatPanelContent.test.ts` | 新增失败回滚用例 |
| `ChatMessageList.test.ts` | 新增 cardKey 转发链用例（三跳各一条） |

**TDD 过程验证**（每项都实测「删掉实现即变红」后还原，生产代码零改动）：

| 移除的实现 | 变红的测试 |
|---|---|
| `restoreAskStateFromStore` 函数体 | 6 条回填用例 |
| ContentBlocks 的 `onUpdated` 钩子 | 2 条钩子用例 |
| `askKeyForBlock` 传递 | 2 条 identity 用例 |
| `useToolDetailDrawer` 的 `askKey` | 3 条抽屉用例 |
| ToolDetailDrawer 的 `onUpdated` 钩子 | 2 条抽屉组件用例 |
| submit 的第三个 emit 参数 | 1 条 cardKey 用例 |
| ChatMessageItem 的 cardKey 转发 | 1 条转发用例 |

初版提交后审计发现 6 处缺口，其中最关键的是**核心修复机制本身无断言保护**（删掉 `onUpdated` 钩子或任一处 `askKey` 测试都不会红），已在第二提交补齐。

## 6.1 端到端验证（真实浏览器 + 真实服务）

单元测试全绿之后，仍在真实环境验证了一遍，并**发现了单元测试漏掉的真实缺陷**（见 §4.3）。

做法：在独立端口（28080）与独立数据目录启动仓库构建的服务，用 Playwright/Chromium 驱动，
**不触碰线上 20000 实例**。脚本流程：

1. 向会话注入一条带 `AskUserQuestion` 卡片的助手消息
2. 在真实输入框里填补充信息、点选选项
3. 给活节点打标记，然后**制造真实重挂载**：向 DB 追加一条消息（模拟后台期间的回复）后触发
   `hidden → visible`，走 `useAppForeground` → `handleManualRefresh` → `loadHistory` → `listKey` 变化 → 列表重挂载
4. 断言：标记已消失（**证明 DOM 真的重建了**）＋ 答案仍在

**关键设计：先证明 DOM 真的重建，否则断言无意义。** 早期版本没有这一步，跑出过一次
「PASS」但实际 DOM 未变（Vue 在字符串未变时会跳过 innerHTML patch），属于假绿。

**对照实验**：先构建「去修复」对照包，同一脚本复现出用户报告的现象 ——
`input-replaced=true card-replaced=true` 且补充信息与勾选双双清空；修复后同脚本全绿。

| 版本 | DOM 重建 | 补充信息 | 勾选 |
|---|---|---|---|
| 去修复对照 | ✅ 已重建 | ❌ 被清空 | ❌ 被清空 |
| 修复后 | ✅ 已重建 | ✅ 保留 | ✅ 保留 |

验证后已停用并清理隔离实例，线上 20000 实例全程未受影响。

全量受影响套件：`EXIT=0` / 1516 suites / 6966 tests / 0 failed。
