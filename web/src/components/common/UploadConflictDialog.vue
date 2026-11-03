<template>
  <ModalDialog
    :open="state.visible"
    :title="t('upload.conflict.title')"
    size="narrow"
    @close="resolveUploadConflict('cancel')"
  >
    <p class="ucd-lead">{{ t('upload.conflict.message', { count: state.conflicts.length }) }}</p>
    <ul class="ucd-list">
      <li v-for="item in shown" :key="item.path" class="ucd-item" :title="item.path">{{ item.path }}</li>
    </ul>
    <p v-if="hiddenCount > 0" class="ucd-more">
      {{ t('upload.conflict.more', { count: hiddenCount }) }}
    </p>
    <p class="ucd-hint">{{ t('upload.conflict.hint') }}</p>

    <template #footer>
      <button class="fbtn" @click="resolveUploadConflict('cancel')">{{ t('common.cancel') }}</button>
      <button class="fbtn" @click="resolveUploadConflict('rename')">{{ t('upload.conflict.keepBoth') }}</button>
      <button class="fbtn fbtn-danger" @click="resolveUploadConflict('overwrite')">
        {{ t('upload.conflict.overwrite') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useUploadConflict } from '@/composables/useUploadConflict'

/**
 * Name-collision prompt for uploads dropped into a directory.
 *
 * Rendered once at the app root; `useUploadConflict` holds the pending batch so
 * the upload loop can await the user's choice. See that composable for why the
 * prompt exists at all.
 */

// Cap the visible rows: a dropped folder can collide on hundreds of files, and
// the dialog is asking one question about the whole batch — the tail is
// summarized rather than scrolled.
const MAX_VISIBLE = 8

const { t } = useI18n()
const { state, resolveUploadConflict } = useUploadConflict()

const shown = computed(() => state.value.conflicts.slice(0, MAX_VISIBLE))
const hiddenCount = computed(() => Math.max(0, state.value.conflicts.length - MAX_VISIBLE))
</script>

<style scoped>
.ucd-lead {
  margin: 0 0 var(--space-5);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  line-height: var(--line-height-normal);
}

.ucd-list {
  margin: 0;
  padding: var(--space-4) var(--space-5);
  list-style: none;
  max-height: calc(160 * var(--dp));
  overflow-y: auto;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
}

.ucd-item {
  padding: calc(2 * var(--dp)) 0;
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  /* Names are paths; keep them monospace-aligned so a column of them reads as
     a list rather than prose. */
  font-family: var(--font-mono);
}

.ucd-more {
  margin: var(--space-3) 0 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.ucd-hint {
  margin: var(--space-5) 0 0;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}
</style>
