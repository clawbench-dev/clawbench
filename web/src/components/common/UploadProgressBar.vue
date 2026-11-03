<template>
  <TransferProgressBar
    :visible="visible"
    :label="label"
    :detail="`${done}/${total}`"
    :value="valueLabel"
    :percent="progress"
    :cancel-title="t('common.cancel')"
    @cancel="emit('cancel')"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import TransferProgressBar from '@/components/common/TransferProgressBar.vue'
import { formatByteRate } from '@/utils/fileType'

/**
 * Adapter: aggregate progress for a multi-item upload.
 *
 * `progress` is byte-based (0-100) and drives the bar; `done`/`total` are item
 * counts shown beside it. Both are surfaced because a large file can hold the
 * byte percentage still while the item count advances — showing only one of
 * them reads as "stuck".
 *
 * `speed` is appended to the right-hand value ("45% · 1.2 MB/s") rather than
 * taking the detail slot, which the item counts already own.
 *
 * Shared by the file manager (directory upload) and the terminal (dropped
 * files). The state lives in the module-level singletons in `useFileUpload`, so
 * both hosts observe the same upload; they are never visible simultaneously
 * (the file manager and terminal are mutually exclusive dock tabs).
 *
 * All visuals come from `TransferProgressBar`, which downloads use too.
 */
const props = defineProps<{
  visible: boolean
  progress: number
  done: number
  total: number
  /** Bytes per second, as measured by `useFileUpload`. */
  speed: number
}>()

const emit = defineEmits<{
  cancel: []
}>()

const { t } = useI18n()

// The aggregate label: an upload is many files, so the "which file" slot that
// downloads fill with a name instead states the operation.
const label = computed(() => t('file.uploading'))

const valueLabel = computed(() => {
  const pct = `${props.progress}%`
  return props.speed > 0 ? `${pct} · ${formatByteRate(props.speed)}` : pct
})
</script>
