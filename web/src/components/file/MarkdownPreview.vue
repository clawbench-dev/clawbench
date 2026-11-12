<template>
  <div class="markdown-preview">
    <!-- Rendered markdown -->
    <div v-if="viewMode === 'rendered'" class="markdown-body" ref="bodyRef" :data-file-path="file?.path || ''" @click="interactions.handleClick" @mousedown="interactions.onTableMouseDown" @touchstart.passive="interactions.onTableTouchStart" @dragstart="interactions.onDragStart" @dragend="interactions.onDragEnd" @load.capture="onImageLoad">
      <div class="markdown-content"><RichTextBlock :html="renderedHtml" /></div>
      <!-- Diff markers: declarative v-for, positioned absolutely inside .markdown-body -->
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
    </div>

    <!-- Change navigation: prev/next over the diff markers, plus clear. Shown
         only while markers exist. Positioned by its own scoped styles. -->
    <FileChangeNav
      :count="changeNav.count.value"
      :index="changeNav.index.value"
      @prev="changeNav.prev"
      @next="changeNav.next"
      @clear="clearChanges"
    />
  </div>

  <!-- Table row expand modal -->
  <TableRowModal
    :data="interactions.tableRowModal.value"
    @close="interactions.closeTableRowModal"
    @prev="interactions.tableRowPrev"
    @next="interactions.tableRowNext"
  />

  <!-- Inline search bar (bottom of the preview), replacing the SearchDrawer
       bottom sheet for rendered markdown. Its open state is driven by the
       searchDrawer state via props; closing is reported upward. -->
  <PreviewSearchBar ref="searchBarRef" :open="!!searchOpen" :container="bodyRef" @close="emit('closeSearch')" />

  <!-- Markdown code-link click preview -->
  <CodeLinkPreview
    v-if="interactions.codeLinkPreview.enabled.value && viewMode === 'rendered'"
    :preview="interactions.codeLinkPreview"
  />
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { languageVersion } from '@/utils/highlight'
import { stampSvgFigures } from '@/utils/svgMediaFit.ts'
import { flashElement } from '@/utils/domFlash'
import TableRowModal from '@/components/common/TableRowModal.vue'
import PreviewSearchBar from '@/components/file/PreviewSearchBar.vue'
import FileChangeNav from '@/components/file/FileChangeNav.vue'
import { useChangeNav } from '@/composables/useChangeNav.ts'
import { clearBaseline } from '@/composables/useFileChangeBaseline.ts'
import { syncMarkersFor } from '@/composables/useFileRefresh.ts'
import {
  diffMarkers,
  clearDiffMarkers,
  extractBlocks,
  extractBlockElements,
  type BlockInfo,
} from '@/composables/useMarkdownDiff.ts'
import { handleDiffMarkerClick } from '@/composables/useDiffMarkerClick.ts'
import { captureMarkdownScroll } from '@/composables/useFileScrollRestore.ts'
import { setFileScroll, type FileScrollEntry } from '@/utils/fileScrollCache.ts'
import { handleShareLinkClick } from '@/share/shareLinks'
import { useMarkdownDocumentRender } from '@/composables/useMarkdownDocumentRender.ts'
import { useMarkdownDocumentInteractions } from '@/composables/useMarkdownDocumentInteractions.ts'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import '@/assets/diff-marker.css'
import RichTextBlock from '@/components/chat/RichTextBlock.vue'

const props = defineProps<{
    file?: { content: string; path: string; error?: boolean }
    viewMode?: string
    searchOpen?: boolean
    wordWrap?: boolean
    showLineNumbers?: boolean
}>()
const emit = defineEmits(['closeSearch', 'captureScroll'])

const { html: renderedHtml, bodyRef, render: renderDocument, cancel: cancelRender } = useMarkdownDocumentRender()
const searchBarRef = ref<InstanceType<typeof PreviewSearchBar> | null>(null)

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

// ─── Change navigation (prev/next over the markers) ───
// Targets are the markers' top offsets; the composable owns the index and
// scroll-derived highlighting.
const markerTops = computed(() => positionedMarkers.value.map(m => m.top))
// A programmatic scroll to a target must not be read back as a user scroll:
// when the target is near the document end the browser clamps scrollTop, so
// `scrollTop + HEADROOM` would land short of the target and syncIndexFromScroll
// would snap the pill back to an earlier change (e.g. 3/3 → 2/3). Skip the
// scroll-sync for the event our own scrollTo produces.
let suppressScrollSync = false
const NAV_HEADROOM = 16
const changeNav = useChangeNav(markerTops, (i) => {
    const el = bodyRef.value
    const target = positionedMarkers.value[i]
    if (!el || !target) return
    suppressScrollSync = true
    // 16px of headroom so the rail is not flush against the viewport top.
    el.scrollTo({ top: Math.max(0, target.top - NAV_HEADROOM), behavior: 'auto' })
    // Flash the rail we landed on so the jump reads even when the scroll barely
    // moves (adjacent changes). The scroll event fires before the next frame;
    // release the guard after it so a subsequent user scroll is tracked again.
    requestAnimationFrame(() => {
        suppressScrollSync = false
        flashRail(target.id)
    })
}, computed(() => props.file?.path))

/** Highlight a change marker's rail after the nav lands on it. */
function flashRail(markerId: string) {
    // Match on the dataset rather than a CSS attribute selector: marker ids can
    // contain characters a selector would need escaping for, and CSS.escape is
    // absent in some environments (jsdom).
    const els = bodyRef.value?.querySelectorAll('.diff-marker-inline')
    if (!els) return
    for (const el of els) {
        if ((el as HTMLElement).dataset.markerId === markerId) {
            flashElement(el, { className: 'diff-marker-rail-flash' })
            return
        }
    }
}

/** Drop this file's baseline and its published markers. */
function clearChanges() {
    const path = props.file?.path
    if (path) clearBaseline(path)
    clearDiffMarkers()
    positionedMarkers.value = []
}

const quoteQuestion = useQuoteQuestion()

/**
 * Touch double-tap on a rendered block opens the quote bar.
 *
 * This stays in the preview rather than the shared composable: the meeting
 * detail copies the block instead, so the action is injected per surface.
 */
function onBlockDoubleClick(target: EventTarget | null, text: string): void {
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
    // Block-level double-click (a paragraph/heading/etc.): the block's
    // source start line is the best line anchor available.
    const lineBlock = el?.closest('[data-source-line]') as HTMLElement | null
    const lineNum = parseInt(lineBlock?.getAttribute('data-source-line') || '0', 10)
    quoteQuestion.showBar({
        text,
        filePath,
        language: '',
        startLine: lineNum || 0,
        endLine: lineNum || 0,
    })
}

const interactions = useMarkdownDocumentInteractions({
    containerRef: bodyRef,
    filePath: () => props.file?.path || '',
    source: 'file',
    onBeforeOpen: () => {
        captureCurrentScrollState()
    },
    onBlockDoubleClick,
    preHandlers: [
        (event) => handleShareLinkClick(event),
        (event) => handleDiffMarkerClick(event, '.diff-marker-inline'),
    ],
})


function captureCurrentScrollState(): FileScrollEntry | null {
    const el = bodyRef.value
    if (!el || !props.file?.path) return null
    const entry = captureMarkdownScroll(el)
    if (entry) {
        setFileScroll(props.file.path, entry)
        emit('captureScroll', entry)
    }
    return entry
}

function onImageLoad() {
    // An SVG file's intrinsic size is only known once it has decoded, so the
    // proportional fill-width sizing is re-stamped on every media load.
    stampSvgFigures(bodyRef.value)
    window.dispatchEvent(new CustomEvent('realign-file-scroll'))
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
    // Shared with the meeting minutes and the HTML exporter: one pipeline for
    // rendering, path annotation, existence verification and mermaid.
    await renderDocument(f.content, f.path || '', {
        onAfterRender: (el) => {
            // Update last block list cache and compute marker positions after
            // rendering completes.
            lastBlockList.value = extractBlocks(el.querySelector('.markdown-content') || el)
            computeMarkerPositions()
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
        },
    })
}

watch(() => props.file, (f) => {
    if (!f || f.error) {
        // Invalidate any render still in flight; the surface is now empty.
        cancelRender()
    }
}, { immediate: true })

watch(() => props.file?.content, (content) => {
    if (!content) return
    const f = props.file
    if (!f || f.error) return
    doRender(f)
}, { immediate: true })

// A fenced code block whose grammar lives in the lazy tier renders as plain
// text on the first pass. Re-render once the grammar lands so the block gets
// coloured; `languageVersion` bumps at most once per language per session.
watch(languageVersion, () => {
    const f = props.file
    if (!f || f.error || !f.content) return
    doRender(f)
})

watch(() => props.viewMode, async (mode) => {
    if (mode !== 'rendered') return
    const f = props.file
    if (!f || f.error || !f.content) return
    await nextTick()
    const el = bodyRef.value
    if (!el) return
    const mermaidTarget = el.querySelector('.markdown-content') as HTMLElement || el
    await renderMermaidInElement(mermaidTarget, 'md-preview')
    // Mermaid re-renders here (returning to the rendered view); the diagrams are
    // freshly produced, so re-stamp their proportional sizing.
    stampSvgFigures(mermaidTarget)
    window.dispatchEvent(new CustomEvent('realign-file-scroll'))
})

// Watch for marker changes and recompute positions
// immediate: true ensures positions are computed when component mounts
// with pre-existing markers (e.g. after tab switch while diff is active)
watch(diffMarkers, () => {
    nextTick(() => computeMarkerPositions())
}, { deep: true, immediate: true })

// ─── Change-navigation scroll tracking ───
// The scroll container is the .markdown-body element (it is the overflow-y
// scroller), and it is behind a v-if, so it appears after mount — track the ref
// rather than attaching once in onMounted.
let scrollEl: HTMLElement | null = null
function onBodyScroll() {
    // Ignore the scroll our own navigation just produced (see suppressScrollSync
    // above): reading it back can snap the index to an earlier change when the
    // target was clamped at the document end.
    if (suppressScrollSync) return
    if (scrollEl) changeNav.syncIndexFromScroll(scrollEl.scrollTop + NAV_HEADROOM)
}
watch(bodyRef, (el, old) => {
    if (old) old.removeEventListener('scroll', onBodyScroll)
    scrollEl = (el as HTMLElement | null) ?? null
    scrollEl?.addEventListener('scroll', onBodyScroll, { passive: true })
})

/** Re-derive this file's markers from the accumulated baseline. */
function restoreMarkers() {
    const f = props.file
    // `content === ''` is a valid (empty) file, not "not loaded" — `null` is the
    // not-loaded/binary marker. A truthy check here dropped the markers for a
    // file that had been edited down to nothing, so switch-away-and-back lost
    // its "all deleted" marker.
    if (f?.path && f.content !== null && f.content !== undefined) syncMarkersFor(f.path, 'markdown', f.content)
}

onMounted(restoreMarkers)

onBeforeUnmount(() => {
    scrollEl?.removeEventListener('scroll', onBodyScroll)
    scrollEl = null
    // Clear the published markers and drawer state. The accumulated BASELINE is
    // deliberately left intact so this file's markers come back on return.
    clearDiffMarkers()
})

// On file switch, re-derive from the baseline so the new file's markers appear
// (a file with no baseline simply clears them). The baseline survives
// navigation, which is what makes switch-away-and-back restore the markers.
watch(() => props.file?.path, () => {
    positionedMarkers.value = []
    restoreMarkers()
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
    captureCurrentScrollState,
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
.markdown-preview .markdown-body .diff-marker-inline {
    position: absolute;
    /* Keep markers at the right edge of the reading column. The capped
       .markdown-body used to be centered with `margin: 0 auto`, so a marker at
       right:0 sat at the element border — i.e. half the slack (W−900)/2 in from
       the screen edge. Now the element is full-width (padding-based cap), so the
       same visual spot is `right: max(0px, (100% − 900px)/2)`.

       Width stays 20px: this element is the HIT AREA. The visible 3px rail is
       drawn by .diff-marker::before at its right edge. */
    /* `0px`, not `0`: `max()` requires both arguments to be the same type, and
       a bare number mixed with a length invalidates the whole declaration —
       the marker then fell back to `right: auto` and was painted past the
       right edge. The comment above already spells the unit out; keep them in
       sync. */
    right: max(0px, (100% - calc(900 * var(--dp))) / 2);
    width: calc(20 * var(--dp));
    height: auto;
    z-index: 2;
}
</style>
