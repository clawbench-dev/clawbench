<template>
  <BottomSheet
    :open="visible"
    :auto="true"
    :transparent-overlay="true"
    @close="emit('close')"
  >
    <template #header>
      <span class="diff-drawer-title">{{ title }}</span>
      <div class="diff-drawer-actions">
        <button
          v-if="canUndo"
          class="diff-action-btn"
          :disabled="busy"
          @click.stop="handleUndo"
        >
          <Undo2 :size="14" />
          {{ busy ? '…' : t('git.diffView.undo') }}
        </button>
      </div>
    </template>
    <div class="diff-drawer-body">
      <!-- Unified diff table view with char-level highlighting -->
      <template v-if="diffLines && diffLines.length > 0">
        <table class="diff-table">
          <tr
            v-for="(row, i) in renderedRows"
            :key="i"
            class="diff-line"
            :class="[`diff-line-${row.type}`, { 'diff-line-ellipsis': row.isEllipsis }]"
          >
            <td class="diff-content"><span v-html="row.html" /></td>
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
import { diffChars } from 'diff'
import { escapeHtml } from '@/utils/html.ts'

const { t } = useI18n()
const toast = useToast()

const props = defineProps({
  visible: { type: Boolean, default: false },
  markerType: { type: String, default: 'modified' },
  charDiff: { type: Object as () => CharDiff | null, default: null },
  diffLines: { type: Array as () => DiffLine[] | undefined, default: undefined },
})

const emit = defineEmits(['close'])

const busy = ref(false)

const title = computed(() => {
  const key = { modified: 'modified', deleted: 'deleted', added: 'added' }[props.markerType]
  return key ? t(`git.diffView.${key}`) : 'Diff'
})

const canUndo = computed(() => diffOldContent.value !== null)

async function handleUndo() {
  const filePath = diffOldFilePath.value
  const currentPath = store.state.currentFile?.path
  const oldContent = diffOldContent.value
  if (!filePath || filePath !== currentPath || oldContent === null) return

  busy.value = true
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
    toast.show(t('git.diffView.undoSuccess'), { type: 'success' })
  } catch {
    toast.show(t('git.diffView.undoFailed'), { type: 'error' })
  } finally {
    busy.value = false
  }
}

// ─── Char-level highlighting for diff table rows ───

interface RenderedRow {
  type: string
  isEllipsis: boolean
  html: string
}

/**
 * Compute char-level diff for paired del/add lines.
 * Pairs adjacent del→add lines and runs diffChars on them,
 * generating HTML with <span class="diff-char-del/add"> for changed regions.
 */
const renderedRows = computed<RenderedRow[]>(() => {
  if (!props.diffLines || props.diffLines.length === 0) return []

  const lines = props.diffLines
  const result: RenderedRow[] = new Array(lines.length)
  // Track which add lines have been paired with a del line
  const pairedAdd = new Set<number>()

  // First pass: pair consecutive del→add lines and compute char-level highlights
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].type === 'del' && i + 1 < lines.length && lines[i + 1].type === 'add') {
      const delHtml = charDiffHtml(lines[i].content, lines[i + 1].content, 'del')
      const addHtml = charDiffHtml(lines[i].content, lines[i + 1].content, 'add')
      result[i] = { type: lines[i].type, isEllipsis: !!lines[i].isEllipsis, html: delHtml }
      result[i + 1] = { type: lines[i + 1].type, isEllipsis: !!lines[i + 1].isEllipsis, html: addHtml }
      pairedAdd.add(i + 1)
      i++ // skip the add line we just processed
    }
  }

  // Second pass: fill in unpaired lines (ctx, unpaired del, unpaired add, ellipsis)
  for (let i = 0; i < lines.length; i++) {
    if (result[i] !== undefined) continue
    result[i] = {
      type: lines[i].type,
      isEllipsis: !!lines[i].isEllipsis,
      html: escapeHtml(lines[i].content),
    }
  }

  return result
})

/**
 * Generate char-level diff HTML for one side (del or add) of a paired line.
 * Changed regions get <span class="diff-char-del"> or <span class="diff-char-add">.
 * Unchanged regions are plain escaped text.
 */
function charDiffHtml(oldText: string, newText: string, side: 'del' | 'add'): string {
  const changes = diffChars(oldText, newText, { timeout: 3 }) ?? []
  const cls = side === 'del' ? 'diff-char-del' : 'diff-char-add'
  let html = ''
  for (const change of changes) {
    const escaped = escapeHtml(change.value)
    if (side === 'del' ? change.removed : change.added) {
      html += `<span class="${cls}">${escaped}</span>`
    } else if (!change.removed && !change.added) {
      html += escaped
    }
    // On the del side, skip added changes; on the add side, skip removed changes
  }
  return html
}

// ─── Legacy inline char diff fallback ───

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

.diff-drawer-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.diff-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  transition: color 0.15s;
}

.diff-action-btn:hover:not(:disabled) {
  color: var(--text-primary);
}

.diff-action-btn:disabled {
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
  background: rgba(239, 68, 68, 0.35);
}

/* Added lines */
.diff-line-add .diff-content {
  color: #16a34a;
}

.diff-line-add {
  border-left: 2px solid #16a34a;
  background: rgba(34, 197, 94, 0.35);
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

/* ─── Char-level highlighting within diff lines ─── */

.diff-char-del {
  background: rgba(239, 68, 68, 0.35);
  border-radius: 2px;
}

.diff-char-add {
  background: rgba(34, 197, 94, 0.35);
  border-radius: 2px;
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
  background: rgba(239, 68, 68, 0.40);
}
[data-theme="dark"] .diff-line-add .diff-content {
  color: #4ade80;
}
[data-theme="dark"] .diff-line-add {
  border-left-color: #4ade80;
  background: rgba(34, 197, 94, 0.40);
}
[data-theme="dark"] .diff-char-del {
  background: rgba(239, 68, 68, 0.40);
}
[data-theme="dark"] .diff-char-add {
  background: rgba(34, 197, 94, 0.40);
}
[data-theme="dark"] .diff-seg-del {
  background: rgba(255, 80, 80, 0.25);
}
[data-theme="dark"] .diff-seg-add {
  background: rgba(100, 200, 255, 0.25);
}
</style>
