<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Search :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('search.title') }}</span>
      <div v-if="file?.path" class="bs-header-description">
        <HeaderMarquee :text="file.path">{{ file.path }}</HeaderMarquee>
      </div>
    </template>

    <div class="search-body">
      <div class="search-input-row">
        <SearchInput ref="inputRef" v-model="query" :placeholder="t('search.placeholder')" @enter="jumpToFirst" @dblclick="query = ''" />
      </div>

      <div class="search-content">
        <div v-if="!file?.content" class="search-empty">{{ t('search.noContent') }}</div>
        <div v-else-if="!query.trim()" class="search-empty">{{ t('search.enterKeyword') }}</div>
        <div v-else-if="results.length === 0" class="search-empty">{{ t('search.notFound', { query }) }}</div>
        <div v-else class="search-results">
          <div class="search-results-count">{{ t('search.matchCount', { count: results.length }) }}</div>
          <div
            v-for="(r, idx) in results"
            :key="isRenderedView ? idx : r.line"
            class="search-result-item"
            @click="jumpTo(r)"
          >
            <span class="search-result-lnum">{{ isRenderedView ? idx + 1 : r.line }}</span>
            <span class="search-result-text" v-html="r.highlighted" />
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { Search } from 'lucide-vue-next'
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from './BottomSheet.vue'
import HeaderMarquee from './HeaderMarquee.vue'
import SearchInput from './SearchInput.vue'
import { escapeHtml } from '@/utils/html.ts'
import { getFileType } from '@/utils/fileType.ts'
import { searchRawContent, highlightText, BLOCK_TAGS } from '@/utils/searchUtils.ts'

const { t } = useI18n()

const props = defineProps({
  file: Object,
  open: Boolean,
  viewMode: String, // 'rendered' | 'raw' | undefined
})
const emit = defineEmits(['close', 'jump'])

const query = ref('')
const inputRef = ref(null)

// Whether the current file is rendered as markdown/HTML (has .markdown-body DOM)
const isRenderedView = computed(() => {
  if (!props.file?.name) return false
  const ft = getFileType(props.file.name)
  return (ft.isMarkdown || ft.isHtml) && props.viewMode === 'rendered'
})

watch(() => props.open, async (val) => {
  if (val) {
    await nextTick()
    inputRef.value?.focus()
  }
})

// Clear query when the file changes
watch(() => props.file?.path, () => {
  query.value = ''
})

function handleClose() {
  emit('close')
}

// --- Rendered mode: search DOM text ---

// BLOCK_TAGS is imported from searchUtils

function findBlockAncestor(node) {
  let el = node.parentElement
  while (el) {
    if (BLOCK_TAGS.has(el.tagName) && el.closest('.markdown-body')) {
      return el
    }
    el = el.parentElement
  }
  return node.parentElement
}

function searchRenderedContent(q) {
  const container = document.querySelector('.markdown-body')
  if (!container) return []

  const lowerQ = q.toLowerCase()
  const out = []
  const seen = new Set()
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT, null)

  while (walker.nextNode()) {
    const textNode = walker.currentNode
    const text = textNode.textContent
    if (!text || !text.toLowerCase().includes(lowerQ)) continue

    const block = findBlockAncestor(textNode)
    if (!block || seen.has(block)) continue
    seen.add(block)

    const fullText = block.textContent.trim()
    const idx = fullText.toLowerCase().indexOf(lowerQ)
    const start = Math.max(0, idx - 30)
    const end = Math.min(fullText.length, idx + q.length + 30)
    const snippet = (start > 0 ? '...' : '') + fullText.slice(start, end) + (end < fullText.length ? '...' : '')

    out.push({
      line: out.length,
      text: snippet,
      highlighted: highlightText(snippet, q),
      _blockEl: block,
    })
  }
  return out
}

// --- Raw mode: search source lines ---
// searchRawContent is imported from searchUtils

const results = computed(() => {
  if (!props.file?.content || !query.value.trim()) return []
  const q = query.value.trim()
  if (isRenderedView.value) {
    return searchRenderedContent(q)
  }
  return searchRawContent(q, props.file.content, props.file.name || '')
})

function jumpTo(result) {
  if (isRenderedView.value) {
    scrollToRenderedMatch(result)
    emit('close')
  } else {
    emit('jump', result.line)
    emit('close')
  }
}

function scrollToRenderedMatch(result) {
  const block = result._blockEl
  if (!block) return
  block.scrollIntoView({ behavior: 'smooth', block: 'center' })
  block.classList.add('line-flash')
  block.addEventListener('animationend', () => block.classList.remove('line-flash'), { once: true })
}

function jumpToFirst() {
  if (results.value.length > 0) {
    jumpTo(results.value[0])
  }
}

defineExpose({
  query,
  results,
  isRenderedView,
  _setQuery(val) { query.value = val },
  _getQuery() { return query.value },
  _getResults() { return results.value },
  _jumpTo(result) { jumpTo(result) },
  _jumpToFirst() { jumpToFirst() },
})
</script>

<style scoped>
.search-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary, #212529);
}

.search-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.search-input-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.search-input-row :deep(.search-pill) {
  flex: 1;
}

.search-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.search-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
  flex-shrink: 0;
}

.search-results {
  flex: 1;
  overflow-y: auto;
}

.search-results-count {
  padding: 6px 14px;
  font-size: 11px;
  color: var(--text-muted, #999);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.search-result-item {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 5px 14px;
  cursor: pointer;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  border-bottom: 1px solid var(--border-color, #f0f0f0);
  transition: background 0.1s;
}

.search-result-item:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.search-result-lnum {
  color: var(--text-muted, #999);
  min-width: 32px;
  text-align: right;
  flex-shrink: 0;
  user-select: none;
}

.search-result-text {
  white-space: pre-wrap;
  word-break: break-all;
}

.search-result-text :deep(em) {
  font-style: normal;
}

.search-result-text :deep(.hljs-keyword),
.search-result-text :deep(.hljs-selector-tag),
.search-result-text :deep(.hljs-built_in) { color: #a626a4; }
.search-result-text :deep(.hljs-type),
.search-result-text :deep(.hljs-class) { color: #c18401; }
.search-result-text :deep(.hljs-string),
.search-result-text :deep(.hljs-addition) { color: #50a14f; }
.search-result-text :deep(.hljs-number),
.search-result-text :deep(.hljs-literal) { color: #986801; }
.search-result-text :deep(.hljs-comment),
.search-result-text :deep(.hljs-quote) { color: #a0a1a7; }
.search-result-text :deep(.hljs-function),
.search-result-text :deep(.hljs-title) { color: #4078f2; }
.search-result-text :deep(.hljs-variable),
.search-result-text :deep(.hljs-attr) { color: #e45649; }
.search-result-text :deep(.hljs-symbol),
.search-result-text :deep(.hljs-bullet) { color: #0184bc; }
.search-result-text :deep(.hljs-meta) { color: #383a42; }
.search-result-text :deep(.hljs-regexp) { color: #50a14f; }
.search-result-text :deep(.hljs-property) { color: #e45649; }
.search-result-text :deep(.hljs-params) { color: #383a42; }
.search-result-text :deep(.hljs-tag) { color: #e45649; }
.search-result-text :deep(.hljs-name) { color: #a626a4; }
.search-result-text :deep(.hljs-attribute) { color: #50a14f; }
.search-result-text :deep(.hljs-selector-class) { color: #c18401; }
.search-result-text :deep(.hljs-selector-id) { color: #4078f2; }
.search-result-text :deep(mark) {
  background: rgba(255, 230, 0, 0.5);
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
}
</style>

<style>
/* Dark theme for search result syntax - must be non-scoped for [data-theme] selector */
[data-theme="dark"] .search-result-text .hljs-keyword,
[data-theme="dark"] .search-result-text .hljs-selector-tag,
[data-theme="dark"] .search-result-text .hljs-built_in { color: #c678dd; }
[data-theme="dark"] .search-result-text .hljs-type,
[data-theme="dark"] .search-result-text .hljs-class { color: #e5c07b; }
[data-theme="dark"] .search-result-text .hljs-string,
[data-theme="dark"] .search-result-text .hljs-addition { color: #98c379; }
[data-theme="dark"] .search-result-text .hljs-number,
[data-theme="dark"] .search-result-text .hljs-literal { color: #d19a66; }
[data-theme="dark"] .search-result-text .hljs-comment,
[data-theme="dark"] .search-result-text .hljs-quote { color: #5c6370; }
[data-theme="dark"] .search-result-text .hljs-function,
[data-theme="dark"] .search-result-text .hljs-title { color: #61afef; }
[data-theme="dark"] .search-result-text .hljs-variable,
[data-theme="dark"] .search-result-text .hljs-attr { color: #e06c75; }
[data-theme="dark"] .search-result-text .hljs-symbol,
[data-theme="dark"] .search-result-text .hljs-bullet { color: #56b6c2; }
[data-theme="dark"] .search-result-text .hljs-meta { color: #abb2bf; }
[data-theme="dark"] .search-result-text .hljs-regexp { color: #98c379; }
[data-theme="dark"] .search-result-text .hljs-property { color: #e06c75; }
[data-theme="dark"] .search-result-text .hljs-params { color: #abb2bf; }
[data-theme="dark"] .search-result-text .hljs-tag { color: #e06c75; }
[data-theme="dark"] .search-result-text .hljs-name { color: #c678dd; }
[data-theme="dark"] .search-result-text .hljs-attribute { color: #98c379; }
[data-theme="dark"] .search-result-text .hljs-selector-class { color: #e5c07b; }
[data-theme="dark"] .search-result-text .hljs-selector-id { color: #61afef; }
[data-theme="dark"] .search-result-text mark {
  background: rgba(255, 230, 0, 0.35);
  color: inherit;
}
</style>
