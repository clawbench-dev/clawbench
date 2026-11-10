<template>
  <TransferProgressBar
    :visible="visible"
    :label="fileName"
    :detail="speedLabel"
    :value="indeterminate ? sizeLabel : pct + '%'"
    :percent="pct"
    :indeterminate="indeterminate"
    :value-title="sizeLabel"
    :cancel-title="t('common.cancel')"
    :floating="floating"
    @cancel="emit('cancel')"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import TransferProgressBar from '@/components/common/TransferProgressBar.vue'
import { formatByteRate, formatFileSize } from '@/utils/fileType'

/**
 * Adapter: in-product progress for a single file download.
 *
 * A download is always one file, so the label is the file name and the value is
 * the percentage (with the byte count as its tooltip). When `total` is 0 the
 * server sent no Content-Length (the streamed archive endpoint) and the bar
 * animates indeterminately, showing the received bytes instead of a percentage.
 *
 * The transfer rate sits between the two as `detail`. It is omitted while the
 * rate is still unknown (0), so a just-started bar does not flash "0 B/s".
 *
 * All visuals come from `TransferProgressBar`, which uploads use too.
 *
 * `floating` is true for the app-level bar (pinned under the header). The
 * lightbox passes false and positions the bar itself: the app-level one lives
 * inside `.app-container`, which is a stacking context, so it can never paint
 * above the full-screen viewer.
 */
const props = withDefaults(defineProps<{
  visible: boolean
  fileName: string
  received: number
  total: number
  /** Bytes per second, as measured by `useDownloadProgress`. */
  speed: number
  /** Pin under the app header; false lets the host position the bar. */
  floating?: boolean
}>(), {
  // The app-level bar is the common case and has always floated.
  floating: true,
})

const emit = defineEmits<{
  cancel: []
}>()

const { t } = useI18n()

const indeterminate = computed(() => props.total <= 0)

const pct = computed(() => {
  if (props.total <= 0) return 0
  return Math.min(100, Math.round((props.received / props.total) * 100))
})

/** Human-readable "received / total" (or just received, when unknown). */
const sizeLabel = computed(() => {
  if (props.total <= 0) return formatFileSize(props.received)
  return `${formatFileSize(props.received)} / ${formatFileSize(props.total)}`
})

const speedLabel = computed(() => (props.speed > 0 ? formatByteRate(props.speed) : ''))
</script>
