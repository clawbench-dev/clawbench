<template>
  <div
    class="mail-row"
    :class="{ 'is-unread': !msg.seen, 'is-active': active }"
    :draggable="isWideScreen"
    @dragstart="onDragStart"
    @dragend="cleanupDragGhost()"
  >
    <!-- Opening the mail is its own button rather than the row itself: the row
         also carries the quote action, and a button cannot contain another
         button. Keeping the real <button> preserves keyboard activation and the
         focus ring, which a clickable <div> would lose. -->
    <button
      class="mail-row__open"
      :aria-current="active ? 'true' : undefined"
      @click="emit('open', msg.id)"
    >
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

    <!-- Same row action as the task and forge lists, anchored to the bottom-right
         corner so it lines up with the last line of text. -->
    <button
      class="mail-row__quote"
      :title="t('file.header.quoteInChat')"
      :aria-label="t('file.header.quoteInChat')"
      :disabled="quoting"
      @click.stop="quoteToChat"
    >
      <MessageSquare :size="14" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquare, Paperclip, Star } from 'lucide-vue-next'
import {
  mailClockTime,
  mailDayLabel,
  mailFullTimestamp,
  mailSenderLabel,
  type MailMessage,
  type MailMessageDetail,
} from '@/utils/mailTypes'
import { mailQuoteLabel, buildMailQuote } from '@/utils/mailQuote'
import { mailDragPayload, startQuoteDrag } from '@/utils/quoteDrag'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion'
import { useToast } from '@/composables/useToast'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'

const props = defineProps<{ msg: MailMessage; active?: boolean }>()
const emit = defineEmits<{ open: [id: number] }>()

const { t } = useI18n()
const { isWideScreen } = useWideScreenLayout()
const quoteQuestion = useQuoteQuestion()
const toast = useToast()

/**
 * Drag a row into the chat → the same quote the reader's button produces.
 *
 * Identity only (a dragstart cannot await): the drop handler fetches the body.
 * Wide-screen only, like every other quote drag source — the drop target is the
 * chat column, which a narrow layout never shows next to this list.
 */
function onDragStart(e: DragEvent): void {
  const payload = mailDragPayload({ id: props.msg.id, label: mailQuoteLabel(props.msg, t('mail.list.noSubject')) })
  if (!startQuoteDrag(e, payload)) e.preventDefault()
}

/**
 * Quote this email from the row's button.
 *
 * The full body has to be fetched first: the list only has a snippet, and the
 * quote exists for the AI, which has no way to read a mailbox from an id. Same
 * builder and same composer as the reader's button, so both produce one card
 * shape.
 */
const quoting = ref(false)

async function quoteToChat(): Promise<void> {
  if (quoting.value) return
  quoting.value = true
  try {
    const data = await apiGet<{ message: MailMessageDetail }>(`/api/mail/messages/${props.msg.id}`)
    if (!data?.message) return
    const quote = buildMailQuote(data.message, t('mail.list.noSubject'))
    quoteQuestion.openComposer({ quote, label: quote.filePath })
  } catch (e) {
    appLog.w('MailListItem', 'quote failed', e)
    toast.show(t('common.operationFailed'), { type: 'error', icon: 'warning' })
  } finally {
    quoting.value = false
  }
}

const dayLabel = computed(() => mailDayLabel(props.msg.sentAt))
const clockTime = computed(() => mailClockTime(props.msg.sentAt))
const fullTimestamp = computed(() => mailFullTimestamp(props.msg.sentAt))
</script>

<style scoped>
/* Three weights of text, three colours — sender (13/500, primary), subject
   (13/400, primary, bold when unread), snippet (11, hint). Before this the
   snippet and the time used an undefined token, so they inherited the row's
   colour and every line read the same; the list looked like one grey block. */
/* The row is the layout/paint box; opening the mail is the inner button (a
   <button> cannot contain the quote button). Padding lives on that button, not
   on the row, so the whole visible line — including its edges — stays one click
   target. */
.mail-row {
  position: relative;
  display: flex;
  align-items: center;
  width: 100%;
  padding-right: calc(10 * var(--dp));
  border-bottom: 1px solid var(--border-color);
  background: none;
  color: inherit;
  transition: background var(--duration-fast, 0.15s) ease;
}
/* The click target: full remaining width, stripped of the browser's button
   chrome so the row still reads as one flat list line. */
.mail-row__open {
  display: flex;
  flex: 1;
  min-width: 0;
  padding: calc(9 * var(--dp)) 0 calc(10 * var(--dp)) calc(10 * var(--dp));
  border: none;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.mail-row:last-child {
  border-bottom: none;
}
.mail-row:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.1));
}
/* Unread is an accent edge, not a dot in its own column: the dot pushed the
   text 15px right while the right edge stayed at 10px, which made every row
   look shifted and off-centre. Drawn with `inset` box-shadow so it costs no
   layout width and the text sits at the same 10px on both sides. */
.mail-row.is-unread {
  box-shadow: inset calc(2 * var(--dp)) 0 0 var(--accent-color);
}
/* Open in the reading pane: a tint, no bar (the bar means unread). */
.mail-row.is-active {
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
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
  /* Only the snippet gives the corner action room: the title and meta lines
     keep the row's full width, which is why the button is positioned instead of
     sitting in its own column. */
  padding-right: calc(28 * var(--dp));
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
  color: var(--text-hint);
}

/* Row-level "quote into a conversation" action, same affordance as the task and
   forge lists: hidden until hover so the list stays dense, always visible on
   touch where there is no hover at all.

   Positioned over the snippet's right end rather than laid out beside the whole
   text column — a flex sibling would reserve a full-height strip on every row,
   shrinking the sender, subject and meta lines too. */
.mail-row__quote {
  position: absolute;
  right: calc(10 * var(--dp));
  bottom: calc(10 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition: opacity var(--duration-base), color var(--duration-base), background var(--duration-base);
}
.mail-row:hover .mail-row__quote,
.mail-row__quote:focus-visible {
  opacity: 1;
}
@media (hover: none) {
  .mail-row__quote { opacity: 1; }
}
@media (hover: hover) {
  .mail-row__quote:hover {
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}
.mail-row__quote:disabled {
  cursor: default;
}
</style>
