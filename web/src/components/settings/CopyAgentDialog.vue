<template>
  <div class="copy-agent-dialog-overlay" @click.self="handleClose">
    <div class="copy-agent-dialog">
      <div class="copy-agent-dialog__header">{{ t('settings.items.agentCopyTitle') }}</div>

      <div class="copy-agent-dialog__field">
        <label class="copy-agent-dialog__label">{{ t('settings.items.agentName') }}</label>
        <input
          ref="nameInputRef"
          type="text"
          class="copy-agent-dialog__input"
          v-model="newName"
          :placeholder="t('settings.items.agentCopyPlaceholder')"
          @keydown.enter="submit"
        />
      </div>

      <div v-if="error" class="copy-agent-dialog__error">{{ error }}</div>

      <div class="copy-agent-dialog__actions">
        <button class="copy-agent-dialog__btn copy-agent-dialog__btn--cancel" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="copy-agent-dialog__btn copy-agent-dialog__btn--submit"
          :disabled="!newName.trim()"
          @click="submit"
        >
          {{ t('settings.items.agentCopyConfirm') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  sourceName: string
}>()

const emit = defineEmits<{
  close: []
  confirmed: [name: string]
}>()

const { t } = useI18n()

const newName = ref('')
const error = ref('')
const nameInputRef = ref<HTMLInputElement | null>(null)

onMounted(() => {
  newName.value = props.sourceName ? `${props.sourceName} (${t('settings.items.agentCopy')})` : ''
  nextTick(() => nameInputRef.value?.focus())
})

function submit() {
  const trimmed = newName.value.trim()
  if (!trimmed) {
    error.value = t('settings.items.agentCopyEmptyName')
    return
  }
  error.value = ''
  emit('confirmed', trimmed)
}

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.copy-agent-dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 16px;
}

.copy-agent-dialog {
  background: var(--bg-primary);
  border-radius: 16px;
  padding: 24px;
  width: 100%;
  max-width: 380px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
}

.copy-agent-dialog__header {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 20px;
  text-align: center;
}

.copy-agent-dialog__field {
  margin-bottom: 16px;
}

.copy-agent-dialog__label {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.copy-agent-dialog__input {
  width: 100%;
  min-width: 0;
  padding: 10px 12px;
  font-size: 15px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.copy-agent-dialog__input:focus {
  border-color: var(--accent-color);
}

.copy-agent-dialog__error {
  font-size: 13px;
  color: #e74c3c;
  margin-bottom: 12px;
  padding: 8px 12px;
  background: rgba(231, 76, 60, 0.1);
  border-radius: 8px;
}

.copy-agent-dialog__actions {
  display: flex;
  gap: 12px;
  margin-top: 20px;
}

.copy-agent-dialog__btn {
  flex: 1;
  padding: 12px;
  border: none;
  border-radius: 10px;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
}

.copy-agent-dialog__btn--cancel {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.copy-agent-dialog__btn--submit {
  background: var(--accent-color);
  color: #fff;
}

.copy-agent-dialog__btn--submit:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .copy-agent-dialog__btn--cancel:hover {
    background: var(--bg-secondary);
  }
  .copy-agent-dialog__btn--submit:not(:disabled):hover {
    background: var(--accent-hover);
  }
}

.copy-agent-dialog__btn--cancel:active {
  background: var(--bg-secondary);
}

.copy-agent-dialog__btn--submit:not(:disabled):active {
  background: var(--accent-hover);
}
</style>
