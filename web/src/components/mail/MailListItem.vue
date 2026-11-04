<template>
  <button
    class="mail-row"
    :class="{ 'is-unread': !msg.seen, 'is-active': active }"
    :aria-current="active ? 'true' : undefined"
    @click="emit('open', msg.id)"
  >
    <span class="mail-row__dot" />
    <span class="mail-row__main">
      <span class="mail-row__line">
        <span class="mail-row__sender">{{ mailSenderLabel(msg) }}</span>
        <span class="mail-row__time">{{ formatMailDate(msg.sentAt, locale) }}</span>
      </span>
      <span class="mail-row__subject">{{ msg.subject || t('mail.list.noSubject') }}</span>
      <span class="mail-row__snippet">{{ msg.snippet }}</span>
    </span>
    <span class="mail-row__marks">
      <Star v-if="msg.flagged" class="mail-row__star" />
      <Paperclip v-if="msg.hasAttachments" class="mail-row__clip" />
    </span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Paperclip, Star } from 'lucide-vue-next'
import { formatMailDate, mailSenderLabel, type MailMessage } from '@/utils/mailTypes'

defineProps<{ msg: MailMessage; active?: boolean }>()
const emit = defineEmits<{ open: [id: number] }>()

const { t, locale } = useI18n()
</script>

<style scoped>
.mail-row {
  display: flex;
  align-items: flex-start;
  gap: calc(6 * var(--dp));
  width: 100%;
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border: none;
  border-bottom: 1px solid var(--border-color);
  background: none;
  text-align: left;
  cursor: pointer;
  color: inherit;
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
.mail-row.is-active .mail-row__sender {
  color: var(--text-primary);
}
.mail-row__dot {
  flex: none;
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
  margin-top: calc(6 * var(--dp));
  border-radius: 50%;
  background: transparent;
}
.mail-row.is-unread .mail-row__dot {
  background: var(--accent-color);
}
.mail-row__main {
  display: flex;
  flex-direction: column;
  gap: calc(1 * var(--dp));
  min-width: 0;
  flex: 1;
}
.mail-row__line {
  display: flex;
  align-items: baseline;
  gap: var(--space-2, 4px);
}
.mail-row__sender {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-secondary);
}
.mail-row.is-unread .mail-row__sender {
  color: var(--text-primary);
  font-weight: 600;
}
.mail-row__time {
  flex: none;
  font-size: var(--font-size-xs, 11px);
  color: var(--text-tertiary);
}
.mail-row__subject {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary);
}
.mail-row__snippet {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
  color: var(--text-tertiary);
}
.mail-row__marks {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
  flex: none;
  padding-top: calc(2 * var(--dp));
  color: var(--text-tertiary);
}
.mail-row__star {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
  color: var(--color-warning, #bf8700);
  fill: currentColor;
}
.mail-row__clip {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
}
</style>
