# 终端三态交互：浏览 / 手势 / 选区

日期：2026-08-07

## 目标

在移动端终端内实现**自由划选复制**，替代现有的「复制输出」按钮 + `OutputDrawer` 抽屉方案。

- 复用现有手势按钮，改为**三状态循环切换**（默认「浏览」）。
- 选区模式下单指拖动直接划选文本，划选后显示悬浮复制条。
- **完全移除**复制输出按钮与 `OutputDrawer` 组件。

xterm 用 canvas 渲染，系统原生长按选区不可用，因此需要一个手势层把「拖动」转成 xterm 选区。xterm 本身提供 `selectLines` / `clearSelection` / `getSelection` / `onSelectionChange`，并用主题的 `selectionBackground` 渲染选区，天然与终端配色一致。

## 范围

仅移动端（`isPC` 为 false）。桌面端已有原生 xterm 鼠标选区，保持不动。手势按钮位于移动端工具栏（`v-show="!isPC"`）。

## 状态模型

`useTerminalGestures.ts`：

- 用 `mode: Ref<'browse' | 'gesture' | 'selection'>` 替换现有 `enabled: Ref<boolean>`，**默认 `browse`**。
- 新增 `cycleMode(): void`，顺序 `browse → gesture → selection → browse`。
- 新增 `setMode(m: Mode): void`（复制完成后切回 browse 用）。
- `applyState()` 按 mode 分支挂监听：
  - `gesture` → 现有 `attachListeners`（方向键、双击 Tab、双指缩放/翻页）+ `touchAction: manipulation`
  - `browse` → 现有 `attachDisabledScrollListeners`（纵向滚动）+ `touchAction: auto`
  - `selection` → 新增 `attachSelectionListeners` + `touchAction: none`（确保 touchmove 持续触发）
- `GestureCallbacks` 新增：
  - `onSelectionStart?(row: number)`
  - `onSelectionExtend?(anchorRow: number, currentRow: number)`
  - `onSelectionEnd?()`
- 选区监听：touchstart 记录锚点行；touchmove 用容器 rect + `term.dimensions.css.cell.height` 把触摸坐标换算成 buffer 行号，触发 `onSelectionExtend`；touchend 触发 `onSelectionEnd`。
- 双指捏合缩放在三态都保留（低成本、体验一致）。

## 手势按钮（TerminalPanelContent.vue）

- 按钮 `@click` 改为调 `gestures.cycleMode()`。
- 三态视觉：
  - `browse` / `gesture`：复用现有 `.active` 样式，图标 `HandIcon`。
  - `selection`：独立图标（`TextCursorInput`）+ 醒目 `outline: 2px solid var(--accent-color)` 边框。
- `visibleKeys`（line 305）判定由 `!gestures.enabled.value` 改为 `mode === 'gesture'`。
- `shouldPreventTerminalContextMenu` 传入 `mode !== 'browse'`。
- 新增 i18n：`terminal.modeBrowse / modeGesture / modeSelection`（zh.ts + en.ts），切换时 toast 提示。

## 选区 + 复制（TerminalPanelContent.vue）

- 接入选区回调：
  - `onSelectionExtend(anchor, current)` → `term.selectLines(anchor, current)`，行号按 buffer 上下界 clamp。
  - `term.onSelectionChange` → 更新 `selectionActive` 与 `selectedText = term.getSelection()`。
- **悬浮复制条**：终端区底部（工具条上方）绝对定位，`selectionActive` 时显示「已选 n 字符 + 复制按钮」。点复制：
  1. `navigator.clipboard.writeText(selectedText)`
  2. toast 提示
  3. `term.clearSelection()`
  4. `gestures.setMode('browse')`
- 剪贴板失败（非 https）回退 `document.execCommand('copy')`，再失败 toast 报错。
- 切 tab 或进入其他态时 `term.clearSelection()`。

## 移除内容

- 工具栏复制按钮（line 117-119）
- `CopyIcon` 导入
- `handleCopyOutput`（679-706）
- `outputDrawer` / `outputDrawerText`（248-249）
- `OutputDrawer` 引用（164-169）与组件文件 `web/src/components/terminal/OutputDrawer.vue`

## 测试（Vitest）

- 新增 `useTerminalGestures.test.ts`：三态循环顺序、各态监听挂载/卸载、坐标→行号换算、`setMode` 行为。
- 调整/新增 `TerminalPanelContent` 相关测试：`selectLines` 调用与行号 clamp、复制后清选并回到 browse、悬浮条显隐、移除抽屉后无残留引用。

## 边界情况

- 选区跨回滚区：`selectLines` 行号与 buffer 实际索引对齐（含 scrollback），需 clamp 到 `[0, buffer.length)`。
- 换行文本：xterm `selectLines` 原生处理 wrapped 行，选区整行连续。
- 字号变化：cell 尺寸动态读取 `term.dimensions.css.cell.height`，不缓存。
- 复制权限：`navigator.clipboard` 失败时回退 `execCommand('copy')`。
