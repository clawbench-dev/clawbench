<template>
  <ModalDialog :open="open" :title="editingCommand ? t('terminal.editCommand') : t('terminal.addCommand')" @close="$emit('close')">
    <template #header>
      <span class="modal-header-icon">
        <PencilIcon v-if="editingCommand" :size="16" />
        <PlusIcon v-else :size="16" />
      </span>
      <span class="modal-title">{{ editingCommand ? t('terminal.editCommand') : t('terminal.addCommand') }}</span>
    </template>

    <div class="qce-edit-content">
      <div class="form-group">
        <label class="form-label">{{ t('terminal.commandLabel') }} <span class="required">*</span></label>
        <input type="text" class="form-input" v-model="form.label" :placeholder="t('terminal.commandLabel')" />
      </div>
      <div class="form-group">
        <label class="form-label">{{ t('terminal.commandText') }} <span class="required">*</span></label>
        <textarea class="form-input form-textarea" v-model="form.command" :placeholder="t('terminal.commandText')" rows="4" />
      </div>
      <div v-if="formError" class="form-error">{{ formError }}</div>

      <label class="form-checkbox">
        <input type="checkbox" v-model="form.hidden" />
        <span>{{ t('terminal.commandHidden') }}</span>
      </label>
      <label class="form-checkbox">
        <input type="checkbox" v-model="form.auto_execute" />
        <span>{{ t('terminal.commandAutoExecute') }}</span>
      </label>
      <div v-if="form.auto_execute && hasExistingAutoExec" class="form-hint">{{ t('terminal.autoExecuteWarning') }}</div>
      <label class="form-checkbox">
        <input type="checkbox" v-model="form.project_only" />
        <span>{{ t('terminal.commandProjectOnly') }}</span>
      </label>
    </div>

    <template #footer>
      <button class="modal-btn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="modal-btn primary" :disabled="saving" @click="saveCommand"><LoadingIndicator v-if="saving" size="sm" inline /><span v-else>{{ t('common.save') }}</span></button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { PencilIcon, PlusIcon } from 'lucide-vue-next'
import { useQuickCommands, type QuickCommand } from '@/composables/useQuickCommands'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  open: boolean
  editingCommand: QuickCommand | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const toast = useToast()
const { commands, addCommand, updateCommand } = useQuickCommands()

const form = ref({ label: '', command: '', hidden: false, auto_execute: false, project_only: false })
const formError = ref('')
const saving = ref(false)

// Whether another command already has auto_execute (for warning)
const hasExistingAutoExec = computed(() => {
  if (!props.editingCommand) {
    return commands.value.some(c => c.auto_execute)
  }
  return commands.value.some(c => c.auto_execute && c.id !== props.editingCommand!.id)
})

// Reset form when dialog opens
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    if (props.editingCommand) {
      form.value = {
        label: props.editingCommand.label,
        command: props.editingCommand.command,
        hidden: props.editingCommand.hidden,
        auto_execute: props.editingCommand.auto_execute,
        project_only: props.editingCommand.project_only,
      }
    } else {
      form.value = { label: '', command: '', hidden: false, auto_execute: false, project_only: false }
    }
    formError.value = ''
  }
})

async function saveCommand() {
  const label = form.value.label.trim()
  const command = form.value.command.trim()
  if (!label || !command) {
    formError.value = t('terminal.commandRequired')
    return
  }
  formError.value = ''
  saving.value = true

  try {
    let ok: boolean
    if (props.editingCommand) {
      ok = await updateCommand(props.editingCommand.id, { label, command, hidden: form.value.hidden, auto_execute: form.value.auto_execute, project_only: form.value.project_only })
    } else {
      ok = await addCommand({ label, command, hidden: form.value.hidden, auto_execute: form.value.auto_execute, project_only: form.value.project_only })
    }

    if (ok) {
      toast.show(t('terminal.commandSaved'), { type: 'success' })
      emit('saved')
    } else {
      formError.value = t('terminal.saveFailed')
    }
  } finally {
    saving.value = false
  }
}
</script>

<style>
.qce-edit-content {
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
  min-height: 80px;
  line-height: 1.5;
  font-family: inherit;
}

.form-error {
  font-size: 12px;
  color: #e53e3e;
}

.form-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-primary);
  cursor: pointer;
}

.form-checkbox input[type="checkbox"] {
  -webkit-appearance: none;
  appearance: none;
  width: 16px;
  height: 16px;
  margin: 0;
  flex-shrink: 0;
  border: 1.5px solid var(--border-color, #999);
  border-radius: 4px;
  background: var(--bg-secondary, #fff);
  cursor: pointer;
  position: relative;
  transition: background 0.15s, border-color 0.15s;
}

.form-checkbox input[type="checkbox"]:checked {
  background: var(--accent-color, #0066cc);
  border-color: var(--accent-color, #0066cc);
}

.form-checkbox input[type="checkbox"]:checked::after {
  content: '';
  position: absolute;
  left: 4px;
  top: 1px;
  width: 5px;
  height: 9px;
  border: solid #fff;
  border-width: 0 2px 2px 0;
  transform: rotate(45deg);
}

.form-checkbox input[type="checkbox"]:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

.form-hint {
  font-size: 11px;
  color: var(--text-muted, #999);
  padding-left: 24px;
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
