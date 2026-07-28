<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <!-- Search results list view -->
      <template v-if="!selectedSession">
        <Search :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('sessionSearch.title') }}</span>
      </template>
      <!-- Drilldown detail view -->
      <template v-else>
        <button class="detail-back-btn" @click.stop="selectedSession = null">
          <ChevronLeft :size="18" />
        </button>
        <span class="bs-header-title detail-header-title">{{ selectedSession.session_title || t('sessionSearch.untitledSession') }}</span>
        <span v-if="selectedSession.deleted" class="detail-deleted-badge">{{ t('sessionSearch.deleted') }}</span>
      </template>
    </template>

    <!-- ═══ Search results list ═══ -->
    <div v-if="!selectedSession" class="session-search-body">
      <div class="session-search-input-row">
        <SearchInput ref="inputRef" :model-value="searchState.query" :placeholder="t('sessionSearch.placeholder')" @update:model-value="search.setQuery" />
        <div class="mode-selector">
          <button class="mode-btn" :class="{ active: searchState.preferMode === 'hybrid' }" @click="setMode('hybrid')">{{ t('sessionSearch.modeHybrid') }}</button>
          <button class="mode-btn" :class="{ active: searchState.preferMode === 'fts' }" @click="setMode('fts')">{{ t('sessionSearch.modeFts') }}</button>
        </div>
      </div>

      <div class="session-search-content">
        <div v-if="!searchState.query.trim()" class="session-search-empty">{{ t('sessionSearch.noQuery') }}</div>
        <div v-else-if="searchState.loading" class="session-search-empty">{{ t('sessionSearch.searching') }}</div>
        <div v-else-if="searchState.error" class="session-search-error">{{ searchState.error }}</div>
        <div v-else-if="searchState.results.length === 0" class="session-search-empty">{{ t('sessionSearch.noResults') }}</div>
        <div v-else class="session-search-results">
          <div class="session-search-count">
            {{ t('sessionSearch.resultCount', { count: searchState.results.length }) }}
            <span v-if="searchState.searchMode" class="session-search-mode">{{ searchModeLabel }}</span>
          </div>
          <div v-for="session in searchState.results" :key="session.session_id" class="session-search-item" @click="selectedSession = session">
            <div class="session-search-item-header">
              <span class="session-search-item-title">{{ session.session_title || t('sessionSearch.untitledSession') }}</span>
              <span class="session-search-item-meta">{{ formatRelativeTime(session.created_at) }}</span>
            </div>
            <div v-if="session.chunks.length > 0" class="session-search-item-preview" v-html="getPreviewHtml(session)" />
            <div class="session-search-item-footer">
              <span v-if="session.deleted" class="session-search-item-deleted">{{ t('sessionSearch.deleted') }}</span>
              <span v-if="session.backend" class="session-search-item-backend">{{ session.backend }}</span>
              <span class="session-search-item-chunks">{{ t('sessionSearch.chunks', { count: session.match_count }) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══ Drilldown detail view ═══ -->
    <div v-else class="detail-page">
      <!-- Session meta bar -->
      <div class="detail-meta-bar">
        <span v-if="selectedSession.backend" class="detail-meta-badge detail-meta-backend">{{ selectedSession.backend }}</span>
        <span class="detail-meta-badge detail-meta-count">{{ t('sessionSearch.chunks', { count: selectedSession.match_count }) }}</span>
        <span class="detail-meta-time">{{ formatRelativeTime(selectedSession.created_at) }}</span>
      </div>

      <!-- Chunk list (scrollable via .bs-body) -->
      <div v-for="chunk in selectedSession.chunks" :key="chunk.chunk_id" class="detail-chunk">
        <div class="detail-chunk-role" :class="'role-' + chunk.role">
          <User :size="11" v-if="chunk.role === 'user'" />
          <Bot :size="11" v-else />
          {{ chunk.role === 'user' ? t('sessionSearch.roleUser') : t('sessionSearch.roleAssistant') }}
        </div>
        <div
          :ref="el => setChunkRef(chunk.chunk_id, el)"
          class="detail-chunk-text markdown-body"
          v-html="renderedChunks[chunk.chunk_id] || ''"
        />
      </div>
    </div>

    <!-- Detail view footer (uses BottomSheet's footer slot — fixed at bottom) -->
    <template v-if="selectedSession" #footer>
      <button v-if="selectedSession.deleted" class="detail-resume-btn" @click="emit('resume', selectedSession)">
        <RotateCcw :size="14" />
        {{ t('sessionSearch.resume') }}
      </button>
      <button v-else class="detail-resume-btn" @click="emit('open', selectedSession)">
        <MessageSquare :size="14" />
        {{ t('sessionSearch.openSession') }}
      </button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUpdate, onBeforeUnmount, onUnmounted, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, ChevronLeft, User, Bot, RotateCcw, MessageSquare } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { useSessionSearch, type SessionSearchResult } from '@/composables/useSessionSearch'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { highlightTextByPositions } from '@/utils/searchUtils'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { escapeHtml } from '@/utils/html.ts'
import { formatRelativeTime } from '@/utils/format'

const { t } = useI18n()

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; resume: [session: SessionSearchResult]; open: [session: SessionSearchResult] }>()

const { state: searchState, setQuery, clear } = useSessionSearch()
const search = { state: searchState, setQuery, clear }

const selectedSession = ref<SessionSearchResult | null>(null)
const inputRef = ref<InstanceType<typeof SearchInput> | null>(null)

// ── Search mode selector ──
function setMode(mode: 'hybrid' | 'fts') {
  searchState.preferMode = mode
  // Re-search with new mode if there's an active query
  if (searchState.query.trim()) {
    search.setQuery(searchState.query)
  }
}

const searchModeLabel = computed(() => {
  if (!searchState.searchMode) return ''
  return searchState.searchMode === 'hybrid' ? t('sessionSearch.modeHybrid') : t('sessionSearch.modeFts')
})

// ── Back handler for drilldown ──
const unregisterBack = registerBackHandler({
  id: 'session-search-detail',
  priority: PRIORITY_OVERLAY + 1,
  canGoBack: () => selectedSession.value !== null,
  goBack: () => { selectedSession.value = null },
})
onUnmounted(unregisterBack)

// ── Chunk DOM refs for highlight application ──
const chunkRefs = new Map<number, HTMLElement>()
function setChunkRef(id: number, el: Element | ComponentPublicInstance | null) {
  const htmlEl = el instanceof HTMLElement ? el : null
  if (htmlEl) chunkRefs.set(id, htmlEl)
  else chunkRefs.delete(id)
}
onBeforeUpdate(() => chunkRefs.clear())
onBeforeUnmount(() => chunkRefs.clear())

// ── Markdown rendering ──
const renderedChunks = computed(() => {
  if (!selectedSession.value) return {} as Record<number, string>
  const map: Record<number, string> = {}
  for (const chunk of selectedSession.value.chunks) {
    map[chunk.chunk_id] = renderMarkdownHtml(chunk.chunk_text, {
      skipEnhancements: true,
      wrapTables: false,
    })
  }
  return map
})

// ── Apply highlights via DOM after rendering ──
watch(selectedSession, () => {
  if (!selectedSession.value) return
  nextTick(() => applyHighlights())
})

function applyHighlights() {
  if (!selectedSession.value) return
  for (const chunk of selectedSession.value.chunks) {
    const el = chunkRefs.get(chunk.chunk_id)
    if (!el) continue
    // Clear previous highlights
    el.querySelectorAll('mark.search-hl').forEach(m => {
      const parent = m.parentNode
      if (parent) {
        parent.replaceChild(document.createTextNode(m.textContent || ''), m)
        parent.normalize()
      }
    })
    if (!chunk.match_positions || chunk.match_positions.length === 0) continue
    // Convert rune-based positions to UTF-16 indices before extracting terms
    const text = chunk.chunk_text
    const runes = [...text]
    const runeToIndex: number[] = []
    let idx = 0
    for (let i = 0; i < runes.length; i++) {
      runeToIndex.push(idx)
      idx += runes[i].length
    }
    runeToIndex.push(idx)
    const terms = [...new Set(
      chunk.match_positions
        .map(p => text.slice(
          runeToIndex[Math.min(p.start, runes.length)],
          runeToIndex[Math.min(p.end, runes.length)]
        ))
        .filter(t => t.length > 0)
    )]
    if (terms.length === 0) continue
    highlightTermsInElement(el, terms)
  }
}

function highlightTermsInElement(el: HTMLElement, terms: string[]) {
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null)
  const textNodes: Text[] = []
  while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

  for (const node of textNodes) {
    const content = node.textContent || ''
    const lowerContent = content.toLowerCase()
    const ranges: { start: number; end: number }[] = []

    for (const term of terms) {
      const lowerTerm = term.toLowerCase()
      let idx = lowerContent.indexOf(lowerTerm)
      while (idx !== -1) {
        ranges.push({ start: idx, end: idx + term.length })
        idx = lowerContent.indexOf(lowerTerm, idx + 1)
      }
    }
    if (ranges.length === 0) continue

    ranges.sort((a, b) => a.start - b.start)

    const parent = node.parentNode
    if (!parent) continue
    let lastIdx = 0
    const frag = document.createDocumentFragment()

    for (const r of ranges) {
      if (r.start < lastIdx) continue
      if (r.start > lastIdx) {
        frag.appendChild(document.createTextNode(content.slice(lastIdx, r.start)))
      }
      const mark = document.createElement('mark')
      mark.className = 'search-hl'
      mark.textContent = content.slice(r.start, r.end)
      frag.appendChild(mark)
      lastIdx = r.end
    }
    if (lastIdx < content.length) {
      frag.appendChild(document.createTextNode(content.slice(lastIdx)))
    }
    parent.replaceChild(frag, node)
  }
}

// ── Search list preview ──
function getPreviewHtml(session: SessionSearchResult) {
  const firstChunk = session.chunks[0]
  if (!firstChunk) return ''
  const text = firstChunk.chunk_text || ''
  // Slice by rune count (150 runes, not 150 UTF-16 units) for CJK safety
  const runes = [...text]
  const maxRunes = 150
  const previewRunes = runes.slice(0, maxRunes)
  const preview = previewRunes.join('')
  if (firstChunk.match_positions && firstChunk.match_positions.length > 0) {
    // match_positions are rune-based; clamp to preview rune boundary
    const clamped = firstChunk.match_positions
      .filter(p => p.start < maxRunes)
      .map(p => ({ start: p.start, end: Math.min(p.end, maxRunes) }))
    return highlightTextByPositions(preview, clamped)
  }
  return escapeHtml(preview)
}

// ── Lifecycle ──
watch(() => props.open, async (val) => {
  if (val) {
    await nextTick()
    inputRef.value?.focus()
  } else {
    search.clear()
    selectedSession.value = null
  }
})

function handleClose() {
  emit('close')
}

function focusSearchInput() {
  inputRef.value?.focus()
}

defineExpose({ focusSearchInput })
</script>

<style scoped>
/* ── Search results list ── */
.session-search-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.session-search-input-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.session-search-input-row :deep(.search-pill) {
  flex: 1;
}

.mode-selector {
  display: flex;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 6px;
  overflow: hidden;
  flex-shrink: 0;
}

.mode-btn {
  padding: 4px 8px;
  font-size: 11px;
  border: none;
  background: var(--bg-primary, #fff);
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: all 0.15s;
  white-space: nowrap;
}

.mode-btn:not(:last-child) {
  border-right: 1px solid var(--border-color, #e5e5e5);
}

.mode-btn.active {
  background: var(--accent-color, #4a90d9);
  color: #fff;
}

.mode-btn:not(.active):hover {
  background: var(--bg-secondary, #f8f9fa);
}

.session-search-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.session-search-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
  flex-shrink: 0;
}

.session-search-error {
  padding: 24px;
  text-align: center;
  color: var(--color-error, #e74c3c);
  font-size: 13px;
  flex-shrink: 0;
}

.session-search-results {
  flex: 1;
  overflow-y: auto;
}

.session-search-count {
  padding: 6px 14px;
  font-size: 11px;
  color: var(--text-muted, #999);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.session-search-mode {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(124, 58, 237, 0.08);
  color: var(--color-purple, #7c3aed);
  margin-left: 6px;
  font-weight: 500;
}

.session-search-item {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #f0f0f0);
  cursor: pointer;
  transition: background 0.1s;
}

.session-search-item:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.session-search-item-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 4px;
}

.session-search-item-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.session-search-item-meta {
  font-size: 11px;
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.session-search-item-preview {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary, #666);
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.session-search-item-preview :deep(mark) {
  background: rgba(255, 230, 0, 0.5);
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
}

.session-search-item-footer {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--text-muted, #999);
}

.session-search-item-deleted {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(230, 162, 60, 0.12);
  color: var(--color-warning, #e6a23c);
}

.session-search-item-backend {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--bg-tertiary, #eee);
  color: var(--text-secondary, #666);
}

.session-search-item-chunks {
  font-size: 10px;
}

/* ── Drilldown detail view ── */
.detail-back-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: none;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  transition: background 0.15s;
  flex-shrink: 0;
  margin-right: 2px;
}

.detail-back-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}

.detail-header-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.detail-deleted-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(230, 162, 60, 0.12);
  color: var(--color-warning, #e6a23c);
  font-weight: 500;
  flex-shrink: 0;
  margin-left: auto;
}

.detail-page {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.detail-meta-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.detail-meta-badge {
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 4px;
  font-weight: 500;
}

.detail-meta-backend {
  background: rgba(0, 102, 204, 0.08);
  color: var(--accent-color, #4a90d9);
}

.detail-meta-count {
  background: rgba(124, 58, 237, 0.08);
  color: var(--color-purple, #7c3aed);
}

.detail-meta-time {
  font-size: 11px;
  color: var(--text-muted, #999);
  margin-left: auto;
}

.detail-chunk {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, rgba(0, 0, 0, 0.04));
}

.detail-chunk-role {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 0;
  letter-spacing: 0.3px;
}

.detail-chunk-role.role-user {
  color: var(--accent-color, #4a90d9);
}

.detail-chunk-role.role-assistant {
  color: var(--color-purple, #7c3aed);
}

.detail-chunk-text {
  font-size: 13px;
  line-height: 1.6;
  padding: 4px 0 0;
  word-break: break-word;
  overflow-wrap: break-word;
}

.detail-chunk-text :deep(mark.search-hl) {
  background: rgba(255, 230, 0, 0.5);
  border-radius: 2px;
  padding: 0 1px;
  color: inherit;
}

.detail-resume-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 16px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color, #4a90d9);
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}

.detail-resume-btn:hover {
  opacity: 0.85;
}

.detail-resume-btn:active {
  opacity: 0.7;
}
</style>

<style>
/* Dark theme overrides — non-scoped for [data-theme] selector */
[data-theme="dark"] .session-search-item-preview mark {
  background: rgba(255, 230, 0, 0.35);
  color: inherit;
}

[data-theme="dark"] .detail-chunk-text mark.search-hl {
  background: rgba(255, 230, 0, 0.35);
  color: inherit;
}

[data-theme="dark"] .detail-chunk {
  border-color: rgba(255, 255, 255, 0.06);
}

[data-theme="dark"] .mode-selector {
  border-color: rgba(255, 255, 255, 0.12);
}

[data-theme="dark"] .mode-btn {
  background: transparent;
  color: var(--text-muted, #999);
}

[data-theme="dark"] .mode-btn:not(:last-child) {
  border-right-color: rgba(255, 255, 255, 0.12);
}

[data-theme="dark"] .mode-btn.active {
  background: var(--accent-color, #4a90d9);
  color: #fff;
}

[data-theme="dark"] .mode-btn:not(.active):hover {
  background: rgba(255, 255, 255, 0.06);
}
</style>
