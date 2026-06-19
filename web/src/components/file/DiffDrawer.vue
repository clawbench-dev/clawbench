<template>
  <Transition name="drawer">
    <div v-if="visible" class="diff-drawer" @keydown.escape="emit('close')">
      <div class="diff-drawer-header">
        <span class="diff-drawer-title">{{ title }}</span>
        <button class="diff-drawer-close" @click="emit('close')" :aria-label="t('common.close')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
            <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
      <div class="diff-drawer-body">
        <!-- Unified diff table view -->
        <template v-if="diffLines && diffLines.length > 0">
          <table class="diff-table">
            <tr
              v-for="(dl, i) in diffLines"
              :key="i"
              class="diff-line"
              :class="`diff-line-${dl.type}`"
            >
              <td class="diff-linum diff-linum-old">{{ dl.oldLine ?? '' }}</td>
              <td class="diff-linum diff-linum-new">{{ dl.newLine ?? '' }}</td>
              <td class="diff-prefix">{{ dl.type === 'add' ? '+' : dl.type === 'del' ? '-' : ' ' }}</td>
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
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CharDiff, DiffLine } from '@/composables/useMarkdownDiff.ts'

const { t } = useI18n()

const props = defineProps({
  visible: { type: Boolean, default: false },
  markerType: { type: String, default: 'modified' },
  charDiff: { type: Object as () => CharDiff | null, default: null },
  diffLines: { type: Array as () => DiffLine[] | undefined, default: undefined },
})

const emit = defineEmits(['close'])

const title = computed(() => {
  const key = { modified: 'modified', deleted: 'deleted', added: 'added' }[props.markerType]
  return key ? t(`git.diffView.${key}`) : 'Diff'
})

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
.diff-drawer {
  border-top: 1px solid var(--border-color);
  background: var(--bg-secondary);
  max-height: 300px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.diff-drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-color);
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
}

.diff-drawer-title {
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.diff-drawer-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  border-radius: 4px;
  transition: background 0.15s;
}

.diff-drawer-close:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.diff-drawer-body {
  flex: 1;
  overflow: auto;
  padding: 0;
  font-family: 'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}

.diff-drawer-empty {
  padding: 12px 16px;
  color: var(--text-muted);
  font-style: italic;
}

/* ─── Unified diff table ─── */

.diff-table {
  width: max-content;
  min-width: 100%;
  border-collapse: collapse;
}

.diff-linum {
  width: 1%;
  min-width: 30px;
  padding: 0 4px;
  text-align: right;
  color: var(--text-muted);
  font-size: 11px;
  user-select: none;
  white-space: nowrap;
  background: var(--bg-tertiary);
  border-right: 1px solid var(--border-color);
}

.diff-prefix {
  width: 1%;
  padding: 0 2px;
  text-align: center;
  font-weight: 700;
  user-select: none;
  white-space: nowrap;
}

.diff-content {
  padding: 0 6px;
  white-space: pre;
  min-width: 0;
}

/* Line type colors */
.diff-line-del {
  background: rgba(239, 68, 68, 0.08);
}
.diff-line-del .diff-prefix {
  color: #dc2626;
}
.diff-line-del .diff-linum {
  color: #dc2626;
  opacity: 0.6;
}

.diff-line-add {
  background: rgba(34, 197, 94, 0.08);
}
.diff-line-add .diff-prefix {
  color: #16a34a;
}
.diff-line-add .diff-linum {
  color: #16a34a;
  opacity: 0.6;
}

.diff-line-ctx .diff-content {
  color: var(--text-primary);
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

/* Drawer slide transition */
.drawer-enter-active,
.drawer-leave-active {
  transition: transform 0.25s ease, opacity 0.25s ease;
}

.drawer-enter-from,
.drawer-leave-to {
  transform: translateY(100%);
  opacity: 0;
}
</style>

<style>
/* Dark theme adjustments */
[data-theme="dark"] .diff-seg-del {
  background: rgba(255, 80, 80, 0.25);
}

[data-theme="dark"] .diff-seg-add {
  background: rgba(100, 200, 255, 0.25);
}
</style>
