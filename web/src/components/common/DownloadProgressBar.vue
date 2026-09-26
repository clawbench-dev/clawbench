<template>
  <div v-if="visible" class="download-progress">
    <div class="download-progress-main" :title="sizeLabel">
      <span class="download-progress-name">{{ fileName }}</span>
      <span class="download-progress-pct">{{ indeterminate ? sizeLabel : pct + '%' }}</span>
      <button class="download-cancel" :title="t('common.cancel')" @click="emit('cancel')">
        <X :size="12" />
      </button>
    </div>
    <div class="download-progress-track">
      <div
        class="download-progress-bar"
        :class="{ 'download-progress-bar--indeterminate': indeterminate }"
        :style="indeterminate ? undefined : { width: pct + '%' }"
      ></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { formatFileSize } from '@/utils/fileType'

/**
 * In-product progress bar for a single file download.
 *
 * Complements `UploadProgressBar` (which is byte-bar + item count for
 * multi-item uploads). A download is always one file, so the label shows the
 * file name and the byte count instead of an item count.
 *
 * When `total` is 0 the server sent no Content-Length (the streamed archive
 * endpoint) and the bar animates indeterminately rather than sitting at 0%.
 *
 * It floats (position: fixed) because it is mounted by hosts whose root is a
 * fixed, overflow-hidden flex column — as a normal flow child it would be
 * pushed past the viewport and clipped. `--download-progress-bottom` lets the
 * host lift it above a bottom dock; it defaults to a plain margin.
 */
const props = defineProps<{
  visible: boolean
  fileName: string
  received: number
  total: number
}>()

const emit = defineEmits<{
  cancel: []
}>()

const { t } = useI18n()

const indeterminate = computed(() => props.total <= 0)

const pct = computed(() => {
  if (props.total <= 0) return 0
  return Math.min(100, Math.round((props.received / props.total) * 100))
})

/** Human-readable "received / total" for the tooltip. */
const sizeLabel = computed(() => {
  if (props.total <= 0) return formatFileSize(props.received)
  return `${formatFileSize(props.received)} / ${formatFileSize(props.total)}`
})
</script>

<style scoped>
.download-progress {
  position: fixed;
  left: var(--space-4, 12px);
  right: var(--space-4, 12px);
  /* Above the bottom dock when one is present, otherwise a plain margin. */
  bottom: var(--download-progress-bottom, var(--space-4, 12px));
  z-index: var(--z-popover, 9999);
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: var(--space-3) var(--space-4, 12px);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 10%, var(--bg-secondary, #fff));
  box-shadow: var(--shadow-md);
  max-width: 520px;
  margin: 0 auto;
}

.download-progress-main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.download-progress-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  line-height: var(--line-height-tight);
}

.download-progress-pct {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  line-height: var(--line-height-tight);
}

.download-progress-track {
  height: 3px;
  overflow: hidden;
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary, #f0f0f0);
}

.download-progress-bar {
  height: 100%;
  background: var(--accent-color, #4a90d9);
  border-radius: var(--radius-xs);
  transition: width var(--duration-base) ease;
}

.download-progress-bar--indeterminate {
  width: 30%;
  animation: download-indeterminate 1.2s ease-in-out infinite;
}

@keyframes download-indeterminate {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(400%); }
}

.download-cancel {
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
  .download-cancel:hover {
    background: var(--color-red);
    color: #fff;
  }
}
</style>
