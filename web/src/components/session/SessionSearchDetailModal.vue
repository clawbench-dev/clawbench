<template>
  <ModalDialog :open="open" :title="t('sessionSearch.detailTitle')" @close="$emit('close')">
    <div v-if="session" class="search-detail-content">
      <div class="search-detail-session-info">
        <div class="search-detail-session-title">{{ session.session_title || t('sessionSearch.untitledSession') }}</div>
        <div class="search-detail-session-meta">
          <span>{{ formatRelativeTime(session.created_at) }}</span>
          <span v-if="session.backend" class="search-detail-backend">{{ session.backend }}</span>
          <span>{{ t('sessionSearch.chunks', { count: session.match_count }) }}</span>
        </div>
      </div>
      <div class="search-detail-chunks">
        <div v-for="chunk in session.chunks" :key="chunk.chunk_id" class="search-detail-chunk">
          <div class="search-detail-chunk-role" :class="'role-' + chunk.role">
            {{ chunk.role === 'user' ? t('sessionSearch.roleUser') : t('sessionSearch.roleAssistant') }}
          </div>
          <div class="search-detail-chunk-text" v-html="highlightChunk(chunk)" />
        </div>
      </div>
    </div>
    <template #footer>
      <button class="search-detail-resume-btn" @click="$emit('resume', session)">
        {{ t('sessionSearch.resume') }}
        <ChevronRight :size="14" />
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { ChevronRight } from 'lucide-vue-next'
import { highlightTextByPositions } from '@/utils/searchUtils'
import { escapeHtml } from '@/utils/html'
import { formatRelativeTime } from '@/utils/format'
import type { SessionSearchResult, ChunkHit } from '@/composables/useSessionSearch'

defineProps<{
  open: boolean
  session: SessionSearchResult | null
}>()

defineEmits<{
  close: []
  resume: [session: SessionSearchResult | null]
}>()

const { t } = useI18n()

function highlightChunk(chunk: ChunkHit): string {
  if (chunk.match_positions && chunk.match_positions.length > 0) {
    return highlightTextByPositions(chunk.chunk_text, chunk.match_positions)
  }
  return escapeHtml(chunk.chunk_text)
}
</script>

<style scoped>
.search-detail-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.search-detail-session-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.search-detail-session-title {
  font-weight: 600;
  font-size: 15px;
}

.search-detail-session-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--color-text-secondary, #888);
}

.search-detail-backend {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  background: var(--color-bg-tertiary, #e8e8e8);
  color: var(--color-text-secondary, #666);
}

.search-detail-chunks {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 50vh;
  overflow-y: auto;
}

.search-detail-chunk {
  background: var(--color-bg-secondary, #f5f5f5);
  border-radius: 8px;
  padding: 10px 12px;
}

.search-detail-chunk-role {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
}

.search-detail-chunk-role.role-user {
  color: #0066cc;
}

.search-detail-chunk-role.role-assistant {
  color: #8b5cf6;
}

.search-detail-chunk-text {
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.search-detail-chunk-text :deep(mark) {
  background: rgba(255, 230, 0, 0.5);
  border-radius: 2px;
  padding: 0 1px;
}

@media (prefers-color-scheme: dark) {
  .search-detail-chunk-text :deep(mark) {
    background: rgba(255, 230, 0, 0.35);
  }
}

.search-detail-resume-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 10px 16px;
  border: none;
  border-radius: 8px;
  background: #8b5cf6;
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}

.search-detail-resume-btn:hover {
  opacity: 0.85;
}

@media (prefers-color-scheme: dark) {
  .search-detail-resume-btn {
    background: #7c3aed;
  }
}
</style>
