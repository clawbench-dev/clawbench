<template>
  <button
    class="mail-row"
    :class="{ 'is-unread': !msg.seen, 'is-active': active }"
    :aria-current="active ? 'true' : undefined"
    @click="emit('open', msg.id)"
  >
    <span class="mail-row__dot" />
    <span class="mail-row__main">
      <!-- Sender + time share the first line: the sender identifies the
           message, the time orders it, and both are what you scan before
           reading the subject. -->
      <span class="mail-row__head">
        <span class="mail-row__sender">{{ mailSenderLabel(msg) }}</span>
        <!-- Two lines: the friendly day on top, the exact clock underneath.
             Showing only one of them answers half the question ("which day?"
             or "when?"), and a single line wide enough for both squeezes the
             sender out of a 320px panel. -->
        <span class="mail-row__time" :title="fullTimestamp">
          <span class="mail-row__day">{{ dayLabel }}</span>
          <span class="mail-row__clock">{{ clockTime }}</span>
        </span>
      </span>

      <span class="mail-row__subject-line">
        <span class="mail-row__subject" :class="{ 'is-placeholder': !msg.subject }">
          {{ msg.subject || t('mail.list.noSubject') }}
        </span>
        <span v-if="msg.hasAttachments || msg.flagged" class="mail-row__marks">
          <Paperclip v-if="msg.hasAttachments" class="mail-row__clip" />
          <Star v-if="msg.flagged" class="mail-row__star" />
        </span>
      </span>

      <span class="mail-row__snippet">{{ msg.snippet }}</span>
    </span>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Paperclip, Star } from 'lucide-vue-next'
import {
  mailClockTime,
  mailDayLabel,
  mailFullTimestamp,
  mailSenderLabel,
  type MailMessage,
} from '@/utils/mailTypes'

const props = defineProps<{ msg: MailMessage; active?: boolean }>()
const emit = defineEmits<{ open: [id: number] }>()

const { t } = useI18n()

const dayLabel = computed(() => mailDayLabel(props.msg.sentAt))
const clockTime = computed(() => mailClockTime(props.msg.sentAt))
const fullTimestamp = computed(() => mailFullTimestamp(props.msg.sentAt))
</script>

<style scoped>
/* Three weights of text, three colours — sender (13/500, primary), subject
   (13/400, primary, bold when unread), snippet (11, hint). Before this the
   snippet and the time used an undefined token, so they inherited the row's
   colour and every line read the same; the list looked like one grey block. */
.mail-row {
  display: flex;
  align-items: flex-start;
  gap: calc(8 * var(--dp));
  width: 100%;
  padding: calc(9 * var(--dp)) calc(10 * var(--dp)) calc(10 * var(--dp));
  border: none;
  border-bottom: 1px solid var(--border-color);
  background: none;
  text-align: left;
  cursor: pointer;
  color: inherit;
  transition: background var(--duration-fast, 0.15s) ease;
}
.mail-row:last-child {
  border-bottom: none;
}
.mail-row:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.1));
}
/* Open in the reading pane. Tint plus an accent bar drawn with `inset`
   box-shadow so selecting a row never shifts its text. */
.mail-row.is-active {
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  box-shadow: inset calc(2 * var(--dp)) 0 0 var(--accent-color);
}

/* Unread marker. Sits on the sender's baseline rather than the row's top edge,
   so it reads as part of the first line instead of floating in the padding. */
.mail-row__dot {
  flex: none;
  width: calc(7 * var(--dp));
  height: calc(7 * var(--dp));
  margin-top: calc(7 * var(--dp));
  border-radius: 50%;
  background: transparent;
}
.mail-row.is-unread .mail-row__dot {
  background: var(--accent-color);
}

.mail-row__main {
  display: flex;
  flex-direction: column;
  gap: calc(3 * var(--dp));
  min-width: 0;
  flex: 1;
}

.mail-row__head {
  display: flex;
  align-items: baseline;
  gap: calc(8 * var(--dp));
  min-width: 0;
}
.mail-row__sender {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md, 13px);
  font-weight: var(--font-weight-medium, 500);
  color: var(--text-primary);
}
.mail-row.is-unread .mail-row__sender {
  font-weight: var(--font-weight-semibold, 600);
}

.mail-row__time {
  flex: none;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: calc(1 * var(--dp));
  line-height: 1.2;
  white-space: nowrap;
}
.mail-row__day {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-hint);
}
.mail-row__clock {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

.mail-row__subject-line {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  min-width: 0;
}
.mail-row__subject {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md, 13px);
  color: var(--text-primary);
}
.mail-row.is-unread .mail-row__subject {
  font-weight: var(--font-weight-semibold, 600);
}
/* A message with no subject says so in a different voice: italic and muted,
   so the placeholder never reads as an actual subject line. */
.mail-row__subject.is-placeholder {
  color: var(--text-muted);
  font-style: italic;
}

.mail-row__marks {
  display: flex;
  align-items: center;
  gap: calc(4 * var(--dp));
  flex: none;
  color: var(--text-hint);
}
.mail-row__star {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
  color: var(--color-orange);
  fill: currentColor;
}
.mail-row__clip {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
}

.mail-row__snippet {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
  color: var(--text-hint);
}
</style>
