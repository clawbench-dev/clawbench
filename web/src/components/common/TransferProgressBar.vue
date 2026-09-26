<template>
  <div v-if="visible" class="transfer-progress" :class="{ 'transfer-progress--centered': centered }">
    <div class="transfer-progress-head">
      <span class="transfer-progress-label" :title="label">{{ label }}</span>
      <span v-if="detail" class="transfer-progress-detail">{{ detail }}</span>
      <span class="transfer-progress-value" :title="valueTitle || undefined">{{ value }}</span>
      <button class="transfer-progress-cancel" :title="cancelTitle" @click="emit('cancel')">
        <X :size="12" />
      </button>
    </div>
    <div class="transfer-progress-track">
      <div
        class="transfer-progress-fill"
        :class="{ 'transfer-progress-fill--indeterminate': indeterminate }"
        :style="indeterminate ? undefined : { width: percent + '%' }"
      ></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { X } from 'lucide-vue-next'

/**
 * Presentational transfer-progress bar, shared by uploads and downloads.
 *
 * Both directions are the same shape — a label, a value, a bar and a cancel
 * button — so only the data differs. Keeping one component means the two can
 * never drift visually, and a styling change lands in both at once. The two
 * callers (`UploadProgressBar`, `DownloadProgressBar`) are thin adapters that
 * map their own state onto these props.
 *
 * It is INLINE (a normal flow child of its host panel), not floating: uploads
 * render inside the file-manager/terminal panel and downloads above the bottom
 * dock, so each host decides placement and no overlay stacking is involved.
 * `centered` caps the width and centers the bar — a download spans the whole
 * window, so at desktop widths an uncapped bar would stretch edge to edge.
 *
 * `indeterminate` is for transfers with no known size (the streamed archive
 * endpoint sends no Content-Length): the bar animates instead of sitting at a
 * misleading 0%.
 */
withDefaults(defineProps<{
  visible: boolean
  /** Left-hand text: the file name, or what is being transferred. */
  label: string
  /** Right-hand value, e.g. "45%" or "2/4". */
  value: string
  /** Bar fill, 0-100. Ignored when `indeterminate`. */
  percent: number
  /** Optional secondary text after the label (e.g. an item count). */
  detail?: string
  /** No known total: animate the bar rather than show a fixed width. */
  indeterminate?: boolean
  /** Tooltip for the value, e.g. "1.2 MB / 4.8 MB". */
  valueTitle?: string
  cancelTitle: string
  /** Cap the width and center it (for bars spanning a whole window). */
  centered?: boolean
}>(), {
  detail: '',
  indeterminate: false,
  valueTitle: '',
  centered: false,
})

const emit = defineEmits<{
  cancel: []
}>()
</script>

<style scoped>
.transfer-progress {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: var(--space-3) var(--space-4, 12px);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 10%, var(--bg-secondary, #fff));
  box-shadow: var(--shadow-md);
  flex-shrink: 0;
}

/* A bar spanning the whole window (the download bar, mounted at app level)
   would stretch edge to edge on desktop; cap it and center it instead. Below
   the cap (narrow viewports) width:100% still fills the available space. */
.transfer-progress--centered {
  width: 100%;
  max-width: 520px;
  margin-inline: auto;
}

.transfer-progress-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.transfer-progress-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  line-height: var(--line-height-tight);
}

.transfer-progress-detail,
.transfer-progress-value {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  line-height: var(--line-height-tight);
  white-space: nowrap;
}

.transfer-progress-track {
  height: 3px;
  overflow: hidden;
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary, #f0f0f0);
}

.transfer-progress-fill {
  height: 100%;
  background: var(--accent-color, #4a90d9);
  border-radius: var(--radius-xs);
  transition: width var(--duration-base) ease;
}

.transfer-progress-fill--indeterminate {
  width: 30%;
  animation: transfer-indeterminate 1.2s ease-in-out infinite;
}

@keyframes transfer-indeterminate {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(400%); }
}

.transfer-progress-cancel {
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
  .transfer-progress-cancel:hover {
    background: var(--color-red);
    color: #fff;
  }
}
</style>
