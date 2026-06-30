<template>
  <BottomSheet :open="open" auto :title="t('terminal.quickCommands')" @close="$emit('close')">
    <template #header>
      <ZapIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.quickCommands') }}</span>
      <button class="create-btn" @click.stop="addNewCommand" :title="t('terminal.addCommand')">
        <PlusIcon :size="16" />
      </button>
    </template>

    <div class="qc-content">
      <div v-if="commands.length > 0" class="qc-list">
        <draggable
          v-model="localCommands"
          handle=".drag-handle"
          item-key="id"
          @end="onDragEnd"
        >
          <template #item="{ element: cmd }">
            <div class="qc-item-wrapper">
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
          </template>
        </draggable>
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
import draggable from 'vuedraggable'
import BottomSheet from '@/components/common/BottomSheet.vue'
import QuickCommandEditModal from './QuickCommandEditModal.vue'
import { ZapIcon, PencilIcon, Trash2Icon, PlusIcon, EyeOffIcon } from 'lucide-vue-next'
import { useQuickCommands, type QuickCommand } from '@/composables/useQuickCommands'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  open: Boolean,
})

const emit = defineEmits(['close'])

const { t } = useI18n()
const toast = useToast()
const { commands, reorderCommands, deleteCommand } = useQuickCommands()

const localCommands = ref<QuickCommand[]>([...commands.value])
const deleteConfirmId = ref<number | null>(null)
const editOpen = ref(false)
const editingCommand = ref<QuickCommand | null>(null)

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

async function doDelete(id: number) {
  deleteConfirmId.value = null
  const ok = await deleteCommand(id)
  if (ok) {
    toast.show(t('terminal.commandDeleted'), { type: 'success' })
  }
}

async function onDragEnd() {
  const ids = localCommands.value.map(c => c.id)
  const ok = await reorderCommands(ids)
  if (!ok) {
    toast.show(t('terminal.reorderFailed'), { type: 'error' })
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
  padding: 4px 0;
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
  gap: 6px;
  padding: 8px 10px;
  font-size: 13px;
  color: var(--text-primary);
  transition: background 0.12s;
}

.qc-row.qc-hidden {
  opacity: 0.55;
}

.qc-row:hover {
  background: var(--bg-tertiary, #f5f5f5);
}

.drag-handle {
  cursor: grab;
  color: var(--text-muted, #999);
  font-size: 16px;
  line-height: 1;
  user-select: none;
  padding: 0 2px;
}

.drag-handle:active {
  cursor: grabbing;
}

.qc-label {
  flex-shrink: 0;
  font-weight: 500;
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
  font-family: monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qc-action {
  background: none;
  border: none;
  color: var(--text-muted, #999);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
  border-radius: 4px;
  transition: background 0.12s, color 0.12s;
}

.qc-action:hover {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-primary);
}

.qc-action.danger:hover {
  color: #e53e3e;
}

.qc-delete-confirm {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px 6px 28px;
  background: color-mix(in srgb, #e53e3e 8%, transparent);
  font-size: 12px;
  color: var(--text-secondary, #666);
}

.qc-confirm-btn {
  padding: 3px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 4px;
  font-size: 12px;
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
  border-radius: 4px;
  transition: background 0.15s;
}

.create-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}

.qc-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 20px;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.qc-empty-icon {
  opacity: 0.3;
}
</style>
