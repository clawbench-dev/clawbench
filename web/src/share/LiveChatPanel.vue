<template>
  <div class="live-chat">
    <div ref="listRef" class="live-chat__list">
      <div v-if="messages.length === 0" class="live-chat__empty">
        {{ t('share.live.chatEmpty') }}
      </div>
      <div
        v-for="m in messages"
        :key="m.id"
        class="live-chat__msg"
        :class="{ 'live-chat__msg--self': m.self }"
      >
        <div class="live-chat__meta">
          <span class="live-chat__name">{{ m.name }}</span>
          <span class="live-chat__time">{{ timeOf(m.at) }}</span>
        </div>
        <div class="live-chat__text">{{ m.text }}</div>
      </div>
    </div>

    <form class="live-chat__compose" @submit.prevent="submit">
      <input
        v-model="draft"
        class="live-chat__input"
        type="text"
        :maxlength="CHAT_MAX_CHARS"
        :placeholder="t('share.live.chatPlaceholder')"
        :disabled="disabled"
        data-role="chat-input"
      />
      <button
        class="live-chat__send"
        type="submit"
        :disabled="disabled || !draft.trim()"
        data-action="chat-send"
      >
        {{ t('share.live.chatSend') }}
      </button>
    </form>
  </div>
</template>

<script setup lang="ts">
/**
 * Chat panel for a live share.
 *
 * Messages ride the control channel, not the media: a chat line must not wait
 * behind a video keyframe, and the publisher relays them so viewers see each
 * other without a server round trip.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CHAT_MAX_CHARS } from '@/share/liveControl'

export interface ChatMessage {
  id: string
  /** The sender's room id; absent in the archived transcript, which has no "self". */
  from?: string
  name: string
  text: string
  at: number
  /** Rendered on the right and not counted as unread. */
  self: boolean
}

const props = defineProps<{ messages: ChatMessage[]; disabled?: boolean }>()
const emit = defineEmits<{ send: [text: string] }>()

const { t } = useI18n()
const draft = ref('')
const listRef = ref<HTMLElement | null>(null)

function timeOf(at: number): string {
  const d = new Date(at)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function submit(): void {
  const text = draft.value.trim()
  if (!text) return
  emit('send', text.slice(0, CHAT_MAX_CHARS))
  draft.value = ''
}

// Follow the conversation, but only when the reader is already at the bottom.
watch(
  () => props.messages.length,
  async () => {
    const el = listRef.value
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
    if (!nearBottom) return
    await nextTick()
    el.scrollTop = el.scrollHeight
  },
)
</script>

<style scoped>
.live-chat {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.live-chat__list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  display: flex;
  flex-direction: column;
  gap: var(--space-4, 8px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
}

.live-chat__empty {
  color: var(--text-muted, #8b949e);
  font-size: var(--font-size-xs, 11px);
}

.live-chat__msg {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: 100%;
}

.live-chat__msg--self {
  align-items: flex-end;
}

.live-chat__meta {
  display: flex;
  gap: var(--space-3, 6px);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #8b949e);
}

.live-chat__text {
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-sm, 13px);
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.live-chat__msg--self .live-chat__text {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 22%, transparent);
}

.live-chat__compose {
  display: flex;
  gap: var(--space-3, 6px);
  padding: var(--space-4, 8px);
  border-top: 1px solid var(--border-color, #30363d);
}

.live-chat__input {
  flex: 1 1 auto;
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border: 1px solid var(--border-color, #30363d);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-primary, #0d1117);
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-sm, 13px);
}

.live-chat__send {
  flex: 0 0 auto;
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: var(--accent-color, #4a90d9);
  color: #fff;
  font-size: var(--font-size-sm, 13px);
  cursor: pointer;
}

.live-chat__send:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
