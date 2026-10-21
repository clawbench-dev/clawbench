<template>
  <SearchBar
    ref="sbRef"
    :open="open"
    :can-nav="canNav"
    :match-text="countText"
    :model-value="query"
    :case-sensitive="caseSensitive"
    :regexp="regexp"
    :whole-word="wholeWord"
    :labels="labels"
    @input="onQueryInput"
    @prev="goPrev"
    @next="goNext"
    @close="close"
    @enter="onEnter"
    @escape="close"
    @case-change="(v) => setOption('caseSensitive', v)"
    @regexp-change="(v) => setOption('regexp', v)"
    @word-change="(v) => setOption('wholeWord', v)"
  />
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { flashElement } from '@/utils/domFlash'
import { useI18n } from 'vue-i18n'
import SearchBar, { type SearchBarLabels } from '@/components/common/SearchBar.vue'
import {
    SEARCH_MARK_ACTIVE_CLASS,
    SEARCH_MARK_CLASS,
    buildSearchRegex,
    collectTextMatches,
    unwrapSearchMarks,
    wrapTextMatch,
    type TextMatch,
} from '@/utils/documentSearch.ts'

/**
 * Inline find bar for a RENDERED preview — markdown body, or the HTML preview
 * iframe's own document.
 *
 * The two differ only in which document the text nodes live in, so the search
 * engine itself lives in utils/documentSearch.ts (shared, document-agnostic)
 * and this component only owns the bar, the active-match navigation and the
 * scroll/flash feedback.
 *
 * `iframe` (optional) selects the previewed document: the match walk, the
 * `<mark>` wrapping and the scroll all happen inside it. Without it the bar
 * searches `container` in the host document, which is the markdown case.
 */

const { t } = useI18n()

// Localized labels, matching the CodeMirror panel (see CodeMirrorViewer's
// searchPhrases) so both search UIs read the same language.
const labels = computed<SearchBarLabels>(() => ({
    find: t('file.editor.searchPanel.find'),
    replace: t('file.editor.searchPanel.replace'),
    previous: t('file.editor.searchPanel.previous'),
    next: t('file.editor.searchPanel.next'),
    matchCase: t('file.editor.searchPanel.matchCase'),
    regexp: t('file.editor.searchPanel.regexp'),
    byWord: t('file.editor.searchPanel.byWord'),
    replaceAction: t('file.editor.searchPanel.replaceAction'),
    replaceAll: t('file.editor.searchPanel.replaceAll'),
    close: t('file.editor.searchPanel.close'),
}))

const props = defineProps<{
    open: boolean
    // Optional scoped container. When set, search/highlight operations are
    // confined to it. Without it we fall back to the first global
    // .markdown-body (legacy behavior — wrong when multiple such containers
    // coexist on the page, e.g. file preview + SessionSearchDrawer).
    container?: HTMLElement | null
    /**
     * Search inside this iframe's document instead of the host document.
     *
     * A rendered HTML preview loads its content into a sandboxed same-origin
     * iframe, so its text nodes belong to a DIFFERENT document: the host's
     * TreeWalker cannot reach them, and a Range created by the host document
     * cannot span them (surroundContents throws). Passing the element here is
     * what makes find work in that preview at all.
     *
     * Takes precedence over `container`.
     */
    iframe?: HTMLIFrameElement | null
}>()
const emit = defineEmits(['close'])

const sbRef = ref<InstanceType<typeof SearchBar> | null>(null)
const query = ref('')
const caseSensitive = ref(false)
const regexp = ref(false)
const wholeWord = ref(false)

// Active match index (0-based) within matches list.
const activeIndex = ref(0)

const matches = ref<TextMatch[]>([])

// All highlighted <mark> elements (one per match), parallel to `matches`.
const marks = ref<HTMLElement[]>([])

/**
 * The node the search walks, or null.
 *
 * In iframe mode this is the previewed document's <body>: its text nodes live
 * there, and the marks must be created by THAT document. Reached lazily on every
 * call (not cached) because the iframe reloads — a cached body would point at a
 * detached document after any content change.
 */
function searchRoot(): ParentNode | null {
    if (props.iframe) {
        let doc: Document | null = null
        try {
            doc = props.iframe.contentDocument
        } catch {
            // Cross-origin (sandbox lost allow-same-origin) — not searchable.
            return null
        }
        return doc?.body ?? doc?.documentElement ?? null
    }
    return props.container ?? document.querySelector('.markdown-body')
}

watch(() => props.open, async (val) => {
    if (val) {
        activeIndex.value = 0
        await nextTick()
        sbRef.value?.focus()
        if (query.value) onInput()
    } else {
        clearHighlights()
    }
})

function setOption(key: 'caseSensitive' | 'regexp' | 'wholeWord', value: boolean) {
    if (key === 'caseSensitive') caseSensitive.value = value
    else if (key === 'regexp') regexp.value = value
    else wholeWord.value = value
    onInput()
}

function onEnter(shiftKey: boolean) {
    if (shiftKey) goPrev()
    else goNext()
}

function onQueryInput(value: string) {
    query.value = value
    activeIndex.value = 0
    onInput()
}

function collectMatches(q: string): TextMatch[] {
    const root = searchRoot()
    if (!root || !q.trim()) return []
    return collectTextMatches(root, buildSearchRegex(q, {
        caseSensitive: caseSensitive.value,
        regexp: regexp.value,
        wholeWord: wholeWord.value,
    }))
}

// ── DOM-based match highlighting ────────────────────────────────────────────
// Wraps every match in a <mark class="md-search-match">. DOM wrapping is used
// (instead of the CSS Custom Highlight API) because ::highlight() fails to
// render inside the file overlay's `isolation: isolate` stacking context in
// Chromium, leaving matches visually unhighlighted.
//
// Query the live DOM rather than trusting a cached array: wrapping mutates the
// tree, so cached references can go stale between onInput calls. Scoped to the
// resolved root only — never globally, so a sibling .markdown-body
// (SessionSearchDrawer, TaskOverviewTab, …) can't be touched.
function unwrapAllMarks() {
    const root = searchRoot()
    if (root) unwrapSearchMarks(root, SEARCH_MARK_CLASS)
    marks.value = []
}

function applyHighlights() {
    // Reset DOM to its pre-mark state before re-wrapping.
    unwrapAllMarks()
    matches.value = collectMatches(query.value)
    // Wrap in reverse document order: collecting marks mutates the DOM, but
    // wrapping back-to-front keeps the offsets of earlier matches valid.
    const newMarks: (HTMLElement | null)[] = []
    for (let i = matches.value.length - 1; i >= 0; i--) {
        const mark = wrapTextMatch(matches.value[i], SEARCH_MARK_CLASS)
        newMarks.unshift(mark)
    }
    marks.value = newMarks.filter((m): m is HTMLElement => m !== null)
    applyActive()
}

// Add/remove the active class on the mark matching the current activeIndex.
function applyActive() {
    marks.value.forEach((mark, i) => {
        mark.classList.toggle(SEARCH_MARK_ACTIVE_CLASS, i === activeIndex.value)
    })
}

function onInput() {
    activeIndex.value = 0
    applyHighlights()
}

// Clear all highlights (marks unwrapped + state reset).
function clearHighlights() {
    unwrapAllMarks()
    matches.value = []
    marks.value = []
}

const canNav = computed(() => matches.value.length > 0)

const countText = computed(() => {
    if (!query.value.trim()) return ''
    if (matches.value.length === 0) return '0/0'
    return `${activeIndex.value + 1}/${matches.value.length}`
})

function goNext() {
    if (!matches.value.length) return
    activeIndex.value = (activeIndex.value + 1) % matches.value.length
    applyActive()
    jumpTo(marks.value[activeIndex.value])
}

function goPrev() {
    if (!matches.value.length) return
    activeIndex.value = (activeIndex.value - 1 + matches.value.length) % matches.value.length
    applyActive()
    jumpTo(marks.value[activeIndex.value])
}

// ── Jump: instant center scroll + match-text flash ──────────────────────────

onBeforeUnmount(() => {
    clearHighlights()
})

function jumpTo(mark: HTMLElement | null | undefined) {
    if (!mark || !mark.isConnected) return
    // Flash the active match text. The scroll is a single instant jump
    // (behavior:'auto' centers immediately — no animation, no settle loop).
    mark.scrollIntoView({ behavior: 'auto', block: 'center' })
    // Aligned to the canonical line-flash (assets/code-viewer.css) — the
    // class cleanup + reduced-motion handling live in domFlash.
    flashElement(mark, { className: 'search-match-flash' })
}

function close() {
    clearHighlights()
    emit('close')
}

defineExpose({
    focus() {
        sbRef.value?.focus()
    },
})
</script>

<style>
/* Match highlighting via DOM <mark> wrappers (global — the marks live inside
   .markdown-body, outside this component's scoped styles). The opacity values
   mirror the CodeMirror .cm-searchMatch / .cm-searchMatch-selected styles in
   search-bar.css so both search UIs look identical. */
.markdown-body mark.md-search-match {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 28%, transparent);
  color: inherit;
  padding: 0;
}
.markdown-body mark.md-search-match-active {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 55%, transparent);
}
</style>
