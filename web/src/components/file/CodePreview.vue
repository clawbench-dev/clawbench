<template>
  <div class="code-preview-wrapper">
    <pre class="raw-content-pre" :class="{ 'word-wrap': wordWrap, 'no-line-num': !showLineNumbers }" ref="codeRef" :data-file-path="filePath" :data-language="language" @click="handleClick">
      <div v-if="stickyLines.length > 0" class="sticky-scroll-overlay">
        <div v-for="s in stickyLines" :key="s.lineNum" class="sticky-line"
          :data-line="s.lineNum" :style="{ top: s.top + 'px', height: s.height + 'px' }"
          @click="handleStickyClick(s.lineNum)">
          <span v-if="showLineNumbers" class="sticky-line-num">{{ s.lineNum }}</span>
          <span class="sticky-code-text" v-html="getStickyLineHtml(s.lineNum)" />
        </div>
      </div>
      <code v-html="codeHtml" />
    </pre>
  </div>
</template>

<script setup>
import { ref, watch, nextTick, computed, onBeforeUnmount } from 'vue'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useStickyScroll } from '@/composables/useStickyScroll.ts'
import { renderCodeLines } from '@/utils/codeRender.ts'
import { tryResolveCodeString, stripCodeString, verifyFilePaths } from '@/composables/useFilePathAnnotation.ts'
import { escapeHtml } from '@/utils/html.ts'
import { store } from '@/stores/app.ts'
import {
  diffMarkers,
  clearDiffMarkers,
} from '@/composables/useMarkdownDiff.ts'
import { handleDiffMarkerClick } from '@/composables/useDiffMarkerClick.ts'
import '@/assets/diff-marker.css'

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

const emit = defineEmits(['openFile'])

const codeHtml = ref('')
const codeRef = ref(null)

const quoteQuestion = useQuoteQuestion()

// Sticky scroll
const { stickyLines, initSticky, teardownSticky, invalidateCache } = useStickyScroll()
const lineHtmlCache = new Map()

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

    // Calculate sticky zone height to scroll target below it (avoid occlusion)
    let stickyHeight = 0
    for (const s of stickyLines.value) {
        stickyHeight += s.height
    }

    const containerTop = codeRef.value.getBoundingClientRect().top
    const lineTop = el.getBoundingClientRect().top
    const scrollDelta = lineTop - containerTop - stickyHeight
    codeRef.value.scrollBy({ top: scrollDelta, behavior: 'smooth' })

    // Flash animation
    el.classList.add('line-flash')
    el.addEventListener('animationend', () => el.classList.remove('line-flash'), { once: true })
}

const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy(target, text) {
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
    // Check for diff marker click first
    if (handleDiffMarkerClick(event, '.diff-marker-inline')) return

    // Intercept clicks on annotated file-path spans in code
    const pathEl = event.target.closest('.code-file-path')
    if (pathEl) {
        event.preventDefault()
        event.stopPropagation()
        const filePath = pathEl.getAttribute('data-file-path')
        const lineStart = pathEl.getAttribute('data-line-start')
        const lineEnd = pathEl.getAttribute('data-line-end')
        if (filePath) {
            emit('openFile', { path: filePath, lineStart: lineStart ? parseInt(lineStart, 10) : undefined, lineEnd: lineEnd ? parseInt(lineEnd, 10) : undefined })
        }
        return
    }
    handleDblClick(event)
}

function annotateFilePaths() {
    if (!codeRef.value) return
    const projectRoot = store.state.projectRoot
    const homeDir = store.state.homeDir
    // Use file's own directory as baseDir for relative path resolution
    const baseDir = props.filePath ? props.filePath.substring(0, props.filePath.lastIndexOf('/')) : undefined
    const detectedPaths = []

    for (const span of codeRef.value.querySelectorAll('.hljs-string')) {
        // Skip already-annotated spans
        if (span.querySelector('.code-file-path')) continue

        const text = span.textContent || ''
        const result = tryResolveCodeString(text, projectRoot, homeDir, baseDir)
        if (!result) continue

        // Get the stripped path text for HTML replacement
        const stripped = stripCodeString(text)
        const isExternal = result.primary.startsWith('/')
        const externalClass = isExternal ? ' external' : ''
        const fallbackAttr = result.fallback !== result.primary ? ` data-fallback-path="${escapeHtml(result.fallback)}"` : ''
        const innerHtml = span.innerHTML
        const escapedPath = stripped.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
        const pathRegex = new RegExp(`(${escapedPath})`)
        if (pathRegex.test(innerHtml)) {
            span.innerHTML = innerHtml.replace(
                pathRegex,
                `<span class="code-file-path${externalClass}" data-file-path="${escapeHtml(result.primary)}"${fallbackAttr}>$1</span>`
            )
            detectedPaths.push(result.primary)
            if (result.fallback !== result.primary) detectedPaths.push(result.fallback)
        }
    }

    // Verify paths asynchronously — removes non-existent annotations
    if (detectedPaths.length > 0) {
        verifyFilePaths(detectedPaths, codeRef.value)
    }
}

/**
 * Build a markerMap from diffMarkers for code rendering.
 * Maps only the first line of each marker group so consecutive changed
 * lines show a single marker instead of per-line overlapping markers.
 */
function buildMarkerMap() {
    const map = new Map()
    for (const marker of diffMarkers.value) {
        if (!marker.lineNumbers || marker.lineNumbers.length === 0) continue
        const info = { type: marker.type, label: marker.label, id: marker.id, lineCount: marker.lineNumbers.length }
        map.set(marker.lineNumbers[0], info)
    }
    return map.size > 0 ? map : undefined
}

function doRender(content) {
    if (!content) return

    // Build flash map for change highlighting
    const flashMap = new Map()
    if (props.flashRanges && props.flashRanges.length > 0) {
        for (const r of props.flashRanges) {
            if (!flashMap.has(r.line)) flashMap.set(r.line, [])
            flashMap.get(r.line).push(r)
        }
    }
    const flashCls = props.flashType === 'delete' ? 'char-flash-delete' : 'char-flash-add'

    // Build marker map from diffMarkers
    const markerMap = buildMarkerMap()

    codeHtml.value = renderCodeLines(
        content,
        props.language,
        props.showLineNumbers,
        flashMap.size > 0 ? flashMap : undefined,
        flashMap.size > 0 ? flashCls : undefined,
        markerMap,
    )
    lineHtmlCache.clear()
    invalidateCache()
    nextTick(() => {
        annotateFilePaths()
        if (props.stickyScroll && props.filePath && codeRef.value) {
            initSticky(props.filePath, codeRef.value)
        } else {
            teardownSticky()
        }
    })
}

watch(
    [() => props.content, () => props.showLineNumbers, () => props.flashRanges, () => props.flashType, () => props.stickyScroll],
    () => doRender(props.content),
    { immediate: true }
)

// Re-render when diff markers change
watch(diffMarkers, () => {
    doRender(props.content)
}, { deep: true })

onBeforeUnmount(() => {
    clearDiffMarkers()
})
</script>

<style scoped>
.code-preview-wrapper {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
}

/* Raw content pre - code display area (CodePreview-specific layout) */
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
    user-select: text;
}

.raw-content-pre code {
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    background: transparent;
    padding: 0;
    font-size: inherit;
    white-space: pre;
    display: block;
    min-width: max-content;
    min-height: 0;
}

/* Word wrap mode */
.raw-content-pre.word-wrap {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.raw-content-pre.word-wrap code {
    white-space: pre-wrap;
    min-width: 0;
    word-break: break-all;
    overflow-wrap: break-word;
}

/* Sticky scroll overlay (CodePreview-specific) */
.raw-content-pre .sticky-scroll-overlay {
    position: sticky;
    top: 0;
    left: 0;
    min-width: max-content;
    height: 0;
    z-index: 2;
    pointer-events: none;
}

.raw-content-pre .sticky-line {
    display: flex;
    align-items: stretch;
    position: absolute;
    left: 0;
    right: 0;
    min-width: max-content;
    background: var(--code-bg);
    border-bottom: 1px solid var(--border-color);
    opacity: 0.92;
    cursor: pointer;
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    pointer-events: auto;
    font-size: 13px;
}

.raw-content-pre .sticky-line:hover {
    opacity: 1;
    background: var(--bg-tertiary);
}

.raw-content-pre .sticky-line-num {
    position: sticky;
    left: 0;
    z-index: 3;
    min-width: 32px;
    padding-right: 6px;
    text-align: right;
    user-select: none;
    color: var(--text-muted);
    opacity: 0.5;
    font-size: 13px;
    line-height: 20.8px;
    border-right: 1px solid var(--border-color);
    background: var(--code-bg);
    flex-shrink: 0;
}

.raw-content-pre .sticky-code-text {
    white-space: pre;
    padding-left: 8px;
    font-size: 13px;
    line-height: 20.8px;
    position: relative;
    z-index: 1;
}

/* Word-wrap mode: sticky lines adapt to wrapped content */
.raw-content-pre.word-wrap .sticky-scroll-overlay {
    min-width: 0;
}

.raw-content-pre.word-wrap .sticky-line {
    min-width: 0;
}

.raw-content-pre.word-wrap .sticky-line-num {
    line-height: normal;
}

.raw-content-pre.word-wrap .sticky-code-text {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
    line-height: normal;
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

/* ─── Diff marker inline (structural only — visual styles in diff-marker.css) ─── */

.diff-marker-inline {
    position: absolute;
    right: 0;
    width: 20px;
    height: 100%;
}

/* Clickable file path in code strings */
.code-file-path {
    cursor: pointer;
    border-bottom: 1px dashed var(--accent-color);
    transition: background 0.15s;
    border-radius: 2px;
}
.code-file-path:hover {
    background: rgba(255, 230, 0, 0.2);
}
/* Project-external file path — orange underline */
.code-file-path.external {
    border-bottom-color: #e67e22;
}
.code-file-path.external:hover {
    background: rgba(230, 126, 34, 0.2);
}
</style>
