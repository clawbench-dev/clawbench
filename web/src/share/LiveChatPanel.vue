<template>
  <div class="live-chat">
    <div ref="listRef" class="live-chat__list" @scroll.passive="onScroll">
      <!-- The same empty state every 会议 tab uses: a centred glyph + title
           instead of one muted sentence in the top corner, which reads as a
           message that failed to render. No `desc` here on purpose — an empty
           chat has no cause worth explaining (see MeetingEmpty). -->
      <MeetingEmpty
        v-if="messages.length === 0"
        :icon="MessagesSquare"
        :title="t('share.live.chatEmpty')"
        data-role="chat-empty"
      />
      <ChatMessageRow
        v-for="m in messages"
        :key="m.id"
        :name="m.name"
        :at="m.at"
        :text="m.text"
        :self="m.self"
      />
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
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagesSquare } from 'lucide-vue-next'
import ChatMessageRow from '@/components/live/ChatMessageRow.vue'
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import { CHAT_MAX_CHARS } from '@/share/liveControl'
import { useStickToBottom } from '@/composables/useStickToBottom.ts'

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

function submit(): void {
  const text = draft.value.trim()
  if (!text) return
  emit('send', text.slice(0, CHAT_MAX_CHARS))
  draft.value = ''
}

// Same rule as the transcript beside it: follow while the reader is at the
// bottom, and open on the newest line instead of the first one ever sent.
const { onScroll } = useStickToBottom(listRef, () => props.messages.length)
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
  /* No padding of its own: the pane already insets every surface by 16dp, and
     a second inset here put the chat 10px deeper than the transcript beside it. */
  gap: var(--space-3, 6px);
  padding: 0;
}

/* The empty state is `MeetingEmpty` (centred glyph + title); this list only
   has to give it the free height. */

/* No rule above the composer: the filled field is the boundary. */
.live-chat__compose {
  display: flex;
  gap: var(--space-3, 6px);
  padding: var(--space-4, 8px) 0 0;
}

/* Flat field: no border, the fill is the affordance. The mix is against the
   panel colour rather than a background step — 15 of the 36 themes have
   secondary↔tertiary within 1.12 of each other, where a stepped fill vanishes. */
.live-chat__input {
  flex: 1 1 auto;
  min-width: 0;
  padding: calc(5 * var(--dp)) calc(9 * var(--dp));
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: color-mix(in srgb, var(--text-primary, #e6edf3) 6%, var(--bg-primary, #0d1117));
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-sm, 13px);
}

.live-chat__input:focus {
  outline: none;
  background: color-mix(in srgb, var(--text-primary, #e6edf3) 10%, var(--bg-primary, #0d1117));
  box-shadow: 0 0 0 1px var(--accent-color, #4a90d9);
}

.live-chat__send {
  flex: 0 0 auto;
  padding: calc(5 * var(--dp)) calc(12 * var(--dp));
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
