<template>
  <ModalDialog
    :open="open"
    :z-index="2500"
    :max-width="420"
    @close="close"
  >
    <template #header>
      <span class="modal-header-icon"><Sparkles :size="16" /></span>
      <span class="modal-title">{{ t('task.createHint.title') }}</span>
    </template>

    <div class="tch-body">
      <p class="tch-desc">{{ t('task.createHint.desc') }}</p>
      <div class="tch-label">{{ t('task.createHint.exampleLabel') }}</div>
      <div class="tch-cmd-row">
        <code class="tch-cmd">{{ exampleCommand }}</code>
        <button
          class="tch-copy"
          type="button"
          :title="copied ? t('common.copied') : t('common.copy')"
          @click="copyExample"
        >
          <Check v-if="copied" :size="14" />
          <Copy v-else :size="14" />
        </button>
      </div>
    </div>

    <template #footer>
      <!-- Pushed to the left edge: "never show this again" is a dismissal, not
           an action, and sitting next to the primary button would make it read
           as an equal-weight choice. -->
      <button class="fbtn tch-dont-show" type="button" @click="dontShowAgain">
        {{ t('task.createHint.dontShowAgain') }}
      </button>
      <button class="fbtn fbtn-primary" type="button" @click="$emit('manual')">
        {{ t('task.createHint.manual') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles, Copy, Check } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { dismissTaskCreateHint } from '@/composables/useTaskCreateHint'
import { copyText } from '@/utils/clipboard'

const props = defineProps<{ open: boolean }>()

const emit = defineEmits<{
  close: []
  /** The user chose the manual form instead of the AI route. */
  manual: []
}>()

const { t } = useI18n()

const exampleCommand = computed(() => t('task.createHint.exampleCommand'))
const copied = ref(false)
let copyResetTimer: ReturnType<typeof setTimeout> | null = null
let unregisterBack: (() => void) | null = null

function clearCopyTimer() {
  if (copyResetTimer) {
    clearTimeout(copyResetTimer)
    copyResetTimer = null
  }
}

// Reset the "copied" affordance whenever the dialog reopens, so a stale check
// mark from a previous visit cannot be mistaken for a fresh copy.
watch(() => props.open, (open) => {
  if (open) {
    copied.value = false
    clearCopyTimer()
    // Android back / edge swipe must close this dialog rather than fall through
    // to the task tab's drill-down handler (which would let back exit the app
    // while the dialog is still up).
    unregisterBack = registerBackHandler({
      id: 'task-create-hint',
      canGoBack: () => true,
      goBack: () => close(),
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
}, { immediate: true })

onBeforeUnmount(() => {
  clearCopyTimer()
  if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})

function copyExample() {
  copyText(exampleCommand.value, () => {
    copied.value = true
    clearCopyTimer()
    copyResetTimer = setTimeout(() => { copied.value = false }, 2000)
  })
}

function close() {
  emit('close')
}

/**
 * Persist the dismissal, then close. Deliberately does NOT open the manual
 * form: "不再提示" answers "stop showing me this", and silently launching the
 * form would turn a dismissal into an action the user did not ask for. The next
 * click on "+" goes straight to the form.
 */
function dontShowAgain() {
  dismissTaskCreateHint()
  close()
}
</script>

<style scoped>
.tch-body {
  padding: 14px var(--space-7) var(--space-4);
  display: flex;
  flex-direction: column;
}

.tch-desc {
  margin: 0 0 var(--space-6);
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}

.tch-label {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  margin-bottom: var(--space-3);
}

.tch-cmd-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: var(--space-4) var(--space-5);
}

.tch-cmd {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  word-break: break-all;
}

.tch-copy {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  /* Explicit border: this class is a <button>, and the shared icon-button
     language elsewhere is defined on <a> — without this the UA button border
     shows through. */
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
  transition: color var(--duration-base);
}

@media (hover: hover) {
  .tch-copy:hover {
    color: var(--accent-color);
  }
}

/* Footer is right-aligned by default; the dismissal sits on the far left. */
.tch-dont-show {
  margin-right: auto;
}
</style>
