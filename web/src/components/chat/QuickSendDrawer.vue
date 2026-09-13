<template>
  <BottomSheet :open="open" auto :title="t('chat.quickSend.title')" @close="$emit('close')">
    <template #header>
      <SendIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.quickSend.title') }}</span>
      <span class="bs-header-actions">
        <button class="create-btn" @click.stop="clustersDrawerRef?.open()" :title="t('chat.messageClusters.title')">
          <SparklesIcon :size="16" />
        </button>
        <button class="create-btn" @click.stop="addNewItem" :title="t('chat.quickSend.addItem')">
          <PlusIcon :size="16" />
        </button>
        <button ref="moreBtnRef" class="create-btn" @click.stop="showMoreMenu = !showMoreMenu" :title="t('chat.quickSend.moreActions')">
          <MoreVerticalIcon :size="16" />
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
        <DownloadIcon :size="14" /> {{ t('chat.quickSend.exportJson') }}
      </button>
      <button class="menu-item" @click="importJson">
        <UploadIcon :size="14" /> {{ t('chat.quickSend.importJson') }}
      </button>
    </PopupMenu>

    <div class="qs-content">
      <div v-if="items.length > 0" class="qs-list">
        <VueDraggable
          v-model="localItems"
          handle=".qs-drag-handle"
          @end="onDragEnd"
        >
            <div v-for="item in localItems" :key="item.id" class="qs-item-wrapper">
              <div class="qs-row">
                <span class="qs-drag-handle">≡</span>
                <span class="qs-label">{{ item.label }}</span>
                <span class="qs-cmd" :title="item.command">{{ item.command }}</span>
                <button class="qs-action" @click="editItem(item)" :title="t('chat.quickSend.editItem')">
                  <PencilIcon :size="14" />
                </button>
                <button class="qs-action danger" @click="toggleDeleteConfirm(item.id)" :title="t('chat.quickSend.deleteItem')">
                  <Trash2Icon :size="14" />
                </button>
              </div>
              <!-- Inline delete confirmation -->
              <div v-if="deleteConfirmId === item.id" class="qs-delete-confirm">
                <span>{{ t('chat.quickSend.deleteConfirm') }}</span>
                <button class="qs-confirm-btn delete" @click="doDelete(item.id)">{{ t('common.confirm') }}</button>
                <button class="qs-confirm-btn cancel" @click="deleteConfirmId = null">{{ t('common.cancel') }}</button>
              </div>
            </div>
        </VueDraggable>
      </div>
      <div v-else class="qs-empty">
        <SendIcon :size="32" class="qs-empty-icon" />
        <span>{{ t('chat.quickSend.emptyHint') }}</span>
      </div>
    </div>

    <!-- Edit modal (separate, not drill-down) -->
    <QuickSendEditModal
      :open="editOpen"
      :editing-item="editingItem"
      @close="editOpen = false"
      @saved="onItemSaved"
    />

    <!-- Message clusters drawer -->
    <MessageClustersDrawer ref="clustersDrawerRef" />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import BottomSheet from '@/components/common/BottomSheet.vue'
import QuickSendEditModal from './QuickSendEditModal.vue'
import MessageClustersDrawer from './MessageClustersDrawer.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { Send as SendIcon, PencilIcon, Trash2Icon, PlusIcon, Sparkles as SparklesIcon, MoreVertical as MoreVerticalIcon, Download as DownloadIcon, Upload as UploadIcon } from 'lucide-vue-next'
import { useQuickSend, type QuickSendItem } from '@/composables/useQuickSend'
import { useToast } from '@/composables/useToast'
import { createJsonImporter, buildExportPayload, downloadJson } from '@/composables/useQuickSendIO'

const props = defineProps({
  open: Boolean,
})

defineEmits(['close'])

const { t } = useI18n()
const toast = useToast()
const { items, addItem, reorderItems, deleteItem } = useQuickSend()

const localItems = ref<QuickSendItem[]>([...items.value])
const deleteConfirmId = ref<number | null>(null)
const editOpen = ref(false)
const editingItem = ref<QuickSendItem | null>(null)
const clustersDrawerRef = ref<InstanceType<typeof MessageClustersDrawer> | null>(null)
const moreBtnRef = ref<HTMLElement | null>(null)
const showMoreMenu = ref(false)

// Sync local list when items change
watch(items, (val) => {
  localItems.value = [...val]
}, { deep: true })

// Reset state when drawer opens/closes
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    deleteConfirmId.value = null
  }
})

function editItem(item: QuickSendItem) {
  editingItem.value = item
  editOpen.value = true
}

function addNewItem() {
  editingItem.value = null
  editOpen.value = true
}

function onItemSaved() {
  editOpen.value = false
  editingItem.value = null
}

function toggleDeleteConfirm(id: number) {
  deleteConfirmId.value = deleteConfirmId.value === id ? null : id
}

// ── Export / Import ──
const importer = createJsonImporter({
  kind: 'chat_quick_send',
  existingLabels: () => new Set(items.value.map(it => it.label)),
  // Import resets project_only to false (global) per design decision.
  addItem: (item) => addItem({ ...item, project_only: false }),
  onError: (reason) => {
    const key = reason === 'parseError'
      ? 'chat.quickSend.importParseError'
      : reason === 'kindMismatch'
        ? 'chat.quickSend.importKindMismatch'
        : 'chat.quickSend.importInvalidFile'
    toast.show(t(key), { icon: '⚠️', type: 'error' })
  },
  onSummary: ({ imported, skipped }) => {
    toast.show(t('chat.quickSend.importSuccess', { imported, skipped }), { icon: '✅', type: 'success' })
  },
})

function exportJson() {
  showMoreMenu.value = false
  downloadJson(buildExportPayload('chat_quick_send', items.value), 'chat_quick_send.json')
}

function importJson() {
  showMoreMenu.value = false
  importer.trigger()
}

async function doDelete(id: number) {
  deleteConfirmId.value = null
  const ok = await deleteItem(id)
  if (ok) {
    toast.show(t('chat.quickSend.itemDeleted'), { icon: '✅', type: 'success' })
  }
}

async function onDragEnd() {
  const ids = localItems.value.map(it => it.id)
  const ok = await reorderItems(ids)
  if (!ok) {
    toast.show(t('chat.quickSend.reorderFailed'), { icon: '❌', type: 'error' })
    localItems.value = [...items.value] // Reset from source of truth
  }
}
</script>

<style>
.qs-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.qs-list {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-2) 0;
}

.qs-item-wrapper {
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.qs-item-wrapper:last-child {
  border-bottom: none;
}

.qs-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .qs-row:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }
}

.qs-drag-handle {
  cursor: grab;
  color: var(--text-muted, #999);
  font-size: var(--font-size-2xl);
  line-height: 1;
  user-select: none;
  padding:0 var(--space-1);
}

.qs-drag-handle:active {
  cursor: grabbing;
}

.qs-label {
  flex-shrink: 0;
  font-weight: var(--font-weight-medium);
  max-width: 100px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qs-cmd {
  flex: 1;
  min-width: 0;
  color: var(--text-muted, #999);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qs-action {
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
  .qs-action:hover {
    background: var(--bg-tertiary, #f0f0f0);
    color: var(--text-primary);
  }

  .qs-action.danger:hover {
    color: #e53e3e;
  }
}

.qs-delete-confirm {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-5) var(--space-3) 28px;
  background: color-mix(in srgb, #e53e3e 8%, transparent);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
}

.qs-confirm-btn {
  padding:3px var(--space-5);
  border: 1px solid var(--border-color, #ddd);
  border-radius: var(--radius-xs);
  font-size: var(--font-size-sm);
  cursor: pointer;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
}

.qs-confirm-btn.delete {
  background: #e53e3e;
  color: #fff;
  border-color: #e53e3e;
}

.qs-confirm-btn.cancel {
  color: var(--text-muted, #999);
}

.bs-header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.create-btn {
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

.qs-empty {
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

.qs-empty-icon {
  opacity: var(--opacity-disabled);
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
