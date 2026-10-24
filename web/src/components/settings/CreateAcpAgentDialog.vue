<template>
  <!-- Full-viewport dialog, same Teleport rationale as CopyAgentDialog: the
       settings tab panel establishes `isolation: isolate`, so an inline
       overlay would be trapped under the chat column in wide-screen mode. -->
  <ModalDialog
    :open="open"
    :title="t('settings.items.agentCreateAcpTitle')"
    :z-index="2500"
    @close="handleClose"
  >
    <div class="create-acp-agent__body">
      <div class="create-acp-agent__field">
        <label class="create-acp-agent__label">{{ t('settings.items.agentName') }}</label>
        <input
          ref="nameInputRef"
          type="text"
          class="create-acp-agent__input"
          v-model="name"
          :placeholder="t('settings.items.agentCreateAcpNamePlaceholder')"
          @keydown.enter="submit"
        />
      </div>

      <div class="create-acp-agent__field">
        <label class="create-acp-agent__label">{{ t('settings.items.agentCreateAcpCommand') }}</label>
        <input
          type="text"
          class="create-acp-agent__input create-acp-agent__input--mono"
          v-model="command"
          :placeholder="t('settings.items.agentCreateAcpCommandPlaceholder')"
          spellcheck="false"
          autocomplete="off"
          @keydown.enter="submit"
        />
        <p class="create-acp-agent__hint">{{ t('settings.items.agentCreateAcpHint') }}</p>
      </div>

      <div v-if="error" class="create-acp-agent__error">{{ error }}</div>
    </div>

    <template #footer>
      <button class="fbtn" @click="handleClose">
        {{ t('common.cancel') }}
      </button>
      <button
        class="fbtn fbtn-primary"
        :disabled="!canSubmit"
        @click="submit"
      >
        {{ t('settings.items.agentCreateAcpConfirm') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const props = defineProps<{
  open: boolean
  // Server-side failure text (a name collision, an empty command), owned by the
  // parent: only it knows the create request failed, and only it can keep the
  // dialog open so the user can correct the input and retry.
  errorMessage?: string
}>()

const emit = defineEmits<{
  close: []
  confirmed: [payload: { name: string; command: string }]
}>()

const { t } = useI18n()

const name = ref('')
const command = ref('')
const error = ref('')
const nameInputRef = ref<HTMLInputElement | null>(null)
let unregisterBack: (() => void) | null = null

// The command is what makes the agent runnable at all — without it the server
// would store an agent that advertises ACP with nothing to spawn. Both fields
// are trimmed before submission, so whitespace alone does not count as filled.
const canSubmit = computed(() => name.value.trim() !== '' && command.value.trim() !== '')

watch(() => props.errorMessage, (msg) => {
  error.value = msg ?? ''
})

watch(() => props.open, (open) => {
  if (open) {
    error.value = props.errorMessage ?? ''
    name.value = ''
    command.value = ''
    nextTick(() => nameInputRef.value?.focus())
    unregisterBack = registerBackHandler({
      id: 'create-acp-agent-dialog',
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
  if (!canSubmit.value) {
    error.value = command.value.trim() === ''
      ? t('settings.items.agentCreateAcpCommandRequired')
      : t('settings.items.agentCopyEmptyName')
    return
  }
  error.value = ''
  emit('confirmed', { name: name.value.trim(), command: command.value.trim() })
}

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.create-acp-agent__body {
  display: flex;
  flex-direction: column;
  padding: calc(14 * var(--dp)) var(--space-7) var(--space-3);
}

.create-acp-agent__field {
  margin-bottom: calc(14 * var(--dp));
}

.create-acp-agent__label {
  display: block;
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  margin-bottom: var(--space-2);
}

.create-acp-agent__input {
  width: 100%;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-6);
  font-size: var(--font-size-xl);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.create-acp-agent__input:focus {
  border-color: var(--accent-color);
}

/* The command is a literal shell command line, so it reads better in a
   monospace face — spaces and flags are otherwise easy to miscount. */
.create-acp-agent__input--mono {
  font-family: var(--font-mono);
  font-size: var(--font-size-lg);
}

.create-acp-agent__hint {
  margin: var(--space-3) 0 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: 1.4;
}

.create-acp-agent__error {
  font-size: var(--font-size-md);
  color: #e74c3c;
  margin-bottom: var(--space-6);
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}
</style>
