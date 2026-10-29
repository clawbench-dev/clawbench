<template>
  <div ref="rootRef" class="live-share-view">
    <div class="live-stage">
      <!-- The <video> is always mounted so `srcObject` survives a re-render.
           It is hidden while there is nothing to show, which keeps the error
           and waiting states from rendering a black rectangle. -->
      <video
        ref="videoRef"
        class="live-video"
        :class="{ 'live-video--hidden': state !== 'playing' }"
        autoplay
        playsinline
        muted
      />

      <div v-if="state !== 'playing'" class="live-overlay">
        <template v-if="state === 'connecting' || state === 'negotiating'">
          <LoadingIndicator />
          <!-- The header used to carry this. A spinner alone does not say
               whether the viewer is still dialling or already negotiating. -->
          <div class="live-overlay-desc">{{ statusText }}</div>
        </template>
        <template v-else>
          <VideoOff :size="40" />
          <div class="live-overlay-title">{{ overlayTitle }}</div>
          <div class="live-overlay-desc">{{ overlayDesc }}</div>
          <button v-if="state === 'failed' || state === 'ended'" class="live-retry" type="button" @click="retry">
            {{ t('share.live.retry') }}
          </button>
        </template>
      </div>
    </div>

    <!-- Floating control dock. Absolutely positioned so it overlays the video
         instead of taking height from it, and always mounted: fullscreen is
         useful even when the publisher never allowed speech. -->
    <div class="live-dock">
      <!-- Brand. Replaces the header's logo; the dot is the "live" signal the
           header's status chip used to provide, and the tooltip carries the
           share title (also mirrored into the tab title). -->
      <span
        class="live-dock-brand"
        :title="title || t('share.live.title')"
        :aria-label="title || t('share.live.title')"
      >
        <img class="live-dock-logo" src="/logo-64.png" alt="ClawBench" />
        <span v-if="state === 'playing'" class="live-dock-dot" />
      </span>

      <!-- Autoplay with audio is blocked without a user gesture, so playback
           starts muted and the viewer opts in here. Showing an error instead
           would leave them with a silent stream and no clue. -->
      <button
        v-if="needsSoundUnlock"
        class="live-dock-btn live-dock-btn--call"
        type="button"
        data-action="enable-sound"
        @click="unlockSound"
      >
        <VolumeX :size="16" />
        <span class="live-dock-label">{{ t('share.live.enableSound') }}</span>
      </button>

      <!-- One toggle: on = live, off = muted. No approval step — the
           publisher's room-level switch is the whole authorisation model, and
           it can be turned off at any moment (arrives as talk-revoked, which
           flips this back to off). -->
      <button
        v-if="talkEnabled"
        class="live-dock-btn"
        :class="{ 'live-dock-btn--live': talking }"
        type="button"
        role="switch"
        :aria-checked="talking"
        :aria-label="t('share.live.micToggle')"
        :title="talking ? t('share.live.micOnHint') : t('share.live.micOffHint')"
        data-action="mic-toggle"
        @click="toggleMic"
      >
        <Mic v-if="talking" :size="16" />
        <MicOff v-else :size="16" />
        <span class="live-dock-label">{{ talking ? t('share.live.micOn') : t('share.live.micOff') }}</span>
      </button>

      <button
        class="live-dock-btn"
        type="button"
        :aria-pressed="isFullscreen"
        :aria-label="isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen')"
        :title="isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen')"
        data-action="fullscreen"
        @click="toggleFullscreen"
      >
        <Minimize v-if="isFullscreen" :size="16" />
        <Maximize v-else :size="16" />
        <span class="live-dock-label">
          {{ isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen') }}
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Public live-share viewer (/share/{token} with kind=live).
 *
 * Renders the P2P video stream and, when the publisher allowed it, the
 * hold-to-talk control. All protocol work lives in LiveViewer so the state
 * machine is unit-testable without a DOM.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Maximize, Mic, MicOff, Minimize, VideoOff, VolumeX } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { LiveViewer, type LiveViewerState } from './liveViewer'
import { getShareToken } from './shareMode'
import { appLog } from '@/utils/appLog'

const TAG = 'LiveShareView'

const { t } = useI18n()

const rootRef = ref<HTMLElement | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
const state = ref<LiveViewerState>('connecting')
const title = ref('')
const failReason = ref('')
const talkEnabled = ref(false)
const talking = ref(false)
const needsSoundUnlock = ref(false)
/** Mirrors the browser's fullscreen state so the icon can follow Esc / F11. */
const isFullscreen = ref(false)

let viewer: LiveViewer | null = null

const statusText = computed(() => {
  switch (state.value) {
    case 'playing':
      return t('share.live.statusLive')
    case 'connecting':
      return t('share.live.statusConnecting')
    case 'negotiating':
      return t('share.live.statusNegotiating')
    case 'ended':
      return t('share.live.statusEnded')
    case 'full':
      return t('share.live.statusFull')
    case 'failed':
      return t('share.live.statusFailed')
    default:
      return ''
  }
})

const overlayTitle = computed(() => {
  switch (state.value) {
    case 'ended':
      return t('share.live.endedTitle')
    case 'full':
      return t('share.live.fullTitle')
    case 'failed':
      return t('share.live.failedTitle')
    default:
      return ''
  }
})

const overlayDesc = computed(() => {
  switch (state.value) {
    case 'ended':
      return t('share.live.endedDesc')
    case 'full':
      return t('share.live.fullDesc')
    case 'failed':
      return failReason.value === 'ice'
        ? t('share.live.failedIceDesc')
        : t('share.live.failedDesc')
    default:
      return ''
  }
})

function attachStream(stream: MediaStream): void {
  const el = videoRef.value
  if (!el) return
  el.srcObject = stream
  // Playback starts muted (autoplay policy). `needsSoundUnlock` only appears
  // when the stream actually carries audio.
  needsSoundUnlock.value = stream.getAudioTracks().length > 0
  void el.play().catch((e) => {
    appLog.w(TAG, 'autoplay blocked', e)
  })
}

function unlockSound(): void {
  const el = videoRef.value
  if (!el) return
  el.muted = false
  needsSoundUnlock.value = false
  void el.play().catch((e) => appLog.w(TAG, 'unmuted play failed', e))
}

function start(): void {
  const token = getShareToken()
  if (!token) {
    state.value = 'failed'
    failReason.value = 'token'
    return
  }
  state.value = 'connecting'
  failReason.value = ''
  talking.value = false

  viewer = new LiveViewer({
    token,
    callbacks: {
      onStream: attachStream,
      onTitle: (v) => {
        title.value = v
      },
      onStateChange: (s, detail) => {
        state.value = s
        if (detail?.reason) failReason.value = detail.reason
        if (s === 'ended' || s === 'full' || s === 'failed') {
          talking.value = false
        }
      },
      onTalkEnabled: (v) => {
        talkEnabled.value = v
      },
      onTalkRevoked: () => {
        // Muted by the publisher, or the room switch went off: the toggle must
        // reflect reality rather than staying "on" while sending nothing.
        talking.value = false
      },
    },
  })
  viewer.connect()
}

function retry(): void {
  viewer?.close()
  viewer = null
  start()
}

/**
 * Flip the microphone.
 *
 * Optimistic: the UI flips first so the button feels responsive, and flips back
 * if the microphone could not be opened (permission denied, no device) or the
 * server refused (room switch off, reported later as talk-disabled).
 */
function toggleMic(): void {
  if (!viewer) return
  if (talking.value) {
    talking.value = false
    void viewer.stopTalking()
    return
  }
  talking.value = true
  void viewer.startTalking().then((ok) => {
    if (!ok) talking.value = false
  })
}

function syncFullscreen(): void {
  // Read from the DOM rather than tracking a boolean: the user can leave
  // fullscreen with Esc or F11, which never reaches our click handler.
  isFullscreen.value = !!document.fullscreenElement
}

/**
 * Toggle fullscreen for the whole viewer.
 *
 * The root element, not the <video>: the dock has to stay reachable, otherwise
 * there is no way back out except Esc. iOS Safari has no element fullscreen and
 * only allows the <video> through its prefixed API, so that is the fallback.
 */
async function toggleFullscreen(): Promise<void> {
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen()
      return
    }
    const el = rootRef.value
    if (!el) return
    if (typeof el.requestFullscreen === 'function') {
      await el.requestFullscreen()
      return
    }
    const v = videoRef.value as
      | (HTMLVideoElement & { webkitEnterFullscreen?: () => void })
      | null
    v?.webkitEnterFullscreen?.()
  } catch (e) {
    // Denied (no user gesture, or the browser blocks it in an iframe). Leaving
    // the layout as-is is the right failure: nothing changed, nothing broken.
    appLog.w(TAG, 'fullscreen toggle failed', e)
  }
}

// A hidden tab can lose the media pipeline; re-attaching on return is cheaper
// than tearing the connection down and renegotiating.
watch(state, (s) => {
  if (s !== 'playing') talking.value = false
})

// The tab used to be the only "header" left once the bar was removed, so keep
// the share title there instead of a generic "ClawBench Share".
watch(title, (v) => {
  document.title = v ? `${v} · ClawBench` : 'ClawBench Share'
})

onMounted(() => {
  start()
  document.addEventListener('fullscreenchange', syncFullscreen)
})

onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', syncFullscreen)
  viewer?.close()
  viewer = null
  const el = videoRef.value
  if (el) el.srcObject = null
})
</script>

<style scoped>
.live-share-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  /* Positioning context for the floating dock. */
  position: relative;
  background: var(--bg-primary, #0d1117);
}

.live-stage {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.live-video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: #000;
}

.live-video--hidden {
  visibility: hidden;
}

.live-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4, 8px);
  padding: var(--space-7, 16px);
  text-align: center;
  color: var(--text-muted, #8b949e);
}

.live-overlay-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #e6edf3);
}

.live-overlay-desc {
  font-size: var(--font-size-sm, 13px);
  max-width: 32em;
  line-height: 1.6;
}

.live-retry {
  margin-top: var(--space-4, 8px);
  padding: 6px 14px;
  border-radius: var(--radius-md, 6px);
  border: 1px solid var(--border-color, #30363d);
  background: var(--bg-secondary, #161b22);
  color: var(--text-primary, #e6edf3);
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
}

/*
 * Floating dock, centred over the bottom of the video.
 *
 * Absolute rather than a flex row: a real bar would take height from the
 * stream, and the whole point of this page is the picture. The translucent
 * background keeps the controls legible over bright content without the blur
 * pass that some WebViews composite badly.
 */
.live-dock {
  position: absolute;
  left: 50%;
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px));
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 86%, transparent);
  border: 1px solid var(--border-color, #30363d);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35);
  z-index: 5;
}

/* Brand inside the dock. Sized to the dock's inner height so the pill keeps a
   single visual rhythm; the dot reuses the status colour. */
.live-dock-brand {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--space-2, 4px) 0 var(--space-3, 6px);
  flex-shrink: 0;
}

.live-dock-logo {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: block;
}

.live-dock-dot {
  position: absolute;
  right: 1px;
  bottom: 0;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-success, #3fb950);
  box-shadow: 0 0 0 2px var(--bg-secondary, #161b22);
}

.live-dock-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-5, 10px);
  border: none;
  border-radius: var(--radius-full, 999px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
  line-height: 1.6;
  white-space: nowrap;
  /* Tapping repeatedly must not select the label. */
  user-select: none;
  touch-action: manipulation;
  transition:
    background var(--duration-fast, 0.1s) ease,
    color var(--duration-fast, 0.1s) ease;
}

.live-dock-btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
}

/* Live microphone: tinted rather than a solid green fill, which would shout
   next to the status chip in the header. */
.live-dock-btn--live {
  background: color-mix(in srgb, var(--color-success, #3fb950) 18%, transparent);
  color: var(--color-success, #3fb950);
}

/* The sound prompt is the only control the viewer MUST act on, so it gets the
   accent tint rather than the neutral one. */
.live-dock-btn--call {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 22%, transparent);
  color: var(--accent-color, #4a90d9);
}

.live-dock-btn--call:hover {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 32%, transparent);
  color: var(--accent-color, #4a90d9);
}

.live-dock-btn--live:hover {
  background: color-mix(in srgb, var(--color-success, #3fb950) 26%, transparent);
  color: var(--color-success, #3fb950);
}

/* The label is the first thing to drop when space runs out: the icon and the
   tint still carry the meaning, and the title has the full sentence. */
@media (max-width: 520px) {
  .live-dock-label {
    display: none;
  }

  /* Brand inside the dock. Sized to the dock's inner height so the pill keeps a
   single visual rhythm; the dot reuses the status colour. */
.live-dock-brand {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--space-2, 4px) 0 var(--space-3, 6px);
  flex-shrink: 0;
}

.live-dock-logo {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: block;
}

.live-dock-dot {
  position: absolute;
  right: 1px;
  bottom: 0;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-success, #3fb950);
  box-shadow: 0 0 0 2px var(--bg-secondary, #161b22);
}

.live-dock-btn {
    padding: var(--space-2, 4px) var(--space-3, 6px);
  }
}
</style>
