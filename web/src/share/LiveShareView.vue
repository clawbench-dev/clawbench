<template>
  <div class="live-share-view">
    <div class="live-topbar">
      <img class="live-logo" src="/logo-64.png" alt="ClawBench" />
      <span class="live-title" :title="title">{{ title || t('share.live.title') }}</span>
      <span class="live-status" :class="statusClass">{{ statusText }}</span>
      <span class="live-spacer" />
      <ShareThemeButton />
    </div>

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
        <LoadingIndicator v-if="state === 'connecting' || state === 'negotiating'" />
        <template v-else>
          <VideoOff :size="40" />
          <div class="live-overlay-title">{{ overlayTitle }}</div>
          <div class="live-overlay-desc">{{ overlayDesc }}</div>
          <button v-if="state === 'failed' || state === 'ended'" class="live-retry" type="button" @click="retry">
            {{ t('share.live.retry') }}
          </button>
        </template>
      </div>

      <!-- Autoplay with audio is blocked without a user gesture, so playback
           starts muted and the viewer opts into sound with one click. Showing an
           error instead would leave them with a silent stream and no clue. -->
      <button v-if="needsSoundUnlock" class="live-sound-btn" type="button" @click="unlockSound">
        <VolumeX :size="16" />
        {{ t('share.live.enableSound') }}
      </button>
    </div>

    <div v-if="talkEnabled" class="live-talkbar">
      <template v-if="canTalk">
        <button
          class="live-talk-btn"
          :class="{ 'live-talk-btn--active': talking }"
          type="button"
          @pointerdown.prevent="onTalkDown"
          @pointerup.prevent="onTalkUp"
          @pointerleave="onTalkUp"
          @pointercancel="onTalkUp"
        >
          <Mic :size="16" />
          {{ talking ? t('share.live.talking') : t('share.live.holdToTalk') }}
        </button>
        <span class="live-talk-hint">{{ t('share.live.talkHint') }}</span>
      </template>
      <template v-else>
        <button
          class="live-talk-btn"
          type="button"
          :disabled="talkPending"
          @click="requestTalk"
        >
          <Mic :size="16" />
          {{ talkPending ? t('share.live.talkPending') : t('share.live.requestTalk') }}
        </button>
        <span class="live-talk-hint">{{ t('share.live.talkNeedsApproval') }}</span>
      </template>
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
import { Mic, VideoOff, VolumeX } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ShareThemeButton from './ShareThemeButton.vue'
import { LiveViewer, type LiveViewerState } from './liveViewer'
import { getShareToken } from './shareMode'
import { appLog } from '@/utils/appLog'

const TAG = 'LiveShareView'

const { t } = useI18n()

const videoRef = ref<HTMLVideoElement | null>(null)
const state = ref<LiveViewerState>('connecting')
const title = ref('')
const failReason = ref('')
const talkEnabled = ref(false)
const canTalk = ref(false)
const talking = ref(false)
const talkPending = ref(false)
const needsSoundUnlock = ref(false)

let viewer: LiveViewer | null = null

const statusClass = computed(() => ({
  'live-status--live': state.value === 'playing',
  'live-status--warn': state.value === 'connecting' || state.value === 'negotiating',
  'live-status--bad': state.value === 'failed' || state.value === 'ended' || state.value === 'full',
}))

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
  talkPending.value = false

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
          canTalk.value = false
          talking.value = false
          talkPending.value = false
        }
      },
      onTalkEnabled: (v) => {
        talkEnabled.value = v
      },
      onTalkGranted: () => {
        canTalk.value = true
        talkPending.value = false
      },
      onTalkRevoked: () => {
        canTalk.value = false
        talking.value = false
        talkPending.value = false
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

function requestTalk(): void {
  if (!viewer) return
  talkPending.value = true
  viewer.requestTalk()
}

function onTalkDown(): void {
  if (!viewer || !canTalk.value) return
  talking.value = true
  void viewer.startTalking().then((ok) => {
    if (!ok) talking.value = false
  })
}

function onTalkUp(): void {
  if (!viewer || !talking.value) return
  talking.value = false
  void viewer.stopTalking()
}

// A hidden tab can lose the media pipeline; re-attaching on return is cheaper
// than tearing the connection down and renegotiating.
watch(state, (s) => {
  if (s !== 'playing') talking.value = false
})

onMounted(start)

onBeforeUnmount(() => {
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
  background: var(--bg-primary, #0d1117);
}

.live-topbar {
  display: flex;
  align-items: center;
  gap: var(--space-4, 8px);
  height: 44px;
  padding: 0 var(--space-6, 12px);
  border-bottom: 1px solid var(--border-color, #30363d);
  flex: 0 0 auto;
}

.live-logo {
  width: 20px;
  height: 20px;
}

.live-title {
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--text-primary, #e6edf3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 50%;
}

.live-status {
  font-size: var(--font-size-xs, 11px);
  padding: 2px 8px;
  border-radius: var(--radius-full, 999px);
  border: 1px solid var(--border-color, #30363d);
  color: var(--text-muted, #8b949e);
}

.live-status--live {
  color: var(--color-success, #3fb950);
  border-color: currentColor;
}

.live-status--warn {
  color: var(--color-yellow, #d29922);
  border-color: currentColor;
}

.live-status--bad {
  color: var(--color-red, #f85149);
  border-color: currentColor;
}

.live-spacer {
  flex: 1;
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

.live-sound-btn {
  position: absolute;
  top: var(--space-6, 12px);
  left: 50%;
  transform: translateX(-50%);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: var(--radius-full, 999px);
  border: 1px solid var(--border-color, #30363d);
  background: rgba(0, 0, 0, 0.7);
  color: #fff;
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
}

.live-talkbar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-4, 8px);
  padding: var(--space-6, 12px);
  border-top: 1px solid var(--border-color, #30363d);
  flex: 0 0 auto;
}

.live-talk-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 18px;
  border-radius: var(--radius-full, 999px);
  border: 1px solid var(--border-color, #30363d);
  background: var(--bg-secondary, #161b22);
  color: var(--text-primary, #e6edf3);
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
  /* Hold-to-talk is a press gesture; selecting text mid-press would break it. */
  user-select: none;
  touch-action: none;
}

.live-talk-btn:disabled {
  opacity: 0.6;
  cursor: default;
}

.live-talk-btn--active {
  background: var(--color-success, #3fb950);
  border-color: var(--color-success, #3fb950);
  color: #000;
}

.live-talk-hint {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #8b949e);
}
</style>
