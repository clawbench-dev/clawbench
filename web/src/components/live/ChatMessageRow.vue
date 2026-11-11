<template>
  <div class="chat-row" :class="{ 'chat-row--self': self }">
    <SpeakerAvatar class="chat-row__avatar" :name="name" />
    <div class="chat-row__body">
      <div class="chat-row__meta">
        <span class="chat-row__name">{{ name }}</span>
        <span class="chat-row__time">{{ time }}</span>
      </div>
      <div class="chat-row__text">{{ text }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * One chat message: avatar, name + time, bubble.
 *
 * Shared by the live chat and the archived meeting record's chat tab — they
 * show the SAME conversation, so they must not be two drawings of it. Before
 * this component they were, and a `meetingChatStyleParity` test existed only to
 * catch the two copies drifting; the archived one still rendered bare text
 * lines while the live one had bubbles, which reads as "the styling was lost".
 *
 * Timestamp formatting also lives here (via the shared `formatClockTime`) so
 * neither caller grows its own copy again.
 */
import { computed } from 'vue'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import { formatClockTime } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    name: string
    /** Epoch ms of the message. */
    at: number
    text: string
    /** The local user's own message: mirrored to the right in the accent bubble. */
    self?: boolean
  }>(),
  { self: false },
)

const time = computed(() => formatClockTime(new Date(props.at)))
</script>

<style scoped>
/* [avatar | meta + bubble]. The avatar column is what makes the sender
   scannable without reading the name on every line. */
.chat-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3, 6px);
  max-width: 100%;
}

.chat-row--self {
  flex-direction: row-reverse;
}

.chat-row__avatar {
  margin-top: calc(2 * var(--dp));
}

.chat-row__body {
  display: flex;
  flex-direction: column;
  gap: calc(1 * var(--dp));
  min-width: 0;
  max-width: 82%;
}

.chat-row--self .chat-row__body {
  align-items: flex-end;
}

.chat-row__meta {
  display: flex;
  gap: var(--space-2, 4px);
  align-items: baseline;
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #8b949e);
}

.chat-row__text {
  padding: var(--space-2, 4px) var(--space-4, 8px);
  border-radius: var(--radius-md);
  background: var(--bg-tertiary, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-sm, 13px);
  line-height: 1.55;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.chat-row--self .chat-row__text {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 22%, transparent);
}
</style>
