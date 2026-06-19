<template>
  <div class="markdown-preview">
    <!-- Rendered markdown -->
    <div v-if="viewMode === 'rendered'" class="markdown-body" ref="bodyRef" :data-file-path="file?.path || ''" @click="handleClick">
      <div class="markdown-content" v-html="renderedHtml" />
      <!-- Diff markers: declarative v-for, positioned absolutely inside .markdown-body -->
      <div
        v-for="pm in positionedMarkers"
        :key="pm.id"
        class="diff-marker-overlay"
        :class="`diff-marker-${pm.type}`"
        :style="{ top: pm.top + 'px', height: pm.height + 'px' }"
        :data-marker-id="pm.id"
      >
        <button
          class="diff-marker-btn"
          :data-marker-id="pm.id"
          role="button"
          tabindex="0"
          :aria-label="pm.ariaLabel"
        >{{ pm.label }}</button>
      </div>
    </div>

    <!-- Raw markdown -->
    <CodePreview
      v-else
      :content="file.content"
      language="markdown"
      :file-path="file.path"
      :word-wrap="wordWrap"
      :show-line-numbers="showLineNumbers"
      :flash-ranges="flashRanges"
      :flash-type="flashType"
      :sticky-scroll="stickyScroll"
    />

    <!-- Diff drawer -->
    <DiffDrawer
      v-if="viewMode === 'rendered'"
      :visible="drawerVisible"
      :marker-type="drawerMarkerType"
      :char-diff="drawerCharDiff"
      :diff-lines="drawerDiffLines"
      @close="closeDrawer"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, computed } from 'vue'
import CodePreview from './CodePreview.vue'
import DiffDrawer from './DiffDrawer.vue'
import { useMarkdownRenderer } from '@/composables/useMarkdownRenderer.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import { dirName, splitPath } from '@/utils/path.ts'
import { flashRanges, flashType } from '@/composables/useFileRefresh.ts'
import {
  diffMarkers,
  diffDrawerVisible,
  diffDrawerMarker,
  openDiffDrawer,
  closeDiffDrawer,
  clearDiffMarkers,
  extractBlocks,
  extractBlockElements,
} from '@/composables/useMarkdownDiff.ts'

const props = defineProps({
    file: Object,
    viewMode: String,
    wordWrap: Boolean,
    showLineNumbers: { type: Boolean, default: true },
    stickyScroll: { type: Boolean, default: true },
})

const renderedHtml = ref('')
const bodyRef = ref(null)
const imageTimestamp = ref(Date.now())
let currentRenderId = 0

// ─── Last block list cache (snapshot before Vue update) ───
const lastBlockList = ref([])

// ─── Positioned markers for v-for rendering ───
interface PositionedMarker {
    id: string
    type: string
    label: string
    ariaLabel: string
    top: number
    height: number
}
const positionedMarkers = ref<PositionedMarker[]>([])

const quoteQuestion = useQuoteQuestion()

const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy(target, text) {
        const lineEl = target && 'closest' in target ? target.closest('.code-line') : null
        if (lineEl) {
            const preEl = lineEl.closest('pre')
            const block = lineEl.closest('.markdown-body')
            const filePath = block?.getAttribute('data-file-path') || props.file?.path || ''
            const language = preEl?.getAttribute('data-language') || ''
            const lineNum = parseInt(lineEl.getAttribute('data-line') || '0')
            quoteQuestion.showBar({
                text,
                filePath,
                language,
                startLine: lineNum,
                endLine: lineNum,
            })
            return
        }
        const block = target && 'closest' in target ? target.closest('.markdown-body') : null
        const filePath = block?.getAttribute('data-file-path') || props.file?.path || ''
        quoteQuestion.showBar({
            text,
            filePath,
            language: '',
            startLine: 0,
            endLine: 0,
        })
    },
})
const { renderMarkdown, renderMermaidInElement } = useMarkdownRenderer()
const { annotateFilePaths, verifyFilePaths, resolveRelativePath, openFilePath } = useFilePathAnnotation()

// ─── Drawer state ───
const drawerVisible = computed(() => diffDrawerVisible.value)
const drawerMarkerType = computed(() => diffDrawerMarker.value?.type || 'modified')
const drawerCharDiff = computed(() => diffDrawerMarker.value?.charDiff || null)
const drawerDiffLines = computed(() => diffDrawerMarker.value?.diffLines)

function closeDrawer() {
    closeDiffDrawer()
}

function handleClick(event) {
    // Check for diff marker click first
    const markerEl = event.target.closest('.diff-marker-overlay')
    if (markerEl) {
        event.preventDefault()
        event.stopPropagation()
        const markerId = markerEl.getAttribute('data-marker-id')
        if (markerId) {
            const marker = diffMarkers.value.find(m => m.id === markerId)
            if (marker) openDiffDrawer(marker)
        }
        return
    }

    // Check for commit-hash click
    const commitEl = event.target.closest('.chat-commit-hash, .chat-commit-open-btn')
    if (commitEl) {
        event.preventDefault()
        event.stopPropagation()
        const sha = commitEl.getAttribute('data-commit-sha')
        if (sha) {
            window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
        }
        return
    }
    // Check for file-open button click
    const btn = event.target.closest('.chat-file-open-btn')
    if (btn) {
        event.preventDefault()
        event.stopPropagation()
        const filePath = btn.getAttribute('data-file-path')
        const lineStart = btn.getAttribute('data-line-start')
        const lineEnd = btn.getAttribute('data-line-end')
        if (filePath) {
            openFilePath(filePath, lineStart ? parseInt(lineStart, 10) : undefined, lineEnd ? parseInt(lineEnd, 10) : undefined)
        }
        return
    }
    // In-page anchor links
    const linkEl = event.target.closest('a[href^="#"]')
    if (linkEl) {
        const href = linkEl.getAttribute('href') || ''
        if (href.length > 1) {
            const targetId = decodeURIComponent(href.slice(1))
            const targetEl = bodyRef.value?.querySelector(`#${CSS.escape(targetId)}`)
            if (targetEl) {
                event.preventDefault()
                event.stopPropagation()
                targetEl.scrollIntoView({ behavior: 'smooth', block: 'start' })
                targetEl.classList.add('line-flash')
                targetEl.addEventListener('animationend', () => targetEl.classList.remove('line-flash'), { once: true })
                return
            }
        }
    }
    handleDblClick(event, (href) => {
        const currentDir = props.file?.path ? dirName(props.file.path) : ''
        const resolvedPath = resolveRelativePath(href, currentDir)
        openFilePath(resolvedPath)
    })
}

function fixLocalImagePaths(html) {
    const currentDir = props.file?.path ? dirName(props.file.path) : ''
    return html.replace(/<img\s+([^>]*src=[^>]*)>/gi, (match, attrs) => {
        const srcMatch = attrs.match(/src="([^"]*)"/)
        if (!srcMatch) return match
        const src = srcMatch[1]
        if (/^(https?:|\/\/|^\/)/i.test(src)) return match
        let resolved = currentDir ? currentDir + '/' + src : src
        try {
            resolved = decodeURIComponent(resolved)
        } catch { /* malformed encoding, use as-is */ }
        const parts = splitPath(resolved)
        const normalized = []
        for (const part of parts) {
            if (part === '.' || part === '') continue
            if (part === '..') { normalized.pop(); continue }
            normalized.push(encodeURIComponent(part))
        }
        return match.replace(`src="${src}"`, `src="/api/local-file/${normalized.join('/')}?t=${imageTimestamp.value}"`)
    })
}

/**
 * Compute marker positions from live DOM.
 * Uses extractBlockElements to get element references directly,
 * then calculates top/height via offsetTop chain relative to .markdown-body.
 */
function computeMarkerPositions() {
    const body = bodyRef.value
    if (!body || diffMarkers.value.length === 0) {
        positionedMarkers.value = []
        return
    }

    const blockEls = extractBlockElements(body.querySelector('.markdown-content') || body)

    const markers: PositionedMarker[] = []
    for (const marker of diffMarkers.value) {
        // Marker id format: "{type}-{blockIndex}-{tag}"
        const idParts = marker.id.split('-')
        const blockIndex = parseInt(idParts[1], 10)

        if (blockIndex < 0 || blockIndex >= blockEls.length) continue

        const blockEl = blockEls[blockIndex].el

        // Calculate top relative to .markdown-body via offsetTop chain
        let top = 0
        let el: Element | null = blockEl
        while (el && el !== body) {
            top += el.offsetTop
            el = el.offsetParent as Element | null
        }

        markers.push({
            id: marker.id,
            type: marker.type,
            label: marker.label,
            ariaLabel: marker.ariaLabel,
            top,
            height: blockEl.offsetHeight,
        })
    }

    positionedMarkers.value = markers
}

async function doRender(f) {
    const renderId = ++currentRenderId
    imageTimestamp.value = Date.now()
    let html = renderMarkdown(f.content, {
        sanitize: false,
        fixImagePaths: fixLocalImagePaths
    })

    const currentDir = f?.path ? dirName(f.path) : ''
    const { html: annotatedHtml, detectedPaths } = annotateFilePaths(html, {
        projectRoot: store.state.projectRoot,
        baseDir: currentDir,
        homeDir: store.state.homeDir
    })
    renderedHtml.value = annotatedHtml

    if (renderId !== currentRenderId) return
    await nextTick()
    if (renderId !== currentRenderId) return
    const el = bodyRef.value
    if (!el) return

    if (detectedPaths.length > 0) {
        const uniquePaths = [...new Set(detectedPaths)]
        verifyFilePaths(uniquePaths, el.querySelector('.markdown-content') || el)
    }

    await renderMermaidInElement(el.querySelector('.markdown-content') || el, 'md-preview')

    // Update last block list cache and compute marker positions after rendering completes
    if (renderId === currentRenderId) {
        lastBlockList.value = extractBlocks(el.querySelector('.markdown-content') || el)
        computeMarkerPositions()
    }
}

watch(() => props.file, (f) => {
    if (!f || f.error) {
        renderedHtml.value = ''
        return
    }
    currentRenderId++
}, { immediate: true })

watch(() => props.file?.content, (content) => {
    if (!content) return
    const f = props.file
    if (!f || f.error) return
    doRender(f)
}, { immediate: true })

watch(() => props.viewMode, async (mode) => {
    if (mode !== 'rendered') return
    const f = props.file
    if (!f || f.error || !f.content) return
    await nextTick()
    const el = bodyRef.value
    if (!el) return
    await renderMermaidInElement(el.querySelector('.markdown-content') || el, 'md-preview')
})

// Watch for marker changes and recompute positions
watch(diffMarkers, () => {
    nextTick(() => computeMarkerPositions())
}, { deep: true })

onBeforeUnmount(() => {
    clearDiffMarkers()
})

// Clear markers when file changes
watch(() => props.file?.path, () => {
    clearDiffMarkers()
    positionedMarkers.value = []
})

// Clear markers when switching to raw mode
watch(() => props.viewMode, () => {
    positionedMarkers.value = []
})

defineExpose({
    lastBlockList,
    bodyRef,
})
</script>

<style scoped>
.markdown-preview {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  position: relative;
}

.markdown-content {
  /* Take up full width, markers overlay on top */
  width: 100%;
}
</style>

<style>
/* ─── Diff marker overlays (inside .markdown-body, declarative v-for) ─── */

/* Overlay: position:absolute relative to .markdown-body, right-aligned.
   Scrolls with content naturally (no JS scroll handler needed). */
.diff-marker-overlay {
  position: absolute;
  right: 0;
  width: 20px;
  min-height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 2;
}

/* Clickable button inside overlay */
.diff-marker-btn {
  width: 100%;
  height: 100%;
  min-height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 3px 0 0 3px;
  cursor: pointer;
  opacity: 0.45;
  transition: opacity 0.15s;
  font-size: 9px;
  font-weight: 700;
  user-select: none;
  color: white;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
  line-height: 1;
  padding: 0;
  font-family: sans-serif;
  pointer-events: auto;
}

.diff-marker-btn:hover,
.diff-marker-btn:focus-visible {
  opacity: 0.85;
  outline: none;
  box-shadow: 0 0 0 2px var(--accent-color);
}

.diff-marker-btn:focus-visible {
  opacity: 1;
}

/* ─── Marker colors + highlight animation ─── */

/* Modified: orange */
.diff-marker-modified .diff-marker-btn {
  background: rgba(255, 165, 0, 0.7);
  animation: diff-marker-highlight 1.5s ease-out;
}

/* Deleted: red */
.diff-marker-deleted .diff-marker-btn {
  background: rgba(255, 80, 80, 0.7);
  animation: diff-marker-highlight 1.5s ease-out;
}

/* Added: green */
.diff-marker-added .diff-marker-btn {
  background: rgba(80, 200, 80, 0.7);
  animation: diff-marker-added-flash 1.5s ease-out;
}

/* Shared highlight animation: brief flash → settle */
@keyframes diff-marker-highlight {
  0% { opacity: 1; }
  100% { opacity: 0.45; }
}

/* Added: blue → green color shift with flash */
@keyframes diff-marker-added-flash {
  0% { background: rgba(100, 200, 255, 0.9); opacity: 1; }
  100% { background: rgba(80, 200, 80, 0.7); opacity: 0.45; }
}

/* Dark theme adjustments */
[data-theme="dark"] .diff-marker-modified .diff-marker-btn {
  background: rgba(255, 165, 0, 0.6);
}
[data-theme="dark"] .diff-marker-deleted .diff-marker-btn {
  background: rgba(255, 80, 80, 0.6);
}
[data-theme="dark"] .diff-marker-added .diff-marker-btn {
  background: rgba(80, 200, 80, 0.6);
}
</style>
