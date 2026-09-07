<template>
  <!-- Full-viewport dialog. ModalDialog Teleports to <body>, escaping the
       settings tab-panel's `isolation: isolate` stacking context — without
       this the inline overlay was trapped below the chat column in
       wide-screen mode and covered by it. -->
  <ModalDialog
    :open="open"
    :title="t('settings.items.agentCopyTitle')"
    :z-index="2500"
    :max-width="380"
    @close="handleClose"
  >
    <div class="copy-agent-dialog__body">
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
    </div>

    <template #footer>
      <button class="fbtn" @click="handleClose">
        {{ t('common.cancel') }}
      </button>
      <button
        class="fbtn fbtn-primary"
        :disabled="!newName.trim()"
        @click="submit"
      >
        {{ t('settings.items.agentCopyConfirm') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const props = defineProps<{
  open: boolean
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
let unregisterBack: (() => void) | null = null

// Reset the pre-filled name and focus the input whenever the dialog opens.
watch(() => props.open, (open) => {
  if (open) {
    error.value = ''
    newName.value = props.sourceName ? `${props.sourceName} (${t('settings.items.agentCopy')})` : ''
    nextTick(() => nameInputRef.value?.focus())
    unregisterBack = registerBackHandler({
      id: 'copy-agent-dialog',
      canGoBack: () => true,
      goBack: () => handleClose(),
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
}, { immediate: true })

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
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
.copy-agent-dialog__body {
  display: flex;
  flex-direction: column;
  padding: 14px 16px 6px;
}

.copy-agent-dialog__field {
  margin-bottom: 14px;
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
</style>
