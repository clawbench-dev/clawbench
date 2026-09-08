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
}>()
const emit = defineEmits(['close'])

const sbRef = ref<InstanceType<typeof SearchBar> | null>(null)
const query = ref('')
const caseSensitive = ref(false)
const regexp = ref(false)
const wholeWord = ref(false)

// Active match index (0-based) within matches list.
const activeIndex = ref(0)

interface Match {
    textNode: Text
    offset: number
    length: number
}

const matches = ref<Match[]>([])

// All highlighted <mark> elements (one per match), parallel to `matches`.
const marks = ref<HTMLElement[]>([])

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

function buildRegex(q: string): RegExp {
    let source = regexp.value ? q : q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    if (wholeWord.value) source = `\\b${source}\\b`
    try {
        return new RegExp(source, caseSensitive.value ? 'g' : 'gi')
    } catch {
        // Invalid regexp — fall back to a literal that can never match.
        return /(?!)/g
    }
}

function collectMatches(q: string): Match[] {
    const container = props.container ?? document.querySelector('.markdown-body')
    if (!container || !q.trim()) return []
    const root = container as Element
    let re: RegExp
    try {
        re = buildRegex(q)
    } catch {
        return []
    }
    const out: Match[] = []
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null)
    while (walker.nextNode()) {
        const textNode = walker.currentNode as Text
        const text = textNode.textContent || ''
        re.lastIndex = 0
        let m
        while ((m = re.exec(text)) !== null) {
            if (m[0].length === 0) { re.lastIndex++; continue }
            out.push({ textNode, offset: m.index, length: m[0].length })
        }
    }
    return out
}

// ── DOM-based match highlighting ────────────────────────────────────────────
// Wraps every match in a <mark class="md-search-match">. DOM wrapping is used
// (instead of the CSS Custom Highlight API) because ::highlight() fails to
// render inside the file overlay's `isolation: isolate` stacking context in
// Chromium, leaving matches visually unhighlighted.
const MARK_CLASS = 'md-search-match'

function unwrapMark(mark: HTMLElement) {
    const parent = mark.parentNode
    if (!parent) return
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
}

// Query the live DOM rather than trusting a cached array: wrapping mutates the
// tree, so cached references can go stale between onInput calls.
function unwrapAllMarks() {
    const container = props.container ?? document.querySelector('.markdown-body')
    // Query marks within the resolved container only — never globally, so a
    // sibling .markdown-body (SessionSearchDrawer, TaskOverviewTab, …) can't
    // be touched.
    for (const mark of (container as Element | null)?.querySelectorAll<HTMLElement>(`mark.${MARK_CLASS}`) ?? []) {
        unwrapMark(mark)
    }
    // Unwrapping splits a text node around each <mark> (e.g. "clawbench" →
    // "claw" + "bench"). Re-merge adjacent text nodes so the regex sees the
    // original text again — otherwise word-boundary matching misfires on the
    // split fragments ("\bclaw\b" would match a lone "claw" node).
    ;(container as Element | null)?.normalize()
    marks.value = []
}

// Wrap a match range in a <mark>, returning the element. The range must be
// created against the CURRENT document state (matches are wrapped in reverse
// document order so earlier offsets stay valid as later ones are wrapped).
function wrapMatch(match: Match): HTMLElement | null {
    const { textNode, offset, length } = match
    if (!textNode || !textNode.parentElement) return null
    try {
        const range = document.createRange()
        const end = Math.min(offset + length, textNode.textContent?.length || 0)
        range.setStart(textNode, offset)
        range.setEnd(textNode, end)
        const mark = document.createElement('mark')
        mark.className = MARK_CLASS
        range.surroundContents(mark)
        return mark
    } catch {
        return null
    }
}

function applyHighlights() {
    // Reset DOM to its pre-mark state before re-wrapping.
    unwrapAllMarks()
    matches.value = collectMatches(query.value)
    // Wrap in reverse document order: collecting marks mutates the DOM, but
    // wrapping back-to-front keeps the offsets of earlier matches valid.
    const newMarks: (HTMLElement | null)[] = []
    for (let i = matches.value.length - 1; i >= 0; i--) {
        const mark = wrapMatch(matches.value[i])
        newMarks.unshift(mark)
    }
    marks.value = newMarks.filter((m): m is HTMLElement => m !== null)
    applyActive()
}

// Add/remove the active class on the mark matching the current activeIndex.
function applyActive() {
    marks.value.forEach((mark, i) => {
        mark.classList.toggle('md-search-match-active', i === activeIndex.value)
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
