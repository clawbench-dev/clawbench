<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <!-- Search results list view -->
      <template v-if="!selectedSession">
        <Search :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('sessionSearch.title') }}</span>
        <button class="acp-resume-header-btn" @click.stop="emit('open-acp-sessions')" :title="t('sessionSearch.loadExternalSession')">
          <Import :size="13" />
          <span>{{ t('sessionSearch.loadExternalSession') }}</span>
        </button>
      </template>
      <!-- Drilldown detail view -->
      <template v-else>
        <button class="detail-back-btn" @click.stop="selectSession(null)">
          <ChevronLeft :size="18" />
        </button>
        <span class="bs-header-title detail-header-title">{{ selectedSession.session_title || t('sessionSearch.untitledSession') }}</span>
        <span v-if="selectedSession.archived" class="detail-archived-badge">{{ t('sessionSearch.archived') }}</span>
      </template>
    </template>

    <!-- ═══ Search results list ═══ -->
    <div v-if="!selectedSession" class="session-search-body">
      <div class="session-search-input-row">
        <SearchInput ref="inputRef" :model-value="searchState.query" :placeholder="t('sessionSearch.placeholder')" @update:model-value="search.setQuery" @enter="listNav.confirm" @down="listNav.down" @up="listNav.up" />
        <button
          ref="modeTriggerRef"
          type="button"
          class="filter-dropdown-btn"
          :title="t('sessionSearch.modeLabel')"
          @click.stop="toggleMenu('mode')"
        >
          <span class="filter-dropdown-label">{{ modeLabel }}</span>
          <ChevronDown :size="12" class="filter-dropdown-caret" />
        </button>
        <button
          ref="archiveTriggerRef"
          type="button"
          class="filter-dropdown-btn"
          :class="{ 'filter-active': searchState.archivedFilter !== 'all' }"
          :title="t('sessionSearch.filterArchive')"
          @click.stop="toggleMenu('archive')"
        >
          <span class="filter-dropdown-label">{{ archiveLabel }}</span>
          <ChevronDown :size="12" class="filter-dropdown-caret" />
        </button>
        <button
          ref="sortTriggerRef"
          type="button"
          class="filter-dropdown-btn"
          :class="{ 'filter-active': searchState.sortOrder !== 'relevance' }"
          :title="t('sessionSearch.sortLabel')"
          @click.stop="toggleMenu('sort')"
        >
          <span class="filter-dropdown-label">{{ sortLabel }}</span>
          <ChevronDown :size="12" class="filter-dropdown-caret" />
        </button>
      </div>

      <PopupMenu
        :show="openMenu === 'mode'"
        :target-element="modeTriggerRef"
        :max-width="150"
        :menu-items-count="2"
        anchor="right"
        @update:show="(v: boolean) => { if (!v) openMenu = null }"
      >
        <button
          v-for="opt in modeOptions"
          :key="opt.value"
          type="button"
          class="filter-menu-item"
          :class="{ selected: searchState.preferMode === opt.value }"
          @click="chooseMode(opt.value)"
        >
          <Check v-if="searchState.preferMode === opt.value" :size="13" class="filter-menu-check" />
          <span v-else class="filter-menu-check" />
          {{ opt.label }}
        </button>
      </PopupMenu>

      <PopupMenu
        :show="openMenu === 'archive'"
        :target-element="archiveTriggerRef"
        :max-width="150"
        :menu-items-count="3"
        anchor="right"
        @update:show="(v: boolean) => { if (!v) openMenu = null }"
      >
        <button
          v-for="opt in archiveOptions"
          :key="opt.value"
          type="button"
          class="filter-menu-item"
          :class="{ selected: searchState.archivedFilter === opt.value }"
          @click="chooseArchive(opt.value)"
        >
          <Check v-if="searchState.archivedFilter === opt.value" :size="13" class="filter-menu-check" />
          <span v-else class="filter-menu-check" />
          {{ opt.label }}
        </button>
      </PopupMenu>

      <PopupMenu
        :show="openMenu === 'sort'"
        :target-element="sortTriggerRef"
        :max-width="150"
        :menu-items-count="3"
        anchor="right"
        @update:show="(v: boolean) => { if (!v) openMenu = null }"
      >
        <button
          v-for="opt in sortOptions"
          :key="opt.value"
          type="button"
          class="filter-menu-item"
          :class="{ selected: searchState.sortOrder === opt.value }"
          @click="chooseSort(opt.value)"
        >
          <Check v-if="searchState.sortOrder === opt.value" :size="13" class="filter-menu-check" />
          <span v-else class="filter-menu-check" />
          {{ opt.label }}
        </button>
      </PopupMenu>

      <div class="session-search-content">
        <LoadingIndicator v-if="searchState.loading" size="md" :label="t('sessionSearch.searching')" />
        <div v-else-if="searchState.error" class="session-search-error">{{ searchState.error }}</div>
        <div v-else-if="searchState.results.length === 0" class="session-search-empty">{{ t('sessionSearch.noResults') }}</div>
        <div v-else class="session-search-results">
          <div class="session-search-count">
            {{ t('sessionSearch.resultCount', { count: searchState.results.length }) }}
            <span v-if="searchState.searchMode" class="session-search-mode">{{ searchModeLabel }}</span>
          </div>
          <div v-for="(session, idx) in searchState.results" :key="session.session_id" class="session-search-item" :class="{ 'session-search-item-active': listNav.activeIndex.value === idx }" @click="selectSession(session)">
            <div class="session-search-item-header">
              <span class="session-search-item-title">{{ session.session_title || t('sessionSearch.untitledSession') }}</span>
              <span class="session-search-item-meta">{{ formatRelativeTime(session.created_at) }}</span>
            </div>
            <div v-if="session.chunks.length > 0" class="session-search-item-preview" v-html="getPreviewHtml(session)" />
            <div class="session-search-item-footer">
              <span v-if="session.archived" class="session-search-item-archived">{{ t('sessionSearch.archived') }}</span>
              <span v-if="session.backend" class="session-search-item-backend">{{ session.backend }}</span>
              <span v-if="!isBrowseMode && session.chunks.length > 0" class="session-search-item-chunks">{{ t('sessionSearch.chunks', { count: session.match_count }) }}</span>
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
        <span v-if="!isBrowseMode && detailChunks.length > 0" class="detail-meta-badge detail-meta-count">{{ t('sessionSearch.chunks', { count: selectedSession.match_count }) }}</span>
        <span class="detail-meta-time">{{ formatRelativeTime(selectedSession.created_at) }}</span>
      </div>

      <!-- Chunk list (scrollable via .bs-body) -->
      <LoadingIndicator v-if="lazyLoading" size="md" :label="t('sessionSearch.loadingPreview')" />
      <div v-else-if="detailChunks.length === 0" class="detail-empty">{{ t('sessionSearch.noPreview') }}</div>
      <div v-for="chunk in detailChunks" :key="chunk.chunk_id" class="detail-chunk">
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
      <div class="detail-footer-row">
        <button v-if="selectedSession.archived" class="fbtn fbtn-primary detail-resume-btn" @click="emit('resume', selectedSession)">
          <RotateCcw :size="14" />
          {{ t('sessionSearch.resume') }}
        </button>
        <button v-else class="fbtn fbtn-primary detail-resume-btn" @click="emit('open', selectedSession)">
          <MessageSquare :size="14" />
          {{ t('sessionSearch.openSession') }}
        </button>
        <button v-if="selectedSession.archived" class="fbtn fbtn-danger detail-destroy-btn" @click="emit('destroy', selectedSession)">
          <Trash2 :size="14" />
          {{ t('sessionSearch.destroy') }}
        </button>
      </div>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUpdate, onBeforeUnmount, onUnmounted, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, ChevronLeft, ChevronDown, Check, User, Bot, RotateCcw, Import, MessageSquare, Trash2 } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { useSessionSearch, fetchSessionFirstMessage, type SessionSearchResult, type ChunkHit, type SessionArchiveFilter, type SessionSortOrder } from '@/composables/useSessionSearch'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { highlightTextByPositions } from '@/utils/searchUtils'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { escapeHtml } from '@/utils/html.ts'
import { formatRelativeTime } from '@/utils/format'

const { t } = useI18n()

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; resume: [session: SessionSearchResult]; open: [session: SessionSearchResult]; destroy: [session: SessionSearchResult]; 'open-acp-sessions': [] }>()

const { state: searchState, setQuery, browse, clear, setFilters } = useSessionSearch()
const search = { state: searchState, setQuery, browse, clear, setFilters }

const selectedSession = ref<SessionSearchResult | null>(null)
const inputRef = ref<InstanceType<typeof SearchInput> | null>(null)

// ── Search mode / filter / sort dropdowns ──
// All three collapse into compact triggers on the search row, keeping the
// header to a single line. Their options live in PopupMenu popovers.
const openMenu = ref<'mode' | 'archive' | 'sort' | null>(null)
const modeTriggerRef = ref<HTMLElement | null>(null)
const archiveTriggerRef = ref<HTMLElement | null>(null)
const sortTriggerRef = ref<HTMLElement | null>(null)

function toggleMenu(menu: 'mode' | 'archive' | 'sort') {
  openMenu.value = openMenu.value === menu ? null : menu
}

const modeOptions = computed(() => [
  { value: 'hybrid' as const, label: t('sessionSearch.modeHybrid') },
  { value: 'fts' as const, label: t('sessionSearch.modeFts') },
])

const modeLabel = computed(() =>
  modeOptions.value.find(o => o.value === searchState.preferMode)?.label ?? ''
)

function chooseMode(mode: 'hybrid' | 'fts') {
  openMenu.value = null
  setMode(mode)
}

const archiveOptions = computed(() => [
  { value: 'all' as SessionArchiveFilter, label: t('sessionSearch.archiveAll') },
  { value: 'active' as SessionArchiveFilter, label: t('sessionSearch.archiveActive') },
  { value: 'archived' as SessionArchiveFilter, label: t('sessionSearch.archiveArchived') },
])

const sortOptions = computed(() => [
  { value: 'relevance' as SessionSortOrder, label: t('sessionSearch.sortRelevance') },
  { value: 'newest' as SessionSortOrder, label: t('sessionSearch.sortNewest') },
  { value: 'oldest' as SessionSortOrder, label: t('sessionSearch.sortOldest') },
])

const archiveLabel = computed(() =>
  archiveOptions.value.find(o => o.value === searchState.archivedFilter)?.label ?? ''
)

const sortLabel = computed(() =>
  sortOptions.value.find(o => o.value === searchState.sortOrder)?.label ?? ''
)

function chooseArchive(filter: SessionArchiveFilter) {
  openMenu.value = null
  setArchiveFilter(filter)
}

function chooseSort(sort: SessionSortOrder) {
  openMenu.value = null
  setSortOrder(sort)
}

// ── Lazy first-message preview (browse mode only) ──
// Browse results carry no chunk content; fetch the session's first message on
// demand when its detail view is opened. Search results already have chunks.
const lazyChunks = ref<ChunkHit[]>([])
const lazyLoading = ref(false)
let lazyRequestId = 0

async function loadFirstMessage(session: SessionSearchResult) {
  const requestId = ++lazyRequestId
  lazyChunks.value = []
  lazyLoading.value = true
  const chunk = await fetchSessionFirstMessage(session.session_id)
  // Ignore stale responses if the user navigated away or picked another session.
  if (requestId !== lazyRequestId) return
  lazyLoading.value = false
  lazyChunks.value = chunk ? [chunk] : []
}

// Open a session's detail view. Browse results have no chunk content, so their
// first message is fetched lazily; search results already carry their hits.
function selectSession(session: SessionSearchResult | null | undefined) {
  selectedSession.value = session ?? null
  if (!session) {
    lazyRequestId++
    lazyChunks.value = []
    lazyLoading.value = false
    return
  }
  if (isBrowseMode.value) {
    void loadFirstMessage(session)
  } else {
    lazyRequestId++
    lazyChunks.value = []
    lazyLoading.value = false
  }
}

// ── Search mode selector ──
function setMode(mode: 'hybrid' | 'fts') {
  searchState.preferMode = mode
  // Re-search with new mode if there's an active query
  if (searchState.query.trim()) {
    search.setQuery(searchState.query)
  }
}

// ── Archive filter / sort order ──
function setArchiveFilter(filter: SessionArchiveFilter) {
  if (searchState.archivedFilter === filter) return
  search.setFilters({ archived: filter })
}

function setSortOrder(sort: SessionSortOrder) {
  if (searchState.sortOrder === sort) return
  search.setFilters({ sort })
}

// ── Keyboard ↑/↓ + Enter navigation over results ──
const listNav = useListNav({
  getCount: () => searchState.results.length,
  onConfirm: (idx) => {
    selectSession(searchState.results[idx])
  },
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation also works when focus leaves the search box
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index: number) {
  const items = document.querySelectorAll('.session-search-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

watch(() => searchState.results, () => listNav.reset())

const searchModeLabel = computed(() => {
  if (!searchState.searchMode) return ''
  return searchState.searchMode === 'hybrid' ? t('sessionSearch.modeHybrid') : t('sessionSearch.modeFts')
})

// Browse mode (empty query) lists all sessions newest-first; its preview chunk
// is not a search hit, so the match count label is hidden.
const isBrowseMode = computed(() => searchState.searchMode === 'recent')

// Chunks shown in the detail view. Search results carry their hits; browse
// results carry none, so their first message is lazily fetched on drilldown.
const detailChunks = computed<ChunkHit[]>(() =>
  isBrowseMode.value ? lazyChunks.value : (selectedSession.value?.chunks ?? [])
)

// ── Back handler for drilldown ──
const unregisterBack = registerBackHandler({
  id: 'session-search-detail',
  priority: PRIORITY_OVERLAY + 1,
  canGoBack: () => selectedSession.value !== null,
  goBack: () => { selectSession(null) },
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
  const map: Record<number, string> = {}
  for (const chunk of detailChunks.value) {
    map[chunk.chunk_id] = renderMarkdownHtml(chunk.chunk_text, {
      skipEnhancements: true,
      wrapTables: false,
    })
  }
  return map
})

// ── Apply highlights via DOM after rendering ──
watch([selectedSession, lazyChunks], () => {
  if (!selectedSession.value) return
  nextTick(() => applyHighlights())
})

function applyHighlights() {
  if (!selectedSession.value) return
  for (const chunk of detailChunks.value) {
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
    // Wait for BottomSheet slide-up animation (250ms) to complete before focusing
    await new Promise(r => setTimeout(r, 300))
    inputRef.value?.focus()
    // Default to browsing all sessions newest-first (no query entered)
    search.browse()
  } else {
    search.clear()
    selectSession(null)
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
  gap: 6px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.session-search-input-row :deep(.search-pill) {
  flex: 1;
  min-width: 0;
}

/* ── Compact filter/sort dropdown triggers ──
   Kept on the search row so the header stays a single line. Each trigger shows
   the current value; a non-default selection is highlighted. */
.filter-dropdown-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
  max-width: 88px;
  height: 26px;
  padding: 0 6px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 6px;
  background: var(--bg-primary, #fff);
  color: var(--text-muted, #999);
  font-size: 10px;
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}

.filter-dropdown-btn.filter-active {
  border-color: var(--accent-color, #4a90d9);
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}

.filter-dropdown-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-dropdown-caret {
  flex-shrink: 0;
  opacity: 0.7;
}

@media (hover: hover) {
  .filter-dropdown-btn:hover {
    background: var(--bg-secondary, #f8f9fa);
    color: var(--text-secondary, #666);
  }
}

/* ── Dropdown menu items (rendered inside PopupMenu, teleported to body) ── */
.filter-menu-item {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: none;
  color: var(--text-primary, #1a1a1a);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.12s, color 0.12s;
}

.filter-menu-item.selected {
  color: var(--accent-color, #4a90d9);
  font-weight: 500;
}

.filter-menu-check {
  flex-shrink: 0;
  width: 13px;
  display: inline-flex;
  justify-content: center;
}

@media (hover: hover) {
  .filter-menu-item:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
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

@media (hover: hover) {
  .session-search-item:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.session-search-item-active {
  background: var(--bg-secondary, #f8f9fa);
  border-radius: 0;
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
  background: color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
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

.session-search-item-archived {
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

@media (hover: hover) {
  .detail-back-btn:hover {
    background: rgba(0, 102, 204, 0.1);
  }

  .acp-resume-header-btn:hover {
    background: rgba(0, 102, 204, 0.1);
    color: var(--accent-color, #4a90d9);
  }
}

/* ACP "load external session" button — right side of the search header */
.acp-resume-header-btn {
  height: 26px;
  padding: 0 8px;
  border: none;
  background: none;
  color: var(--text-secondary, #495057);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-radius: 6px;
  font-size: 12px;
  transition: background 0.15s, color 0.15s;
  margin-left: auto;
  flex-shrink: 0;
  white-space: nowrap;
}

/* Higher specificity than .bs-header-title so flex:1 reliably wins, keeping the
   title an independently shrinkable area (ellipsis) and the archived badge a
   separate, fixed right-aligned area instead of overlapping the title tail. */
.bs-header .detail-header-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1 1 0;
  min-width: 0;
  /* Override .bs-header-title's inline-flex, which breaks text-overflow */
  display: block;
}

.detail-archived-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(230, 162, 60, 0.12);
  color: var(--color-warning, #e6a23c);
  font-weight: 500;
  flex-shrink: 0;
  margin-left: 6px;
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

.detail-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
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
  background: color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
  border-radius: 2px;
  padding: 0 1px;
  color: inherit;
}

.detail-footer-row {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  width: 100%;
  flex-shrink: 0;
}

/* Override BottomSheet's default footer padding/border to stay compact
   (matches the task-exec-detail bottom action bar). */
:deep(.bs-footer) {
  padding: 6px 8px;
  border-top: none;
  gap: 6px;
}
</style>

<style>
/* Dark theme overrides — non-scoped for [data-theme] selector */
[data-theme-base="dark"] .session-search-item-preview mark {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 28%, transparent);
  color: inherit;
}

[data-theme-base="dark"] .detail-chunk-text mark.search-hl {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 28%, transparent);
  color: inherit;
}

[data-theme-base="dark"] .detail-chunk {
  border-color: rgba(255, 255, 255, 0.06);
}

[data-theme-base="dark"] .filter-dropdown-btn {
  background: transparent;
  border-color: rgba(255, 255, 255, 0.12);
  color: var(--text-muted, #999);
}

[data-theme-base="dark"] .filter-dropdown-btn.filter-active {
  border-color: var(--accent-color, #4a90d9);
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 18%, transparent);
}

@media (hover: hover) {
  [data-theme-base="dark"] .filter-dropdown-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-primary, #fff);
  }

  [data-theme-base="dark"] .filter-menu-item:hover {
    background: rgba(255, 255, 255, 0.06);
  }
}
</style>
