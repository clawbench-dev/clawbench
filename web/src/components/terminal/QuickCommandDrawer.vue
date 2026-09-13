<template>
  <BottomSheet :open="open" auto :title="t('terminal.quickCommands')" @close="$emit('close')">
    <template #header>
      <ZapIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.quickCommands') }}</span>
      <button class="create-btn" @click.stop="addNewCommand" :title="t('terminal.addCommand')">
        <PlusIcon :size="16" />
      </button>
      <button ref="moreBtnRef" class="create-btn" @click.stop="showMoreMenu = !showMoreMenu" :title="t('terminal.moreActions')">
        <MoreVerticalIcon :size="16" />
      </button>
    </template>

    <PopupMenu
      v-model:show="showMoreMenu"
      :target-element="moreBtnRef"
      anchor="right"
      :max-width="180"
      :menu-items-count="2"
    >
      <button class="menu-item" @click="exportJson">
        <DownloadIcon :size="14" /> {{ t('terminal.exportJson') }}
      </button>
      <button class="menu-item" @click="importJson">
        <UploadIcon :size="14" /> {{ t('terminal.importJson') }}
      </button>
    </PopupMenu>

    <div class="qc-content">
      <div v-if="commands.length > 0" class="qc-list">
        <VueDraggable
          v-model="localCommands"
          handle=".drag-handle"
          @end="onDragEnd"
        >
            <div v-for="cmd in localCommands" :key="cmd.id" class="qc-item-wrapper">
              <div class="qc-row" :class="{ 'qc-hidden': cmd.hidden }">
                <span class="drag-handle">≡</span>
                <span class="qc-label">
                  <ZapIcon v-if="cmd.auto_execute" :size="12" class="qc-badge-auto" />
                  <EyeOffIcon v-if="cmd.hidden" :size="12" class="qc-badge-dim" />
                  {{ cmd.label }}
                </span>
                <span class="qc-cmd" :title="cmd.command">{{ cmd.command }}</span>
                <button class="qc-action" @click="editCommand(cmd)" :title="t('terminal.editCommand')">
                  <PencilIcon :size="14" />
                </button>
                <button class="qc-action danger" @click="toggleDeleteConfirm(cmd.id)" :title="t('terminal.deleteCommand')">
                  <Trash2Icon :size="14" />
                </button>
              </div>
              <!-- Inline delete confirmation -->
              <div v-if="deleteConfirmId === cmd.id" class="qc-delete-confirm">
                <span>{{ t('terminal.deleteConfirm') }}</span>
                <button class="qc-confirm-btn delete" @click="doDelete(cmd.id)">{{ t('common.confirm') }}</button>
                <button class="qc-confirm-btn cancel" @click="deleteConfirmId = null">{{ t('common.cancel') }}</button>
              </div>
            </div>
        </VueDraggable>
      </div>
      <div v-else class="qc-empty">
        <ZapIcon :size="32" class="qc-empty-icon" />
        <span>{{ t('terminal.quickCommandsEmpty') }}</span>
      </div>
    </div>

    <!-- Edit modal (separate, not drill-down) -->
    <QuickCommandEditModal
      :open="editOpen"
      :editing-command="editingCommand"
      @close="editOpen = false"
      @saved="onCommandSaved"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import BottomSheet from '@/components/common/BottomSheet.vue'
import QuickCommandEditModal from './QuickCommandEditModal.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { ZapIcon, PencilIcon, Trash2Icon, PlusIcon, EyeOffIcon, MoreVertical as MoreVerticalIcon, Download as DownloadIcon, Upload as UploadIcon } from 'lucide-vue-next'
import { useQuickCommands, type QuickCommand } from '@/composables/useQuickCommands'
import { useToast } from '@/composables/useToast'
import { createJsonImporter, buildExportPayload, downloadJson } from '@/composables/useQuickSendIO'

const props = defineProps({
  open: Boolean,
})

defineEmits(['close'])

const { t } = useI18n()
const toast = useToast()
const { commands, addCommand, reorderCommands, deleteCommand } = useQuickCommands()

const localCommands = ref<QuickCommand[]>([...commands.value])
const deleteConfirmId = ref<number | null>(null)
const editOpen = ref(false)
const editingCommand = ref<QuickCommand | null>(null)
const moreBtnRef = ref<HTMLElement | null>(null)
const showMoreMenu = ref(false)

// Sync local list when commands change
watch(commands, (val) => {
  localCommands.value = [...val]
}, { deep: true })

// Reset state when drawer opens/closes
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    deleteConfirmId.value = null
  }
})

function editCommand(cmd: QuickCommand) {
  editingCommand.value = cmd
  editOpen.value = true
}

function addNewCommand() {
  editingCommand.value = null
  editOpen.value = true
}

function onCommandSaved() {
  editOpen.value = false
  editingCommand.value = null
}

function toggleDeleteConfirm(id: number) {
  deleteConfirmId.value = deleteConfirmId.value === id ? null : id
}

// ── Export / Import ──
// Import resets all optional flags to defaults (hidden=false, auto_execute=false,
// project_only=false → global) per design decision.
const importer = createJsonImporter({
  kind: 'terminal_quick_command',
  existingLabels: () => new Set(commands.value.map(c => c.label)),
  addItem: (item) => addCommand({ ...item, hidden: false, auto_execute: false, project_only: false }),
  onError: (reason) => {
    const key = reason === 'parseError'
      ? 'terminal.importParseError'
      : reason === 'kindMismatch'
        ? 'terminal.importKindMismatch'
        : 'terminal.importInvalidFile'
    toast.show(t(key), { icon: '⚠️', type: 'error' })
  },
  onSummary: ({ imported, skipped }) => {
    toast.show(t('terminal.importSuccess', { imported, skipped }), { icon: '✅', type: 'success' })
  },
})

function exportJson() {
  showMoreMenu.value = false
  downloadJson(buildExportPayload('terminal_quick_command', commands.value), 'terminal_quick_command.json')
}

function importJson() {
  showMoreMenu.value = false
  importer.trigger()
}

async function doDelete(id: number) {
  deleteConfirmId.value = null
  const ok = await deleteCommand(id)
  if (ok) {
    toast.show(t('terminal.commandDeleted'), { icon: '✅', type: 'success' })
  }
}

async function onDragEnd() {
  const ids = localCommands.value.map(c => c.id)
  const ok = await reorderCommands(ids)
  if (!ok) {
    toast.show(t('terminal.reorderFailed'), { icon: '❌', type: 'error' })
    localCommands.value = [...commands.value] // Reset from source of truth
  }
}
</script>

<style>
.qc-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.qc-list {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-2) 0;
}

.qc-item-wrapper {
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.qc-item-wrapper:last-child {
  border-bottom: none;
}

.qc-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  transition: background var(--duration-base);
}

.qc-row.qc-hidden {
  opacity: 0.55;
}

@media (hover: hover) {
  .qc-row:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }
}

.drag-handle {
  cursor: grab;
  color: var(--text-muted, #999);
  font-size: var(--font-size-2xl);
  line-height: 1;
  user-select: none;
  padding:0 var(--space-1);
}

.drag-handle:active {
  cursor: grabbing;
}

.qc-label {
  flex-shrink: 0;
  font-weight: var(--font-weight-medium);
  max-width: 100px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 3px;
}

.qc-badge-auto {
  color: var(--accent-color, #0066cc);
  flex-shrink: 0;
}

.qc-badge-dim {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.qc-cmd {
  flex: 1;
  min-width: 0;
  color: var(--text-muted, #999);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qc-action {
  background: none;
  border: none;
  color: var(--text-muted, #999);
  cursor: pointer;
  padding: var(--space-2);
  display: flex;
  align-items: center;
  border-radius: var(--radius-xs);
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .qc-action:hover {
    background: var(--bg-tertiary, #f0f0f0);
    color: var(--text-primary);
  }

  .qc-action.danger:hover {
    color: #e53e3e;
  }
}

.qc-delete-confirm {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-5) var(--space-3) 28px;
  background: color-mix(in srgb, #e53e3e 8%, transparent);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
}

.qc-confirm-btn {
  padding:3px var(--space-5);
  border: 1px solid var(--border-color, #ddd);
  border-radius: var(--radius-xs);
  font-size: var(--font-size-sm);
  cursor: pointer;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
}

.qc-confirm-btn.delete {
  background: #e53e3e;
  color: #fff;
  border-color: #e53e3e;
}

.qc-confirm-btn.cancel {
  color: var(--text-muted, #999);
}

.create-btn {
  margin-left: auto;
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-xs);
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .create-btn:hover {
    background: rgba(0, 102, 204, 0.1);
  }
}

.qc-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  padding: var(--space-8);
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.qc-empty-icon {
  opacity: 0.3;
}

/* PopupMenu teleports to body — these styles must be unscoped */
.menu-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-4) var(--space-6);
  border: none;
  background: none;
  font-size: var(--font-size-md);
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
}

@media (hover: hover) {
  .menu-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
  }
}
</style>
