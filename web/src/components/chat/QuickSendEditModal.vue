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
      <label class="form-checkbox">
        <input type="checkbox" v-model="form.project_only" />
        <span>{{ t('chat.quickSend.projectOnly') }}</span>
      </label>
      <div v-if="formError" class="form-error">{{ formError }}</div>
    </div>

    <template #footer>
      <button class="fbtn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" :disabled="saving" @click="saveItem"><LoadingIndicator v-if="saving" size="sm" inline /><span v-else>{{ t('common.save') }}</span></button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { PencilIcon, PlusIcon } from 'lucide-vue-next'
import { useQuickSend, type QuickSendItem } from '@/composables/useQuickSend'
import { useToast } from '@/composables/useToast'
import { validateQuickSendForm } from '@/utils/quickSendValidation.ts'

const props = defineProps<{
  open: boolean
  editingItem: QuickSendItem | null
  initialValues?: { label: string; command: string }
}>()
const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const toast = useToast()
const { addItem, updateItem } = useQuickSend()

const form = ref({ label: '', command: '', project_only: false })
const formError = ref('')
const saving = ref(false)

// Reset form when dialog opens
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    if (props.editingItem) {
      form.value = { label: props.editingItem.label, command: props.editingItem.command, project_only: props.editingItem.project_only }
    } else if (props.initialValues) {
      form.value = { label: props.initialValues.label, command: props.initialValues.command, project_only: false }
    } else {
      form.value = { label: '', command: '', project_only: false }
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
      ok = await updateItem(props.editingItem.id, { label, command, project_only: form.value.project_only })
    } else {
      ok = await addItem({ label, command, project_only: form.value.project_only })
    }

    if (ok) {
      toast.show(t('chat.quickSend.itemSaved'), { icon: '✅', type: 'success' })
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
  padding: var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.form-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary, #666);
}

.form-label .required {
  color: #e53e3e;
}

.form-input {
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color, #ddd);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  outline: none;
  transition: border-color var(--duration-base);
}

.form-input:focus {
  border-color: var(--accent-color, #0066cc);
}

.form-textarea {
  resize: vertical;
  min-height: 160px;
  line-height: var(--line-height-normal);
  font-family: inherit;
}

.form-error {
  font-size: var(--font-size-sm);
  color: #e53e3e;
}

.form-checkbox {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  cursor: pointer;
}

.form-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  accent-color: var(--accent-color, #0066cc);
}
</style>
