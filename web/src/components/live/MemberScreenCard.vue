<template>
  <div
    class="capture-source member-screen-card"
    role="group"
    :aria-label="title"
    data-role="member-screen-card"
  >
    <!--
      A member's shared picture, as a card of its own above the source grid.

      Not a source tile you can pick: it is what is already going out. So it is
      a <div> (a <button> may not contain the three <button>s on its frame) and
      it carries no selected state — the accent ring belongs to the host's own
      choice in the grid below, and a second highlighted box would only fight
      it. No caption line either: the pill on the picture names the sharer.
    -->
    <span class="capture-source__thumb">
      <!-- The same track the fullscreen surface draws. A MediaStream can feed
           any number of <video> elements; this one is the panel's preview. -->
      <video
        ref="videoRef"
        class="member-screen-card__video"
        autoplay
        playsinline
        muted
        data-role="member-screen-video"
      />
      <!-- Who is on air, tagged on the picture the way a video tile names its
           speaker. The name truncates rather than widening the pill: a long
           nickname must not cover the picture or push the tag off the frame. -->
      <span class="member-screen-card__who" :title="name" data-role="member-screen-who">
        <SpeakerAvatar :name="name" />
        <span class="member-screen-card__who-name">{{ name }}</span>
      </span>

      <span class="member-screen-card__acts">
        <button
          v-if="!windowOpen"
          class="member-screen-card__act"
          type="button"
          :title="t('liveShare.viewScreen')"
          :aria-label="t('liveShare.viewScreen')"
          data-action="show-screen-fullscreen"
          @click="emit('fullscreen')"
        >
          <Maximize2 :size="12" />
        </button>
        <button
          v-if="!windowOpen && windowAvailable"
          class="member-screen-card__act"
          type="button"
          :title="t('liveShare.openInWindow')"
          :aria-label="t('liveShare.openInWindow')"
          data-action="open-screen-window"
          @click="emit('window')"
        >
          <ExternalLink :size="12" />
        </button>
        <!-- The one destructive action on the card, so it is the one solid
             colour: a dark chip that turns red on hover would read as another
             "open this" button. -->
        <button
          class="member-screen-card__act member-screen-card__act--stop"
          type="button"
          :title="t('liveShare.stopScreenShare')"
          :aria-label="t('liveShare.stopScreenShare')"
          data-action="stop-screen-share"
          @click="emit('stop')"
        >
          <Square :size="11" />
        </button>
      </span>
    </span>
  </div>
</template>

<script setup lang="ts">
/**
 * The member's shared screen, in the panel.
 *
 * The picture itself lives on MemberScreenSurface (fullscreen and the detached
 * window). This card is the panel's handle on it: which picture, whose, and the
 * three things a host can do about it.
 */
import { computed, nextTick, ref, watch } from 'vue'
import { ExternalLink, Maximize2, Square } from 'lucide-vue-next'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'
// Same tile frame / badge / name line as the source grid.
import '@/assets/capture-sources.css'

const props = withDefaults(
  defineProps<{
    /** Display name of the member sharing, already resolved (may be an id). */
    name: string
    stream: MediaStream | null
    /** This browser can detach the picture into its own window. */
    windowAvailable?: boolean
    /** The picture is already in that window. */
    windowOpen?: boolean
  }>(),
  { windowAvailable: false, windowOpen: false },
)

const emit = defineEmits<{ fullscreen: []; window: []; stop: [] }>()

const { t } = useI18n()

/**
 * The sentence the pill cannot say: "Alex's shared screen". Kept as the card's
 * accessible name — the visible label is only the nickname.
 */
const title = computed(() => t('liveShare.memberScreenTitle', { name: props.name }))

const videoRef = ref<HTMLVideoElement | null>(null)

/**
 * Attach the track, and attach it again when the element remounts.
 *
 * The card appears only while someone shares, so the element can mount with the
 * stream already in hand — watching the stream alone would miss that case.
 */
watch(
  [() => props.stream, videoRef],
  async ([stream]) => {
    await nextTick()
    const el = videoRef.value
    if (!el) return
    if (el.srcObject !== stream) el.srcObject = stream
    if (stream) {
      // jsdom's play() returns undefined (and says "not implemented"), so the
      // promise is optional here; a real browser always gives one back.
      const played = el.play() as Promise<void> | undefined
      if (played) void played.catch((e) => appLog.w('MemberScreenCard', 'preview play failed', e))
    }
  },
  { immediate: true },
)
</script>

<style scoped>
/* One row of the panel, not one cell of the grid: the live picture is the
   thing the host is looking at, so it gets the full width instead of a 120px
   thumbnail. Everything else is the shared tile styling. */
.member-screen-card {
  width: 100%;
  /* Not a choice: clicking the box does nothing, only the buttons act. */
  cursor: default;
}

/* Hovering the box does nothing — only the three buttons act — so it must not
   light up the accent border the way a selectable tile does. Two classes so
   this wins over `.capture-source:hover` whatever order the stylesheets land
   in. */
.member-screen-card.capture-source:hover {
  border-color: var(--border-color);
}

.member-screen-card__video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: #000;
}

/* The name tag: avatar + nickname on a dark chip, top-right, the same chip
   language as the buttons at the bottom. It never grows past the frame — a
   long nickname ellipsizes instead of spilling over the picture. */
.member-screen-card__who {
  position: absolute;
  top: calc(4 * var(--dp));
  right: calc(4 * var(--dp));
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  max-width: calc(100% - 8 * var(--dp));
  height: calc(18 * var(--dp));
  padding: 0 var(--space-4) 0 calc(2 * var(--dp));
  border-radius: var(--radius-full);
  background: rgba(0, 0, 0, 0.55);
  color: rgba(255, 255, 255, 0.92);
  font-size: var(--font-size-2xs);
  line-height: 1;
  pointer-events: none;
}

/* A round avatar, not the rounded square the transcript uses: the pill is the
   only place it sits in a circle. */
.member-screen-card__who :deep(.speaker-avatar) {
  --sa-size: calc(14 * var(--dp));
  border-radius: var(--radius-full);
}

.member-screen-card__who-name {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.member-screen-card__acts {
  position: absolute;
  right: calc(4 * var(--dp));
  bottom: calc(4 * var(--dp));
  display: flex;
  gap: var(--space-2);
}

.member-screen-card__act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(21 * var(--dp));
  height: calc(21 * var(--dp));
  padding: 0;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: var(--radius-xs);
  background: rgba(0, 0, 0, 0.55);
  color: rgba(255, 255, 255, 0.92);
  cursor: pointer;
  transition: background var(--duration-fast) ease, border-color var(--duration-fast) ease;
}

.member-screen-card__act:hover {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

.member-screen-card__act:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.member-screen-card__act--stop {
  background: var(--color-red);
  border-color: var(--color-red);
  color: #fff;
}

.member-screen-card__act--stop:hover {
  background: #c9302c;
  border-color: #c9302c;
  color: #fff;
}
</style>
