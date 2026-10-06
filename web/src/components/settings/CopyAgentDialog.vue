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
  // Server-side failure text (e.g. a name collision) to show inside the dialog.
  // Owned by the parent: only it knows the create request failed, and only it
  // can keep the dialog open so the user can correct the name and retry.
  errorMessage?: string
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

// Show the parent-supplied server error (e.g. a name collision). The parent
// keeps the dialog open so the user can correct the name and retry.
watch(() => props.errorMessage, (msg) => {
  error.value = msg ?? ''
})

// Reset the pre-filled name and focus the input whenever the dialog opens.
watch(() => props.open, (open) => {
  if (open) {
    error.value = props.errorMessage ?? ''
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
  padding:14px var(--space-7) var(--space-3);
}

.copy-agent-dialog__field {
  margin-bottom: 14px;
}

.copy-agent-dialog__label {
  display: block;
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  margin-bottom: var(--space-2);
}

.copy-agent-dialog__input {
  width: 100%;
  min-width: 0;
  height: 30px;
  padding: 0 var(--space-6);
  font-size: var(--font-size-xl);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.copy-agent-dialog__input:focus {
  border-color: var(--accent-color);
}

.copy-agent-dialog__error {
  font-size: var(--font-size-md);
  color: #e74c3c;
  margin-bottom: var(--space-6);
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}
</style>
