<template>
  <BottomSheet :open="open" auto :title="t('terminal.quickCommands')" @close="$emit('close')">
    <template #header>
      <ZapIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.quickCommands') }}</span>
      <span class="bs-header-actions">
        <template v-if="activeTab === 'saved'">
          <button class="create-btn" @click.stop="addNewCommand" :title="t('terminal.addCommand')">
            <PlusIcon :size="16" />
          </button>
          <button ref="moreBtnRef" class="create-btn" @click.stop="showMoreMenu = !showMoreMenu" :title="t('terminal.moreActions')">
            <MoreVerticalIcon :size="16" />
          </button>
        </template>
        <button
          v-else
          class="create-btn"
          :class="{ danger: clearArmed }"
          @click.stop="handleClearHistory"
          :title="t('terminal.historyClear')"
        >
          <Trash2Icon :size="16" />
        </button>
      </span>
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
      <!-- Two lists in one drawer: saved commands are hand-picked, history is
           collected automatically. Splitting them into two drawers would make
           the user remember which one holds a given command. -->
      <div class="qc-tabs">
        <SegmentedTabs
          :model-value="activeTab"
          :options="[
            { value: 'saved', label: t('terminal.commandsSaved') },
            { value: 'recent', label: t('terminal.commandsRecent') },
          ]"
          @update:model-value="(v: string) => (activeTab = v as 'saved' | 'recent')"
        />
      </div>

      <template v-if="activeTab === 'saved'">
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
          <ZapIcon class="qc-empty-icon empty-state__icon" />
          <span>{{ t('terminal.quickCommandsEmpty') }}</span>
        </div>
      </template>

      <template v-else>
        <div v-if="!historyEnabled" class="qc-empty">
          <HistoryIcon class="qc-empty-icon empty-state__icon" />
          <span>{{ t('terminal.historyDisabled') }}</span>
          <span class="qc-empty-hint">{{ t('terminal.historyDisabledHint') }}</span>
        </div>
        <div v-else-if="historyLoading && history.length === 0" class="qc-empty">
          <LoadingIndicator size="sm" />
        </div>
        <div v-else-if="history.length > 0" class="qc-list">
          <div v-for="cmd in history" :key="cmd.id" class="qc-item-wrapper">
            <div class="qc-row qc-history-row" @click="insertCommand(cmd.command)">
              <span class="qc-cmd qc-cmd-lead" :title="cmd.command">{{ cmd.command }}</span>
              <span v-if="cmd.use_count > 1" class="qc-count" :title="t('terminal.historyUseCount', { n: cmd.use_count })">
                {{ cmd.use_count }}
              </span>
              <button
                v-if="!isPinned(cmd.command)"
                class="qc-action"
                @click.stop="pinCommand(cmd.command)"
                :title="t('terminal.historyPin')"
              >
                <PinIcon :size="14" />
              </button>
              <span v-else class="qc-pinned">{{ t('terminal.historyPinned') }}</span>
              <button class="qc-action danger" @click.stop="removeHistory(cmd.id)" :title="t('terminal.historyDelete')">
                <Trash2Icon :size="14" />
              </button>
            </div>
          </div>
        </div>
        <div v-else class="qc-empty">
          <HistoryIcon class="qc-empty-icon empty-state__icon" />
          <span>{{ t('terminal.historyEmpty') }}</span>
          <span class="qc-empty-hint">{{ t('terminal.historyEmptyHint') }}</span>
        </div>
      </template>
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
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SegmentedTabs from '@/components/common/SegmentedTabs.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import QuickCommandEditModal from './QuickCommandEditModal.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import {
  Zap as ZapIcon,
  Pencil as PencilIcon,
  Trash2 as Trash2Icon,
  Plus as PlusIcon,
  EyeOff as EyeOffIcon,
  Pin as PinIcon,
  History as HistoryIcon,
  MoreVertical as MoreVerticalIcon,
  Download as DownloadIcon,
  Upload as UploadIcon,
} from 'lucide-vue-next'
import { useQuickCommands, type QuickCommand } from '@/composables/useQuickCommands'
import { useCommandHistory } from '@/composables/useCommandHistory'
import { localConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { createJsonImporter, buildExportPayload, downloadJson } from '@/composables/useQuickSendIO'

const props = defineProps({
  open: Boolean,
})

const emit = defineEmits<{
  (e: 'close'): void
  /** Put a remembered command on the terminal's input line (not executed). */
  (e: 'insert', command: string): void
}>()

const { t } = useI18n()
const toast = useToast()
const { commands, addCommand, reorderCommands, deleteCommand } = useQuickCommands()
const {
  items: history,
  loading: historyLoading,
  refresh: refreshHistory,
  remove: removeHistoryEntry,
  clear: clearHistory,
} = useCommandHistory()

const activeTab = ref<'saved' | 'recent'>('saved')
// Reading the setting here (rather than gating capture alone) is what makes the
// switch honest: with recording off, the panel must not quietly import the
// shell's history files behind the user's back either.
const historyEnabled = computed(() => localConfig.terminalHistoryEnabled !== false)
const localCommands = ref<QuickCommand[]>([...commands.value])
const deleteConfirmId = ref<number | null>(null)
const editOpen = ref(false)
const editingCommand = ref<QuickCommand | null>(null)
const moreBtnRef = ref<HTMLElement | null>(null)
const showMoreMenu = ref(false)
const clearArmed = ref(false)
let clearArmTimer: ReturnType<typeof setTimeout> | null = null

// Sync local list when commands change
watch(commands, (val) => {
  localCommands.value = [...val]
}, { deep: true })

// Reset state when drawer opens/closes
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    deleteConfirmId.value = null
    clearArmed.value = false
    // Opening the recent tab is the moment history is worth loading: importing
    // from the shell files is a disk read plus a few hundred UPSERTs.
    if (activeTab.value === 'recent' && historyEnabled.value) void refreshHistory()
  } else {
    // The "more actions" menu is a PopupMenu teleported to <body>, so it is not
    // covered by the BottomSheet's own v-show. Left open it would hover over
    // whatever replaced the drawer — including after a tab switch, which closes
    // the drawer via useTabDrawer's effectiveOpen.
    showMoreMenu.value = false
  }
})

watch(activeTab, (tab) => {
  if (tab === 'recent' && historyEnabled.value) void refreshHistory()
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

// ── History ──

function insertCommand(command: string) {
  emit('insert', command)
  emit('close')
}

function isPinned(command: string): boolean {
  return commands.value.some(c => c.command === command)
}

async function pinCommand(command: string) {
  // Label is the command itself, truncated: a one-tap pin must not require
  // typing a name on a phone keyboard, and the command is what the user
  // recognizes in the list.
  const label = command.length > 24 ? `${command.slice(0, 24)}…` : command
  const ok = await addCommand({
    label,
    command,
    hidden: false,
    auto_execute: false,
    project_only: false,
  })
  if (ok) {
    toast.show(t('terminal.historyPinnedToast'), { icon: 'success', type: 'success' })
  } else {
    toast.show(t('terminal.saveFailed'), { icon: 'error', type: 'error' })
  }
}

async function removeHistory(id: number) {
  await removeHistoryEntry(id)
}

/**
 * Clearing history is destructive and irreversible, so the first tap arms the
 * button and the second one clears — the same "tap twice" pattern the delete
 * confirmation uses, without a second dialog.
 */
function handleClearHistory() {
  if (!clearArmed.value) {
    clearArmed.value = true
    clearArmTimer = setTimeout(() => {
      clearArmed.value = false
      clearArmTimer = null
    }, 4000)
    return
  }
  if (clearArmTimer) {
    clearTimeout(clearArmTimer)
    clearArmTimer = null
  }
  clearArmed.value = false
  void doClearHistory()
}

async function doClearHistory() {
  const ok = await clearHistory()
  toast.show(
    ok ? t('terminal.historyCleared') : t('terminal.saveFailed'),
    { icon: ok ? 'success' : 'error', type: ok ? 'success' : 'error' },
  )
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
    toast.show(t(key), { icon: 'warning', type: 'error' })
  },
  onSummary: ({ imported, skipped }) => {
    toast.show(t('terminal.importSuccess', { imported, skipped }), { icon: 'success', type: 'success' })
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
    toast.show(t('terminal.commandDeleted'), { icon: 'success', type: 'success' })
  }
}

async function onDragEnd() {
  const ids = localCommands.value.map(c => c.id)
  const ok = await reorderCommands(ids)
  if (!ok) {
    toast.show(t('terminal.reorderFailed'), { icon: 'error', type: 'error' })
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

.qc-tabs {
  flex-shrink: 0;
  padding: var(--space-3) var(--space-5) 0;
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
  opacity: var(--opacity-muted);
}

@media (hover: hover) {
  .qc-row:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }
}

/* History rows are tappable as a whole (insert the command), so they get the
   pointer affordance the saved rows deliberately do not have. */
.qc-history-row {
  cursor: pointer;
}

.qc-cmd-lead {
  order: -1;
  flex: 1;
  min-width: 0;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qc-count {
  flex-shrink: 0;
  min-width: calc(18 * var(--dp));
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
  text-align: center;
}

.qc-pinned {
  flex-shrink: 0;
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
  white-space: nowrap;
}

.drag-handle {
  cursor: grab;
  color: var(--text-muted, #999);
  font-size: var(--font-size-2xl);
  line-height: 1;
  user-select: none;
  padding: 0 var(--space-1);
}

.drag-handle:active {
  cursor: grabbing;
}

.qc-label {
  flex-shrink: 0;
  font-weight: var(--font-weight-medium);
  max-width: calc(100 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: calc(3 * var(--dp));
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
  padding: var(--space-3) var(--space-5) var(--space-3) calc(28 * var(--dp));
  background: color-mix(in srgb, #e53e3e 8%, transparent);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
}

.qc-confirm-btn {
  padding: calc(3 * var(--dp)) var(--space-5);
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

.bs-header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.create-btn {
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-xs);
  transition: background var(--duration-base), color var(--duration-base);
}

.create-btn.danger {
  color: #e53e3e;
}

@media (hover: hover) {
  .create-btn:hover {
    background: rgba(0, 102, 204, 0.1);
  }

  .create-btn.danger:hover {
    background: color-mix(in srgb, #e53e3e 12%, transparent);
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

.qc-empty-hint {
  font-size: var(--font-size-sm);
  text-align: center;
  opacity: var(--opacity-muted);
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
