# CodeMirror 6 粘性滚动（Sticky Scroll）设计

## 背景

代码浏览界面早期版本（CodePreview，highlight.js 渲染）实现了 VS Code 风格的粘性滚动：滚动时把当前作用域（函数/类定义行）钉在顶部，提示当前所在 scope。该功能在迁移到 CodeMirror 6 统一查看器时被移除（commit `e692a0ef`）。

本设计基于当前 CodeMirror 6 查看器（`web/src/components/file/CodeMirrorViewer.vue`）恢复该功能，并保留语法高亮与「工具栏按钮 + 设置项」开关。

## 目标

- 在 CodeMirror 6 查看器的源码浏览/编辑模式下，恢复粘性滚动。
- 复用后端 tree-sitter 符号 API（`fetchCodeSymbols`），与旧实现一致。
- 粘性定义行保持语法高亮（沿用旧高亮样式）。
- 恢复开关：FileHeader 工具栏 Pin 按钮 + 设置页 switch，默认开启。
- 满足 AGENTS.md 单元测试与覆盖率门槛。

## 方案

### 几何模型（关键点）

`.cm-scroller` 是滚动容器。使用 CodeMirror 几何 API 而非旧代码的 DOM 测量：

- 首可见行：`view.lineBlockAtHeight(scroller.scrollTop + 1)` → `doc.lineAt(block.from).number`
- 包围作用域：`sym.line <= firstVisibleLine && sym.endLine >= firstVisibleLine`，按作用域宽度 `endLine - line` 降序（外层优先）
- 仅保留定义行已滚出顶部者：`view.lineBlockAt(sym.line.from).top < scrollTop`
- 最多钉住 5 条
- 每行高度：`view.lineBlockAt(line.from).height`（天然支持 word-wrap 折行）

### 高亮渲染

`@lezer/highlight` 的 `highlightTree(tree, codeHighlightStyle, cb, from, to)` 作用域渲染定义行 HTML，**按行号缓存**（`Map<line, html>`）。滚动时仅读缓存；文件内容变化时清缓存重建。避免滚动时重复高亮导致的卡顿。

### 触发与生命周期

- `EditorView.domEventHandlers({ scroll })` + rAF 节流（`passive: true`）。
- 内容变更 watch：重新 fetch 符号 + 清高亮缓存 + 重算。
- 符号为异步返回：返回后再 attach scroll 并重算（处理晚于首帧到达）。
- 卸载时 detach scroll / 取消 rAF / 清缓存。

### UI

- `position: sticky; top: 0; z-index` 覆盖层挂在 `.cm-scroller` 内、`.cm-content` 之前。
- 每行：左侧行号（对齐 CM gutter，用 `.cm-gutters` 实际宽度做偏移）+ 高亮文本。
- `pointer-events`：容器 none、行 auto（不干扰选择/双击复制）。
- 点击粘性行：平滑滚动到定义行 + `line-flash`。

### 开关与配置

- `useSettingsConfig`：`stickyScroll` default `true` + legacy key `clawbench-sticky-scroll`（与 `wordWrap/lineNumbers` 同构）。
- `FileViewer`：`stickyScroll` computed + `toggleStickyScroll`，传 prop 给 `CodeMirrorViewer`。
- `CodeMirrorViewer`：新增 `stickyScroll` prop。
- `FileHeader`：恢复 Pin 按钮（inline + dropdown），`toolbarIds` 加入 `stickyScroll`。
- `settingsFieldMap`：新增 `stickyScroll` switch。
- i18n zh/en：`file.header.stickyScroll` 与 `settings.items.stickyScroll(Desc)`。

## 模块划分

- `web/src/composables/useCodeStickyScroll.ts`：核心逻辑。将纯计算抽成可测纯函数（findEnclosingScopes / firstVisibleLine / buildStickyLines / highlightLineHtml）。
- `web/src/components/file/CodeMirrorViewer.vue`：接入 composable，新增 `stickyScroll` prop，挂载覆盖层 DOM。
- `web/src/composables/useSettingsConfig.ts`：配置项。
- `web/src/components/file/FileViewer.vue`：开关接线。
- `web/src/components/file/FileHeader.vue`：Pin 按钮。
- `web/src/components/settings/settingsFieldMap.ts`：设置项。
- `web/src/i18n/locales/{zh,en}.ts`：文案。

## 测试

- `useCodeStickyScroll.test.ts`：纯函数单测（首可见行、包围作用域、最多 5 条、word-wrap 高度、行号缓存、高亮 HTML）。用 jsdom + 注入 mock 几何对象，不依赖真实 CM 渲染。
- `FileHeader.test.ts` / `FileViewer.test.ts`：恢复 stickyScroll prop/emit 断言。
- `CodeMirrorViewer.test.ts`：验证 stickyScroll prop 传递。

## 坑位规避

- 符号异步晚于首帧 → 返回后重算。
- 滚动 rAF 节流 + passive，避免高频重排。
- 高亮行号缓存，内容变化才重建。
- 覆盖层 pointer-events 区分容器/行。
- word-wrap 行高走 `lineBlockAt().height`，不做 DOM 测量。
