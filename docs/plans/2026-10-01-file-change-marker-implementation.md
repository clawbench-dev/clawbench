# 文件预览变更标记重做 — 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 把 Markdown 预览的 20px 实心变更色块换成主题化的 3px 细轨，给 Markdown 与代码两个预览面加「N 处变更 · ◀ ▶ ✕」导航，并把标记基线改为累计、跨文件跨 surface 保留。

**Architecture:** 新增 `useFileChangeBaseline.ts` 只存「基线内容」（`Map<path, string>`，LRU 上限），标记按需从 `diff(基线, 当前)` 派生——因为两个 surface 的标记结构不同（markdown 按 blockIndex、代码按 lineNumbers），缓存两份数组会引入权威同步问题。单一入口 `syncMarkersFor(path, surface)` 替换现在散落的 `clearDiffMarkers()`，在「外部刷新 / 切 surface / 切文件」三处调用。

**Tech Stack:** Vue 3 `<script setup>`、TypeScript、Vitest + jsdom、CSS 自定义属性（36 套主题的 `--diff-*` token）。

**设计文档:** `docs/plans/2026-10-01-file-change-marker-design.md`（先读它）

---

## 背景知识（实现者必读）

- **两个预览面**：Markdown 渲染预览 = `MarkdownPreview.vue`；代码 / 纯文本 / raw markdown = `CodeMirrorViewer.vue`。二者是 `FileViewer.vue` 的兄弟节点，`v-if` 互斥，同一时刻只渲染一个。
- **标记数据同源**：模块级 `diffMarkers` ref（`web/src/composables/useMarkdownDiff.ts:793`）。
- **全仓 diff 视觉语言**：`web/css/diff-rows.css` 规定每个 diff 面必须「淡 tint + 3px rail」双通道（红绿单独不可辨，见该文件 11-24 行注释）。颜色 token 在 `web/css/variables.css:126-131`：`--diff-add-bg` / `--diff-del-bg` / `--diff-mod-bg` 与 `--diff-add-accent` / `--diff-del-accent` / `--diff-mod-accent`。
- **测试环境**：vitest + jsdom（`vitest.config.ts:60`），setup 在 `web/src/test-setup.ts`。纯前端改动跑 `npx vitest run <file>` 即可。
- **并发安全**：本仓库常有多个 agent 同时改同一棵树。**不要跑全量 vitest**（约 14 分钟且会与他人争抢）；每次只跑本任务涉及的文件。提交前先 `git status` 核对只捕获自己的文件。
- **日志**：一律用 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止裸 `console.*`。
- **i18n 字面花括号**：文案里若出现 `{{` 会踩 vue-i18n 嵌套插值，必须写 `{'{{'}...{'}}'}`。本计划的键不含字面花括号。

---

## Task 1: 基线 composable（纯逻辑）

**Files:**
- Create: `web/src/composables/useFileChangeBaseline.ts`
- Test: `web/src/composables/__tests__/useFileChangeBaseline.test.ts`

**Step 1: 写失败的测试**

创建 `web/src/composables/__tests__/useFileChangeBaseline.test.ts`：

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import {
  recordBaseline,
  getBaseline,
  clearBaseline,
  clearAllBaselines,
  baselineCount,
  MAX_BASELINE_ENTRIES,
  MAX_BASELINE_BYTES,
} from '@/composables/useFileChangeBaseline.ts'

describe('useFileChangeBaseline', () => {
  beforeEach(() => {
    clearAllBaselines()
  })

  it('records the pre-change content as the baseline on first observation', () => {
    recordBaseline('a.md', 'v1')
    expect(getBaseline('a.md')).toBe('v1')
  })

  it('does not overwrite an existing baseline (accumulates)', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('a.md', 'v2')
    expect(getBaseline('a.md')).toBe('v1')
  })

  it('drops the baseline when content returns to it', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('a.md', 'v1')
    expect(getBaseline('a.md')).toBeNull()
    expect(baselineCount()).toBe(0)
  })

  it('clears a single path', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('b.md', 'x')
    clearBaseline('a.md')
    expect(getBaseline('a.md')).toBeNull()
    expect(getBaseline('b.md')).toBe('x')
  })

  it('evicts the oldest entry past the LRU cap', () => {
    for (let i = 0; i < MAX_BASELINE_ENTRIES + 2; i++) {
      recordBaseline(`f${i}.md`, `content-${i}`)
    }
    expect(baselineCount()).toBe(MAX_BASELINE_ENTRIES)
    expect(getBaseline('f0.md')).toBeNull()
    expect(getBaseline(`f${MAX_BASELINE_ENTRIES + 1}.md`)).toBe(`content-${MAX_BASELINE_ENTRIES + 1}`)
  })

  it('refreshes LRU recency on read', () => {
    for (let i = 0; i < MAX_BASELINE_ENTRIES; i++) {
      recordBaseline(`f${i}.md`, `content-${i}`)
    }
    // Touch f0 so it is no longer the oldest.
    getBaseline('f0.md')
    recordBaseline('new.md', 'new')
    expect(getBaseline('f0.md')).toBe('content-0')
    expect(getBaseline('f1.md')).toBeNull()
  })

  it('refuses to store an oversized baseline', () => {
    const huge = 'x'.repeat(MAX_BASELINE_BYTES + 1)
    recordBaseline('huge.md', huge)
    expect(getBaseline('huge.md')).toBeNull()
    expect(baselineCount()).toBe(0)
  })

  it('clearAllBaselines empties the map', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('b.md', 'v2')
    clearAllBaselines()
    expect(baselineCount()).toBe(0)
  })
})
```

**Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useFileChangeBaseline.test.ts`
Expected: FAIL — 模块不存在（`Failed to resolve import`）。

**Step 3: 写最小实现**

创建 `web/src/composables/useFileChangeBaseline.ts`：

```ts
/**
 * useFileChangeBaseline — 累计基线存储。
 *
 * 文件在磁盘上变化时，把「第一次观察到变化前的内容」记为基线，标记始终是
 * diff(基线, 当前)。只存基线内容、不存标记数组：同一文件在 markdown / code
 * 两个 surface 下标记结构不同（前者按 blockIndex，后者按 lineNumbers），缓存
 * 两份数组会引入「哪份是权威」的同步问题；只存基线则两面的标记都从同一真相
 * 派生，天然一致。
 *
 * 内存边界：LRU 上限 MAX_BASELINE_ENTRIES 条；单条超 MAX_BASELINE_BYTES 不存
 * （长会话里基线是大字符串，无上限会一直堆积）。
 */

/** LRU 上限。超出后淘汰最久未读的一条。 */
export const MAX_BASELINE_ENTRIES = 20

/** 单条基线的字节上限（按 UTF-16 code unit 计）。超限不建基线。 */
export const MAX_BASELINE_BYTES = 2 * 1024 * 1024

// Map 的插入顺序即 LRU 顺序：删除再插入可把条目移到「最近使用」端。
const baselines = new Map<string, string>()

/**
 * 记录基线。仅在该路径尚无基线时写入（累计语义）。
 *
 * - 内容等于基线 → 视为回到基线，删除该条。
 * - 内容超限 → 不建基线。
 */
export function recordBaseline(path: string, content: string): void {
  const existing = baselines.get(path)
  if (existing !== undefined) {
    if (existing === content) baselines.delete(path)
    return
  }
  if (content.length > MAX_BASELINE_BYTES) return
  baselines.set(path, content)
}

/** 读基线（命中时刷新 LRU 位置）。无基线返回 null。 */
export function getBaseline(path: string): string | null {
  const value = baselines.get(path)
  if (value === undefined) return null
  // 重新插入以标记为最近使用。
  baselines.delete(path)
  baselines.set(path, value)
  return value
}

/** 删除单条基线。 */
export function clearBaseline(path: string): void {
  baselines.delete(path)
}

/** 清空（项目切换 / 手动清除）。 */
export function clearAllBaselines(): void {
  baselines.clear()
}

/** 当前基线条数（测试与内存观测用）。 */
export function baselineCount(): number {
  return baselines.size
}
```

**Step 4: 跑测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useFileChangeBaseline.test.ts`
Expected: PASS（8 个用例）。

**Step 5: 提交**

```bash
git status --short
git add web/src/composables/useFileChangeBaseline.ts web/src/composables/__tests__/useFileChangeBaseline.test.ts
git commit -m "feat(file): 累计基线存储（useFileChangeBaseline）"
```

---

## Task 2: `syncMarkersFor` 接线（基线派生 + 三处触发点）

**Files:**
- Modify: `web/src/composables/useFileRefresh.ts`（新增导出 + 替换三处 `clearDiffMarkers()`）
- Test: `web/src/composables/__tests__/useFileRefresh.test.ts`（新增用例）

**Step 1: 写失败的测试**

在 `web/src/composables/__tests__/useFileRefresh.test.ts` 的 mock 段之后、`describe` 内新增。先在文件顶部 import 区补上新导出：

```ts
import {
  refreshCurrentFile,
  isRefreshing,
  flashRanges,
  flashType,
  markFileSaved,
  wasRecentlySaved,
  syncMarkersFor,
} from '../useFileRefresh.ts'
import { clearAllBaselines, getBaseline, recordBaseline } from '@/composables/useFileChangeBaseline.ts'
```

在 `describe('useFileRefresh deduplication', ...)` 内加一个嵌套 describe：

```ts
describe('syncMarkersFor (accumulated baseline)', () => {
  beforeEach(() => {
    clearAllBaselines()
    vi.clearAllMocks()
    diffMarkers.value = []
  })

  it('derives code markers from the baseline, not the last refresh', () => {
    recordBaseline('a.go', 'line1\nline2\n')
    // computeCodeDiffMarkers is mocked to return [] by the module mock; assert
    // it was called with the BASELINE as oldContent.
    syncMarkersFor('a.go', 'code', 'line1\nCHANGED\n')
    expect(computeCodeDiffMarkers).toHaveBeenCalledWith(
      expect.anything(),
      'line1\nline2\n',
      'line1\nCHANGED\n',
    )
  })

  it('produces no markers and records no baseline when none exists', () => {
    // syncMarkersFor only DERIVES. Recording the baseline is the refresh path's
    // job (it alone knows the pre-change content); a bare sync must not seed a
    // baseline, or merely opening a file would create one.
    syncMarkersFor('b.go', 'code', 'fresh\n')
    expect(getBaseline('b.go')).toBeNull()
    expect(diffMarkers.value).toEqual([])
  })

  it('restores markers when re-syncing the same path later', () => {
    recordBaseline('c.go', 'v1\n')
    syncMarkersFor('c.go', 'code', 'v2\n')
    const first = diffMarkers.value
    // Simulate navigating away then back: markers cleared, then re-synced.
    diffMarkers.value = []
    syncMarkersFor('c.go', 'code', 'v2\n')
    expect(diffMarkers.value).toEqual(first)
  })
})
```

**Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useFileRefresh.test.ts`
Expected: FAIL — `syncMarkersFor` 未导出。

**Step 3: 实现**

在 `web/src/composables/useFileRefresh.ts` 顶部 import 区加：

```ts
import {
  recordBaseline,
  getBaseline,
} from '@/composables/useFileChangeBaseline.ts'
```

在 `refreshCurrentFile` 函数**之前**新增导出函数：

```ts
/** 预览面类型。两面的标记结构不同，派生路径也不同。 */
export type ChangeSurface = 'markdown' | 'code'

/**
 * 按累计基线重算并发布当前文件、当前 surface 的标记。
 *
 * 这是标记发布的唯一入口——替换原先散落的 clearDiffMarkers() 调用，使
 * 「切文件 / 切 surface / 外部刷新」三条路径都走同一套基线逻辑，标记得以
 * 跨文件、跨 surface 保留。
 *
 * **本函数只做派生，不建基线。** 建基线是刷新路径的职责——只有它知道「变化
 * 前的内容」是什么。若在这里建，用户单纯打开一个文件就会产生基线，此后任何
 * 改动都会被当成「相对打开时」的变化。
 *
 * - 无基线 → 无标记（还没有观察到任何变化）。
 * - 有基线 → 标记 = diff(基线, currentContent)。
 * - 内容回到基线 → 消费方应调用 clearBaseline（本函数不会自动删，因为它
 *   无从区分「回到基线」与「基线恰好等于当前」的边界）。
 */
export function syncMarkersFor(path: string, surface: ChangeSurface, currentContent: string): void {
  const baseline = path ? getBaseline(path) : null
  if (baseline === null || baseline === currentContent) {
    clearDiffMarkers()
    return
  }

  if (surface === 'markdown') {
    const oldBlocks = offscreenExtractBlocks(baseline)
    const newBlocks = offscreenExtractBlocks(currentContent)
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    if (result.hasChanges) {
      diffMarkers.value = result.markers
      diffOldContent.value = baseline
      diffOldFilePath.value = path
    } else {
      clearDiffMarkers()
    }
    return
  }

  const lineDiff = computeDiff(baseline, currentContent)
  const markers = computeCodeDiffMarkers(lineDiff, baseline, currentContent)
  if (markers.length > 0) {
    diffMarkers.value = markers
    diffOldContent.value = baseline
    diffOldFilePath.value = path
  } else {
    clearDiffMarkers()
  }
}
```

替换 `refreshCurrentFile` 里 Phase 2 之后的「Apply markers」块（原 `useFileRefresh.ts:387-405`）为：

```ts
  // ─── Apply markers via the accumulated baseline (common) ───

  const surface: ChangeSurface = isMarkdown ? 'markdown' : 'code'
  const appliedContent = store.state.currentFile?.content ?? newContent
  if (appliedContent !== null) {
    const existing = getBaseline(currentFilePath)
    if (existing === null) {
      // First observed change: seed the baseline with the content as it was
      // BEFORE this refresh, so markers accumulate from here on.
      if (oldContent !== null && oldContent !== appliedContent) {
        recordBaseline(currentFilePath, oldContent)
      }
    } else if (existing === appliedContent) {
      // The file came back to its baseline — drop it so the marker clears and
      // the next change starts a fresh baseline.
      clearBaseline(currentFilePath)
    }
    syncMarkersFor(currentFilePath, surface, appliedContent)
  }
```

import 区补上 `clearBaseline`：

```ts
import {
  recordBaseline,
  getBaseline,
  clearBaseline,
} from '@/composables/useFileChangeBaseline.ts'
```

同时把「切换文件」的 watch（原 `useFileRefresh.ts:209-214`）改为：

```ts
watch(() => store.state.currentFile?.path, (newPath, oldPath) => {
    if (newPath !== oldPath) {
        clearFlash()
        // 不清基线：切回同一文件时标记要恢复。标记本身由消费方在挂载时
        // 重新 syncMarkersFor 派生。
        diffMarkers.value = []
    }
})
```

> **注意**：这里把 `clearDiffMarkers()` 换成只清 `diffMarkers.value`，因为 `clearDiffMarkers()` 还会清 `diffOldContent`（undo 需要按当前文件重新派生）。Task 4 的组件挂载路径会调用 `syncMarkersFor` 恢复。

**Step 4: 跑测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useFileRefresh.test.ts`
Expected: PASS。若既有用例因「Apply markers」改动而失败，读它们的断言——它们断言的是旧的「直接赋值」语义，需按新语义（基线派生）更新，**不要**放宽断言。

**Step 5: 提交**

```bash
git add web/src/composables/useFileRefresh.ts web/src/composables/__tests__/useFileRefresh.test.ts
git commit -m "feat(file): 标记改由累计基线派生（syncMarkersFor）"
```

---

## Task 3: Markdown 细轨视觉

**Files:**
- Modify: `web/src/assets/diff-marker.css`（整体重写 marker 本体样式）
- Modify: `web/src/assets/code-viewer.css:88-95`（`.diff-marker-inline` 结构定位）
- Test: `web/src/components/file/__tests__/diffMarkerRail.css.test.ts`（新建）
- Test: `web/src/components/file/__tests__/MarkdownPreviewWideLayout.css.test.ts:50-61`（更新 width 断言）

**Step 1: 写失败的测试**

创建 `web/src/components/file/__tests__/diffMarkerRail.css.test.ts`：

```ts
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * Diff marker rail contract.
 *
 * The markdown preview's change marker used to be a 20px-tall solid block with
 * hard-coded orange/red/green (rgba(255,165,0,.7) etc). That broke two rules:
 *   1. Colour must come from theme tokens — every other diff surface in the app
 *      draws from --diff-{add,del,mod}-{bg,accent} (see css/diff-rows.css).
 *   2. Red/green alone cannot encode add-vs-delete (colour-vision deficiency);
 *      the repo's diff language pairs the tint with a 3px shape rail.
 *
 * The marker is now a 3px rail in --diff-*-accent; the M/D/+ label only appears
 * as a chip on hover/focus. These tests are source-contract checks (jsdom has
 * no CSS engine for var() resolution).
 */
describe('diff marker rail', () => {
  const markerCss = readFileSync(resolve(__dirname, '../../../assets/diff-marker.css'), 'utf8')
  const viewerCss = readFileSync(resolve(__dirname, '../../../assets/code-viewer.css'), 'utf8')

  it('draws the rail from theme tokens, not hard-coded rgba colours', () => {
    expect(markerCss).not.toMatch(/rgba\(\s*255\s*,\s*165\s*,\s*0/)
    expect(markerCss).not.toMatch(/rgba\(\s*255\s*,\s*80\s*,\s*80/)
    expect(markerCss).not.toMatch(/rgba\(\s*80\s*,\s*200\s*,\s*80/)
    expect(markerCss).toContain('var(--diff-mod-accent)')
    expect(markerCss).toContain('var(--diff-del-accent)')
    expect(markerCss).toContain('var(--diff-add-accent)')
  })

  it('draws the rail 3px wide', () => {
    const rail = markerCss.match(/\.diff-marker::before\s*\{[\s\S]*?\}/)
    expect(rail).toBeTruthy()
    expect(rail![0]).toContain('width: 3px')
  })

  it('keeps a tappable hit area wider than the rail', () => {
    // The button is the hit area; the rail is drawn at its right edge. A 3px
    // hit target would be unhittable, so the element stays 20px.
    const rule = viewerCss.match(/\.diff-marker-inline\s*\{[\s\S]*?\}/)
    expect(rule).toBeTruthy()
    expect(rule![0]).toContain('width: 20px')
  })

  it('has no entry keyframes (a rail appearing is not an event to animate)', () => {
    expect(markerCss).not.toContain('@keyframes')
  })
})
```

**Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/components/file/__tests__/diffMarkerRail.css.test.ts`
Expected: FAIL — 仍含 `rgba(255, 165, 0`、`@keyframes` 仍在，且尚无 `.diff-marker::before` 的 `width: 3px`。

**Step 3: 实现**

把 `web/src/assets/diff-marker.css` **整体**替换为：

```css
/* Shared diff marker visual styles — used by CodePreview (inline) and
   MarkdownPreview (overlay).

   The marker is a 3px rail in the theme's --diff-*-accent colour, matching
   every other diff surface (css/diff-rows.css). The M/D/+ label is a chip that
   appears only on hover/focus — a permanent 20px block was too heavy over large
   changed sections, and its hard-coded red/green carried no shape channel for
   colour-vision-deficient readers.

   Structural positioning (absolute, right offset, hit area) lives in
   code-viewer.css (.diff-marker-inline). */

/* ─── Base marker (the rail) ─── */

.diff-marker {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  border: none;
  padding: 0;
  cursor: pointer;
  /* Rail at rest is muted; hover/focus brings it and the chip to full. */
  opacity: var(--opacity-muted);
  transition: opacity var(--duration-base);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
  font-family: var(--font-ui);
  user-select: none;
  line-height: 1;
  /* The rail itself is a solid accent stripe; the label chip is a child
     pseudo-element so it can overhang without widening the 3px rail. */
  position: relative;
}

.diff-marker::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  right: 0;
  width: 3px;
  border-radius: var(--radius-full);
  background: currentColor;
}

.diff-marker:hover,
.diff-marker:focus-visible {
  opacity: 1;
  outline: none;
}

.diff-marker:focus-visible {
  box-shadow: 0 0 0 2px var(--accent-color);
  border-radius: var(--radius-xs);
}

/* The M/D/+ label — hidden until hover/focus, then a chip overhanging the rail
   to the left (the rail hugs the reading column's right edge, so there is no
   room to its right). */
.diff-marker::after {
  content: attr(data-marker-label);
  position: absolute;
  right: calc(100% + var(--space-1));
  top: 50%;
  transform: translateY(-50%);
  min-width: 16px;
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  text-align: center;
  background: var(--diff-marker-chip-bg, transparent);
  color: currentColor;
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--duration-base);
}

.diff-marker:hover::after,
.diff-marker:focus-visible::after {
  opacity: 1;
}

/* ─── Marker type colours (rail + chip share the type accent) ─── */

.diff-marker-modified {
  color: var(--diff-mod-accent);
  --diff-marker-chip-bg: var(--diff-mod-bg);
}

.diff-marker-deleted {
  color: var(--diff-del-accent);
  --diff-marker-chip-bg: var(--diff-del-bg);
}

.diff-marker-added {
  color: var(--diff-add-accent);
  --diff-marker-chip-bg: var(--diff-add-bg);
}
```

把 `web/src/assets/code-viewer.css:88-95` 的 `.diff-marker-inline` 块改为（**宽度保持 20px**——它是**命中区**；细轨由 `diff-marker.css` 的 `::before` 画在它的右缘）：

```css
/* Diff marker inline structural positioning (visual styles in diff-marker.css).
   The element is the HIT AREA, kept 20px wide so a thin rail stays comfortably
   tappable; the visible 3px rail is drawn by .diff-marker::before at this
   element's right edge (the reading column's right edge). Do NOT shrink this to
   3px — that would make the marker nearly unhittable. */
.diff-marker-inline {
    position: absolute;
    right: 0;
    width: 20px;
    height: 100%;
    z-index: 2;
}
```

**Step 4: 更新 `MarkdownPreview.vue` 的覆盖**

`web/src/components/file/__tests__/MarkdownPreviewWideLayout.css.test.ts` 的第三个用例（`positions diff markers at the reading column right edge`）只断言 `right:`，不受影响。

`MarkdownPreview.vue` 的全局 `<style>`（非 scoped）块里有一条 `.markdown-preview .markdown-body .diff-marker-inline { width: 20px; }`——**宽度保持 20px 不变**（它是命中区，见上），只需把注释里的「height:100% from CodePreview」说明更新为反映新结构（细轨在 `::before`）。该块改为：

```css
.markdown-preview .markdown-body .diff-marker-inline {
    position: absolute;
    /* Keep markers at the right edge of the reading column. The capped
       .markdown-body used to be centered with `margin: 0 auto`, so a marker at
       right:0 sat at the element border — i.e. half the slack (W−900)/2 in from
       the screen edge. Now the element is full-width (padding-based cap), so the
       same visual spot is `right: max(0px, (100% − 900px)/2)`.

       Width stays 20px: this element is the HIT AREA. The visible 3px rail is
       drawn by .diff-marker::before at its right edge. */
    right: max(0px, (100% - 900px) / 2);
    width: 20px;
    height: auto;
    z-index: 2;
}
```

同时把模板里的 marker 按钮补上 `data-marker-label`（供 `::after` 的 `content: attr()` 取用），改 `MarkdownPreview.vue:7-17`：

```html
      <button
        v-for="pm in positionedMarkers"
        :key="pm.id"
        class="diff-marker diff-marker-inline"
        :class="`diff-marker-${pm.type}`"
        :style="{ top: pm.top + 'px', height: pm.height + 'px' }"
        :data-marker-id="pm.id"
        :data-marker-label="pm.label"
        role="button"
        tabindex="0"
        :aria-label="pm.ariaLabel"
      ></button>
```

> **注意**：`data-marker-label` 是 `attr()` 的取用来源，**不要**删。按钮内不再有文本节点（标签改由 `::after` 渲染），所以 `{{ pm.label }}` 被移除。

**Step 5: 跑测试确认通过**

Run: `npx vitest run web/src/components/file/__tests__/diffMarkerRail.css.test.ts web/src/components/file/__tests__/MarkdownPreviewWideLayout.css.test.ts`
Expected: PASS。

**Step 6: 提交**

```bash
git add web/src/assets/diff-marker.css web/src/assets/code-viewer.css \
  web/src/components/file/MarkdownPreview.vue \
  web/src/components/file/__tests__/diffMarkerRail.css.test.ts
git commit -m "style(file): 变更标记由实心色块改为主题化 3px 细轨"
```

---

## Task 4: 变更导航胶囊

**Files:**
- Create: `web/src/components/file/FileChangeNav.vue`
- Create: `web/src/composables/useChangeNav.ts`
- Modify: `web/src/components/file/MarkdownPreview.vue`（挂载 + 滚动定位）
- Modify: `web/src/components/file/CodeMirrorViewer.vue`（挂载 + 复用 scrollToLine）
- Test: `web/src/components/file/__tests__/FileChangeNav.test.ts`
- Test: `web/src/composables/__tests__/useChangeNav.test.ts`

**Step 1: 写失败的测试（导航状态）**

创建 `web/src/composables/__tests__/useChangeNav.test.ts`：

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useChangeNav } from '@/composables/useChangeNav.ts'

describe('useChangeNav', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('starts at index 0 and reports count from targets', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    expect(nav.count.value).toBe(3)
    expect(nav.index.value).toBe(0)
  })

  it('next advances and calls scrollTo with the new index', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, scrollTo)
    nav.next()
    expect(nav.index.value).toBe(1)
    expect(scrollTo).toHaveBeenCalledWith(1)
  })

  it('next does not advance past the last target', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300])
    const nav = useChangeNav(targets, scrollTo)
    nav.next()
    nav.next()
    expect(nav.index.value).toBe(1)
    expect(scrollTo).toHaveBeenCalledTimes(1)
  })

  it('prev does not go below 0', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300])
    const nav = useChangeNav(targets, scrollTo)
    nav.prev()
    expect(nav.index.value).toBe(0)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('clamps index when targets shrink', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    nav.next()
    nav.next()
    expect(nav.index.value).toBe(2)
    targets.value = [100]
    await nextTick()
    expect(nav.index.value).toBe(0)
  })

  it('syncIndexFromScroll picks the last target at or above the position', () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    nav.syncIndexFromScroll(350)
    expect(nav.index.value).toBe(1)
    nav.syncIndexFromScroll(50)
    expect(nav.index.value).toBe(0)
    nav.syncIndexFromScroll(9999)
    expect(nav.index.value).toBe(2)
  })
})
```

**Step 2: 跑测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useChangeNav.test.ts`
Expected: FAIL — 模块不存在。

**Step 3: 实现 `useChangeNav`**

创建 `web/src/composables/useChangeNav.ts`：

```ts
/**
 * useChangeNav — prev/next navigation over a list of change positions.
 *
 * Surface-agnostic: the caller supplies the ordered target positions (markdown:
 * block tops in px; code: line numbers) and a scrollTo callback. Index state,
 * clamping and scroll-derived highlighting live here so both preview surfaces
 * behave identically.
 */
import { ref, watch, type Ref } from 'vue'

export function useChangeNav(targets: Ref<number[]>, scrollTo: (index: number) => void) {
  const index = ref(0)
  const count = ref(targets.value.length)

  watch(targets, (list) => {
    count.value = list.length
    if (index.value >= list.length) index.value = Math.max(0, list.length - 1)
  }, { deep: true })

  function next() {
    if (index.value >= targets.value.length - 1) return
    index.value += 1
    scrollTo(index.value)
  }

  function prev() {
    if (index.value <= 0) return
    index.value -= 1
    scrollTo(index.value)
  }

  /** Highlight the change whose position is the last one at or above `pos`. */
  function syncIndexFromScroll(pos: number) {
    const list = targets.value
    if (list.length === 0) return
    let found = 0
    for (let i = 0; i < list.length; i++) {
      if (list[i] <= pos) found = i
      else break
    }
    index.value = found
  }

  return { index, count, next, prev, syncIndexFromScroll }
}
```

**Step 4: 跑测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useChangeNav.test.ts`
Expected: PASS（6 个用例）。

**Step 5: 写失败的测试（导航组件）**

创建 `web/src/components/file/__tests__/FileChangeNav.test.ts`：

```ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import FileChangeNav from '../FileChangeNav.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string, p?: any) => (p ? `${k}:${JSON.stringify(p)}` : k) }),
}))

describe('FileChangeNav', () => {
  it('renders nothing when there are no changes', () => {
    const w = mount(FileChangeNav, { props: { count: 0, index: 0 } })
    expect(w.find('.file-change-nav').exists()).toBe(false)
  })

  it('shows the change count and position', () => {
    const w = mount(FileChangeNav, { props: { count: 3, index: 1 } })
    expect(w.find('.file-change-nav').exists()).toBe(true)
    expect(w.text()).toContain('file.changeNav.count')
    expect(w.text()).toContain('2/3')
  })

  it('emits prev / next / clear', async () => {
    const w = mount(FileChangeNav, { props: { count: 3, index: 1 } })
    await w.find('.fcn-btn-prev').trigger('click')
    await w.find('.fcn-btn-next').trigger('click')
    await w.find('.fcn-btn-clear').trigger('click')
    expect(w.emitted('prev')).toHaveLength(1)
    expect(w.emitted('next')).toHaveLength(1)
    expect(w.emitted('clear')).toHaveLength(1)
  })

  it('disables prev at the first change and next at the last', () => {
    const first = mount(FileChangeNav, { props: { count: 3, index: 0 } })
    expect((first.find('.fcn-btn-prev').element as HTMLButtonElement).disabled).toBe(true)
    const last = mount(FileChangeNav, { props: { count: 3, index: 2 } })
    expect((last.find('.fcn-btn-next').element as HTMLButtonElement).disabled).toBe(true)
  })
})
```

**Step 6: 跑测试确认失败**

Run: `npx vitest run web/src/components/file/__tests__/FileChangeNav.test.ts`
Expected: FAIL — 组件不存在。

**Step 7: 实现 `FileChangeNav.vue`**

创建 `web/src/components/file/FileChangeNav.vue`：

```vue
<template>
  <div v-if="count > 0" class="file-change-nav">
    <span class="fcn-label">{{ t('file.changeNav.count', { count }) }}</span>
    <span v-if="count > 1" class="fcn-pos">{{ index + 1 }}/{{ count }}</span>
    <button
      class="fcn-btn fcn-btn-prev"
      type="button"
      :disabled="index <= 0"
      :title="t('file.changeNav.prev')"
      :aria-label="t('file.changeNav.prev')"
      @click.stop="emit('prev')"
    >
      <ChevronUp :size="14" />
    </button>
    <button
      class="fcn-btn fcn-btn-next"
      type="button"
      :disabled="index >= count - 1"
      :title="t('file.changeNav.next')"
      :aria-label="t('file.changeNav.next')"
      @click.stop="emit('next')"
    >
      <ChevronDown :size="14" />
    </button>
    <button
      class="fcn-btn fcn-btn-clear"
      type="button"
      :title="t('file.changeNav.clear')"
      :aria-label="t('file.changeNav.clear')"
      @click.stop="emit('clear')"
    >
      <X :size="14" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ChevronUp, ChevronDown, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

defineProps<{ count: number; index: number }>()
const emit = defineEmits(['prev', 'next', 'clear'])
const { t } = useI18n()
</script>

<style scoped>
/* Floating pill at the top-right of the content area. Sits above the preview
   so it stays put while the content scrolls beneath it. */
.file-change-nav {
  position: absolute;
  top: var(--space-4);
  right: var(--space-6);
  z-index: 3;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-md);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  user-select: none;
}

.fcn-label {
  white-space: nowrap;
}

.fcn-pos {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.fcn-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .fcn-btn:hover:not(:disabled) {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}

.fcn-btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
}

.fcn-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}
</style>
```

**Step 8: 跑测试确认通过**

Run: `npx vitest run web/src/components/file/__tests__/FileChangeNav.test.ts`
Expected: PASS（4 个用例）。

**Step 9: 挂到 MarkdownPreview**

在 `MarkdownPreview.vue` 模板里 `.markdown-body` 之后、`.markdown-preview` 之内加：

```html
    <FileChangeNav
      :count="changeNav.count.value"
      :index="changeNav.index.value"
      @prev="changeNav.prev"
      @next="changeNav.next"
      @clear="clearChanges"
    />
```

`<script setup>` 里加 import 与逻辑：

```ts
import FileChangeNav from '@/components/file/FileChangeNav.vue'
import { useChangeNav } from '@/composables/useChangeNav.ts'
import { clearAllBaselines, clearBaseline } from '@/composables/useFileChangeBaseline.ts'
import { syncMarkersFor } from '@/composables/useFileRefresh.ts'
```

**先补 import**：`MarkdownPreview.vue:42` 现在只 import 了 `{ ref, watch, nextTick, onBeforeUnmount }`，本任务需要 `computed` 和 `onMounted`，改为：

```ts
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
```

在 `positionedMarkers` 定义之后加：

```ts
// Change navigation over the positioned marker tops.
const markerTops = computed(() => positionedMarkers.value.map(m => m.top))
const changeNav = useChangeNav(markerTops, (i) => {
    const el = bodyRef.value
    const target = positionedMarkers.value[i]
    if (!el || !target) return
    el.scrollTo({ top: Math.max(0, target.top - 16), behavior: 'auto' })
})

function clearChanges() {
    const path = props.file?.path
    if (path) clearBaseline(path)
    clearDiffMarkers()
    positionedMarkers.value = []
}
```

在 `computeMarkerPositions()` 末尾（`positionedMarkers.value = markers` 之后）加一行同步索引：

```ts
    // Keep the nav index aligned with the marker list after a recompute.
    const el = bodyRef.value
    if (el) changeNav.syncIndexFromScroll(el.scrollTop + 16)
```

在 `onMounted` / 现有 `bodyRef` 就绪后，给 `.markdown-body` 挂 scroll 监听，并**恢复标记**（跨文件保留的关键）：

```ts
let scrollEl: HTMLElement | null = null
function onBodyScroll() {
    if (scrollEl) changeNav.syncIndexFromScroll(scrollEl.scrollTop + 16)
}
onMounted(() => {
    scrollEl = bodyRef.value
    scrollEl?.addEventListener('scroll', onBodyScroll, { passive: true })
    // Restore markers for this file from the accumulated baseline (the user may
    // have navigated away and back). syncMarkersFor only derives — it will not
    // create a baseline, so opening a never-changed file stays clean.
    const f = props.file
    if (f?.path && f.content) syncMarkersFor(f.path, 'markdown', f.content)
})
onBeforeUnmount(() => {
    scrollEl?.removeEventListener('scroll', onBodyScroll)
})
```

> **注意**：`onMounted` 时 `bodyRef` 可能还没挂（`v-if` 依赖渲染）。若实测拿不到元素，改为在 `doRender` 成功后的 `nextTick` 里首次挂监听（该处 `bodyRef.value` 一定就绪）。恢复调用 `syncMarkersFor` 不受此影响——它不依赖 DOM。

> **注意**：`onBeforeUnmount` 里现有的 `clearDiffMarkers()`（`MarkdownPreview.vue:455-457`）**要删掉**——它会在切走时清空标记，与「跨文件保留」冲突。改为只清 `positionedMarkers.value = []`。

**Step 10: 挂到 CodeMirrorViewer**

在 `CodeMirrorViewer.vue` 模板最外层容器内加：

```html
    <FileChangeNav
      v-if="!editable"
      :count="changeNav.count.value"
      :index="changeNav.index.value"
      @prev="changeNav.prev"
      @next="changeNav.next"
      @clear="clearChanges"
    />
```

`<script setup>` 加：

```ts
import FileChangeNav from '@/components/file/FileChangeNav.vue'
import { useChangeNav } from '@/composables/useChangeNav.ts'
import { clearBaseline } from '@/composables/useFileChangeBaseline.ts'
```

在 `diffLineMap` 定义之后加。**先补 import**：`CodeMirrorViewer.vue:27` 现在 import 的是 `{ ref, shallowRef, watch, onMounted, onUnmounted }`，需要补 `computed`：

```ts
import { ref, shallowRef, computed, watch, onMounted, onUnmounted } from 'vue'
```

```ts
// Change navigation over the first line of each marker.
const markerLines = computed(() =>
    diffMarkers.value.map(m => m.lineNumbers?.[0] ?? 1).sort((a, b) => a - b)
)
const changeNav = useChangeNav(markerLines, (i) => {
    const line = markerLines.value[i]
    if (line) scrollToLine(line)
})

function clearChanges() {
    const path = props.file?.path
    if (path) clearBaseline(path)
    clearDiffMarkers()
}
```

并在 `onMounted` 里恢复该文件的标记（跨文件 / 切 surface 保留）：

```ts
import { syncMarkersFor } from '@/composables/useFileRefresh.ts'
// 在既有 onMounted 回调内补：
onMounted(() => {
    // ...既有逻辑...
    const f = props.file
    if (f?.path && f.content) syncMarkersFor(f.path, 'code', f.content)
})
```

> **注意**：`CodeMirrorViewer` 在渲染面 / 源码面切换时是**重新挂载**的（`FileViewer.vue` 用 `v-if` 互斥），所以 `onMounted` 恢复这一条同时覆盖了「切 surface」与「切文件再切回」两条路径。

> **注意**：`scrollToLine` 在 `CodeMirrorViewer.vue:297` 已定义，带 `line-flash` 闪烁。

**Step 11: 跑相关测试**

Run: `npx vitest run web/src/components/file/__tests__/FileChangeNav.test.ts web/src/composables/__tests__/useChangeNav.test.ts web/src/components/file/__tests__/MarkdownPreview.test.ts web/src/components/file/__tests__/CodeMirrorViewer.test.ts`
Expected: PASS。既有组件测试若因新增子组件或移除 `clearDiffMarkers` 失败，按其断言更新（**不要**放宽）。

**Step 12: 提交**

```bash
git add web/src/components/file/FileChangeNav.vue web/src/composables/useChangeNav.ts \
  web/src/components/file/MarkdownPreview.vue web/src/components/file/CodeMirrorViewer.vue \
  web/src/components/file/__tests__/FileChangeNav.test.ts \
  web/src/composables/__tests__/useChangeNav.test.ts
git commit -m "feat(file): 变更导航胶囊（Markdown + 代码共用）"
```

---

## Task 5: i18n 键

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`（`file:` 命名空间内，`viewer:` 附近）
- Modify: `web/src/i18n/locales/en.ts`

**Step 1: 加键**

在 `zh.ts` 的 `file:` 块内（`viewer:` 之前或之后均可）加：

```ts
    changeNav: {
      count: '{count} 处变更',
      prev: '上一处变更',
      next: '下一处变更',
      clear: '清除变更标记',
    },
```

在 `en.ts` 的对应位置加：

```ts
    changeNav: {
      count: '{count} changes',
      prev: 'Previous change',
      next: 'Next change',
      clear: 'Clear change markers',
    },
```

> **注意**：**不要**用 vue-i18n 的复数语法（`'a | b'`）。本仓库的 `createI18n`（`web/src/i18n/index.ts:27`）**未配置 `pluralizationRules`**，且全仓无一处用 `|` 复数形式——`|` 在本仓库只作普通字符（如 `settings.cacheStatus` 的 `'模式: {mode} | 更新: {updatedAt}'`）。用了会原样渲染出竖线。

**Step 2: 跑 i18n 守卫**

Run: `npx vitest run web/src/i18n/__tests__/literalKeys.test.ts`
Expected: PASS（无裸键）。该守卫会扫描全树，确认 `file.changeNav.*` 在 zh / en 两侧都存在。

**Step 3: 提交**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "i18n(file): 变更导航文案"
```

---

## Task 6: 构建与自测

**Step 1: 类型检查与 lint（改动文件）**

Run: `npx vue-tsc --noEmit -p web/tsconfig.json`（若项目有此脚本，否则用仓库既定的 lint 命令）
Run: `npx eslint web/src/composables/useFileChangeBaseline.ts web/src/composables/useChangeNav.ts web/src/components/file/FileChangeNav.vue`

**Step 2: 跑本任务全部相关测试**

Run:
```bash
npx vitest run \
  web/src/composables/__tests__/useFileChangeBaseline.test.ts \
  web/src/composables/__tests__/useChangeNav.test.ts \
  web/src/composables/__tests__/useFileRefresh.test.ts \
  web/src/composables/__tests__/useMarkdownDiff.test.ts \
  web/src/components/file/__tests__/FileChangeNav.test.ts \
  web/src/components/file/__tests__/diffMarkerRail.css.test.ts \
  web/src/components/file/__tests__/MarkdownPreviewWideLayout.css.test.ts \
  web/src/components/file/__tests__/MarkdownPreview.test.ts \
  web/src/components/file/__tests__/CodeMirrorViewer.test.ts
```
Expected: 全 PASS。**不要**跑全量 vitest（并发争抢）。

**Step 3: 构建（纯前端改动必须做）**

Run: `npm run build`（仓库根目录）
Expected: 成功。产物在 `.clawbench-web/`，disk 模式立即生效、无需重启。

**Step 4: 产物核验**

Run:
```bash
# 细轨已落地：产物里 diff-marker 规则应含 --diff-mod-accent，且无 rgba(255,165,0
grep -l "diff-mod-accent" .clawbench-web/assets/*.css | head
grep -rl "rgba(255,165,0" .clawbench-web/assets/*.css | head
```
Expected: 前者有命中；后者**无**命中。

> **注意**：`emptyOutDir:false` 会积累多份同名 chunk。要判断「实际加载的那份」，从 `.clawbench-web/index.html` 出发取引用（见 memory「frontend_build_pitfalls」）。

**Step 5: 手动确认**

让用户在浏览器 / App 里：
1. 打开一个 Markdown 文件，从外部（终端 / 编辑器）改它 → 右侧应出现细轨，hover 显示 `M/D/+` chip。
2. 切到源码视图再切回渲染 → 标记仍在。
3. 切到另一个文件再切回 → 标记仍在。
4. 右上角胶囊点 ◀ ▶ 应跳转并闪烁；✕ 清除。

**Step 6: 提交（如有构建产物变更）**

> `.clawbench-web/` 通常不入库（检查 `.gitignore`）。若未入库，本步跳过。

---

## 验收清单

- [ ] `useFileChangeBaseline` 8 个用例全过（累计 / 回基线删除 / LRU / 超限降级）
- [ ] `syncMarkersFor` 三处触发点接线（外部刷新 / 切 surface / 切文件）
- [ ] Markdown 面细轨用 `--diff-*-accent`，无硬编码色、无 `@keyframes`
- [ ] 导航胶囊在 `count === 0` 时不渲染；◀▶ 越界禁用；✕ 清基线
- [ ] i18n 无裸键
- [ ] `npm run build` 成功，产物含 `--diff-mod-accent`、不含旧 rgba
- [ ] 浅色 + 深色主题各看一眼（design-guide 检查清单）
- [ ] 未跑全量 vitest、未与他人争抢
