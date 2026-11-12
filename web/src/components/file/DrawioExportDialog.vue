<template>
  <ModalDialog :open="open" :title="t('file.drawioExport.title')" @close="$emit('close')">
    <div class="dx-body">
      <p class="dx-label">{{ t('file.drawioExport.formatLabel') }}</p>
      <div class="dx-chips">
        <button
          v-for="f in formats"
          :key="f.id"
          class="dx-chip"
          :class="{ active: format === f.id }"
          :data-format="f.id"
          @click="format = f.id"
        >
          {{ t(f.labelKey) }}
        </button>
      </div>

      <p class="dx-label">{{ t('file.drawioExport.destLabel') }}</p>
      <div class="dx-chips">
        <button
          class="dx-chip"
          :class="{ active: destination === 'download' }"
          data-dest="download"
          @click="destination = 'download'"
        >
          {{ t('file.drawioExport.destDownload') }}
        </button>
        <button
          class="dx-chip"
          :class="{ active: destination === 'project' }"
          data-dest="project"
          @click="destination = 'project'"
        >
          {{ t('file.drawioExport.destProject') }}
        </button>
      </div>

      <p class="dx-hint">
        {{ destination === 'project' ? t('file.drawioExport.projectHint', { dir: dirName }) : t('file.drawioExport.downloadHint') }}
      </p>
    </div>

    <template #footer>
      <button class="dx-btn" :disabled="busy" @click="$emit('close')">
        {{ t('common.cancel') }}
      </button>
      <button class="dx-btn is-primary" :disabled="busy" @click="submit">
        {{ busy ? t('file.drawioExport.working') : t('file.drawioExport.confirm') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { DRAWIO_EXPORT_FORMATS } from '@/utils/drawioExport'

/**
 * Export chooser for a .drawio diagram.
 *
 * Format and destination are both asked up front: drawio produces the artifact
 * in one round trip, so there is no cheap way to offer the destination later
 * without either exporting twice or holding the payload in memory.
 */
const props = defineProps({
  open: { type: Boolean, default: false },
  /** Directory the diagram lives in, shown so "project" is not a guess. */
  dirName: { type: String, default: '' },
  busy: { type: Boolean, default: false },
})

const emit = defineEmits(['close', 'confirm'])

const { t } = useI18n()
const formats = DRAWIO_EXPORT_FORMATS

const format = ref('png')
const destination = ref('project')

// Reopening should not carry over the previous run's choice.
watch(
  () => props.open,
  (open) => {
    if (open) {
      format.value = 'png'
      destination.value = 'project'
    }
  }
)

function submit() {
  emit('confirm', { format: format.value, destination: destination.value })
}

</script>

<style scoped>
/* The shared .modal-body is padding-free by design — it only provides the
   scroll container, so every dialog insets its own content (same values as
   SessionTagDialog / UploadConflictDialog) or it renders flush against the
   card edge. */
.dx-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7) calc(14 * var(--dp));
}

.dx-label {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.dx-chips {
  display: flex;
  flex-wrap: wrap;
  gap: calc(8 * var(--dp));
}

.dx-chip {
  padding: calc(6 * var(--dp)) calc(14 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.dx-chip.active {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.dx-hint {
  margin: calc(2 * var(--dp)) 0 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  word-break: break-all;
}

.dx-btn {
  padding: calc(7 * var(--dp)) calc(16 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.dx-btn.is-primary {
  border-color: var(--accent-color);
  background: var(--accent-color);
  color: #fff;
}

.dx-btn:disabled {
  opacity: 0.55;
  cursor: default;
}
</style>
