<template>
  <div class="toc-body">
    <SearchInput v-model="searchQuery" :placeholder="t('toc.searchPlaceholder')" @enter="listNav.confirm" @down="listNav.down" @up="listNav.up" @dblclick="clearSearch" />
    <div class="toc-list" ref="listRef">
      <LoadingIndicator v-if="loading" :label="t('toc.loading')" size="md" />
      <div v-else-if="filteredToc.length === 0" class="toc-empty">{{ searchQuery ? t('toc.noMatch') : t('toc.noHeadings') }}</div>
      <a
        v-for="(item, idx) in filteredToc"
        :key="item.id"
        class="toc-item"
        :class="{ active: activeId === item.id, 'toc-item-active': listNav.activeIndex.value === idx }"
        :data-level="item.level"
        @click.prevent="scrollTo(item)"
      >
        <component
          v-if="item.kind"
          :is="kindIcon(item.kind).icon"
          :size="13"
          class="toc-kind-icon"
          :class="kindIcon(item.kind).cls"
        />
        <span v-if="isPdfOutline" class="toc-page-badge">P{{ item.line }}</span>
        {{ item.text }}
      </a>
    </div>
  </div>
</template>

<script setup>
import { Braces, Box, Boxes, FileCode2, SquareAsterisk, ListOrdered, Variable, Hash, Package, FolderTree, CircleDot, Settings2, Hammer, Layers, Puzzle, Zap, Code2, Heading } from 'lucide-vue-next'
import { ref, watch, nextTick, onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import SearchInput from '@/components/common/SearchInput.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { extractToc, slugify } from '@/utils/toc.ts'
import { flashElement } from '@/utils/domFlash'
import { protectMarkdown } from '@/utils/markdownProtect.ts'
import { getFileType } from '@/utils/fileType.ts'
import { fetchCodeSymbols } from '@/composables/useCodeSymbols'
import { useFileEditor } from '@/composables/useFileEditor'

const { t } = useI18n()
const { isEditorDirty } = useFileEditor()

/** Map symbol kind → { icon component, CSS class } */
const KIND_ICON_MAP = {
  function:     { icon: Braces,        cls: 'kind-function' },
  method:       { icon: Braces,        cls: 'kind-method' },
  constructor:  { icon: Hammer,        cls: 'kind-constructor' },
  class:        { icon: Box,           cls: 'kind-class' },
  struct:       { icon: Boxes,         cls: 'kind-struct' },
  interface:    { icon: FileCode2,     cls: 'kind-interface' },
  type:         { icon: SquareAsterisk, cls: 'kind-type' },
  enum:         { icon: ListOrdered,   cls: 'kind-enum' },
  variable:     { icon: Variable,      cls: 'kind-variable' },
  constant:     { icon: Hash,          cls: 'kind-constant' },
  module:       { icon: Package,       cls: 'kind-module' },
  namespace:    { icon: FolderTree,    cls: 'kind-namespace' },
  field:        { icon: CircleDot,     cls: 'kind-field' },
  property:     { icon: Settings2,     cls: 'kind-property' },
  trait:        { icon: Layers,        cls: 'kind-trait' },
  impl:         { icon: Puzzle,        cls: 'kind-impl' },
  macro:        { icon: Zap,           cls: 'kind-macro' },
  heading:      { icon: Heading,       cls: 'kind-heading' },
}
const KIND_FALLBACK = { icon: Code2, cls: 'kind-other' }

const props = defineProps({
    file: Object,
    pdfOutline: { type: Array, default: () => [] },
    /** Whether the panel is visible. Drives the document-level keyboard nav. */
    open: { type: Boolean, default: true },
    /**
     * Whether the content is rendered by CodeMirror (code files, or markdown
     * in raw/editing view). Scroll-follow then uses the editor's
     * `cm-editor-viewport-line` event instead of heading-DOM observation.
     */
    codeView: { type: Boolean, default: false },
})
const emit = defineEmits(['jump', 'jumpPage', 'activated'])

const toc = ref([])
const activeId = ref('')
const isCode = ref(false)
const isPdfOutline = ref(false)
const searchQuery = ref('')
const filteredToc = ref([])
const loading = ref(false)
const listRef = ref(null)

// Keep the highlight visible: when scroll-follow moves the active item off the
// panel's visible area, scroll the list so the active .toc-item stays in view.
// `block: 'nearest'` is a no-op when the item is already visible, so normal
// scrolling produces no feedback loop.
watch(activeId, () => {
    nextTick(() => {
        const list = listRef.value
        if (!list) return
        const el = list.querySelector('.toc-item.active')
        if (el && typeof el.scrollIntoView === 'function') {
            el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
        }
    })
})

watch([() => props.file, () => props.file?.content, () => props.pdfOutline], ([file, content, pdfOut], _, onCleanup) => {
    let cancelled = false
    onCleanup(() => { cancelled = true })

    // PDF outline
    if (file && pdfOut && pdfOut.length > 0) {
        isPdfOutline.value = true
        isCode.value = false
        toc.value = pdfOut
        activeId.value = toc.value[0]?.id || ''
        searchQuery.value = ''
        filteredToc.value = toc.value
        loading.value = false
        return
    }
    isPdfOutline.value = false

    // Text-based TOC
    if (!content) {
        toc.value = []
        filteredToc.value = []
        isCode.value = false
        loading.value = false
        return
    }
    const lang = getFileType(file.name)?.lang || 'plaintext'
    isCode.value = lang !== 'markdown'

    // When editor has unsaved changes, fetchCodeSymbols reads from disk (stale),
    // so use client-side regex extraction with the live content instead.
    const editorDirty = isEditorDirty()

    // For code files and markdown, try backend tree-sitter API first, then fallback to regex
    if (file?.path && !editorDirty) {
        loading.value = true
        fetchCodeSymbols(file.path).then(result => {
            if (cancelled) return
            if (result && result.symbols.length > 0) {
                // Convert backend symbols to TocItem format
                // Deduplicate heading IDs to match markedConfig.ts logic
                const headingIdCounts = {}
                // For markdown headings, backend symbols carry the raw heading
                // text. The render pipeline derives anchor ids from the SAME
                // protected text toc.ts uses (math → \x00MATHI0\x00 placeholders),
                // so recompute ids through protectMarkdown to stay in sync —
                // otherwise math headings would get a mismatched slug.
                const protectedByLine = new Map()
                if (lang === 'markdown' && content) {
                    const res = protectMarkdown(content)
                    res.protected.split('\n').forEach((line, i) => protectedByLine.set(i + 1, line))
                }
                toc.value = result.symbols.map(s => {
                    let id
                    if (s.kind === 'heading') {
                        let baseId
                        if (lang === 'markdown' && protectedByLine.size > 0) {
                            const protLine = protectedByLine.get(s.line)
                            baseId = protLine ? slugify(protLine.replace(/^\s*#+\s*/, '').trim()) : slugify(s.name)
                        } else {
                            baseId = slugify(s.name)
                        }
                        const count = (headingIdCounts[baseId] || 0) + 1
                        headingIdCounts[baseId] = count
                        id = count > 1 ? `${baseId}-${count}` : baseId
                    } else {
                        id = 'toc-l' + s.line
                    }
                    return {
                        level: s.level,
                        text: s.name,
                        kind: s.kind,
                        id,
                        line: s.line,
                    }
                })
            } else {
                // Fallback to regex-based extraction
                toc.value = extractToc(content, lang)
            }
            activeId.value = toc.value[0]?.id || ''
            searchQuery.value = ''
            filteredToc.value = toc.value
            loading.value = false
        }).catch(() => {
            if (cancelled) return
            // Fallback to regex-based extraction on error
            toc.value = extractToc(content, lang)
            activeId.value = toc.value[0]?.id || ''
            searchQuery.value = ''
            filteredToc.value = toc.value
            loading.value = false
        })
    } else {
        loading.value = false
        toc.value = extractToc(content, lang)
        activeId.value = toc.value[0]?.id || ''
        searchQuery.value = ''
        filteredToc.value = toc.value
    }
}, { immediate: true })

watch(searchQuery, () => handleSearch())

// ── Keyboard ↑/↓ + Enter navigation over TOC ──
const listNav = useListNav({
  getCount: () => filteredToc.value.length,
  onConfirm: (idx) => scrollTo(filteredToc.value[idx]),
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation also works when focus leaves the search box
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index) {
  const list = listRef.value
  if (!list) return
  const items = list.querySelectorAll('.toc-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

watch(filteredToc, () => listNav.reset())

function handleSearch() {
    const query = searchQuery.value.toLowerCase().trim()
    if (!query) {
        filteredToc.value = toc.value
        return
    }
    filteredToc.value = toc.value.filter(item =>
        item.text.toLowerCase().includes(query)
    )
}

function clearSearch() {
    searchQuery.value = ''
    filteredToc.value = toc.value
}

function kindIcon(kind) {
    return KIND_ICON_MAP[kind] || KIND_FALLBACK
}

function scrollTo(item) {
    // PDF: jump to page number
    if (isPdfOutline.value && item.line > 0) {
        emit('jumpPage', item.line)
        activeId.value = item.id
        return
    }

    // Hold the clicked highlight while the programmatic scroll (instant jump)
    // lands, so the scroll-follow (observer / viewport-line) doesn't steal it
    // to a nearby heading on the immediately following user scroll.
    holdActiveHighlight()

    // Find the heading element scoped to the CURRENT file's content container.
    // A bare document.getElementById could hit the same id in another file
    // stacked underneath an overlay (FileOverlay nav stack), scrolling a hidden
    // container instead of the visible one.
    const elById = findHeadingEl(item.id)
    if (elById) {
        elById.scrollIntoView({ behavior: 'auto', block: 'start' })
        flashElement(elById)
        activeId.value = item.id
        // The jump was handled right here (rendered markdown/PDF heading DOM),
        // so the host is not going to see a `jump`/`jumpPage` event. Notify it
        // that an item was activated anyway — e.g. a drawer host can dismiss
        // itself while a persistent dock stays open.
        emit('activated', item.id)
        return
    }
    if (item.line) {
        // For CodeMirror-rendered views the scroll itself is driven upstream
        // (scrollToLine) and viewport-line events will follow — set the clicked
        // id now so it stays highlighted through the hold window.
        activeId.value = item.id
        emit('jump', item.line, item.id)
    }
}

/** Resolve a heading anchor scoped to the file this TOC belongs to. */
function findHeadingEl(id) {
    const filePath = props.file?.path
    if (!id) return null
    // 1. Markdown rendered preview: the container exposes its source path.
    if (filePath) {
        const containers = document.querySelectorAll(`[data-file-path="${escAttr(filePath)}"]`)
        for (const c of containers) {
            const el = c.querySelector(`#${escId(id)}`)
            if (el) return el
        }
    }
    // 2. Fallback: plain document lookup (CodeMirror views have no anchor DOM,
    //    but headings in raw/other views may still match).
    return document.getElementById(id)
}

/**
 * Minimal CSS escaping without depending on global `CSS` (absent in jsdom).
 * - `escId`: escape for `#<id>` selectors (id chars may need CSS escaping).
 * - `escAttr`: escape for `[data-file-path="..."]` — the value inside the
 *   quoted attribute is a literal string, so only quote/backslash need
 *   escaping; CSS.escape must NOT be used here (it escapes `/` etc. that are
 *   valid literal attribute characters).
 */
function escId(id) {
    if (typeof CSS !== 'undefined' && typeof CSS.escape === 'function') return CSS.escape(id)
    return String(id).replace(/["'\\]/g, '')
}
function escAttr(value) {
    return String(value).replace(/["\\]/g, '')
}

let observer = null

// ── Click-jump highlight hold ──
// After a TOC item is clicked the viewport lands instantly on the target, but
// the flash runs for ~1.2s and the user's next scroll usually starts within
// that window — scroll-follow would then immediately steal the highlight to a
// *nearby* (not the clicked) item. Hold the clicked highlight for a short
// window so the flash is not overridden, then let normal scroll-follow resume
// on the user's next scroll.
const FOLLOW_HOLD_MS = 1500
let followHoldUntil = 0
let followHoldTimer = null

function holdActiveHighlight() {
    followHoldUntil = Date.now() + FOLLOW_HOLD_MS
    clearTimeout(followHoldTimer)
    followHoldTimer = setTimeout(() => {
        followHoldUntil = 0
        followHoldTimer = null
    }, FOLLOW_HOLD_MS)
}

function scrollFollowHeld() {
    return Date.now() < followHoldUntil
}

function releaseFollowHold() {
    clearTimeout(followHoldTimer)
    followHoldUntil = 0
    followHoldTimer = null
}

/** Set up IntersectionObserver to track the currently visible TOC item */
function setupObserver() {
    const prevObserver = observer
    observer = null
    prevObserver?.disconnect()
    if (isPdfOutline.value) return
    // CodeMirror-rendered content virtualizes its DOM — scroll-follow there is
    // driven by cm-editor-viewport-line instead (see onEditorViewportLine).
    if (props.codeView) return

    nextTick(() => {
        // If another setupObserver() was called before this nextTick fired,
        // observer will be non-null — skip creating a duplicate.
        if (observer) return
        if (isCode.value) {
            observer = new IntersectionObserver((entries) => {
                if (scrollFollowHeld()) return
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        const line = entry.target.getAttribute('data-line')
                        const match = toc.value.find(t => t.line == line)
                        if (match) activeId.value = match.id
                        break
                    }
                }
            }, { rootMargin: '-60px 0px -70% 0px' })
            toc.value.forEach(item => {
                const el = document.querySelector(`[data-line="${item.line}"]`)
                if (el) observer.observe(el)
            })
        } else {
            observer = new IntersectionObserver((entries) => {
                if (scrollFollowHeld()) return
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        activeId.value = entry.target.id
                        break
                    }
                }
            }, { rootMargin: '-60px 0px -70% 0px' })
            toc.value.forEach(item => {
                const el = document.getElementById(item.id)
                if (el) observer.observe(el)
            })
        }
    })
}

// Re-setup observer when TOC items change (e.g., after content update)
watch(toc, () => {
    setupObserver()
})

// ── Code-view scroll-follow ──
// CodeMirror virtualizes its DOM (only visible lines exist as elements), so an
// IntersectionObserver cannot reliably track the active line there. The editor
// reports its top visible line via `cm-editor-viewport-line`; we highlight the
// deepest TOC symbol at or above that line.
function onEditorViewportLine(e) {
    if (!props.codeView) return
    if (scrollFollowHeld()) return
    const viewportLine = e.detail?.line
    if (typeof viewportLine !== 'number') return
    let match = null
    for (const item of toc.value) {
        if (typeof item.line !== 'number') continue
        if (item.line <= viewportLine) {
            // Keep the deepest (largest line) symbol at-or-above the viewport top.
            match = item
        } else {
            break
        }
    }
    if (match) activeId.value = match.id
}

// ── Markdown preview DOM-rebuild tracking ──
// When the user toggles between rendered preview and source/code view,
// MarkdownPreview is unmounted and re-mounted, rebuilding the heading DOM. The
// IntersectionObserver holds references to the OLD (detached) elements, so it
// silently stops following scroll. A body-level MutationObserver re-runs
// setupObserver() when the .markdown-body tree is replaced.
let domObserver = null
let domObserverTimer = 0
function ensureDomObserver() {
    // CodeMirror-rendered content (code view) mutates its DOM constantly and
    // scroll-follow there is driven by cm-editor-viewport-line instead — no
    // DOM watching needed.
    if (domObserver) return
    domObserver = new MutationObserver(() => {
        if (domObserverTimer) return
        domObserverTimer = setTimeout(() => {
            domObserverTimer = 0
            // Only non-CodeMirror content (markdown rendered preview) uses
            // heading-DOM observation.
            if (!props.codeView && !isPdfOutline.value) setupObserver()
        }, 50)
    })
    domObserver.observe(document.body, { childList: true, subtree: true })
}
function stopDomObserver() {
    if (domObserver) {
        domObserver.disconnect()
        domObserver = null
    }
    if (domObserverTimer) clearTimeout(domObserverTimer)
    domObserverTimer = 0
}
// Re-engage DOM watching when switching back to markdown rendered preview.
watch(() => props.codeView, (codeView) => {
    if (codeView) stopDomObserver()
    else ensureDomObserver()
})

onMounted(() => {
    window.addEventListener('cm-editor-viewport-line', onEditorViewportLine)
    ensureDomObserver()
})
onBeforeUnmount(() => {
    observer?.disconnect()
    observer = null
    stopDomObserver()
    releaseFollowHold()
    window.removeEventListener('cm-editor-viewport-line', onEditorViewportLine)
})
</script>

<style scoped>
.toc-body {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding: 8px 6px 0;
}

.toc-list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    -webkit-overflow-scrolling: touch;
    margin-top: 8px;
    padding-bottom: 8px;
}

.toc-empty {
    text-align: center;
    padding: 32px 16px;
    color: var(--text-muted);
    font-size: 13px;
}

.toc-item {
    display: block;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 13px;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
    border-left: 2px solid transparent;
    white-space: nowrap;
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
}
@media (hover: hover) {
  .toc-item:hover { background: var(--bg-tertiary); color: var(--accent-color); }
}
.toc-item.active { color: var(--accent-color); border-left-color: var(--accent-color); background: var(--bg-tertiary); border-radius: 0; }
.toc-item-active { color: var(--accent-color); background: var(--bg-tertiary); border-radius: 0; }
.toc-item[data-level="2"] { padding-left: 20px; }
.toc-item[data-level="3"] { padding-left: 32px; }
.toc-item[data-level="4"] { padding-left: 44px; }
.toc-item[data-level="5"] { padding-left: 56px; }
.toc-item[data-level="6"] { padding-left: 68px; }

.toc-page-badge {
    display: inline-block;
    font-size: 10px;
    font-weight: 600;
    background: var(--bg-tertiary);
    color: var(--text-muted);
    padding: 1px 5px;
    border-radius: 3px;
    margin-right: 4px;
    flex-shrink: 0;
    vertical-align: middle;
}

.toc-item.active .toc-page-badge {
    background: rgba(255,255,255,0.15);
    color: var(--accent-color);
}

.toc-kind-icon {
    flex-shrink: 0;
    margin-right: 5px;
    vertical-align: middle;
    opacity: 0.75;
}
.toc-item.active .toc-kind-icon { opacity: 1; }

.kind-function, .kind-method     { color: #c586c0; }
.kind-constructor                { color: #dcdcaa; }
.kind-class                      { color: #e06c75; }
.kind-struct                     { color: #e5a54a; }
.kind-interface                  { color: #4ec9b0; }
.kind-type                       { color: #2ec4b6; }
.kind-enum                       { color: #d4a017; }
.kind-variable                   { color: #75aadb; }
.kind-constant                   { color: #8899aa; }
.kind-module, .kind-namespace    { color: #569cd6; }
.kind-field, .kind-property      { color: #9cdcfe; }
.kind-trait                      { color: #6a9955; }
.kind-impl                       { color: #4fb3bf; }
.kind-macro                      { color: #e5c07b; }
.kind-heading                    { color: #56b6c2; }
.kind-other                      { color: var(--text-muted); }

</style>
