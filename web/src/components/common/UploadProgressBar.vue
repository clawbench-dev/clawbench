<template>
  <div v-if="visible" class="dir-upload-progress">
    <div class="dir-upload-progress-main">
      <div class="dir-upload-progress-bar" :style="{ width: progress + '%' }"></div>
      <button class="dir-upload-cancel" :title="t('common.cancel')" @click="emit('cancel')">
        <X :size="12" />
      </button>
    </div>
    <div class="dir-upload-progress-count">{{ done }}/{{ total }}</div>
  </div>
</template>

<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

/**
 * Aggregate progress for a multi-item upload.
 *
 * `progress` is byte-based (0-100) and drives the bar width; `done`/`total` are
 * item counts shown below it. Both are surfaced because a large file can hold
 * the byte percentage still while the item count advances — showing only one of
 * them reads as "stuck".
 *
 * Shared by the file manager (directory upload) and the terminal (dropped files).
 * The state lives in the module-level singletons in `useFileUpload`, so both
 * hosts observe the same upload; they are never visible simultaneously (the file
 * manager and terminal are mutually exclusive dock tabs).
 *
 * Class names are kept verbatim from the file manager's original inline markup
 * so existing tests and selectors keep working.
 */
defineProps<{
  visible: boolean
  progress: number
  done: number
  total: number
}>()

const emit = defineEmits<{
  cancel: []
}>()

const { t } = useI18n()
</script>

<style scoped>
.dir-upload-progress {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: var(--space-3) var(--space-6);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
  flex-shrink: 0;
}

.dir-upload-progress-main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.dir-upload-progress-bar {
  flex: 1;
  height: 3px;
  min-width: 0;
  background: var(--accent-color, #4a90d9);
  border-radius: var(--radius-xs);
  transition: width var(--duration-base) ease;
}

.dir-upload-cancel {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  cursor: pointer;
  transition: all var(--duration-base);
}

@media (hover: hover) {
  .dir-upload-cancel:hover {
    background: var(--color-red);
    color: #fff;
  }
}

.dir-upload-progress-count {
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  white-space: nowrap;
  line-height: var(--line-height-tight);
}
</style>
