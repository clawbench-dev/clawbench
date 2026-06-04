<template>
  <pre class="raw-content-pre" :class="{ 'word-wrap': wordWrap, 'no-line-num': !showLineNumbers }" ref="codeRef" :data-file-path="filePath" :data-language="language" @click="handleClick">
    <div v-if="stickyLines.length > 0 && !wordWrap" class="sticky-scroll-overlay" :ref="onOverlayMounted">
      <div v-for="s in stickyLines" :key="s.lineNum" class="sticky-line"
        :data-line="s.lineNum" :style="{ top: s.top * stickyLineHeight + 'px' }"
        @click="handleStickyClick(s.lineNum)">
        <span v-if="showLineNumbers" class="sticky-line-num">{{ s.lineNum }}</span>
        <span class="sticky-code-text" v-html="getStickyLineHtml(s.lineNum)" />
      </div>
    </div>
    <code v-html="codeHtml" />
  </pre>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { hljs } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useStickyScroll } from '@/composables/useStickyScroll.ts'

const props = defineProps({
    /** Raw file content */
    content: { type: String, default: '' },
    /** Language for syntax highlighting */
    language: { type: String, default: 'plaintext' },
    /** File path for quote-question feature */
    filePath: { type: String, default: null },
    /** Enable word wrap */
    wordWrap: { type: Boolean, default: false },
    /** Show line numbers */
    showLineNumbers: { type: Boolean, default: true },
    /** Character ranges to flash-highlight */
    flashRanges: { type: Array, default: () => [] },
    /** Flash type: 'delete' (red) or 'add' (blue) */
    flashType: { type: String, default: 'add' },
    /** Enable VS Code-style sticky scroll */
    stickyScroll: { type: Boolean, default: true },
})

const codeHtml = ref('')
const codeRef = ref(null)

const quoteQuestion = useQuoteQuestion()

// Sticky scroll
const { stickyLines, initSticky, teardownSticky, invalidateCache, setOverlayEl } = useStickyScroll()
const stickyLineHeight = 20.8  // matches code line height (13px * 1.6)
const lineHtmlCache = new Map()

function onOverlayMounted(el) {
    setOverlayEl(el)
}

function getStickyLineHtml(lineNum) {
    if (lineHtmlCache.has(lineNum)) return lineHtmlCache.get(lineNum)
    const lineEls = codeRef.value?.querySelectorAll(':scope > code > .code-line')
    if (!lineEls) return ''
    const el = lineEls[lineNum - 1]
    if (!el) return ''
    const codeText = el.querySelector('.code-text')
    const html = codeText?.innerHTML || ''
    lineHtmlCache.set(lineNum, html)
    return html
}

function handleStickyClick(lineNum) {
    const lineEls = codeRef.value?.querySelectorAll(':scope > code > .code-line')
    if (!lineEls) return
    const el = lineEls[lineNum - 1]
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    // Flash animation
    el.classList.add('line-flash')
    setTimeout(() => el.classList.remove('line-flash'), 1200)
}

const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy(target, text) {
        // 从 DOM 读取文件信息和行号
        const lineEl = target && 'closest' in target ? target.closest('.code-line') : null
        const preEl = lineEl?.closest('.raw-content-pre')
        const filePath = preEl?.getAttribute('data-file-path') || ''
        const language = preEl?.getAttribute('data-language') || ''
        const lineNum = lineEl ? parseInt(lineEl.getAttribute('data-line') || '0') : 0

        quoteQuestion.showBar({
            text,
            filePath,
            language,
            startLine: lineNum,
            endLine: lineNum,
        })
    },
})

function handleClick(event) {
    handleDblClick(event)
}

/**
 * Wrap flash ranges in highlighted spans within a line's raw text.
 * Strategy: split the raw line into flash/non-flash segments,
 * escape flash segments directly (no syntax highlighting — the flash
 * animation background makes them visually distinct enough),
 * and syntax-highlight non-flash segments individually.
 */
function applyFlashToLine(rawLine, lineRanges, flashCls) {
    // Sort ranges by start offset
    const sorted = [...lineRanges].sort((a, b) => a.start - b.start)

    // Build segments: [{text, flash: boolean}]
    const segments = []
    let pos = 0
    for (const r of sorted) {
        const start = Math.min(r.start, rawLine.length)
        const end = Math.min(r.end, rawLine.length)
        if (start > pos) {
            segments.push({ text: rawLine.slice(pos, start), flash: false })
        }
        if (end > start) {
            segments.push({ text: rawLine.slice(start, end), flash: true })
        }
        pos = end
    }
    if (pos < rawLine.length) {
        segments.push({ text: rawLine.slice(pos), flash: false })
    }

    // Build HTML: flash segments get escaped + wrapped in span,
    // non-flash segments get syntax-highlighted
    let result = ''
    for (const seg of segments) {
        if (seg.flash) {
            result += `<span class="${flashCls}">${escapeHtml(seg.text)}</span>`
        } else {
            try {
                result += hljs.highlight(seg.text, { language: props.language, ignoreIllegals: true }).value
            } catch {
                result += escapeHtml(seg.text)
            }
        }
    }
    return result
}

function renderCode(content, lang, showNums) {
    const flashMap = new Map() // lineNum (1-based) → FlashRange[]
    if (props.flashRanges && props.flashRanges.length > 0) {
        for (const r of props.flashRanges) {
            if (!flashMap.has(r.line)) flashMap.set(r.line, [])
            flashMap.get(r.line).push(r)
        }
    }
    const flashCls = props.flashType === 'delete' ? 'char-flash-delete' : 'char-flash-add'

    return content.split('\n').map((rawLine, i) => {
        const lineNum = i + 1
        const lineRanges = flashMap.get(lineNum)
        let h

        if (lineRanges) {
            h = applyFlashToLine(rawLine, lineRanges, flashCls)
        } else {
            try { h = hljs.highlight(rawLine, { language: lang, ignoreIllegals: true }).value } catch { h = escapeHtml(rawLine) }
            h = h.replace(/^<span class="line">/, '').replace(/<\/span>\s*$/, '')
        }

        const lineNumHtml = showNums ? `<span class="line-num">${lineNum}</span>` : ''
        return `<div class="code-line" data-line="${lineNum}">${lineNumHtml}<span class="code-text">${h}</span></div>`
    }).join('')
}

function doRender(content) {
    if (!content) return
    codeHtml.value = renderCode(content, props.language, props.showLineNumbers)
    lineHtmlCache.clear()
    invalidateCache()
    nextTick(() => {
        if (props.stickyScroll && props.filePath && codeRef.value) {
            initSticky(props.filePath, codeRef.value)
        }
    })
}

watch(
    [() => props.content, () => props.showLineNumbers, () => props.flashRanges, () => props.flashType, () => props.stickyScroll],
    () => doRender(props.content),
    { immediate: true }
)
</script>

<style scoped>
pre {
    user-select: text;
    min-height: 0;
}
pre :deep(code) {
    min-height: 0;
}

/* Raw content pre - code display area */
.raw-content-pre {
    margin: 0;
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: var(--code-bg);
    border: none;
    font-size: 13px;
    line-height: 1.6;
    tab-size: 4;
}

.raw-content-pre :deep(code) {
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    background: transparent;
    padding: 0;
    font-size: inherit;
    white-space: pre;
    display: block;
    min-width: max-content;
}

.raw-content-pre :deep(code .code-line) {
    display: flex;
    align-items: start;
}

.raw-content-pre :deep(code .line-num) {
    position: sticky;
    left: 0;
    display: inline-block;
    min-width: 48px;
    padding-right: 12px;
    margin-right: 0;
    color: var(--text-muted);
    text-align: right;
    user-select: none;
    cursor: pointer;
    border-right: 1px solid var(--border-color);
    opacity: 0.5;
    transition: opacity 0.15s, color 0.15s;
    font-size: inherit;
    line-height: inherit;
    background: var(--code-bg);
}

.raw-content-pre :deep(code .code-text) {
    white-space: pre;
    padding-left: 12px;
    padding-right: 8px;
}

/* Word wrap mode */
.raw-content-pre.word-wrap {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.raw-content-pre.word-wrap :deep(code) {
    white-space: pre-wrap;
    min-width: 0;
    word-break: break-all;
    overflow-wrap: break-word;
}

.raw-content-pre.word-wrap :deep(code .code-text) {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.raw-content-pre.word-wrap :deep(code .code-line) {
    align-items: stretch;
}

.raw-content-pre.word-wrap :deep(code .line-num) {
    position: static;
    border-right: 1px solid var(--border-color);
}

/* Sticky scroll overlay */
.raw-content-pre :deep(.sticky-scroll-overlay) {
    position: sticky;
    top: 0;
    left: 0;
    width: 100%;
    height: 0;
    z-index: 2;
    pointer-events: none;
}

.raw-content-pre :deep(.sticky-line) {
    display: flex;
    position: absolute;
    left: 0;
    right: 0;
    height: 20.8px;
    background: var(--code-bg);
    border-bottom: 1px solid var(--border-color);
    border-left: 2px solid var(--accent-color);
    opacity: 0.92;
    cursor: pointer;
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    pointer-events: auto;
    font-size: 13px;
    line-height: 20.8px;
}

.raw-content-pre :deep(.sticky-line:hover) {
    opacity: 1;
    background: var(--bg-tertiary);
}

.raw-content-pre :deep(.sticky-line-num) {
    min-width: 48px;
    padding-right: 12px;
    text-align: right;
    user-select: none;
    color: var(--text-muted);
    opacity: 0.5;
    font-size: 13px;
    line-height: 20.8px;
    border-right: 1px solid var(--border-color);
    flex-shrink: 0;
}

.raw-content-pre :deep(.sticky-code-text) {
    white-space: pre;
    padding-left: 12px;
    font-size: 13px;
    line-height: 20.8px;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* Disable sticky scroll in word-wrap mode */
.raw-content-pre.word-wrap :deep(.sticky-scroll-overlay) {
    display: none;
}

/* No line numbers mode */
.raw-content-pre.no-line-num :deep(code .code-text) {
    padding-left: 8px;
    padding-right: 8px;
}

.raw-content-pre :deep(code .line-num:hover) {
    opacity: 1;
    color: var(--accent-color);
}
</style>

<style>
/* Line flash animation for TOC jump and search jump */
@keyframes line-flash {
    0%, 100% { background: transparent; }
    10%, 30%  { background: rgba(255, 230, 0, 0.4); }
    20%, 40%  { background: transparent; }
    50%, 70%  { background: rgba(255, 230, 0, 0.3); }
    60%, 80%  { background: transparent; }
    90%       { background: rgba(255, 230, 0, 0.2); }
}
.line-flash {
    animation: line-flash 1.2s ease-out forwards;
}

/* Copy flash animation for block elements — used by useDoubleClickCopy */
@keyframes copy-flash {
    0%, 100% { background: transparent; }
    50%      { background: rgba(255, 230, 0, 0.4); }
}
.copy-flash {
    animation: copy-flash 0.4s ease-out forwards;
    border-radius: 4px;
}

/* ─── Change flash animations (char-level) ─── */

/* Red flash for deleted characters */
@keyframes char-flash-delete-anim {
    0%, 100% { background: transparent; }
    8%, 28%  { background: rgba(255, 80, 80, 0.45); }
    18%, 38% { background: transparent; }
    48%, 68% { background: rgba(255, 80, 80, 0.3); }
    58%, 78% { background: transparent; }
    88%      { background: rgba(255, 80, 80, 0.15); }
}
.char-flash-delete {
    animation: char-flash-delete-anim 1.2s ease-out forwards;
    border-radius: 2px;
    text-decoration: line-through;
    text-decoration-color: rgba(255, 80, 80, 0.6);
}

/* Blue flash for added characters */
@keyframes char-flash-add-anim {
    0%, 100% { background: transparent; }
    8%, 28%  { background: rgba(100, 200, 255, 0.45); }
    18%, 38% { background: transparent; }
    48%, 68% { background: rgba(100, 200, 255, 0.3); }
    58%, 78% { background: transparent; }
    88%      { background: rgba(100, 200, 255, 0.15); }
}
.char-flash-add {
    animation: char-flash-add-anim 1.5s ease-out forwards;
    border-radius: 2px;
}
</style>
