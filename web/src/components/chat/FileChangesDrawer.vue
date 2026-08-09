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
          <div v-for="change in created" :key="'c-' + change.path" class="fc-file-item">
            <button class="fc-file-main" @click="$emit('select-file', { ...change, toolName: 'Write' })">
              <FileIcon :path="change.path" :size="16" class="fc-file-icon" />
              <span class="fc-file-name">{{ baseName(change.path) }}</span>
            </button>
            <button class="fc-file-jump" :title="t('chat.fileChanges.openFile')" :aria-label="t('chat.fileChanges.openFile')" @click="$emit('open-file', change.path)">
              <ExternalLink :size="14" />
            </button>
          </div>
        </div>
      </div>
      <!-- Modified section -->
      <div v-if="modified.length" class="fc-section">
        <div class="fc-section-title">{{ t('chat.fileChanges.modified') }}</div>
        <div class="fc-file-list">
          <div v-for="change in modified" :key="'m-' + change.path" class="fc-file-item">
            <button class="fc-file-main" @click="$emit('select-file', { ...change, toolName: 'Edit' })">
              <FileIcon :path="change.path" :size="16" class="fc-file-icon" />
              <span class="fc-file-name">{{ baseName(change.path) }}</span>
            </button>
            <button class="fc-file-jump" :title="t('chat.fileChanges.openFile')" :aria-label="t('chat.fileChanges.openFile')" @click="$emit('open-file', change.path)">
              <ExternalLink :size="14" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { FileDiff, ExternalLink } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import FileIcon from '@/components/common/FileIcon.vue'

const { t } = useI18n()

defineProps({
  open: Boolean,
  created: { type: Array, default: () => [] },
  modified: { type: Array, default: () => [] },
})

defineEmits(['close', 'open-file', 'select-file'])

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
  padding: 0 16px;
  transition: background 0.15s;
}

.fc-file-item:hover {
  background: var(--bg-tertiary);
}

.fc-file-item:active {
  background: var(--bg-primary);
}

.fc-file-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  padding: 8px 0;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  color: inherit;
  font: inherit;
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

.fc-file-jump {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  margin-left: 4px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.fc-file-jump:hover {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
}
</style>
