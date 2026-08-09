<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <FileIcon :path="filePath" :size="16" class="bs-header-icon" />
      <span class="fd-header-path">{{ displayPath }}</span>
      <span class="fd-header-badge">{{ badgeLabel }}</span>
      <span v-if="diffItems.length > 1" class="fd-header-count">{{ diffItems.length }}</span>
    </template>
    <div class="fd-body tool-detail-body" @click="handleBodyClick" @mousedown="onTableMouseDown" @touchstart="onTableTouchStart">
      <div v-if="diffItems.length" class="fd-diffs">
        <div v-for="(item, i) in diffItems" :key="item.key" class="fd-diff-item">
          <div class="fd-diff-label">
            <span class="fd-diff-label-name">{{ item.name }}</span>
            <span class="fd-diff-label-index">{{ i + 1 }}</span>
          </div>
          <div v-if="item.loading" class="fd-loading"><span class="fd-loading-spinner"></span><span>{{ t('chat.fileChanges.loadingDiff') }}</span></div>
          <div v-else-if="item.error" class="fd-error">
            <span>{{ t('chat.fileChanges.diffLoadFailed') }}</span>
            <button class="fd-retry-btn" @click="fetchDiff(item)">{{ t('common.retry') }}</button>
          </div>
          <div v-else v-html="item.inputHtml"></div>
        </div>
      </div>
      <div v-else class="fd-empty">{{ t('chat.fileChanges.noDiffs') }}</div>
    </div>
  </BottomSheet>

  <!-- Table row expand modal -->
  <TableRowModal
    :data="tableRowModal"
    @close="closeTableRowModal"
    @prev="tableRowPrev"
    @next="tableRowNext"
  />
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import TableRowModal from '@/components/common/TableRowModal.vue'
import { handleToolAction, handleToolContentHeaderClick } from '@/utils/renderToolDetail.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'

const props = defineProps({
  open: { type: Boolean, default: false },
  filePath: { type: String, default: '' },
  /** 'Write' (created) or 'Edit' (modified) — the tool type to show diffs for. */
  toolName: { type: String, default: '' },
  /** Full content blocks of the message; used for live messages where input is inline. */
  blocks: { type: Array, default: () => [] },
  /** Message ID — required to fetch diff content by tool_id from the tool-call API. */
  msgId: { type: [String, Number], default: '' },
  /** Write/Edit tool call IDs for this file (from summaryCards when blocks are stripped). */
  toolIds: { type: Array, default: () => [] },
  formatToolInput: { type: Function, required: true },
})

const emit = defineEmits(['close', 'file-open'])

const { t } = useI18n()

const displayPath = computed(() => props.filePath.replace(/^\.\//, ''))
const badgeLabel = computed(() => props.toolName === 'Write' ? t('chat.fileChanges.created') : t('chat.fileChanges.modified'))

// tool_use blocks matching the selected file + tool type.
const matchingBlocks = computed(() => {
  const out = []
  for (const b of props.blocks || []) {
    if (b.type !== 'tool_use' || !b.done) continue
    if (b.name !== props.toolName) continue
    const fp = b.file_path || b.input?.file_path
    if (fp === props.filePath) out.push(b)
  }
  return out
})

function hasInlineInput(block) {
  return !!block.input && Object.keys(block.input).length > 0
}

// Ordered diff items: inline blocks render directly; the rest are fetched by
// tool_id + message_id from the tool-call API (loaded/summary-only view where
// blocks are slim and carry no input).
const diffItems = ref([])

function buildItems() {
  const items = []
  const seen = new Set()

  const pushFetch = (id) => {
    if (!id || seen.has(id)) return
    seen.add(id)
    items.push({ key: 'fetch-' + id, name: props.toolName, toolId: id, inputHtml: '', loading: true, error: false })
  }

  // Live messages: blocks carry input inline — render directly.
  for (const b of matchingBlocks.value) {
    if (hasInlineInput(b)) {
      seen.add(b.id)
      items.push({
        key: b.id || 'inline-' + items.length,
        name: b.name,
        toolId: b.id,
        inputHtml: props.formatToolInput(b.input, b.name, { done: b.done, status: b.status, output: b.output }),
        loading: false,
        error: false,
      })
    }
  }

  // Loaded/summary view: slim blocks (id only) + summaryCards tool IDs → fetch.
  for (const b of matchingBlocks.value) {
    if (!hasInlineInput(b)) pushFetch(b.id)
  }
  for (const id of props.toolIds || []) pushFetch(id)

  diffItems.value = items
  // Iterate the reactive array so item mutations in fetchDiff trigger re-render.
  for (const item of diffItems.value) if (item.loading) fetchDiff(item)
}

async function fetchDiff(item) {
  if (!props.msgId) {
    item.loading = false
    item.error = true
    return
  }
  try {
    const resp = await fetch(`/api/ai/chat/tool-call?tool_id=${encodeURIComponent(item.toolId)}&message_id=${encodeURIComponent(props.msgId)}`)
    if (!resp.ok) throw new Error('tool-call fetch failed')
    const data = await resp.json()
    let input
    if (data.input) {
      input = typeof data.input === 'string' ? JSON.parse(data.input) : data.input
    }
    if (input) {
      item.inputHtml = props.formatToolInput(input, data.name || item.name || props.toolName, { done: data.done !== false, status: data.status || '', output: data.output || '' })
      item.error = false
    } else {
      item.error = true
    }
  } catch {
    item.error = true
  } finally {
    item.loading = false
  }
}

watch(() => [props.open, props.filePath, props.toolName], () => {
  if (props.open) buildItems()
}, { immediate: true })

const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()
const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

function handleBodyClick(event) {
  // Tool content header buttons (copy + wrap toggle) — highest priority
  if (handleToolContentHeaderClick(event)) return

  if (props.toolName && handleToolAction(props.toolName, event, emit)) return

  // Localhost URL open buttons — bottom sheet is teleported to <body>
  if (handleLocalhostUrlClick(event)) return

  // Table row click — open row-form modal
  if (handleTableRowClick(event)) return

  // File-open buttons inside the diff headers
  const fileBtn = event.target.closest('.chat-file-open-btn')
  if (fileBtn) {
    const path = fileBtn.getAttribute('data-file-path')
    const lineStart = fileBtn.getAttribute('data-line-start')
    const lineEnd = fileBtn.getAttribute('data-line-end')
    if (path) emit('file-open', { path, lineStart: lineStart ? parseInt(lineStart, 10) : undefined, lineEnd: lineEnd ? parseInt(lineEnd, 10) : undefined })
    return
  }

  event.stopPropagation()
}
</script>

<style scoped>
.fd-body {
  padding: 10px 14px 14px;
  overflow-y: auto;
  overflow-x: clip;
  font-size: 12px;
  line-height: 1.5;
  flex: 1;
  cursor: default;
}

.fd-diffs {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.fd-diff-item {
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 6px 8px;
  background: var(--bg-secondary);
}

.fd-diff-label {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}

.fd-diff-label-name {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted, #999);
}

.fd-diff-label-index {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-tertiary, #bbb);
}

.fd-empty {
  padding: 16px 8px;
  text-align: center;
  font-size: 12px;
  color: var(--text-muted, #999);
}

.fd-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  font-size: 12px;
  color: var(--text-muted, #999);
}

.fd-loading-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid var(--border-color);
  border-top-color: var(--accent-color, #0066cc);
  border-radius: 50%;
  animation: fd-spin 0.6s linear infinite;
  flex-shrink: 0;
}

@keyframes fd-spin {
  to { transform: rotate(360deg); }
}

.fd-error {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  font-size: 12px;
  color: var(--text-secondary, #888);
}

.fd-retry-btn {
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--accent-color, #0066cc);
  font-size: 12px;
  padding: 2px 10px;
  cursor: pointer;
}

.fd-retry-btn:hover {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
}

.fd-header-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fd-header-badge {
  flex-shrink: 0;
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
  font-weight: 600;
  white-space: nowrap;
}

.fd-header-count {
  flex-shrink: 0;
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
</style>
