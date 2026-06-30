<template>
  <div class="markdown-preview">
    <!-- Rendered markdown -->
    <div v-if="viewMode === 'rendered'" class="markdown-body" ref="bodyRef" :data-file-path="file?.path || ''" @click="handleClick" @mousedown="onTableMouseDown" @touchstart="onTableTouchStart">
      <div class="markdown-content" v-html="renderedHtml" />
      <!-- Diff markers: declarative v-for, positioned absolutely inside .markdown-body -->
      <button
        v-for="pm in positionedMarkers"
        :key="pm.id"
        class="diff-marker diff-marker-inline"
        :class="`diff-marker-${pm.type}`"
        :style="{ top: pm.top + 'px', height: pm.height + 'px' }"
        :data-marker-id="pm.id"
        role="button"
        tabindex="0"
        :aria-label="pm.ariaLabel"
      >{{ pm.label }}</button>
    </div>

    <!-- Raw markdown -->
    <CodePreview
      v-else
      :content="file?.content ?? ''"
      language="markdown"
      :file-path="file?.path ?? ''"
      :word-wrap="wordWrap"
      :show-line-numbers="showLineNumbers"
      :flash-ranges="flashRanges"
      :flash-type="flashType"
      :sticky-scroll="stickyScroll"
    />
  </div>

  <!-- Table row expand modal -->
  <TableRowModal
    :data="tableRowModal"
    @close="closeTableRowModal"
    @prev="tableRowPrev"
    @next="tableRowNext"
  />
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import CodePreview from './CodePreview.vue'
import { useMarkdownRenderer } from '@/composables/useMarkdownRenderer.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { annotateCodeBlockHeaders, handleCodeBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { store } from '@/stores/app.ts'
import { dirName, splitPath, joinPath } from '@/utils/path.ts'
import { flashRanges, flashType } from '@/composables/useFileRefresh.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import TableRowModal from '@/components/common/TableRowModal.vue'
import {
  diffMarkers,
  clearDiffMarkers,
  extractBlocks,
  extractBlockElements,
  type BlockInfo,
} from '@/composables/useMarkdownDiff.ts'
import { handleDiffMarkerClick } from '@/composables/useDiffMarkerClick.ts'
import '@/assets/diff-marker.css'

const props = defineProps<{
    file?: { content: string; path: string; error?: boolean }
    viewMode?: string
    wordWrap?: boolean
    showLineNumbers?: boolean
    stickyScroll?: boolean
}>()

const renderedHtml = ref('')
const bodyRef = ref<HTMLElement | null>(null)
const imageTimestamp = ref(Date.now())
let currentRenderId = 0

// ─── Last block list cache (snapshot before Vue update) ───
const lastBlockList = ref<BlockInfo[]>([])

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
const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy(target, text) {
        const el = target as HTMLElement | null
        const lineEl = el?.closest('.code-line') ?? null
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
        const block = el?.closest('.markdown-body') ?? null
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

function handleClick(event: MouseEvent) {
    // Code block header buttons (copy/wrap)
    if (handleCodeBlockClick(event)) return

    // Check for diff marker click first
    if (handleDiffMarkerClick(event, '.diff-marker-inline')) return

    const target = event.target as HTMLElement | null

    // Check for table row click — open row-form modal
    if (handleTableRowClick(event)) return

    // Check for commit-hash click
    const commitEl = target?.closest('.chat-commit-hash, .chat-commit-open-btn')
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
    const btn = target?.closest('.chat-file-open-btn')
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
    const linkEl = target?.closest('a[href^="#"]')
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

function fixLocalImagePaths(html: string): string {
    const currentDir = props.file?.path ? dirName(props.file.path) : ''
    return html.replace(/<img\s+([^>]*src=[^>]*)>/gi, (match: string, attrs: string) => {
        const srcMatch = attrs.match(/src="([^"]*)"/)
        if (!srcMatch) return match
        const src = srcMatch[1]
        if (/^(https?:|\/\/|^\/)/i.test(src)) return match
        let resolved = joinPath(currentDir, src)
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
        // Marker id formats:
        //   "{type}-{blockIndex}-{tag}"          (modified, added)
        //   "{type}-{blockIndex}-old{idx}-{tag}" (deleted, merged blocks)
        // blockIndex is always the first number after the type prefix
        const idParts = marker.id.split('-')
        const blockIndex = parseInt(idParts[1], 10)

        if (blockIndex < 0 || blockIndex >= blockEls.length) continue

        const blockEl = blockEls[blockIndex].el

        // Calculate top relative to .markdown-body via offsetTop chain
        let top = 0
        let el: HTMLElement | null = blockEl as HTMLElement
        while (el && el !== body) {
            top += el.offsetTop
            el = el.offsetParent as HTMLElement | null
        }

        markers.push({
            id: marker.id,
            type: marker.type,
            label: marker.label,
            ariaLabel: marker.ariaLabel,
            top,
            height: (blockEl as HTMLElement).offsetHeight,
        })
    }

    positionedMarkers.value = markers
}

async function doRender(f: { content: string; path?: string; error?: boolean }) {
    const renderId = ++currentRenderId
    imageTimestamp.value = Date.now()
    let html = renderMarkdown(f.content, {
        sanitize: false,
        fixImagePaths: fixLocalImagePaths
    })

    // Add code block headers (language label + copy/wrap buttons)
    html = annotateCodeBlockHeaders(html)

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
/* ─── Diff markers (same style as CodePreview inline markers) ─── */

/* Override height:100% from CodePreview's global .diff-marker-inline —
   Markdown markers use inline :style for height from DOM measurement */
.markdown-body .diff-marker-inline {
    position: absolute;
    right: 0;
    width: 20px;
    height: auto;
    z-index: 2;
}
</style>
