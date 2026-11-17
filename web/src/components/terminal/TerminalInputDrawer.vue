<template>
  <BottomSheet :open="open" auto :title="t('terminal.input')" @close="$emit('close')">
    <template #header>
      <PenLineIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.input') }}</span>
      <button class="ti-btn ti-btn-fill" @click.stop="fillFromClipboard" :title="t('terminal.inputFillClipboard')">
        <ClipboardPasteIcon :size="16" />
      </button>
      <button class="ti-btn ti-btn-clear" @click.stop="clearText" :disabled="!text" :title="t('terminal.inputClear')">
        <EraserIcon :size="16" />
      </button>
      <button class="ti-btn ti-btn-send" @click.stop="doInput" :disabled="!text" :title="t('terminal.inputSend')">
        <SendHorizontalIcon :size="16" />
      </button>
    </template>

    <div class="ti-content">
      <!-- Remembered commands. This drawer exists because typing on a phone is
           painful, and the single most common thing to type is something you
           already typed: with an empty box it lists the most recent five, and
           as you type it narrows to what matches. Same list, one behaviour. -->
      <div v-if="suggestions.length > 0" class="ti-recent">
        <div class="ti-recent-title">{{ suggestionTitle }}</div>
        <button
          v-for="cmd in suggestions"
          :key="cmd.id"
          class="ti-recent-item"
          :title="cmd.command"
          @click="fillCommand(cmd.command)"
        >{{ cmd.command }}</button>
      </div>

      <!-- data-voice-ignore: this box takes a shell command, not prose.
           Dictation here could only produce something that runs nothing, and
           the floating mic button would sit on top of the drawer. -->
      <textarea
        ref="textareaRef"
        v-model="text"
        class="ti-textarea"
        data-voice-ignore
        :placeholder="t('terminal.inputPlaceholder')"
        rows="8"
        @keydown.enter.exact.prevent="doInput"
      />
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { PenLine as PenLineIcon, ClipboardPaste as ClipboardPasteIcon, SendHorizontal as SendHorizontalIcon, Eraser as EraserIcon } from 'lucide-vue-next'
import { readClipboardText } from '@/utils/clipboard'
import { useToast } from '@/composables/useToast'
import { useCommandHistory } from '@/composables/useCommandHistory'
import { localConfig } from '@/composables/useSettingsConfig'
import { suggestCommands } from '@/utils/commandSuggestion'

const props = defineProps({
  open: Boolean,
})

const emit = defineEmits(['close', 'input'])

const { t } = useI18n()
const toast = useToast()
const { items: history, refresh: refreshHistory } = useCommandHistory()

const text = ref('')
const textareaRef = ref<HTMLElement | null>(null)

/**
 * What to offer for the current text: the five most recent commands when the box
 * is empty, otherwise the remembered commands that match what has been typed.
 * Empty when recording is off.
 */
const suggestions = computed(() =>
  localConfig.terminalHistoryEnabled === false
    ? []
    : suggestCommands(history.value, text.value, 5),
)

const suggestionTitle = computed(() =>
  text.value.trim() ? t('terminal.historyMatching') : t('terminal.historyRecent'),
)

watch(() => props.open, (isOpen) => {
  if (!isOpen) return
  text.value = ''
  nextTick(() => textareaRef.value?.focus())
  // Refresh on open: a command run seconds ago must be here. The sync behind
  // this is TTL-gated, so reopening the drawer does not re-read the files.
  if (localConfig.terminalHistoryEnabled !== false) void refreshHistory()
})

/** Put a remembered command into the textarea so it can be edited first. */
function fillCommand(command: string) {
  text.value = command
  nextTick(() => textareaRef.value?.focus())
}

async function fillFromClipboard() {
  try {
    const clip = await readClipboardText()
    if (!clip) {
      toast.show(t('terminal.clipboardEmpty'), { icon: 'copy', type: 'info' })
      return
    }
    text.value = clip
    nextTick(() => textareaRef.value?.focus())
  } catch {
    toast.show(t('terminal.clipboardReadFailed'), { icon: 'error', type: 'error' })
  }
}

function clearText() {
  text.value = ''
  nextTick(() => textareaRef.value?.focus())
}

function doInput() {
  const value = text.value
  if (!value) return
  emit('input', value)
  emit('close')
}
</script>

<style>
.ti-content {
  padding: var(--space-2) calc(14 * var(--dp)) calc(14 * var(--dp));
}

.ti-recent {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
  padding-bottom: var(--space-3);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.ti-recent-title {
  padding: var(--space-2) 0;
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
}

.ti-recent-item {
  display: block;
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: none;
  border-radius: var(--radius-xs);
  background: none;
  color: var(--text-primary, #1a1a1a);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  text-align: left;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .ti-recent-item:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }
}

.ti-textarea {
  display: block;
  width: 100%;
  box-sizing: border-box;
  resize: none;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-primary, #1a1a1a);
  font-family: var(--font-mono);
  font-size: var(--font-size-md);
  line-height: var(--line-height-relaxed);
  padding: var(--space-4) 0;
  outline: none;
}

.ti-textarea::placeholder {
  color: var(--text-muted, #999);
}

/* Icon-only buttons: no shape, no background — just the icon */
.ti-btn {
  margin-left: var(--space-1);
  border: none;
  background: none;
  padding: var(--space-2);
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 0;
  transition: color var(--duration-base);
}

@media (hover: hover) {
  .ti-btn:hover {
    color: var(--text-primary, #1a1a1a);
  }
}

.ti-btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
  color: var(--text-muted, #999);
}

.ti-btn-send {
  margin-left: auto;
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .ti-btn-send:hover {
    color: var(--accent-color, #0066cc);
  }
}
</style>
