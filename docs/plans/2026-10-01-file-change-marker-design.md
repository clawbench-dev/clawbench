# 文件预览的变更标记重做 — 细轨 + 变更导航 + 累计基线

日期：2026-10-01
状态：设计已确认，待实现

## 目标

文件在磁盘上发生变化后，预览界面会标出「哪里改了」。当前实现有两个问题：

1. **Markdown 渲染预览**右侧是一条 **20px 全高实心色块**（`M`/`D`/`+`），既不合主题语言、视觉重量又过重。
2. 标记只在「外部改动触发自动刷新」那一刻算出来，**切文件 / 切 raw 模式 / 组件卸载就清掉**，而且**没有任何「哪里改了」的入口**——长文件只能自己滚着找。

本设计把 Markdown 面的色块换成与全仓 diff 面同语言的**细轨**，给两个预览面加上**变更导航**，并把标记的基线改为**累计**（跨文件、跨 surface 保留）。

## 现状（实现依据）

| 面 | 渲染 | 定位 |
|---|---|---|
| Markdown 渲染预览 | `MarkdownPreview.vue:7-17` 的 `<button class="diff-marker diff-marker-inline">`，20px 全高实心块 | `positionedMarkers[].top/height`（由 blockIndex → 实时 DOM 量出） |
| 代码 / 纯文本 | `CodeMirrorViewer.vue:150-171` 的 gutter `M/D/+` 字母 + 行 tint + 3px rail | `marker.lineNumbers` |

- 两份样式：`web/src/assets/diff-marker.css`（marker 本体）+ `code-viewer.css:88`（`.diff-marker-inline` 结构定位）。
- 颜色**硬编码** `rgba(255,165,0,.7)` / `rgba(255,80,80,.7)` / `rgba(80,200,80,.7)`，不随 36 套主题。
- 数据同源：`useMarkdownDiff.ts` 的模块级 `diffMarkers`，由 `useFileRefresh.ts:329-405` 在刷新时算出。
- 代码面已合规：`--diff-*-accent` token + `diff-rows.css` 的「tint + 3px rail」双通道。

### 色块难看的三处根因

1. **不合主题语言**——硬编码色，不走 token；全仓其余 diff 面都走 `--diff-*-accent`。
2. **视觉重量过重**——整块全高实心板，大段落 / 大代码块时尤其刺眼。
3. **红绿单独承载信息**——`diff-rows.css:11-24` 已写明：红绿 tint 相互对比仅 ~1.1:1，色觉障碍者拿不到信息，**必须**配形状通道。色块恰好只有颜色。

## 已确认的决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 视觉方向 | **细轨 + 悬停标签**（方向 A） | 3px 竖向细轨用 `--diff-*-accent`，与代码 gutter、git 历史同语言；`M/D/+` 只在 hover/聚焦时以小 chip 出现 |
| 逻辑范围 | **导航 + 跨文件保留** | 直接解决「找不到」；切走切回、切 raw 切回标记都在 |
| 覆盖面 | **Markdown + 代码** | 两面共用一套「N 处变更 · ◀ ▶」；代码面已有 gutter 与 `scrollToLine`，接线成本低 |
| 基线语义 | **累计基线**（A） | 以「第一次观察到变化前的内容」为基线，标记始终 = 基线与当前的 diff，直到手动清除或回到基线 |
| 导航位置 | **内容区右上角浮动胶囊**（A） | 紧贴预览、随滚动更新当前序号；两个面共用一套 |

**已知语义边界**：累计基线的起点是「第一次观察到变化」。若文件在用户打开**之前**就已被改、观看期间不再变化，则不会产生标记。这是该选项的固有边界（选项 C「以 git HEAD 为基线」可避免，但需接后端、改动最大，已被否决）。

## 数据模型

新增 `web/src/composables/useFileChangeBaseline.ts`。

```
baselines: Map<path, baselineContent>   // 累计基线（内存，LRU 上限）
```

**基线只存内容，不存标记数组**——标记按需从 `diff(baseline, current)` 派生。理由：同一文件在 markdown / code 两个 surface 下标记结构不同（前者按 blockIndex，后者按 lineNumbers），缓存两份标记数组会引入「哪份是权威」的同步问题；只存基线则两面的标记都从同一真相派生，天然一致。

### 生命周期

- **首次**观察到某文件变化（`refreshCurrentFile` 里 `oldContent !== newContent` 且尚无基线）→ 记 `baselines.set(path, oldContent)`。
- 已有基线 → 不动，标记始终 = `diff(基线, 当前)`（**累计**）。
- 文件回到基线（`current === baseline`）→ 自动删该条基线。
- 手动清除（导航胶囊的 ✕）/ 项目切换 → 清空。
- 上限：LRU 限 N 条（建议 20）；单文件超阈值（建议 2MB）不建基线，避免长会话堆积大字符串。

### 派生标记

```
deriveMarkers(path, surface, baselineContent, currentContent)
  → surface === 'markdown'
      ? computeMarkdownDiff(offscreenExtractBlocks(baseline), offscreenExtractBlocks(current))
      : computeCodeDiffMarkers(computeDiff(baseline, current), baseline, current)
```

**关键**：markdown 面两侧都走 `offscreenExtractBlocks()`（离线渲染），不再用 `getOldBlockList()` 读实时 DOM。这样标记可以在文件**未渲染**时也算出来（切文件、切 surface 的恢复路径），且两侧对称、不受实时 DOM 的渲染痕迹（mermaid / katex / 路径标注）干扰。

## 触发点统一

单一入口 `syncMarkersFor(path, surface)`，替换现在散落的 `clearDiffMarkers()`：

| 触发 | 现在 | 改为 |
|---|---|---|
| 外部变更刷新后 | 算 diff 后直接赋值 | 建/更新基线 → `syncMarkersFor` |
| raw ↔ rendered 切换 | `clearDiffMarkers()` | `syncMarkersFor`（用缓存基线重算另一面） |
| 切换文件 | `clearDiffMarkers()`（`useFileRefresh.ts:209`） | `syncMarkersFor`（从缓存基线恢复） |
| 组件卸载 | `clearDiffMarkers()`（`MarkdownPreview.vue:455`） | **删除该清除**（改为只清实时状态，不动基线） |

`diffMarkers` 仍是模块级 ref（组件读它），但语义改为「当前文件、当前 surface 的标记」；`syncMarkersFor` 负责在切换时把它刷成正确的那份。`diffOldContent` / `diffOldFilePath`（供 undo）同步改为按当前文件从基线取。

## 视觉（Markdown 面）

改 `web/src/assets/diff-marker.css` + `code-viewer.css` 的 `.diff-marker-inline`：

- **20px 实心块 → 3px 竖向细轨**：`background: var(--diff-{mod,del,add}-accent)`，锚点沿用今天的右缘（`right: max(0px, (100% - 900px) / 2)`）。
- **静息 `--opacity-muted`**，hover / `:focus-visible` → `opacity: 1` 并淡入 `M/D/+` **小 chip**（底 `--diff-*-bg`、字 `--diff-*-accent`，`border-radius: var(--radius-xs)`，`--duration-base`）。
- **触屏命中区**：视觉 3px，但 `<button>` 保持 ≥20px 宽（透明区外扩），避免细到点不中。
- **去掉入场动画** `diff-marker-highlight` / `diff-marker-added-flash`——它只是「淡入」，标记的**存在**本身才是信息；保留会让细轨每次刷新都闪一下。删除后 `diff-marker.css` 不再有 `@keyframes`，也不再有 reduced-motion 负担。
- **删除硬编码色与 `[data-theme-base="dark"]` 覆盖**——token 自带主题适配。
- 代码面 gutter **维持现状**（已合规）。

## 导航

内容区右上角浮动胶囊，两个面共用。建议抽成独立组件 `web/src/components/file/FileChangeNav.vue`，挂在 `FileViewer.vue` 的 `.file-viewer-content`（`position: relative` 已有）内。

- 显示条件：`diffMarkers.length > 0`。
- 内容：`N 处变更` + `◀` + `▶` + `✕`（清除基线）。
- **跳转**：markdown 用 `positionedMarkers[].top` → `.markdown-body` 滚动 + 闪烁；代码复用 `CodeMirrorViewer.scrollToLine()`（`CodeMirrorViewer.vue:297`，已带 `line-flash`）。
- **当前序号**：markdown 监听 `.markdown-body` 的 scroll、代码监听已有的 viewport-line 事件（`CodeMirrorViewer.vue:334`），高亮当前处。
- 样式复用共享 `.count-badge`（`components.css:39`）+ git 历史那套 `.diff-nav` 按钮观感。

### i18n

新增 `file.changeNav.*`（`zh.ts` / `en.ts` 各一套）：

```
changeNav: {
  count: '{count} 处变更',   // en: '{count} changes'
  prev: '上一处变更',
  next: '下一处变更',
  clear: '清除变更标记',
}
```

## 边界与风险

- **导出 HTML**：`exportMarkdownHtml.ts:1204` 会移除 `.diff-marker` 元素、`:393` 会内联 `diff-marker` CSS。细轨改造后这两处需复核（移除逻辑不受影响；内联的 CSS 内容会变）。
- **既有测试**：`MarkdownPreviewWideLayout.css.test.ts` 断言 `.diff-marker-inline` 的 `right` 与 `width`——width 从 20px 改掉会让它失败，需同步更新并保留「右缘锚点」这条不变量。
- **长会话内存**：基线是大字符串，必须有 LRU 上限 + 超大文件降级。
- **`prefers-reduced-motion`**：本设计**移除**入场动画而非 opt-out，所以不新增 media query；跳转闪烁沿用已有 `line-flash`（`flashReducedMotion.css.test.ts` 已守）。
- **主题适配**：细轨 + chip 全部走 token，需在 `github-light` + `github-dark` 各看一眼（design-guide 检查清单）。

## 测试

- `useFileChangeBaseline`：首次建基线 / 累计不覆盖 / 回到基线自动删 / LRU 上限 / 超大文件降级 / 项目切换清空。
- `syncMarkersFor`：切文件恢复、切 surface 重算、卸载不清基线。
- 源码守卫：`diff-marker.css` 不再含硬编码 `rgba(...)` 颜色、不再含 `@keyframes`；细轨用 `--diff-*-accent`。
- 组件：导航胶囊在 `diffMarkers.length === 0` 时不渲染；◀▶ 触发跳转；✕ 清基线。
- 既有 `useFileRefresh.test.ts` / `useMarkdownDiff.test.ts` / `MarkdownPreview*.test.ts` 同步更新。

## 实施顺序

1. `useFileChangeBaseline.ts` + 单测（纯逻辑，先立住）
2. `syncMarkersFor` 接线三处触发点 + 单测
3. `diff-marker.css` 细轨改造 + 更新 CSS 守卫测试
4. `FileChangeNav.vue` + 挂载 + 跳转/当前序号 + 组件测试
5. i18n 键
6. `npm run build` 供用户实测
