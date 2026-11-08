<template>
  <div
    class="notes-status"
    :class="`notes-status--${tone}`"
    data-role="notes-status"
    :data-health="health"
  >
    <span class="notes-status__dot" aria-hidden="true" />
    <span class="notes-status__text" data-role="notes-status-text">{{ text }}</span>
    <span v-if="lost > 0" class="notes-status__lost" data-role="notes-status-lost">
      {{ t('liveShare.notes.statusLost', { n: lost }) }}
    </span>
    <button
      v-if="showRetry"
      class="meeting-text-btn notes-status__action"
      type="button"
      data-role="notes-retry-mic"
      @click="emit('retry')"
    >
      {{ t('liveShare.notes.retryMic') }}
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * One line that answers "is transcription working right now".
 *
 * The panel used to show the same empty card whether nobody had spoken yet or
 * the microphone had died twenty minutes ago — the two look identical from the
 * outside, and only one of them needs the user to do something. This bar is the
 * difference: it always names the current state, and when the state is
 * recoverable it carries the action that recovers it.
 *
 * It is deliberately NOT a toast. A toast disappears; this condition does not,
 * and the whole point is that the user can look at the pane and know.
 *
 * `health` comes from useLiveNotes, where the priority between causes lives:
 * a dead microphone also means "no speech" and "nothing queued", so reporting
 * the general symptom instead of the cause would be true and useless.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NotesHealthKind } from '@/composables/useLiveNotes'

const props = defineProps<{
  health: NotesHealthKind
  /** How long the meeting has been without speech. */
  silentForMs: number
  /** Segments recorded but not yet transcribed. */
  pending: number
  /** Segments lost for good (failed uploads, queue overflow). */
  lost: number
  /** A microphone recovery attempt is running. */
  retrying: boolean
  /** Lines transcribed so far. */
  segments: number
}>()

const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

/** Grey for normal, amber for "catching up / waiting", red for "stopped". */
const tone = computed<'ok' | 'warn' | 'error'>(() => {
  switch (props.health) {
    case 'micDead':
    case 'offline':
      return 'error'
    case 'suspended':
    case 'reconnecting':
    case 'queued':
    case 'silent':
      return 'warn'
    default:
      return 'ok'
  }
})

const text = computed(() => {
  switch (props.health) {
    case 'micDead':
      return props.retrying
        ? t('liveShare.notes.statusMicRecovering')
        : t('liveShare.notes.statusMicLost')
    case 'suspended':
      return t('liveShare.notes.statusSuspended')
    case 'reconnecting':
      return t('liveShare.notes.statusReconnecting')
    case 'offline':
      return t('liveShare.notes.statusOffline')
    case 'queued':
      return t('liveShare.notes.statusQueued', { n: props.pending })
    case 'silent':
      // Whole minutes, never zero: the state only exists once the threshold
      // has passed, so "0 minutes without sound" would be nonsense.
      return t('liveShare.notes.statusSilent', {
        n: Math.max(1, Math.floor(props.silentForMs / 60_000)),
      })
    default:
      return props.segments > 0
        ? t('liveShare.notes.statusListening', { n: props.segments })
        : t('liveShare.notes.statusListeningEmpty')
  }
})

/** Only a microphone that has NOT come back on its own offers the button. */
const showRetry = computed(() => props.health === 'micDead' && !props.retrying)
</script>

<style scoped>
.notes-status {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-radius: var(--radius-sm, 6px);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
  color: var(--text-muted, #656d76);
}

/* A warning is a block, not a floating sentence: on a pane with no transcript
   yet, an 11px line in the top-left corner reads as a rendering failure. */
.notes-status--warn,
.notes-status--error {
  border: 1px solid transparent;
}

.notes-status--warn {
  background: color-mix(in srgb, var(--color-orange, #d97706) 10%, transparent);
  border-color: color-mix(in srgb, var(--color-orange, #d97706) 28%, transparent);
  color: var(--text-primary);
}

.notes-status--error {
  background: color-mix(in srgb, var(--color-red, #dc2626) 10%, transparent);
  border-color: color-mix(in srgb, var(--color-red, #dc2626) 28%, transparent);
  color: var(--text-primary);
}

.notes-status__dot {
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--text-muted, #656d76);
}

/* Deliberately still: the tab badge next to the pane title already pulses for
   "this meeting is live", and a second pulsing dot one line below it is noise.
   The colour is what this one carries. */
.notes-status--ok .notes-status__dot {
  background: var(--accent-color, #4a90d9);
}

.notes-status--warn .notes-status__dot { background: var(--color-orange, #d97706); }
.notes-status--error .notes-status__dot { background: var(--color-red, #dc2626); }

.notes-status__text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Permanent damage sits apart from the live state: "3 segments were lost" is
   not something that changes, and it must not look like a current condition. */
.notes-status__lost {
  flex-shrink: 0;
  color: var(--color-red, #dc2626);
}

.notes-status__action {
  margin-left: auto;
  flex-shrink: 0;
}
</style>
