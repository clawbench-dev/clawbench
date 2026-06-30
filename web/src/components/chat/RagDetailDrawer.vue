<template>
  <BottomSheet :open="!!item" handleOnly auto @close="$emit('close')">
    <template v-if="item">
      <div class="rag-detail-content">
        <div class="rag-detail-title">{{ item.sessionTitle || t('chat.contentBlocks.ragUntitled') }}</div>
        <div v-if="item.createdAt" class="rag-detail-time">{{ formatDetailTime(item.createdAt) }}</div>
        <div v-if="item.summary" class="rag-detail-summary">{{ item.summary }}</div>
      </div>
      <div class="rag-detail-footer">
        <button class="rag-detail-resume-btn" @click="$emit('resume', item)">
          {{ t('chat.contentBlocks.ragResume') }}
          <ChevronRight :size="14" />
        </button>
      </div>
    </template>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ChevronRight } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { formatDetailTime } from '@/utils/chatBlocks.ts'

const { t } = useI18n()

defineProps({
  item: { type: Object, default: null },
})

defineEmits(['close', 'resume'])
</script>

<style scoped>
.rag-detail-content {
  padding: 8px 16px 16px;
}

.rag-detail-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.4;
  margin-bottom: 8px;
  word-break: break-word;
}

.rag-detail-time {
  font-size: 12px;
  color: var(--text-muted, #999);
  margin-bottom: 12px;
}

.rag-detail-summary {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary, #495057);
  white-space: pre-wrap;
  word-break: break-word;
}

.rag-detail-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--border-color, #e5e5e5);
}

.rag-detail-resume-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 10px 0;
  border: none;
  border-radius: 8px;
  background: #8b5cf6;
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}

:root[data-theme="dark"] .rag-detail-resume-btn {
  background: #7c3aed;
}

.rag-detail-resume-btn:hover {
  opacity: 0.85;
}

.rag-detail-resume-btn:active {
  opacity: 0.7;
}
</style>
