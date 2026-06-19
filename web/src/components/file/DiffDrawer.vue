<template>
  <BottomSheet
    :open="visible"
    :auto="true"
    :transparent-overlay="true"
    @close="emit('close')"
  >
    <template #header>
      <span class="diff-drawer-title">{{ title }}</span>
      <button
        v-if="canRevert"
        class="diff-revert-btn"
        :disabled="reverting"
        @click.stop="handleRevert"
      >
        <Undo2 :size="14" />
        {{ reverting ? '…' : t('git.diffView.revert') }}
      </button>
    </template>
    <div class="diff-drawer-body">
      <!-- Unified diff table view -->
      <template v-if="diffLines && diffLines.length > 0">
        <table class="diff-table">
          <tr
            v-for="(dl, i) in diffLines"
            :key="i"
            class="diff-line"
            :class="[`diff-line-${dl.type}`, { 'diff-line-ellipsis': dl.isEllipsis }]"
          >
            <td class="diff-content">{{ dl.content }}</td>
          </tr>
        </table>
      </template>
      <!-- Fallback: inline char-level diff (legacy) -->
      <template v-else-if="charDiff">
        <div class="diff-inline-view">
          <span
            v-for="(seg, i) in segments"
            :key="i"
            :class="seg.cls"
          >{{ seg.text }}</span>
        </div>
      </template>
      <div v-else class="diff-drawer-empty">{{ t('git.diffView.noDiffDetails') }}</div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { Undo2 } from 'lucide-vue-next'
import { diffOldContent, diffOldFilePath, clearDiffMarkers } from '@/composables/useMarkdownDiff.ts'
import type { CharDiff, DiffLine } from '@/composables/useMarkdownDiff.ts'
import { store } from '@/stores/app.ts'
import { useToast } from '@/composables/useToast.ts'
import { useDialog } from '@/composables/useDialog.ts'

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()

const props = defineProps({
  visible: { type: Boolean, default: false },
  markerType: { type: String, default: 'modified' },
  charDiff: { type: Object as () => CharDiff | null, default: null },
  diffLines: { type: Array as () => DiffLine[] | undefined, default: undefined },
})

const emit = defineEmits(['close'])

const reverting = ref(false)

const title = computed(() => {
  const key = { modified: 'modified', deleted: 'deleted', added: 'added' }[props.markerType]
  return key ? t(`git.diffView.${key}`) : 'Diff'
})

const canRevert = computed(() => diffOldContent.value !== null)

async function handleRevert() {
  const filePath = diffOldFilePath.value
  const currentPath = store.state.currentFile?.path
  const oldContent = diffOldContent.value
  if (!filePath || filePath !== currentPath || oldContent === null) return

  if (!await dialog.confirm(t('git.diffView.revertConfirm'), { dangerous: true })) return

  reverting.value = true
  try {
    const resp = await fetch('/api/file/write', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: filePath, content: oldContent }),
    })
    if (!resp.ok) throw new Error('write failed')
    await store.selectFile(filePath, false, false, false)
    clearDiffMarkers()
    emit('close')
    toast.show(t('git.diffView.revertSuccess'), { type: 'success' })
  } catch {
    toast.show(t('git.diffView.revertFailed'), { type: 'error' })
  } finally {
    reverting.value = false
  }
}

interface Segment {
  text: string
  cls: string
}

const segments = computed<Segment[]>(() => {
  if (!props.charDiff?.changes) return []
  const result: Segment[] = []
  for (const change of props.charDiff.changes) {
    if (change.added) {
      result.push({ text: change.value, cls: 'diff-seg-add' })
    } else if (change.removed) {
      result.push({ text: change.value, cls: 'diff-seg-del' })
    } else {
      result.push({ text: change.value, cls: 'diff-seg-common' })
    }
  }
  return result
})
</script>

<style scoped>
.diff-drawer-body {
  overflow: auto;
  font-family: 'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}

.diff-drawer-title {
  flex: 1;
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary);
}

.diff-revert-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 500;
  color: #dc2626;
  background: rgba(239, 68, 68, 0.08);
  border: 1px solid rgba(239, 68, 68, 0.2);
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}

.diff-revert-btn:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.15);
}

.diff-revert-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.diff-drawer-empty {
  padding: 12px 16px;
  color: var(--text-muted);
  font-style: italic;
}

/* ─── Unified diff table ─── */

.diff-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}

.diff-content {
  padding: 0 12px;
  white-space: pre-wrap;
  word-break: break-all;
  overflow-wrap: break-word;
  min-width: 0;
}

/* Deleted lines */
.diff-line-del .diff-content {
  color: #dc2626;
}

.diff-line-del {
  background: rgba(239, 68, 68, 0.18);
}

/* Added lines */
.diff-line-add .diff-content {
  color: #16a34a;
}

.diff-line-add {
  border-left: 2px solid #16a34a;
  background: rgba(34, 197, 94, 0.18);
}

/* Context lines */
.diff-line-ctx .diff-content {
  color: var(--text-secondary);
}

.diff-line-ctx {
  border-left: 2px solid transparent;
}

/* Ellipsis separator */
.diff-line-ellipsis .diff-content {
  color: var(--text-muted);
  text-align: center;
  padding: 2px 12px;
  letter-spacing: 2px;
}

/* ─── Inline char diff (legacy fallback) ─── */

.diff-inline-view {
  padding: 12px 16px;
  white-space: pre-wrap;
  word-break: break-all;
}

.diff-seg-common {
  color: var(--text-primary);
}

.diff-seg-del {
  background: rgba(255, 80, 80, 0.2);
  color: var(--text-primary);
  text-decoration: line-through;
  text-decoration-color: rgba(255, 80, 80, 0.6);
  border-radius: 2px;
}

.diff-seg-add {
  background: rgba(100, 200, 255, 0.2);
  color: var(--text-primary);
  border-radius: 2px;
}
</style>

<style>
/* Dark theme adjustments */
[data-theme="dark"] .diff-line-del .diff-content {
  color: #f87171;
}
[data-theme="dark"] .diff-line-del {
  background: rgba(239, 68, 68, 0.22);
}
[data-theme="dark"] .diff-line-add .diff-content {
  color: #4ade80;
}
[data-theme="dark"] .diff-line-add {
  border-left-color: #4ade80;
  background: rgba(34, 197, 94, 0.22);
}
[data-theme="dark"] .diff-revert-btn {
  color: #f87171;
  background: rgba(239, 68, 68, 0.12);
  border-color: rgba(239, 68, 68, 0.25);
}
[data-theme="dark"] .diff-revert-btn:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.20);
}
[data-theme="dark"] .diff-seg-del {
  background: rgba(255, 80, 80, 0.25);
}
[data-theme="dark"] .diff-seg-add {
  background: rgba(100, 200, 255, 0.25);
}
</style>
