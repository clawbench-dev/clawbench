<template>
  <BottomSheet :open="open" auto :title="t('chat.quickSend.title')" @close="$emit('close')">
    <template #header>
      <SendIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.quickSend.title') }}</span>
      <button class="create-btn" @click.stop="addNewItem" :title="t('chat.quickSend.addItem')">
        <PlusIcon :size="16" />
      </button>
    </template>

    <div class="qs-content">
      <div v-if="items.length > 0" class="qs-list">
        <draggable
          v-model="localItems"
          handle=".qs-drag-handle"
          item-key="id"
          @end="onDragEnd"
        >
          <template #item="{ element: item }">
            <div class="qs-item-wrapper">
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
          </template>
        </draggable>
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
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import BottomSheet from '@/components/common/BottomSheet.vue'
import QuickSendEditModal from './QuickSendEditModal.vue'
import { Send as SendIcon, PencilIcon, Trash2Icon, PlusIcon } from 'lucide-vue-next'
import { useQuickSend, type QuickSendItem } from '@/composables/useQuickSend'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  open: Boolean,
})

const emit = defineEmits(['close'])

const { t } = useI18n()
const toast = useToast()
const { items, reorderItems, deleteItem } = useQuickSend()

const localItems = ref<QuickSendItem[]>([...items.value])
const deleteConfirmId = ref<number | null>(null)
const editOpen = ref(false)
const editingItem = ref<QuickSendItem | null>(null)

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

async function doDelete(id: number) {
  deleteConfirmId.value = null
  const ok = await deleteItem(id)
  if (ok) {
    toast.show(t('chat.quickSend.itemDeleted'), { type: 'success' })
  }
}

async function onDragEnd() {
  const ids = localItems.value.map(it => it.id)
  const ok = await reorderItems(ids)
  if (!ok) {
    toast.show(t('chat.quickSend.reorderFailed'), { type: 'error' })
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
  padding: 4px 0;
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
  gap: 6px;
  padding: 8px 10px;
  font-size: 13px;
  color: var(--text-primary);
  transition: background 0.12s;
}

.qs-row:hover {
  background: var(--bg-tertiary, #f5f5f5);
}

.qs-drag-handle {
  cursor: grab;
  color: var(--text-muted, #999);
  font-size: 16px;
  line-height: 1;
  user-select: none;
  padding: 0 2px;
}

.qs-drag-handle:active {
  cursor: grabbing;
}

.qs-label {
  flex-shrink: 0;
  font-weight: 500;
  max-width: 100px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qs-cmd {
  flex: 1;
  min-width: 0;
  color: var(--text-muted, #999);
  font-family: monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qs-action {
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

.qs-action:hover {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-primary);
}

.qs-action.danger:hover {
  color: #e53e3e;
}

.qs-delete-confirm {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px 6px 28px;
  background: color-mix(in srgb, #e53e3e 8%, transparent);
  font-size: 12px;
  color: var(--text-secondary, #666);
}

.qs-confirm-btn {
  padding: 3px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 4px;
  font-size: 12px;
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

.qs-empty {
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

.qs-empty-icon {
  opacity: 0.3;
}
</style>
