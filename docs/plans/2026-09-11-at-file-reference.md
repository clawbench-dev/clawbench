# 聊天输入框 @ 文件引用 + 补全交互组件抽取

日期：2026-09-11
状态：已实施（TDD，未使用独立 worktree）

## 实施结果

新增文件：
- `web/src/utils/completionMatch.ts` — `fuzzyMatch` / `buildFileCandidates` / `parseAtQuery` / `parseSlashQuery` / `middleEllipsis` / `SOURCE_PRIORITY`
- `web/src/utils/completionSources.ts` — `SOURCE_META`（7 类来源 → 图标/颜色/i18n key）
- `web/src/composables/useCompletionMenu.ts` — 状态机（show/activeIndex/sticky/键盘/selectByKey/close/clearSticky）
- `web/src/components/common/CompletionMenu.vue` — 通用列表展示

修改文件：
- `web/src/components/chat/ChatInputBar.vue` — 移除内联斜杠菜单逻辑与 `.at-menu-*` 样式；接入两个 `useCompletionMenu` + 两个 `CompletionMenu`
- `web/src/i18n/locales/{zh,en}.ts` — 新增 `chat.completion.source.*`
- 测试：`ChatInputBar.test.ts`（选择器与 mock 更新 + 新增 @ 菜单用例）、`completionMatch.test.ts`、`useCompletionMenu.test.ts`、`CompletionMenu.test.ts`

测试：4 个相关测试文件共 225 例全绿；`vue-tsc` 类型检查通过；ESLint 通过。
`src/components/chat` 全量回归 763 例中 2 例失败为既有并发超时 flake（`ChatMessageList.test.ts` 的 `useCodeBlockHeader` 断言，单独运行通过，且未改动相关文件）。

## 实施期修正（相对原始设计）

1. **fuzzy 匹配目标**：最终为 **basename**（`buildFileCandidates` 只对 `label` 打分），斜杠命令对完整命令名（含 `/`）打分。
2. **`middleEllipsis` 新增**：目录路径的"中间省略"改为 JS 实现（原计划的 `direction: rtl` 只能行首省略，不符合需求）。
3. **sticky/browse 模式**：`useCompletionMenu` 增加 `stickyAfterSelect`——@ 菜单选中后文本里已无 `@`，靠内部 sticky 标志保持打开。
4. **@ 触发时的数据加载**：分享/上传在"出现 @ 触发"时即拉取，而非"菜单可见时"——空目录下菜单不显示，否则远程来源永远不合并。
5. **caret 兜底**：`currentCaret()` 在 textarea DOM 与 ref 不同步（草稿恢复/失焦）时回退到文本末尾。
6. **菜单内点击不关闭**：`CompletionMenu` 行加 `@click.stop`，避免 `PopupMenu` 根节点的关闭处理误伤 @ 菜单的连续多选。
7. **FileIcon 行内图标**：文件候选统一挂 `icon: FileIcon`，斜杠命令不带 `icon`。

## Code Review 修复（第二轮）

子智能体 review 发现 4 Major + 5 Minor，已修 4 Major + m5/m6/m7：

- **M1 caret 非响应式**：新增 `caretVersion` ref，`fileMenuItems` 显式依赖它；`selectionchange` 中仅在 caret 真变化时自增（避免 `setSelectionRange` → `selectionchange` → 自增的无限循环，该循环曾导致测试 400s 超时 + 6 万行渲染警告）。
- **M2 sticky 永不释放**：`refresh()` 增加"文本快照比对"——进入 browse 模式时记录当时文本，用户一旦编辑即自动退出 browse；`clearSticky()` 同步清快照。
- **M3 双菜单叠加**：`watch(showCommandMenu)` / `watch(showFileMenu)` 互相调用对方 `close()`。
- **M4 异步来源合并后不刷新**：`ensureFileSourcesLoaded()` 在 `Promise.all` resolve 后调 `fileMenu.refresh()`，失败时允许重试。
- **m5 `close()` 破坏 Esc 闩锁**：`close()` 不再清 `dismissed`，仅 `clearSticky`/新触发释放。
- **m6 重复 key**：`selectByKey(key, source?)` 支持按 source 消歧；`handleCommandSelect` 传入 `cmd.source`。
- **m7 绝对/相对路径**：`buildFileCandidates` 增加 `projectRoot` 参数，用 `toProjectRelative` 归一化后再比对已附件；ChatInputBar 传入 `store.state.projectRoot`。

未修（记录在案）：m8（emoji/Unicode 边界的高亮劈开与 `İ` 小写变长，罕见文件名）、m9（`@mousedown` 在部分移动端 WebView 的可能不触发，与既有斜杠菜单同写法）。

测试：4 个相关文件共 236 例全绿；改动文件 `vue-tsc` 0 错误；ESLint 通过。

---

## 1. 目标

在聊天输入框 `ChatInputBar.vue` 中新增 `@` 文件引用能力：敲 `@` 弹出文件候选，fuzzy 匹配，选中即加入附件列表（复用既有引用通道，后端零改动）。

同时把斜杠命令（`/`）与 `@` 菜单**重叠的交互逻辑**抽取为公共 composable + 展示组件复用。斜杠命令的**触发解析保持原样**，仅替换匹配算法为 fuzzy 并复用统一交互。

## 2. 已确认决策汇总

| 维度 | 决定 |
|---|---|
| 落地语义 | 选中即 `addAttachedFile(path, isDir=false)` 加入 `useChatContext().attachedFiles`，走既有 `buildSendChannels` 的 `filePaths`/`entries` 通道；**后端零改动** |
| 触发文本处理 | 选中后删除触发区间（`@query` 或 `/cmd`），仅留附件卡片 |
| `@` 触发规则 | `@` 前须为行首或空白；取光标左侧文本，找最近的合法 `@`，`@`→光标为查询串 |
| `/` 触发规则 | **保持不变**：`text.startsWith('/') && !text.includes(' ')` |
| 匹配算法 | fuzzy：大小写不敏感；连续命中加分、词首/边界命中加分；未命中排除。斜杠命令一并迁移 |
| 匹配目标 | **仅 basename**（最后一段文件名） |
| 高亮 | 逐个字符高亮命中位置（复用 `highlightTextByPositions`） |
| 候选来源 | 5 类：最近打开 → 当前目录 → 最近引用 → 最近上传 → 最近分享 |
| 去重 | 按路径去重只显示一次，来源标识取优先级最高；**已选中的直接过滤** |
| 排序 | 有查询串：按 fuzzy score 降序，来源优先级作同分决胜；空查询：按来源优先级，**同来源内按路径字母序** |
| 候选上限 | 50 条 |
| 行结构 | 来源图标 + 可选文件类型图标（item.icon 有则渲染）+ basename（主行）+ 目录（次行，灰色、中间省略）+ 彩色来源标签 |
| 选中后菜单 | **保持打开**连续多选；删除 query 后回到空查询态显示全部候选 |
| 关闭条件 | 查询串出现空格 / 光标移出查询区间 / Esc / 点击输入框外部 / 无匹配结果 |
| 无匹配关闭后 | 输入框保留 `@xyz` 原样，作为普通文本发送（不自动删除） |
| Esc 抑制 | dismissed 闩锁：Esc 后只要仍在同一 @ 查询上下文内不重开；查询结束/光标移出/重新敲 `@` 时解除 |
| 确认键 | Enter 和 Tab 均选中当前候选，不发送消息（与斜杠菜单一致） |
| 异步加载 | 首次触发 `@` 时拉取分享/上传，先显示已就绪来源，加载完成后自动合并 |
| 作用范围 | 仅 `ChatInputBar.vue` |
| 斜杠视觉 | 统一升级为 @ 的新样式（彩色来源标签替代色条）；命令本身无独立图标，item.icon 缺省即不渲染 |
| i18n | 复用现有 `attach.*` key，缺失项新增 zh+en |

## 3. 现有代码事实（探索结论）

- 斜杠菜单完全内联在 `web/src/components/chat/ChatInputBar.vue`：
  - 匹配 `commandMenuItems` computed（L802-844，`label.toLowerCase().includes(q)`）
  - 触发 `watch(inputText)`（L847-857）
  - 键盘 `handleMenuKeydown`（L883-917），由 `onTextareaKeydown`（L1101-1115）优先调用
  - 选中 `handleCommandSelect`（L872-880，写回 `cmd.key + ' '`）
  - UI：`PopupMenu` + `.at-menu-*` 样式（模板 L181-189，样式 L2494-2602）
- `PopupMenu.vue` 通用、无键盘逻辑，已适配 visualViewport（移动端键盘）。
- `highlightTextByPositions(text, positions)` 在 `web/src/utils/searchUtils.ts:93`，按 rune 位置高亮，可直接复用。
- 5 类候选源数据均已存在：
  1. 最近打开：`useRecentFiles().entries`（`{path, accessedAt}`，localStorage，上限受 `localConfig.recentFilesCount` 控制）
  2. 当前目录：`store.state.dirEntries`（`DirEntry {name, type, ...}`）+ `store.state.currentDir`；仅 `type==='file'`；完整相对路径 = `joinPath(currentDir, name)`
  3. 最近引用：`computeRecentReferencedFiles(messages, attachedFiles, currentFile)`（`utils/chatInputUtils.ts:25`），返回 `{path, count}[]`
  4. 最近上传：`useUploadRecent().recentUploads`（`{name, path, size, modTime}`）
  5. 最近分享：`useShareIn().recentShares`（`{name, path, size, modTime}`）
- 分享/上传仅在 `AttachDrawer.vue:345`（抽屉打开时）拉取，@ 菜单需自行触发首次加载。
- 路径统一为**项目相对路径 + 正斜杠**（`utils/path.ts`）；`baseName`/`dirName` 可派生显示名。
- 项目**无现成 fuzzy 实现**，需新写。
- i18n 已有 `chat.attach.currentDir / recentReferences / recentShares / recentUploads`（`zh.ts:470-473`，`en.ts` 对应）。

## 4. 文件改动

### 新增

**`web/src/utils/completionMatch.ts`** — 纯函数（可单测）
- `fuzzyMatch(query: string, target: string): { score: number; positions: number[] } | null`
  - 大小写不敏感；字符须按序出现；连续命中加分、词首/分隔符（`/ - _ . ` 及大小写切换）后命中加分；未命中返回 `null`。
- `type CompletionSource = 'recent-open' | 'current-dir' | 'recent-ref' | 'recent-upload' | 'recent-share' | 'clawbench' | 'agent'`
- `interface CompletionItem { key; label; description; source; icon?; positions?; score? }`
- `buildFileCandidates(sources, query): CompletionItem[]`
  - 合并 5 源 → 按路径去重（保留最高优先级来源）→ 过滤已选中 → fuzzy 匹配 basename → 排序（有 query 按 score，空 query 按来源优先级 + 路径字母序）→ 截断 50。
- `parseAtQuery(text: string, caret: number): { start: number; query: string } | null`
  - 取 `text.slice(0, caret)`，找最近 `@`，校验其前为行首或空白，且区间内无空白。
- `parseSlashQuery(text: string): { start: number; query: string } | null`
  - 保留原语义：`startsWith('/') && !includes(' ')`，`start = 0`。

**`web/src/composables/useCompletionMenu.ts`** — 交互状态机（可单测）
- 状态：`show`、`activeIndex`、`dismissed`。
- `handleKeydown(e): boolean`：IME 放行 → ArrowUp/Down 循环 → Enter/Tab 选中 → Esc 关闭并置 `dismissed`。
- `select(item)`：调 `onSelect(item)` 业务钩子 → 由 composable 统一删除触发区间 `[start, caret)` → 按 `closeOnSelect` 策略决定关闭或复位 `activeIndex=0` 并保持打开。
- `scrollActiveIntoView()`：查 `[data-completion-idx]` + `scrollIntoView({block:'nearest'})`。
- 参数：`getTrigger()` 返回触发区间（注入 `parseAtQuery`/`parseSlashQuery`）、`closeOnSelect`、`onSelect`、`items`。
- `dismissed` 解除条件：触发区间失效（查询结束/光标移出）或重新出现新的触发符。

**`web/src/components/common/CompletionMenu.vue`** — 通用列表展示
- Props：`items: CompletionItem[]`、`activeIndex: number`、`show: boolean`、`targetElement`。
- 内置 `SOURCE_META: Record<CompletionSource, { icon; color; labelKey }>`（7 类）。
- Emit：`select(item)`。
- 渲染：`PopupMenu` 包裹，每行 = 来源图标 + `item.icon`（可选，缺省不占位）+ basename 主行（`v-html` 逐个字符高亮）+ 目录次行（中间省略）+ 彩色来源标签。
- 迁移 `.at-menu-*` 样式进组件（`at-menu-item--<source>` 修饰类扩展为 7 类）。

**测试**：`utils/__tests__/completionMatch.test.ts`、`composables/__tests__/useCompletionMenu.test.ts`、`components/common/__tests__/CompletionMenu.test.ts`

### 修改

**`web/src/components/chat/ChatInputBar.vue`**
- 移除内联斜杠逻辑：`commandMenuItems`、`showCommandMenu`、`commandMenuIndex`、`handleMenuKeydown`、`handleCommandSelect`、`watch(inputText)` 中菜单部分、`.at-menu-*` 样式。
- 接入两次 `useCompletionMenu`（slash / at）+ 两个 `CompletionMenu` 实例。
- `onTextareaKeydown` 依次尝试 slash 菜单键盘 → at 菜单键盘 → 历史导航 → Enter 发送。
- `@` 触发需监听 `input` 与 `keyup`/`click`（光标移动）更新触发区间；选中后 `setSelectionRange` 精确复位光标。
- 复用/调整 `recentReferencedFiles`，理顺与"过滤已选中"的重复排除。

**`web/src/i18n/locales/zh.ts` / `en.ts`**
- 新增 `chat.completion.source.recentOpen`（最近打开）等缺失 key；斜杠来源标签（内置/智能体）。

## 5. TDD 实施顺序

1. 写 `completionMatch.test.ts`（fuzzy 打分、按序匹配、边界加分、未命中、basename 匹配、去重优先级、已选过滤、排序、50 截断、`parseAtQuery` 各边界、`parseSlashQuery`）→ 实现 `completionMatch.ts` 至全绿。
2. 写 `useCompletionMenu.test.ts`（键盘循环、Enter/Tab 选中、Esc 闩锁与解除、选中后删区间/关留策略、IME 放行）→ 实现 `useCompletionMenu.ts` 至全绿。
3. 写 `CompletionMenu.test.ts`（7 类来源徽标、可选 icon 缺省不占位、高亮位置、activeIndex 高亮、select 事件）→ 实现 `CompletionMenu.vue` 至全绿。
4. 改造 `ChatInputBar.vue` 接入，更新/补充 `ChatInputBar.test.ts`（@ 触发、连续多选、Esc 抑制、无匹配保留文本、与历史导航/IME 协调）。
5. 补 i18n。
6. 运行 `./scripts/pre-push-checks.sh`（前端改动，注意 ESLint 在 worktree 的已知问题；本次不开 worktree）。

## 6. 风险与注意

1. **斜杠匹配语义变更**：`includes` → fuzzy 会扩大命中集合（松散匹配），需关注排序质量与既有测试断言。
2. **Esc 闩锁 vs `historyNavSuppressMenu`**：两套抑制机制需协调，避免互相覆盖导致菜单误弹。
3. **光标复位**：删除 `@query` 后必须 `setSelectionRange` 到删除点，且需处理多行文本中的位置。
4. **`recentReferencedFiles` 重复排除**：`computeRecentReferencedFiles` 已排除 `attachedFiles`，与 @ 菜单"过滤已选中"重叠，需统一在一处。
5. **移动端行内容**：来源图标 + 类型图标 + basename + 目录 + 标签，行内元素较多，需控制间距避免溢出。
6. **异步合并抖动**：分享/上传加载完成后列表重排，用户可能正在浏览；需保证 `activeIndex` 不指向错位项。
