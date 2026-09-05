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
  </div>

  <!-- Table row expand modal -->
  <TableRowModal
    :data="tableRowModal"
    @close="closeTableRowModal"
    @prev="tableRowPrev"
    @next="tableRowNext"
  />

  <!-- Inline search bar (bottom of the preview), replacing the SearchDrawer
       bottom sheet for rendered markdown. Its open state is driven by the
       searchDrawer state via props; closing is reported upward. -->
  <MarkdownSearchBar ref="searchBarRef" :open="!!searchOpen" :container="bodyRef" @close="emit('closeSearch')" />

  <!-- Markdown code-link click preview -->
  <CodeLinkPreview
    v-if="codeLinkPreview.enabled.value && viewMode === 'rendered'"
    :preview="codeLinkPreview"
  />
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { store } from '@/stores/app.ts'
import { dirName } from '@/utils/path.ts'
import { buildMarkdownPreviewDom } from '@/composables/useMarkdownRenderPipeline.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import TableRowModal from '@/components/common/TableRowModal.vue'
import MarkdownSearchBar from '@/components/file/MarkdownSearchBar.vue'
import {
  diffMarkers,
  clearDiffMarkers,
  extractBlocks,
  extractBlockElements,
  type BlockInfo,
} from '@/composables/useMarkdownDiff.ts'
import { handleDiffMarkerClick } from '@/composables/useDiffMarkerClick.ts'
import { useCodeLinkPreview } from '@/composables/useCodeLinkPreview.ts'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import '@/assets/diff-marker.css'

const props = defineProps<{
    file?: { content: string; path: string; error?: boolean }
    viewMode?: string
    searchOpen?: boolean
    wordWrap?: boolean
    showLineNumbers?: boolean
}>()
const emit = defineEmits(['closeSearch'])

const renderedHtml = ref('')
const bodyRef = ref<HTMLElement | null>(null)
const searchBarRef = ref<InstanceType<typeof MarkdownSearchBar> | null>(null)
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

const { verifyFilePaths, resolveRelativePath, openFilePath, parseFileUri } = useFilePathAnnotation()
const { isPC } = usePlatformDetect()
const codeLinkPreview = useCodeLinkPreview({ containerRef: bodyRef })

function handleClick(event: MouseEvent) {
    // Code block header buttons (copy/wrap)
    if (handleCodeBlockClick(event)) return

    // Table block header buttons (copy/wrap)
    if (handleTableBlockClick(event)) return

    // Check for diff marker click first
    if (handleDiffMarkerClick(event, '.diff-marker-inline')) return

    const target = event.target as HTMLElement | null

    // Check for table row click — open row-form modal
    if (handleTableRowClick(event)) return

    // Handle code-link preview clicks (desktop) or touch taps.
    // Only verified *file* paths are intercepted — directories and paths that
    // have not yet been verified (data-path-type unset) fall through to the
    // handlers below (anchor navigation / open button / dbl-click), preserving
    // the pre-feature behavior for those cases.
    if (codeLinkPreview.enabled.value) {
        const isTouch = codeLinkPreview.isTouchDevice()
        const isModifier = !isTouch && (event.ctrlKey || event.metaKey)
        const linkOrBtn = target?.closest<HTMLElement>('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]')
        const pathEl = target?.closest<HTMLElement>('.chat-file-path[data-file-path]')
        const isVerifiedFile = linkOrBtn?.getAttribute('data-path-type') === 'file'
        if (isVerifiedFile && ((isModifier && linkOrBtn) || (!isTouch && pathEl))) {
            codeLinkPreview.handleClick(event)
            return
        }
        if (isVerifiedFile && isTouch && pathEl) {
            codeLinkPreview.handleClick(event)
            return
        }
    }

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
            codeLinkPreview.close()
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
                targetEl.scrollIntoView({ behavior: 'auto', block: 'start' })
                targetEl.classList.add('line-flash')
                targetEl.addEventListener('animationend', () => targetEl.classList.remove('line-flash'), { once: true })
                return
            }
        }
    }
    handleDblClick(event, (href, lineStart, lineEnd) => {
        const anchor = target?.closest<HTMLAnchorElement>('a[href]')
        const annotatedPath = anchor?.getAttribute('data-file-path')
        const currentDir = props.file?.path ? dirName(props.file.path) : ''
        // Prefer the annotated resolved path; for a file:// link take its path
        // directly; otherwise resolve the relative href against the md's dir.
        const resolvedPath = annotatedPath
            || (href.startsWith('file://') ? parseFileUri(href).path : resolveRelativePath(href, currentDir))
        codeLinkPreview.close()
        openFilePath(resolvedPath, lineStart, lineEnd)
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

    // Shared with the HTML exporter so exported files match the preview exactly.
    const { html: annotatedHtml, detectedPaths } = buildMarkdownPreviewDom(
        {
            content: f.content,
            path: f.path || '',
            projectRoot: store.state.projectRoot,
            homeDir: store.state.homeDir,
        },
        { isPC: isPC.value, imageTimestamp: imageTimestamp.value }
    )
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

    const mermaidTarget = el.querySelector('.markdown-content') as HTMLElement || el
    await renderMermaidInElement(mermaidTarget, 'md-preview')

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
    const mermaidTarget = el.querySelector('.markdown-content') as HTMLElement || el
    await renderMermaidInElement(mermaidTarget, 'md-preview')
})

// Watch for marker changes and recompute positions
// immediate: true ensures positions are computed when component mounts
// with pre-existing markers (e.g. after tab switch while diff is active)
watch(diffMarkers, () => {
    nextTick(() => computeMarkerPositions())
}, { deep: true, immediate: true })

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

// Focus the inline search bar's input (opened via the searchOpen prop).
// Used by FileViewer.focusSearchInput so the toolbar/ctrl-F flow focuses the
// search box, matching CodeMirror's openSearch behavior.
function focusSearchInput() {
    searchBarRef.value?.focus()
}

defineExpose({
    lastBlockList,
    bodyRef,
    focusSearchInput,
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

/* Lightbox image wrapper — positions the expand icon overlay */
.markdown-body .lightbox-img-wrap {
  position: relative;
  display: inline-block;
}

.markdown-body .lightbox-img-wrap .lightbox-img {
  cursor: default;
}

.markdown-body .lightbox-img-wrap .lightbox-expand-icon {
  display: none;
  position: absolute;
  top: 4px;
  right: 4px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  cursor: pointer;
  z-index: 2;
  pointer-events: auto;
}

@media (hover: hover) {
  .markdown-body .lightbox-img-wrap:hover .lightbox-expand-icon {
    display: flex;
    align-items: center;
    justify-content: center;
  }
}

.markdown-body .lightbox-img-wrap .lightbox-expand-icon::after {
  content: '⤢';
  font-size: 14px;
  line-height: 1;
}

/* Mermaid expand icon — top-right corner, visible on hover (PC mode) */
.markdown-body .mermaid {
  position: relative;
}

.markdown-body .mermaid .lightbox-expand-icon {
  display: none;
  position: absolute;
  top: 4px;
  right: 4px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 14px;
  line-height: 24px;
  text-align: center;
  cursor: pointer;
  z-index: 2;
  align-items: center;
  justify-content: center;
}

.markdown-body .mermaid .lightbox-expand-icon::after {
  content: '⤢';
}

@media (hover: hover) {
  .markdown-body .mermaid:hover .lightbox-expand-icon {
    display: flex;
  }
}
</style>
