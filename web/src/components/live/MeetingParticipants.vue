<template>
  <ul class="mp" data-role="participants">
    <!-- The host is a participant too: pinning them first is what makes the
         list read as "who is in this meeting" rather than "who is watching
         me". Their icon comes from the same nickname → DiceBear mapping the
         transcript and the chat use, so the same person looks the same in all
         three places. -->
    <li class="mp-row mp-row--host" data-role="participant-host">
      <SpeakerAvatar class="mp-avatar" :name="hostName" />
      <div class="mp-main">
        <div class="mp-name-line">
          <span class="mp-name" :title="hostName">{{ hostName }}</span>
          <span class="mp-chip mp-chip--role">{{ t('liveShare.hostName') }}</span>
        </div>
      </div>
    </li>

    <li
      v-for="v in viewers"
      :key="v.id"
      class="mp-row"
      :class="{ 'mp-row--away': presence[v.id] === false }"
      :data-viewer-id="v.id"
    >
      <SpeakerAvatar
        class="mp-avatar"
        :class="{ 'mp-avatar--talking': v.talk === 'talking' }"
        :name="label(v)"
      />

      <div class="mp-main">
        <div class="mp-name-line">
          <span class="mp-name" :title="label(v)">{{ label(v) }}</span>
          <!-- Status chips are mutually exclusive by importance: a host-muted
               viewer cannot also be speaking, and "away" says more about the
               row than the microphone state does. -->
          <span v-if="v.muted" class="mp-chip mp-chip--muted" data-role="chip-muted">
            {{ t('liveShare.participant.muted') }}
          </span>
          <span
            v-else-if="v.talk === 'talking'"
            class="mp-chip mp-chip--live"
            data-role="chip-talking"
          >
            {{ t('liveShare.talkTalking') }}
          </span>
          <span v-if="sharingId === v.id" class="mp-chip mp-chip--screen" data-role="chip-sharing">
            {{ t('liveShare.participant.sharing') }}
          </span>
          <span v-if="presence[v.id] === false" class="mp-chip mp-chip--away" data-role="chip-away">
            {{ t('liveShare.viewerAway') }}
          </span>
        </div>
        <div v-if="subline(v)" class="mp-sub">{{ subline(v) }}</div>
      </div>

      <div class="mp-actions">
        <!-- A remote-control request is the one thing that needs an answer, so
             it gets text buttons instead of icons: the host must not have to
             decode a glyph while somebody is waiting. -->
        <template v-if="controlRequests.has(v.id)">
          <span v-if="!controlSupported" class="mp-note" data-role="control-unsupported">
            {{ t('liveShare.controlUnsupported') }}
          </span>
          <template v-else>
            <button
              class="mp-action mp-action--wide"
              type="button"
              data-action="control-allow"
              @click="emit('allow-control', v.id)"
            >
              {{ t('liveShare.controlAllow') }}
            </button>
            <button
              class="mp-action mp-action--wide"
              type="button"
              data-action="control-deny"
              @click="emit('deny-control', v.id)"
            >
              {{ t('liveShare.controlDeny') }}
            </button>
          </template>
        </template>
        <button
          v-else-if="controlledBy === v.id"
          class="mp-action mp-action--wide"
          type="button"
          data-action="control-stop"
          @click="emit('revoke-control', v.id)"
        >
          {{ t('liveShare.controlStop') }}
        </button>

        <!-- Only offered while the room allows speech: with the switch off the
             viewer cannot speak anyway, so a mute button would be a control
             that changes nothing. -->
        <button
          v-if="talkEnabled && !v.muted"
          class="mp-action"
          type="button"
          :title="t('liveShare.participant.mute')"
          :aria-label="t('liveShare.participant.mute')"
          data-action="participant-mute"
          @click="emit('mute', v.id)"
        >
          <MicOff :size="14" />
        </button>
        <button
          v-if="talkEnabled && v.muted"
          class="mp-action"
          type="button"
          :title="t('liveShare.participant.unmute')"
          :aria-label="t('liveShare.participant.unmute')"
          data-action="participant-unmute"
          @click="emit('unmute', v.id)"
        >
          <Mic :size="14" />
        </button>
        <button
          v-if="sharingId === v.id"
          class="mp-action"
          type="button"
          :title="t('liveShare.stopScreenShare')"
          :aria-label="t('liveShare.stopScreenShare')"
          data-action="participant-stop-screen"
          @click="emit('stop-screen', v.id)"
        >
          <MonitorOff :size="14" />
        </button>
        <button
          class="mp-action mp-action--danger"
          type="button"
          :title="t('liveShare.participant.kick')"
          :aria-label="t('liveShare.participant.kick')"
          data-action="participant-kick"
          @click="emit('kick', v.id)"
        >
          <UserX :size="14" />
        </button>
      </div>
    </li>
  </ul>

  <!-- An empty list is a state, not a missing list: without this the section
       shows a count and nothing else, which reads as a rendering bug. -->
  <p v-if="viewers.length === 0" class="mp-empty" data-role="participants-empty">
    {{ t('liveShare.participant.empty') }}
  </p>
</template>

<script setup lang="ts">
/**
 * Who is in the meeting, and what the host can do about each of them.
 *
 * The list was previously a bare column of viewer ids with a single "静音"
 * button that only appeared while somebody was talking. It answered "how many
 * people are watching" but not "who are they" — no icon, no role, and no way to
 * remove a disruptive guest.
 *
 * Everything here is presentation: the rows are derived from the same
 * `viewers` array the transport owns, and every action is an emit the panel
 * forwards to `useLiveShare`. That keeps the protocol in one place and lets the
 * list be tested without a peer connection.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Mic, MicOff, MonitorOff, UserX } from 'lucide-vue-next'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import type { LiveViewerInfo } from '@/composables/useLiveShare'

const props = defineProps<{
  /** The host's own nickname (already resolved from the account). */
  hostName: string
  viewers: LiveViewerInfo[]
  /** viewerId → whether that page is currently visible. */
  presence: Record<string, boolean>
  talkEnabled: boolean
  /** Viewer id currently sharing a screen ('' = nobody). */
  sharingId: string
  controlRequests: Set<string>
  controlledBy: string
  controlSupported: boolean
}>()

const emit = defineEmits<{
  mute: [id: string]
  unmute: [id: string]
  kick: [id: string]
  'stop-screen': [id: string]
  'allow-control': [id: string]
  'deny-control': [id: string]
  'revoke-control': [id: string]
}>()

const { t } = useI18n()

/**
 * Display name per viewer, with a `#2` suffix for duplicates.
 *
 * Nicknames are not unique (the server does not reserve them): two people may
 * both be "Alex". The list is keyed by viewerId, so the suffix only affects
 * what is shown, never who is who.
 */
const labels = computed(() => {
  const counts = new Map<string, number>()
  const out: Record<string, string> = {}
  for (const v of props.viewers) {
    const base = v.name || v.id
    const n = (counts.get(base) ?? 0) + 1
    counts.set(base, n)
    out[v.id] = n > 1 ? `${base} #${n}` : base
  }
  return out
})

function label(v: LiveViewerInfo): string {
  return labels.value[v.id] || v.name || v.id
}

/**
 * The second line, only when it says something the chips cannot.
 *
 * "Controlling your screen" and "asked to control" are both states that need a
 * sentence: one is a capability currently handed out, the other is a request
 * the buttons next to it are about to answer.
 */
function subline(v: LiveViewerInfo): string {
  if (props.controlledBy === v.id) return t('liveShare.participant.controlling')
  if (props.controlRequests.has(v.id)) return t('liveShare.participant.wantsControl')
  return ''
}
</script>

<style scoped>
.mp {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.mp-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
}

.mp-row--host {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
}

/* A viewer whose page is hidden keeps its row (they are still in the meeting)
   but stops competing for attention with the people who are actually there. */
.mp-row--away .mp-avatar,
.mp-row--away .mp-name {
  opacity: var(--opacity-muted);
}

.mp-avatar {
  /* The shared avatar keeps its own shape (a rounded square); only the size is
     set here, through the variable the component exposes. */
  --sa-size: calc(28 * var(--dp));
}

/* The ring is the "who is talking" signal at a glance; the chip spells it out
   for anybody who cannot see the colour. */
.mp-avatar--talking {
  box-shadow: 0 0 0 2px var(--color-success);
}

.mp-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.mp-name-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.mp-name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mp-sub {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.mp-chip {
  flex: 0 0 auto;
  padding: 0 var(--space-3);
  border-radius: var(--radius-full);
  font-size: var(--font-size-2xs);
  line-height: calc(16 * var(--dp));
  color: var(--text-secondary);
  background: var(--bg-secondary);
}

/* Tinted from the chip's own text colour, not from a fixed step of the
   background: 36 themes do not agree on what "one step darker" looks like. */
.mp-chip--role {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
}

.mp-chip--live {
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 14%, transparent);
}

.mp-chip--muted {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}

.mp-chip--screen {
  color: var(--color-info);
  background: color-mix(in srgb, var(--color-info) 14%, transparent);
}

.mp-chip--away {
  color: var(--text-muted);
}

.mp-actions {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: var(--space-1);
  /* The dock can be 320px wide; a control request adds two text buttons on top
     of the icon row, so the block is allowed to wrap instead of pushing the
     nickname out of the panel. */
  max-width: 60%;
}

/* Inline row actions: the design guide allows a smaller local class for these
   (footer actions stay `.fbtn`). Icon-only so a narrow dock still fits four. */
.mp-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0 var(--space-2);
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast);
}

.mp-action:hover {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.mp-action--danger:hover {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}

.mp-action--wide {
  padding: 0 var(--space-4);
}

.mp-note {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  max-width: calc(120 * var(--dp));
}

.mp-empty {
  margin: 0;
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
</style>
