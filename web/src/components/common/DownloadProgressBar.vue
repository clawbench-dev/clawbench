<template>
  <TransferProgressBar
    :visible="visible"
    :label="fileName"
    :value="indeterminate ? sizeLabel : pct + '%'"
    :percent="pct"
    :indeterminate="indeterminate"
    :value-title="sizeLabel"
    :cancel-title="t('common.cancel')"
    centered
    @cancel="emit('cancel')"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import TransferProgressBar from '@/components/common/TransferProgressBar.vue'
import { formatFileSize } from '@/utils/fileType'

/**
 * Adapter: in-product progress for a single file download.
 *
 * A download is always one file, so the label is the file name and the value is
 * the percentage (with the byte count as its tooltip). When `total` is 0 the
 * server sent no Content-Length (the streamed archive endpoint) and the bar
 * animates indeterminately, showing the received bytes instead of a percentage.
 *
 * All visuals come from `TransferProgressBar`, which uploads use too.
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

/** Human-readable "received / total" (or just received, when unknown). */
const sizeLabel = computed(() => {
  if (props.total <= 0) return formatFileSize(props.received)
  return `${formatFileSize(props.received)} / ${formatFileSize(props.total)}`
})
</script>
