<template>
  <Transition name="retry-notice-fade">
    <div
      v-if="visible"
      class="retry-notice"
      role="status"
      aria-live="polite"
      :title="tooltip"
    >
      <LoadingIndicator size="sm" inline class="retry-notice-spinner" />
      <span class="retry-notice-text">{{ label }}</span>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useRetryNotice } from '@/composables/useRetryNotice'

/**
 * RetryNotice — a transient line above the composer shown while the agent is
 * retrying an upstream failure on its own.
 *
 * The user's problem it solves: a rate-limited agent retries silently for over a
 * minute (opencode: 11 attempts / ~85s on a persistent 429), so the UI looks
 * frozen. This says "still working, nothing for you to do" without writing
 * anything into the transcript — a retry that recovers must leave no trace.
 *
 * Placement mirrors QueuedMessageBar (same card gutter/rhythm, directly above
 * the input) so the two read as one column of status cards. It is NOT a toast:
 * the toast is a single global slot, and an 85-second occupancy would block
 * every other notification.
 */
const props = defineProps<{
  /** The session whose conversation is on screen; a notice for any other
   *  session is not shown (a background turn must not intrude). */
  sessionId: string
  /**
   * Whether a turn is still in flight. The notice is only meaningful while it
   * is: if a turn ends without its terminal event reaching this client (WS blip,
   * session switch), a stale notice must not sit above the composer forever.
   */
  active: boolean
}>()

const { t } = useI18n()
const { notice } = useRetryNotice()

const visible = computed(
  () => props.active && !!notice.value && notice.value.sessionId === props.sessionId,
)

const label = computed(() => {
  const n = notice.value
  if (!n) return ''
  return n.attempt > 0
    ? t('chat.retryNotice.retryingAttempt', { attempt: n.attempt })
    : t('chat.retryNotice.retrying')
})

// The provider's own wording (usually English) goes in the tooltip: the visible
// line stays localized, but a user who wants the raw reason can hover.
const tooltip = computed(() => {
  const n = notice.value
  if (!n) return ''
  const parts = [t('chat.retryNotice.hint')]
  if (n.message) parts.push(n.httpStatus ? `${n.httpStatus}: ${n.message}` : n.message)
  return parts.join('\n')
})
</script>

<style scoped>
.retry-notice {
  flex-shrink: 0;
  /* Same horizontal inset + bottom rhythm as the queued bar, so the two stack
     as one column of status cards above the input. */
  margin: 0 var(--chat-content-card-gutter) var(--space-4);
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.retry-notice-spinner {
  --li-color: var(--accent-color, currentColor);
}

.retry-notice-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.retry-notice-fade-enter-active {
  transition: opacity var(--duration-base) ease-out;
}
.retry-notice-fade-leave-active {
  transition: opacity var(--duration-slow) ease-in;
}
.retry-notice-fade-enter-from,
.retry-notice-fade-leave-to {
  opacity: 0;
}
</style>
