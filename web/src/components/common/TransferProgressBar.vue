<template>
  <div v-if="visible" class="transfer-progress" :class="{ 'transfer-progress--floating': floating }">
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
 * Two placements, chosen by the host:
 *  - INLINE (default): a normal flow child. Uploads use this inside the
 *    file-manager/terminal panel, where the bar should push the list down.
 *  - FLOATING (`floating`): fixed under the app header, centered, capped in
 *    width. Downloads use this — a download can start from any surface, and as
 *    a flow child it either pushed the whole layout down or sat at the window
 *    bottom. Floating keeps it in one predictable place without disturbing the
 *    content beneath it.
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
  /** Float under the app header instead of occupying layout space. */
  floating?: boolean
}>(), {
  detail: '',
  indeterminate: false,
  valueTitle: '',
  floating: false,
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

/* Floated under the app header, centered and width-capped.
   `left: 50%` + `translateX(-50%)` centers it at any width without the bar
   having to know its container's size; the cap keeps it from stretching edge
   to edge on desktop, and `calc(100% - 2*inset)` keeps a gutter on narrow
   viewports (where the cap does not apply) so it never touches the edges.
   The top offset clears the fixed app header, whose height already includes
   the top safe-area inset. A host with different chrome above it (the share
   SPA's 44px topbar) overrides `--transfer-progress-top`. */
.transfer-progress--floating {
  position: fixed;
  top: var(--transfer-progress-top,
    calc(var(--header-height, 36px) + var(--header-safe-area-top, 0px) + var(--space-2, 4px)));
  left: 50%;
  transform: translateX(-50%);
  width: calc(100% - 2 * var(--space-4, 12px));
  max-width: 520px;
  z-index: var(--z-header, 1100);
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
