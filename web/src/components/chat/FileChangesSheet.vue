<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <FileDiff :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.fileChanges.title') }}</span>
    </template>
    <div class="fc-content">
      <!-- Created section -->
      <div v-if="created.length" class="fc-section">
        <div class="fc-section-title">{{ t('chat.fileChanges.created') }}</div>
        <div class="fc-file-list">
          <button v-for="path in created" :key="'c-' + path" class="fc-file-item" @click="$emit('open-file', path)">
            <FileText :size="16" :color="getFileType(path).color" class="fc-file-icon" />
            <span class="fc-file-name">{{ baseName(path) }}</span>
          </button>
        </div>
      </div>
      <!-- Modified section -->
      <div v-if="modified.length" class="fc-section">
        <div class="fc-section-title">{{ t('chat.fileChanges.modified') }}</div>
        <div class="fc-file-list">
          <button v-for="path in modified" :key="'m-' + path" class="fc-file-item" @click="$emit('open-file', path)">
            <FileText :size="16" :color="getFileType(path).color" class="fc-file-icon" />
            <span class="fc-file-name">{{ baseName(path) }}</span>
          </button>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { FileText, FileDiff } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { getFileType } from '@/utils/fileType.ts'

const { t } = useI18n()

defineProps({
  open: Boolean,
  created: { type: Array, default: () => [] },
  modified: { type: Array, default: () => [] },
})

defineEmits(['close', 'open-file'])

function baseName(path) {
  const idx = path.lastIndexOf('/')
  return idx >= 0 ? path.slice(idx + 1) : path
}
</script>

<style scoped>
.fc-content {
  padding: 8px 0 16px;
}

.fc-section + .fc-section {
  margin-top: 8px;
}

.fc-section-title {
  padding: 4px 16px 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted, #999);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.fc-file-list {
  display: flex;
  flex-direction: column;
}

.fc-file-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  width: 100%;
  transition: background 0.15s;
}

.fc-file-item:hover {
  background: var(--bg-tertiary);
}

.fc-file-item:active {
  background: var(--bg-primary);
}

.fc-file-icon {
  flex-shrink: 0;
}

.fc-file-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex: 1;
}
</style>
