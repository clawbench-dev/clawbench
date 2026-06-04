<template>
  <ModalDialog :open="open" :title="editingItem ? t('chat.quickSend.editItem') : t('chat.quickSend.addItem')" @close="$emit('close')">
    <template #header>
      <span class="modal-header-icon">
        <PencilIcon v-if="editingItem" :size="16" />
        <PlusIcon v-else :size="16" />
      </span>
      <span class="modal-title">{{ editingItem ? t('chat.quickSend.editItem') : t('chat.quickSend.addItem') }}</span>
    </template>

    <div class="qse-edit-content">
      <div class="form-group">
        <label class="form-label">{{ t('chat.quickSend.itemLabel') }} <span class="required">*</span></label>
        <input type="text" class="form-input" v-model="form.label" :placeholder="t('chat.quickSend.itemLabel')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('chat.quickSend.itemCommand') }} <span class="required">*</span></label>
        <textarea class="form-input form-textarea" v-model="form.command" :placeholder="t('chat.quickSend.itemCommand')" rows="8" />
      </div>
      <div v-if="formError" class="form-error">{{ formError }}</div>
    </div>

    <template #footer>
      <button class="modal-btn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="modal-btn primary" :disabled="saving" @click="saveItem">{{ saving ? '...' : t('common.save') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { PencilIcon, PlusIcon } from 'lucide-vue-next'
import { useQuickSend, type QuickSendItem } from '@/composables/useQuickSend'
import { useToast } from '@/composables/useToast'
import { validateQuickSendForm } from '@/utils/quickSendValidation.ts'

const props = defineProps<{
  open: boolean
  editingItem: QuickSendItem | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const toast = useToast()
const { addItem, updateItem } = useQuickSend()

const form = ref({ label: '', command: '' })
const formError = ref('')
const saving = ref(false)

// Reset form when dialog opens
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    if (props.editingItem) {
      form.value = { label: props.editingItem.label, command: props.editingItem.command }
    } else {
      form.value = { label: '', command: '' }
    }
    formError.value = ''
  }
})

async function saveItem() {
  const label = form.value.label.trim()
  const command = form.value.command.trim()
  const error = validateQuickSendForm({ label, command })
  if (error) {
    formError.value = t(error)
    return
  }
  formError.value = ''
  saving.value = true

  try {
    let ok: boolean
    if (props.editingItem) {
      ok = await updateItem(props.editingItem.id, { label, command })
    } else {
      ok = await addItem({ label, command })
    }

    if (ok) {
      toast.show(t('chat.quickSend.itemSaved'), { type: 'success' })
      emit('saved')
    } else {
      formError.value = t('chat.quickSend.saveFailed')
    }
  } finally {
    saving.value = false
  }
}
</script>

<style>
.qse-edit-content {
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.form-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary, #666);
}

.form-label .required {
  color: #e53e3e;
}

.form-input {
  padding: 8px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 6px;
  font-size: 13px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  outline: none;
  transition: border-color 0.15s;
}

.form-input:focus {
  border-color: var(--accent-color, #0066cc);
}

.form-textarea {
  resize: vertical;
  min-height: 160px;
  line-height: 1.5;
  font-family: inherit;
}

.form-error {
  font-size: 12px;
  color: #e53e3e;
}

.modal-btn {
  padding: 6px 16px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 6px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.12s;
}

.modal-btn:hover {
  background: var(--bg-tertiary, #f5f5f5);
}

.modal-btn.primary {
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-color: var(--accent-color, #0066cc);
}

.modal-btn.primary:hover {
  opacity: 0.9;
}

.modal-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
