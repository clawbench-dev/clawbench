<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Search :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('sessionSearch.title') }}</span>
    </template>

    <div class="session-search-body">
      <div class="session-search-input-row">
        <SearchInput ref="inputRef" :model-value="searchState.query" :placeholder="t('sessionSearch.placeholder')" @update:model-value="search.setQuery" />
      </div>

      <div class="session-search-content">
        <!-- States: no query, loading, error, no results, results -->
        <div v-if="!searchState.query.trim()" class="session-search-empty">{{ t('sessionSearch.noQuery') }}</div>
        <div v-else-if="searchState.loading" class="session-search-empty">{{ t('sessionSearch.searching') }}</div>
        <div v-else-if="searchState.error" class="session-search-error">{{ searchState.error }}</div>
        <div v-else-if="searchState.results.length === 0" class="session-search-empty">{{ t('sessionSearch.noResults') }}</div>
        <div v-else class="session-search-results">
          <div class="session-search-count">{{ t('sessionSearch.resultCount', { count: searchState.results.length }) }}</div>
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

    <SessionSearchDetailModal :open="!!selectedSession" :session="selectedSession" @close="selectedSession = null" @resume="handleResume" />
  </BottomSheet>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SessionSearchDetailModal from './SessionSearchDetailModal.vue'
import { useSessionSearch } from '@/composables/useSessionSearch'
import { highlightTextByPositions } from '@/utils/searchUtils'
import { escapeHtml } from '@/utils/html.ts'
import { formatRelativeTime } from '@/utils/format'

const { t } = useI18n()

const props = defineProps({
  open: Boolean,
})
const emit = defineEmits(['close', 'resume'])

const { state: searchState, setQuery, clear } = useSessionSearch()
const search = { state: searchState, setQuery, clear }

const selectedSession = ref(null)
const inputRef = ref(null)

watch(() => props.open, async (val) => {
  if (val) {
    await nextTick()
    inputRef.value?.focus()
  } else {
    search.clear()
    selectedSession.value = null
  }
})

function getPreviewHtml(session) {
  const firstChunk = session.chunks[0]
  if (!firstChunk) return ''
  const text = firstChunk.chunk_text || ''
  const preview = text.slice(0, 150)
  if (firstChunk.match_positions && firstChunk.match_positions.length > 0) {
    // Filter and clamp positions to the truncated preview length
    const clamped = firstChunk.match_positions
      .filter(p => p.start < 150)
      .map(p => ({ start: p.start, end: Math.min(p.end, 150) }))
    return highlightTextByPositions(preview, clamped)
  }
  return escapeHtml(preview)
}

function handleClose() {
  emit('close')
}

function handleResume(session) {
  emit('resume', session)
  selectedSession.value = null
}
</script>

<style scoped>
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
  background: rgba(239, 68, 68, 0.12);
  color: var(--color-error, #e74c3c);
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
</style>

<style>
/* Dark theme highlight - must be non-scoped for [data-theme] selector */
[data-theme="dark"] .session-search-item-preview mark {
  background: rgba(255, 230, 0, 0.35);
  color: inherit;
}
</style>
