<template>
  <!-- The nickname gate runs BEFORE any connection: an empty name must not take
       an audience slot. It also carries the meeting title the server already
       resolved from the share link. -->
  <LiveJoinGate v-if="!joined" :title="title" @join="onJoin" />
  <div v-else ref="rootRef" class="live-share-view">
    <div
      ref="stageRef"
      class="live-stage"
      :class="{ 'live-stage--zoomed': zoom.scale > 1 }"
      @wheel.prevent="onStageWheel"
      @pointerdown="onStageDown"
      @pointermove="onStageMove"
      @pointerup="onStageUp"
      @pointercancel="onStageUp"
      @dblclick="resetZoom"
    >
      <!-- Zoom/pan transform wraps the picture AND the ink layer, so an
           annotation stays glued to the pixel it was drawn on. -->
      <div class="live-zoom" :style="zoomStyle">
        <!-- The <video> is always mounted so `srcObject` survives a re-render.
             It is hidden while there is nothing to show, which keeps the error
             and waiting states from rendering a black rectangle. -->
        <video
          ref="videoRef"
          class="live-video"
          :class="{ 'live-video--hidden': state !== 'playing' || viewMode === 'member' }"
          autoplay
          playsinline
          muted
          @loadedmetadata="syncInkRect"
        />

        <!-- A member's screen, forwarded by the host. Its own element rather than
             swapping srcObject: switching back to the host must not depend on
             re-attaching a stream that is still live. -->
        <video
          v-show="viewMode === 'member'"
          ref="memberVideoRef"
          class="live-video"
          autoplay
          playsinline
          muted
          @loadedmetadata="syncInkRect"
        />

        <!-- Ink covers exactly the picture, not the letterbox: a stroke drawn
             at 0.5/0.5 must land in the middle of the CONTENT on every screen. -->
        <div v-if="videoEnabled" class="live-ink-wrap" :style="inkWrapStyle">
          <LiveInkLayer :strokes="board.strokes.value" :laser="board.laser.value" />
        </div>
      </div>

      <!-- Voice meeting: there is no picture to show. A placeholder says what
           this is, instead of leaving a black rectangle that reads as a broken
           stream. -->
      <div v-if="!videoEnabled" class="live-voice-stage" data-role="voice-stage">
        <Mic :size="34" />
        <div class="live-voice-stage__title">{{ t('share.live.voiceTitle') }}</div>
        <div class="live-voice-stage__desc">{{ t('share.live.voiceDesc') }}</div>
      </div>

      <!-- Connection health. Only shown once there is a reading, and it never
           covers the picture: a corner chip is enough to answer "is it me or
           them" when the video stutters. -->
      <div
        v-if="stats && state === 'playing'"
        class="live-quality"
        :class="`live-quality--${quality.level}`"
        :title="quality.detail"
        data-role="quality"
      >
        <span class="live-quality__dot" />
        {{ quality.label }}
      </div>

      <!-- Remote control state. Loud on purpose: while it is on, this viewer's
           pointer is moving someone else's mouse. -->
      <div v-if="controlState !== 'none'" class="live-screen-banner live-control-banner" data-role="control-banner">
        <span>{{ controlState === 'granted' ? t('share.live.controlling') : t('share.live.controlPending') }}</span>
        <button
          class="live-screen-banner-btn"
          type="button"
          data-action="control-release"
          @click="releaseControl"
        >
          {{ controlState === 'granted' ? t('share.live.controlRelease') : t('share.live.controlCancel') }}
        </button>
      </div>

      <!-- Who is presenting. Without it a viewer cannot tell whose screen this
           is, and "why did the picture change" becomes a support question. -->
      <div v-if="screenBanner" class="live-screen-banner" data-role="screen-banner">
        <span>{{ screenBanner }}</span>
        <button
          v-if="memberStream"
          class="live-screen-banner-btn"
          type="button"
          data-action="toggle-view"
          @click="toggleView"
        >
          {{ viewMode === 'member' ? t('share.live.backToHost') : t('share.live.viewShared') }}
        </button>
      </div>

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

    <!-- AI 听记 panel. Collapsed by default: it overlays the picture, and most
         viewers are here to watch, not to read. Opened from the dock; polling
         only runs while it is open. -->
    <div v-if="notesOpen" class="live-notes-panel" data-panel="notes">
      <div class="live-notes-head">
        <span class="live-notes-title">{{ t('share.live.notesTitle') }}</span>
        <button
          class="live-notes-close"
          type="button"
          :aria-label="t('common.close')"
          data-action="notes-close"
          @click="closeNotes"
        >
          <X :size="16" />
        </button>
      </div>
      <div ref="notesBodyRef" class="live-notes-body">
        <div v-if="noteBlocks.length === 0" class="live-notes-hint">
          {{ t('share.live.notesWaiting') }}
        </div>
        <div v-for="b in noteBlocks" :key="b.key" class="live-note-line">
          <span class="live-note-speaker">{{ b.speaker }}</span>
          <span class="live-note-text">{{ b.text }}</span>
        </div>
      </div>
    </div>

    <!-- Browser-only hint: the link works, but the desktop client is what
         unlocks capture and remote control. Shown only when the host actually
         allows screen sharing, so it is not noise on every share. -->
    <div
      v-if="!clientMode && screenEnabled && !canShareScreen && !clientHintClosed"
      class="live-client-hint"
      data-role="client-hint"
    >
      <span>{{ t('share.live.clientHint') }}</span>
      <button
        class="live-client-hint__close"
        type="button"
        :aria-label="t('common.close')"
        @click="clientHintClosed = true"
      >
        ×
      </button>
    </div>

    <!-- Client-mode source picker. Electron refuses an undeclared capture
         source, so the choice has to happen before getDisplayMedia. -->
    <div
      v-if="pickingSource"
      class="live-picker"
      data-role="source-picker"
      @click.self="pickingSource = false"
    >
      <div class="live-picker__card">
        <div class="live-picker__title">{{ t('share.live.pickSource') }}</div>
        <div class="live-picker__list">
          <button
            v-for="src in clientSources"
            :key="src.id"
            class="live-picker__item"
            type="button"
            @click="startClientShare(src.id)"
          >
            {{ src.name }}
          </button>
        </div>
        <button class="live-picker__cancel" type="button" @click="pickingSource = false">
          {{ t('common.cancel') }}
        </button>
      </div>
    </div>

    <!-- Chat. Same overlay slot as the notes panel; only one is open at a time
         so they never fight over the picture. -->
    <div v-if="chatOpen" class="live-chat-panel" data-panel="chat">
      <div class="live-notes-head">
        <span class="live-notes-title">{{ t('share.live.chatTitle') }}</span>
        <button
          class="live-notes-close"
          type="button"
          :aria-label="t('common.close')"
          data-action="chat-close"
          @click="chatOpen = false"
        >
          <X :size="16" />
        </button>
      </div>
      <LiveChatPanel :messages="chatMessages" @send="sendChat" />
    </div>

    <!-- Who is speaking. The publisher measures every microphone (it is the
         only place all of them meet) and broadcasts a level; without this chip
         a viewer hears a voice with no idea whose it is. -->
    <div v-if="speaking" class="live-speaker" data-role="speaker">
      <span class="live-speaker__dot" />
      {{ t('share.live.speaking', { name: speakerLabel }) }}
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
        :disabled="!canTalk"
        :title="
          !canTalk
            ? t('share.live.micNeedsHttps')
            : talking
              ? t('share.live.micOnHint')
              : t('share.live.micOffHint')
        "
        data-action="mic-toggle"
        @click="toggleMic"
      >
        <Mic v-if="talking" :size="16" />
        <MicOff v-else :size="16" />
        <span class="live-dock-label">{{ talking ? t('share.live.micOn') : t('share.live.micOff') }}</span>
      </button>

      <!-- Shown whenever the host allows it, even when THIS browser cannot
           capture: hiding it entirely leaves a member wondering why the button
           their colleague sees is missing. Disabled + a title explains it. -->
      <button
        v-if="controlState === 'none' && videoEnabled"
        class="live-dock-btn"
        type="button"
        :aria-label="t('share.live.requestControl')"
        :title="t('share.live.requestControlHint')"
        data-action="request-control"
        @click="requestControl"
      >
        <MousePointer2 :size="16" />
        <span class="live-dock-label">{{ t('share.live.requestControl') }}</span>
      </button>

      <button
        v-if="screenEnabled"
        class="live-dock-btn"
        :class="{ 'live-dock-btn--live': sharingScreen }"
        type="button"
        role="switch"
        :aria-checked="sharingScreen"
        :disabled="!canShareScreen"
        :aria-label="t('share.live.shareScreen')"
        :title="
          !canShareScreen
            ? t('share.live.screenNeedsHttps')
            : sharingScreen
              ? t('share.live.stopSharingHint')
              : t('share.live.shareScreenHint')
        "
        data-action="screen-share"
        @click="toggleScreenShare"
      >
        <ScreenShare v-if="!sharingScreen" :size="16" />
        <ScreenShareOff v-else :size="16" />
        <span class="live-dock-label">
          {{ sharingScreen ? t('share.live.stopSharing') : t('share.live.shareScreen') }}
        </span>
      </button>

      <button
        class="live-dock-btn"
        :class="{ 'live-dock-btn--live': chatOpen }"
        type="button"
        :aria-pressed="chatOpen"
        :aria-label="t('share.live.chatTitle')"
        :title="t('share.live.chatTitle')"
        data-action="chat"
        @click="toggleChat"
      >
        <MessageSquare :size="16" />
        <span class="live-dock-label">{{ t('share.live.chat') }}</span>
        <span v-if="unread > 0" class="live-dock-badge">{{ unread > 9 ? '9+' : unread }}</span>
      </button>

      <button
        v-if="notesEnabled"
        class="live-dock-btn"
        :class="{ 'live-dock-btn--live': notesOpen }"
        type="button"
        :aria-pressed="notesOpen"
        :aria-label="t('share.live.notesTitle')"
        :title="t('share.live.notesTitle')"
        data-action="notes"
        @click="toggleNotes"
      >
        <FileText :size="16" />
        <span class="live-dock-label">{{ t('share.live.notes') }}</span>
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
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  FileText,
  Maximize,
  MessageSquare,
  Mic,
  MousePointer2,
  MicOff,
  Minimize,
  ScreenShare,
  ScreenShareOff,
  VideoOff,
  VolumeX,
  X,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { LiveViewer, type LiveStats, type LiveViewerState } from './liveViewer'
import LiveJoinGate from './LiveJoinGate.vue'
import LiveInkLayer from './LiveInkLayer.vue'
import LiveChatPanel, { type ChatMessage } from './LiveChatPanel.vue'
import { useInkBoard } from './useInkBoard'
import { contentRectOf, toNormalized, type ContentRect, type ControlMessage } from './liveControl'
import { ShareNotesPoller } from './liveNotesClient'
import { getShareToken } from './shareMode'
import { groupNoteSegments, type LiveNoteSegment } from '@/composables/liveNotes'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'

const TAG = 'LiveShareView'

const { t } = useI18n()

const rootRef = ref<HTMLElement | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
const state = ref<LiveViewerState>('connecting')
/** The nickname gate has been passed; only then is a socket opened. */
const joined = ref(false)
const nick = ref('')
/** Whether the publisher sends a picture (false = voice meeting). */
const videoEnabled = ref(true)
const title = ref('')
const failReason = ref('')
const talkEnabled = ref(false)
const talking = ref(false)
const needsSoundUnlock = ref(false)
/** Mirrors the browser's fullscreen state so the icon can follow Esc / F11. */
const isFullscreen = ref(false)

// ── AI 听记 (viewer side) ────────────────────────────────────────────────────
/** Whether the publisher enabled 听记. The panel is only offered when it did. */
const notesEnabled = ref(false)
/** Collapsed by default: the panel overlays the picture. */
const notesOpen = ref(false)
const notesSegments = ref<LiveNoteSegment[]>([])

// ── Control channel: ink, chat, quality, presence ───────────────────────────
const board = useInkBoard()
const stageRef = ref<HTMLElement | null>(null)
const inkRect = ref<ContentRect>({ left: 0, top: 0, width: 0, height: 0 })
const chatMessages = ref<ChatMessage[]>([])
const chatOpen = ref(false)
const unread = ref(0)
const stats = ref<LiveStats | null>(null)
const zoom = ref({ scale: 1, x: 0, y: 0 })
/** Remote control: none → requested → granted. */
const controlState = ref<'none' | 'requested' | 'granted'>('none')
/**
 * Who is speaking right now, from the publisher's level broadcast.
 *
 * The viewer only receives the mix, so it cannot measure this itself — the
 * level has to come over the control channel. A frame is followed by a
 * timeout: if the publisher stops sending (track muted, tab hidden) the chip
 * must not stay lit forever.
 */
const speaking = ref('')
let speakerTimer: ReturnType<typeof setTimeout> | null = null

function noteSpeaker(id: string, level: number): void {
  if (level < 0.05) {
    if (speaking.value === id) speaking.value = ''
    return
  }
  speaking.value = id
  if (speakerTimer) clearTimeout(speakerTimer)
  speakerTimer = setTimeout(() => {
    speaking.value = ''
  }, 900)
}

const speakerLabel = computed(() =>
  speaking.value === 'host'
    ? t('share.live.host')
    : t('share.live.viewerName', { id: speaking.value }),
)

const inkWrapStyle = computed(() => ({
  left: `${inkRect.value.left}px`,
  top: `${inkRect.value.top}px`,
  width: `${inkRect.value.width}px`,
  height: `${inkRect.value.height}px`,
}))

const zoomStyle = computed(() => ({
  transform: `translate(${zoom.value.x}px, ${zoom.value.y}px) scale(${zoom.value.scale})`,
}))

/**
 * The picture's real rectangle inside the stage (letterbox excluded).
 *
 * The ink canvas is positioned to THIS rect, not to the stage: otherwise a
 * stroke drawn at the top of the picture lands in the black bar on a screen
 * whose aspect ratio differs.
 */
function syncInkRect(): void {
  const stage = stageRef.value
  const video = viewMode.value === 'member' ? memberVideoRef.value : videoRef.value
  if (!stage) return
  inkRect.value = contentRectOf(
    {
      clientWidth: stage.clientWidth,
      clientHeight: stage.clientHeight,
      videoWidth: video?.videoWidth ?? 0,
      videoHeight: video?.videoHeight ?? 0,
    },
    { left: 0, top: 0 },
  )
}

/**
 * Give control back.
 *
 * The local state alone is not enough: the publisher keeps its grant until it
 * hears otherwise, and its gate is what actually protects the injector.
 */
function releaseControl(): void {
  if (controlState.value === 'none') return
  // Release a held button BEFORE giving control back: once `ctrl-release` has
  // been sent the publisher no longer accepts our events, so a late `up` would
  // be dropped and the host's mouse button would stay down.
  endControlPress()
  controlState.value = 'none'
  viewer?.sendControl({ t: 'ctrl-release' })
}

function requestControl(): void {
  if (!viewer?.sendControl({ t: 'ctrl-req' })) return
  controlState.value = 'requested'
}

/** Client pixels → normalised picture coordinates (null when there is no stage). */
function normalizedPoint(ev: PointerEvent | WheelEvent): { x: number; y: number } | null {
  const stage = stageRef.value
  if (!stage) return null
  const rect = contentRectOf(
    {
      clientWidth: stage.clientWidth,
      clientHeight: stage.clientHeight,
      videoWidth: videoRef.value?.videoWidth ?? 0,
      videoHeight: videoRef.value?.videoHeight ?? 0,
    },
    { left: 0, top: 0 },
  )
  return toNormalized(ev.clientX, ev.clientY, rect)
}

/**
 * Forwards a pointer event while we hold control.
 *
 * Coordinates are normalised to the picture, exactly like ink: the host's
 * screen has a different size, so a client pixel would land somewhere else.
 */
function sendControlEvent(ev: PointerEvent | WheelEvent, kind: 'move' | 'down' | 'up' | 'wheel'): void {
  if (controlState.value !== 'granted') return
  const p = normalizedPoint(ev)
  if (!p) return
  viewer?.sendControl({
    t: 'ctrl-ev',
    kind,
    x: p.x,
    y: p.y,
    button: kind === 'wheel' ? undefined : (ev as PointerEvent).button,
    delta: kind === 'wheel' ? (ev as WheelEvent).deltaY : undefined,
  })
}

/** Browser key names → the names the helper understands. */
const KEY_MAP: Record<string, string> = {
  Enter: 'enter',
  Backspace: 'backspace',
  Tab: 'tab',
  Escape: 'esc',
  Delete: 'delete',
  Home: 'home',
  End: 'end',
  PageUp: 'pageup',
  PageDown: 'pagedown',
  ArrowLeft: 'left',
  ArrowRight: 'right',
  ArrowUp: 'up',
  ArrowDown: 'down',
  ' ': 'space',
}

/**
 * Whether the event is going into something the viewer is typing in.
 *
 * While control is granted every keystroke is forwarded to the host, so a
 * viewer who opens the chat box would type into whatever window the host has
 * focused (a password field, a terminal) and `preventDefault` would keep the
 * character out of the local input as well.
 */
function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false
  if ((target as HTMLElement).isContentEditable) return true
  return target.closest('input, textarea, [contenteditable]') !== null
}

function onControlKey(ev: KeyboardEvent): void {
  if (controlState.value !== 'granted') return
  // Local typing is not remote control: leave the key alone (no forward, no
  // preventDefault) so the chat box keeps working.
  if (isEditableTarget(ev.target)) return
  // Escape is the local escape hatch: it must never be forwarded, or a viewer
  // could not stop controlling without reaching for the mouse.
  if (ev.key === 'Escape' && !ev.ctrlKey && !ev.altKey) {
    releaseControl()
    ev.preventDefault()
    return
  }
  // While an IME is composing (pinyin → 汉字), every keydown is a latin letter
  // of the composition. Forwarding those would type "nihao" instead of "你好";
  // the composed text is sent on compositionend instead.
  if (ev.isComposing) return

  const mods: string[] = []
  if (ev.ctrlKey) mods.push('ctrl')
  if (ev.shiftKey) mods.push('shift')
  if (ev.altKey) mods.push('alt')
  if (ev.metaKey) mods.push('meta')

  // A single printable character with no modifier is typed literally: that is
  // what makes non-ASCII input (Chinese, emoji) work without a key-code table.
  // A modified one is a shortcut (Ctrl+C, Alt+F), and sending it as `text`
  // would deliver the bare character while silently dropping the modifier.
  if (ev.key.length === 1 && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
    viewer?.sendControl({ t: 'ctrl-ev', kind: 'text', text: ev.key })
    ev.preventDefault()
    return
  }

  const named = KEY_MAP[ev.key] ?? (/^F\d{1,2}$/.test(ev.key) ? ev.key.toLowerCase() : '')
  if (!named) {
    // Shortcut on a printable key: the injector takes it as a key press with
    // modifiers, which is the only way the host sees the combination.
    if (ev.key.length === 1 && /^[a-z0-9]$/i.test(ev.key) && mods.length > 0) {
      viewer?.sendControl({ t: 'ctrl-ev', kind: 'key', key: ev.key.toLowerCase(), modifiers: mods })
      ev.preventDefault()
    }
    return
  }
  viewer?.sendControl({ t: 'ctrl-ev', kind: 'key', key: named, modifiers: mods })
  ev.preventDefault()
}

/** IME result: send the composed text as one literal string. */
function onControlComposition(ev: CompositionEvent): void {
  if (controlState.value !== 'granted') return
  const text = ev.data
  if (!text) return
  viewer?.sendControl({ t: 'ctrl-ev', kind: 'text', text })
}

/**
 * The press currently open in control mode, with its last picture position.
 *
 * The host holds the button down until it hears `up`, so this is what lets a
 * release that arrives from somewhere other than the stage still produce a
 * well-formed frame — and what keeps one release from sending two.
 */
let controlPress: { x: number; y: number; button: number } | null = null

/**
 * Close an open press.
 *
 * Every way out of a press has to run through here: the pointer released
 * outside the picture, control given back, the grant revoked. Leaving it open
 * leaves the host's mouse button down until the viewer clicks again.
 */
function endControlPress(): void {
  const press = controlPress
  if (!press) return
  controlPress = null
  viewer?.sendControl({ t: 'ctrl-ev', kind: 'up', x: press.x, y: press.y, button: press.button })
}

/**
 * A press released anywhere on the page.
 *
 * `pointerup` is targeted at whatever is under the cursor, so a drag that
 * leaves the picture never reaches the stage and the host would keep its mouse
 * button down forever.
 */
function onWindowPointerUp(ev: PointerEvent): void {
  if (controlState.value !== 'granted') return
  const press = controlPress
  if (!press) return
  controlPress = null
  const p = normalizedPoint(ev) ?? press
  viewer?.sendControl({ t: 'ctrl-ev', kind: 'up', x: p.x, y: p.y, button: press.button })
}

function onStageDown(ev: PointerEvent): void {
  if (controlState.value === 'granted') {
    const p = normalizedPoint(ev)
    if (p) controlPress = { ...p, button: ev.button }
    // Keep the pointer events coming once the cursor leaves the picture, and
    // make sure the matching `up` is not lost: the window listener below is the
    // fallback for a capture that never happened or was lost.
    try {
      stageRef.value?.setPointerCapture?.(ev.pointerId)
    } catch (e) {
      appLog.d(TAG, 'setPointerCapture failed', e)
    }
    sendControlEvent(ev, 'down')
    return
  }
  onPanStart(ev)
}

function onStageMove(ev: PointerEvent): void {
  if (controlState.value === 'granted') {
    sendControlEvent(ev, 'move')
    return
  }
  onPanMove(ev)
}

function onStageUp(ev: PointerEvent): void {
  if (controlState.value === 'granted') {
    // Only close a press we actually opened; the window fallback uses the same
    // flag so one release never produces two `up` frames.
    if (!controlPress) return
    controlPress = null
    sendControlEvent(ev, 'up')
    return
  }
  onPanEnd()
}

function onStageWheel(ev: WheelEvent): void {
  if (controlState.value === 'granted') {
    sendControlEvent(ev, 'wheel')
    return
  }
  onWheel(ev)
}

function toggleChat(): void {
  chatOpen.value = !chatOpen.value
  if (chatOpen.value) {
    unread.value = 0
    closeNotes()
  }
}

function sendChat(text: string): void {
  const msg: ControlMessage = {
    t: 'chat',
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    from: viewer?.viewerId ?? '',
    name: t('share.live.viewerName', { id: viewer?.viewerId ?? '?' }),
    text,
    at: Date.now(),
  }
  if (!viewer?.sendControl(msg)) return
  chatMessages.value = [...chatMessages.value, { ...msg, self: true }]
}

function applyControl(msg: ControlMessage): void {
  if (msg.t === 'chat') {
    chatMessages.value = [
      ...chatMessages.value,
      {
        id: msg.id,
        name: msg.name || t('share.live.viewerName', { id: msg.from }),
        text: msg.text,
        at: msg.at,
        self: msg.from === viewer?.viewerId,
      },
    ]
    if (!chatOpen.value) unread.value += 1
    return
  }
  if (msg.t === 'ctrl-grant') {
    // The grant is addressed to ONE viewer. Without this check a broadcast
    // grant would put every viewer into "controlling" — their keydown handler
    // would then swallow typing in the chat box.
    if (msg.viewerId && msg.viewerId !== viewer?.viewerId) return
    controlState.value = msg.granted ? 'granted' : 'none'
    if (msg.granted) resetZoom()
    return
  }
  if (msg.t === 'talk') {
    noteSpeaker(msg.viewerId, msg.level)
    return
  }
  // Ink and laser are the only other frames a viewer receives today.
  board.apply(msg)
}

const quality = computed(() => {
  const st = stats.value
  if (!st) return { level: 'good', label: '', detail: '' }
  const loss = st.lossPct ?? 0
  const rtt = st.rttMs ?? 0
  const level = loss >= 8 || rtt >= 400 ? 'poor' : loss >= 2 || rtt >= 200 ? 'fair' : 'good'
  const label =
    level === 'good'
      ? t('share.live.qualityGood')
      : level === 'fair'
        ? t('share.live.qualityFair')
        : t('share.live.qualityPoor')
  const detail = [
    st.rttMs !== null ? `RTT ${st.rttMs}ms` : '',
    st.lossPct !== null ? `${st.lossPct}% loss` : '',
    st.bitrateKbps !== null ? `${st.bitrateKbps}kbps` : '',
    st.jitterMs !== null ? `jitter ${st.jitterMs}ms` : '',
  ]
    .filter(Boolean)
    .join(' · ')
  return { level, label, detail }
})

// ── Zoom / pan ──────────────────────────────────────────────────────────────

const ZOOM_MIN = 1
const ZOOM_MAX = 6

function onWheel(ev: WheelEvent): void {
  if (state.value !== 'playing') return
  const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, zoom.value.scale * Math.exp(-ev.deltaY * 0.0015)))
  if (next === zoom.value.scale) return
  zoom.value = next <= ZOOM_MIN ? { scale: 1, x: 0, y: 0 } : { ...zoom.value, scale: next }
}

let panFrom: { x: number; y: number; ox: number; oy: number } | null = null

function onPanStart(ev: PointerEvent): void {
  if (zoom.value.scale <= 1) return
  panFrom = { x: ev.clientX, y: ev.clientY, ox: zoom.value.x, oy: zoom.value.y }
  ;(ev.currentTarget as HTMLElement).setPointerCapture?.(ev.pointerId)
}

function onPanMove(ev: PointerEvent): void {
  if (!panFrom) return
  zoom.value = {
    ...zoom.value,
    x: panFrom.ox + (ev.clientX - panFrom.x),
    y: panFrom.oy + (ev.clientY - panFrom.y),
  }
}

function onPanEnd(): void {
  panFrom = null
}

function resetZoom(): void {
  zoom.value = { scale: 1, x: 0, y: 0 }
}

// ── Member screen share (viewer side) ───────────────────────────────────────
const memberVideoRef = ref<HTMLVideoElement | null>(null)
/** The screen another viewer is sharing, forwarded by the host. */
const memberStream = ref<MediaStream | null>(null)
/** Room-level switch for audience screen sharing. */
const screenEnabled = ref(false)
/** Viewer id currently sharing ('' = nobody). */
const screenSharer = ref('')
const sharingScreen = ref(false)
const viewMode = ref<'host' | 'member'>('host')
const screenError = ref('')
/** True when this page runs inside the desktop shell. */
const clientMode = computed(() => Boolean(getNative()?.isNativeApp?.()))
/** Client mode picks a source through the native picker, not the browser's. */
const pickingSource = ref(false)
const clientSources = ref<Array<{ id: string; name: string; kind: string }>>([])
const clientHintClosed = ref(false)

/**
 * Screen capture needs a secure context (HTTPS or localhost): on a plain
 * http://LAN address `navigator.mediaDevices` does not exist at all, so the
 * button would silently do nothing. Hiding it is the honest option.
 */
const canShareScreen = computed(
  () =>
    screenEnabled.value &&
    typeof window !== 'undefined' &&
    // The desktop shell promotes its server origin to a secure context at
    // startup, so `isSecureContext` is true there too — but the native picker
    // is what makes capture work over plain HTTP without that switch.
    (clientMode.value || window.isSecureContext) &&
    !!navigator.mediaDevices?.getDisplayMedia,
)

/**
 * Talking needs `getUserMedia` — the same secure-context gate as screen
 * capture, on a different API. Without this check the switch looks usable on a
 * plain http://LAN page and does nothing when pressed: `startTalking()` cannot
 * reach `mediaDevices`, returns false, and the switch silently flips back.
 *
 * Gated on the method rather than the room switch: `talkEnabled` says the host
 * allows speech, this says this browser can produce any.
 */
const canTalk = computed(
  () =>
    typeof window !== 'undefined' &&
    (clientMode.value || window.isSecureContext) &&
    !!navigator.mediaDevices?.getUserMedia,
)

const screenBanner = computed(() => {
  if (screenError.value) return screenError.value
  if (sharingScreen.value) return t('share.live.youAreSharing')
  if (screenSharer.value) return t('share.live.viewingSharer', { id: screenSharer.value })
  return ''
})

function toggleView(): void {
  viewMode.value = viewMode.value === 'member' ? 'host' : 'member'
}

function attachMember(stream: MediaStream | null): void {
  const el = memberVideoRef.value
  if (!el) return
  el.srcObject = stream
  if (stream) {
    void el.play().catch((e) => appLog.w(TAG, 'member screen autoplay blocked', e))
  }
}

async function toggleScreenShare(): Promise<void> {
  if (!viewer) return
  screenError.value = ''
  if (sharingScreen.value) {
    await viewer.stopScreenShare()
    sharingScreen.value = false
    return
  }
  if (clientMode.value) {
    // Electron's display-media handler refuses an undeclared source, so the
    // picker has to run first.
    await openClientPicker()
    return
  }
  const ok = await viewer.startScreenShare()
  sharingScreen.value = ok
  if (!ok) screenError.value = t('share.live.shareFailed')
}

async function openClientPicker(): Promise<void> {
  const bridge = getNative()
  if (!bridge?.listCaptureSources) {
    screenError.value = t('share.live.shareFailed')
    return
  }
  try {
    const raw = await bridge.listCaptureSources()
    const list = JSON.parse(raw) as Array<{ id: string; name: string; kind: string }>
    // Wayland exposes one nameless placeholder (the system dialog does the real
    // picking), so an unlabelled entry would render as an empty button.
    clientSources.value = list.map((s) => ({ ...s, name: s.name || t('liveShare.entireScreen') }))
    pickingSource.value = true
  } catch (e) {
    appLog.w(TAG, 'list capture sources failed', e)
    screenError.value = t('share.live.shareFailed')
  }
}

async function startClientShare(sourceId: string): Promise<void> {
  pickingSource.value = false
  const bridge = getNative()
  if (!bridge?.setPendingCaptureSource || !viewer) return
  try {
    await bridge.setPendingCaptureSource(sourceId, false)
    const ok = await viewer.startScreenShare()
    sharingScreen.value = ok
    if (!ok) screenError.value = t('share.live.shareFailed')
  } catch (e) {
    appLog.w(TAG, 'client screen share failed', e)
    screenError.value = t('share.live.shareFailed')
  }
}
const noteBlocks = computed(() => groupNoteSegments(notesSegments.value).slice(-60))
let notesPoller: ShareNotesPoller | null = null

function openNotes(): void {
  notesOpen.value = true
  if (!notesPoller) {
    notesPoller = new ShareNotesPoller((snapshot) => {
      notesSegments.value = snapshot.segments
    })
  }
  notesPoller.start()
}

function closeNotes(): void {
  notesOpen.value = false
  notesPoller?.stop()
  notesPoller = null
}

function toggleNotes(): void {
  if (notesOpen.value) closeNotes()
  else openNotes()
}

const notesBodyRef = ref<HTMLElement | null>(null)

// Follow the transcript, but only when the reader is already at the bottom:
// scrolling away to re-read an earlier line must not be undone by the next
// poll.
watch(
  () => noteBlocks.value.length,
  async () => {
    const el = notesBodyRef.value
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
    if (!nearBottom) return
    await nextTick()
    el.scrollTop = el.scrollHeight
  },
)

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

/** Gate callback: remember the name and connect. */
function onJoin(name: string): void {
  nick.value = name
  joined.value = true
  void nextTick(() => start())
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
    name: nick.value,
    callbacks: {
      onStream: attachStream,
      onTitle: (v) => {
        title.value = v
      },
      onVideoMode: (video) => {
        videoEnabled.value = video
      },
      onStateChange: (s, detail) => {
        state.value = s
        if (detail?.reason) failReason.value = detail.reason
        if (s === 'ended' || s === 'full' || s === 'failed') {
          talking.value = false
          sharingScreen.value = false
          screenSharer.value = ''
          memberStream.value = null
          // The room is gone, so the transcript endpoint 404s from here on.
          closeNotes()
        }
      },
      onTalkEnabled: (v) => {
        talkEnabled.value = v
      },
      onNotesEnabled: (v) => {
        notesEnabled.value = v
        if (!v) closeNotes()
      },
      onScreenEnabled: (v) => {
        screenEnabled.value = v
      },
      onScreenSharer: (id) => {
        screenSharer.value = id
        if (!id) {
          memberStream.value = null
          viewMode.value = 'host'
          attachMember(null)
        }
      },
      onMemberScreenStream: (stream) => {
        memberStream.value = stream
        // Presenting takes over the main picture; the host's screen is one
        // click away in the banner.
        viewMode.value = stream && !sharingScreen.value ? 'member' : 'host'
        attachMember(stream)
      },
      onControl: applyControl,
      onScreenDenied: (reason) => {
        sharingScreen.value = false
        screenError.value =
          reason === 'busy' ? t('share.live.screenBusy') : t('share.live.screenDisabled')
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
watch(controlState, (next) => {
  if (next === 'granted') {
    window.addEventListener('keydown', onControlKey)
    window.addEventListener('compositionend', onControlComposition)
    // A release outside the picture never reaches the stage, so the window has
    // to be listened to as well.
    window.addEventListener('pointerup', onWindowPointerUp)
    window.addEventListener('pointercancel', onWindowPointerUp)
  } else {
    window.removeEventListener('keydown', onControlKey)
    window.removeEventListener('compositionend', onControlComposition)
    window.removeEventListener('pointerup', onWindowPointerUp)
    window.removeEventListener('pointercancel', onWindowPointerUp)
    // Safety net for a grant revoked by the publisher while a button is held.
    endControlPress()
  }
})

watch(state, (s) => {
  if (s !== 'playing') talking.value = false
  if (s === 'playing') {
    startStatsPolling()
    onVisibility()
  }
})

let statsTimer: ReturnType<typeof setInterval> | null = null

function onVisibility(): void {
  // `viewer?.sendControl({ viewerId: viewer.viewerId })` would still evaluate
  // the argument (and throw) when the viewer is null.
  if (!viewer) return
  viewer.sendControl({
    t: 'presence',
    viewerId: viewer.viewerId,
    visible: document.visibilityState === 'visible',
  })
}

function startStatsPolling(): void {
  if (statsTimer) return
  statsTimer = setInterval(() => {
    if (!viewer || state.value !== 'playing') return
    void viewer.getStats().then((next) => {
      stats.value = next
    })
  }, 3000)
}

// The tab used to be the only "header" left once the bar was removed, so keep
// the share title there instead of a generic "ClawBench Share".
watch(title, (v) => {
  document.title = v ? `${v} · ClawBench` : 'ClawBench Share'
})

onMounted(() => {
  document.addEventListener('fullscreenchange', syncFullscreen)
  document.addEventListener('visibilitychange', onVisibility)
  window.addEventListener('resize', syncInkRect)
})

onBeforeUnmount(() => {
  if (statsTimer) clearInterval(statsTimer)
  statsTimer = null
  document.removeEventListener('visibilitychange', onVisibility)
  window.removeEventListener('keydown', onControlKey)
  window.removeEventListener('compositionend', onControlComposition)
  window.removeEventListener('pointerup', onWindowPointerUp)
  window.removeEventListener('pointercancel', onWindowPointerUp)
  window.removeEventListener('resize', syncInkRect)
  // The page is going away mid-drag: close the press while the channel is
  // still open, or the host keeps the button down with nobody left to release.
  endControlPress()
  board.dispose()
  document.removeEventListener('fullscreenchange', syncFullscreen)
  attachMember(null)
  closeNotes()
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

  .live-dock-btn {
    padding: var(--space-2, 4px) var(--space-3, 6px);
  }
}

/* ── Zoom container + ink ───────────────────────────────────────────────── */

/*
 * The transform lives on ONE wrapper around both videos and the ink canvas.
 * Transforming them separately would let a pan move the picture but not the
 * strokes, and every annotation would drift by the pan offset.
 */
.live-zoom {
  position: absolute;
  inset: 0;
  transform-origin: center center;
  will-change: transform;
}

.live-stage--zoomed {
  cursor: grab;
  touch-action: none;
}

.live-ink-wrap {
  position: absolute;
  pointer-events: none;
}

/* ── Connection quality chip ────────────────────────────────────────────── */

.live-quality {
  position: absolute;
  top: calc(var(--space-5, 10px) + env(safe-area-inset-top, 0px));
  left: calc(var(--space-5, 10px) + env(safe-area-inset-left, 0px));
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-1, 2px) var(--space-4, 8px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 82%, transparent);
  border: 1px solid var(--border-color, #30363d);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary, #8b949e);
  z-index: 7;
}

.live-quality__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-success, #3fb950);
}

.live-quality--fair .live-quality__dot {
  background: var(--color-orange, #d18616);
}

.live-quality--poor .live-quality__dot {
  background: var(--color-red, #cf222e);
}

.live-control-banner {
  top: auto;
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px) + calc(52 * var(--dp)));
  border-color: color-mix(in srgb, var(--color-red, #cf222e) 55%, transparent);
  color: var(--color-red, #cf222e);
}

/* ── Client-mode hint + source picker ───────────────────────────────────── */

.live-client-hint {
  position: absolute;
  top: calc(var(--space-5, 10px) + env(safe-area-inset-top, 0px));
  right: calc(var(--space-5, 10px) + env(safe-area-inset-right, 0px));
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  max-width: min(calc(420 * var(--dp)), 70vw);
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border-radius: var(--radius-md, 6px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 90%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #4a90d9) 45%, transparent);
  color: var(--text-secondary, #8b949e);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
  z-index: 7;
}

.live-client-hint__close {
  flex: 0 0 auto;
  border: none;
  background: transparent;
  color: inherit;
  font-size: calc(14 * var(--dp));
  cursor: pointer;
  padding: 0 2px;
}

.live-picker {
  position: absolute;
  inset: 0;
  z-index: 8;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
}

.live-picker__card {
  display: flex;
  flex-direction: column;
  gap: var(--space-4, 8px);
  width: min(calc(360 * var(--dp)), calc(100vw - 32px));
  max-height: 70vh;
  padding: var(--space-5, 10px);
  border-radius: var(--radius-lg, 10px);
  background: var(--bg-secondary, #161b22);
  border: 1px solid var(--border-color, #30363d);
}

.live-picker__title {
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--text-primary, #e6edf3);
}

.live-picker__list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  overflow-y: auto;
}

.live-picker__item {
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border: 1px solid var(--border-color, #30363d);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-primary, #0d1117);
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-sm, 13px);
  text-align: left;
  cursor: pointer;
}

.live-picker__item:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
}

.live-picker__cancel {
  align-self: flex-end;
  padding: var(--space-2, 4px) var(--space-4, 8px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-muted, #8b949e);
  font-size: var(--font-size-sm, 13px);
  cursor: pointer;
}

/* ── Chat panel ─────────────────────────────────────────────────────────── */

.live-chat-panel {
  position: absolute;
  right: calc(var(--space-6, 12px) + env(safe-area-inset-right, 0px));
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px) + calc(52 * var(--dp)));
  display: flex;
  flex-direction: column;
  width: min(calc(340 * var(--dp)), calc(100vw - 2 * var(--space-6, 12px)));
  height: min(50vh, calc(360 * var(--dp)));
  border-radius: var(--radius-lg, 10px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 94%, transparent);
  border: 1px solid var(--border-color, #30363d);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
  z-index: 6;
  overflow: hidden;
}

/* Unread counter on the dock button. */
.live-dock-badge {
  min-width: 16px;
  padding: 0 4px;
  border-radius: var(--radius-full, 999px);
  background: var(--color-red, #cf222e);
  color: #fff;
  font-size: calc(10 * var(--dp));
  line-height: 16px;
  text-align: center;
}

/* ── Speaking indicator ─────────────────────────────────────────────────── */

.live-speaker {
  position: absolute;
  top: calc(var(--space-5, 10px) + env(safe-area-inset-top, 0px));
  left: calc(var(--space-5, 10px) + env(safe-area-inset-left, 0px));
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-2, 4px) var(--space-4, 8px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 88%, transparent);
  border: 1px solid var(--border-color, #30363d);
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-xs, 11px);
  z-index: 7;
  white-space: nowrap;
}

.live-speaker__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-success, #3fb950);
  animation: live-speaker-pulse 1.2s ease-in-out infinite;
}

@keyframes live-speaker-pulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}

/* ── Member screen banner ───────────────────────────────────────────────── */

/* Floating pill at the top: who is presenting, plus the way back to the host's
   picture. Absolutely positioned so it never steals height from the video. */
.live-screen-banner {
  position: absolute;
  top: calc(var(--space-5, 10px) + env(safe-area-inset-top, 0px));
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  max-width: calc(100vw - 2 * var(--space-6, 12px));
  padding: var(--space-2, 4px) var(--space-3, 6px) var(--space-2, 4px) var(--space-5, 10px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 88%, transparent);
  border: 1px solid var(--border-color, #30363d);
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-xs, 11px);
  z-index: 7;
  white-space: nowrap;
}

.live-screen-banner-btn {
  padding: var(--space-1, 2px) var(--space-4, 8px);
  border: none;
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 22%, transparent);
  color: var(--accent-color, #4a90d9);
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

/* ── AI 听记 panel ───────────────────────────────────────────────────────── */

/*
 * Sits directly above the dock, centred, and never wider than the viewport.
 * `max-height` in vh keeps it from covering the whole picture on a phone; the
 * body scrolls instead.
 */
.live-notes-panel {
  position: absolute;
  left: 50%;
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px) + calc(52 * var(--dp)));
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  width: min(calc(560 * var(--dp)), calc(100vw - 2 * var(--space-6, 12px)));
  max-height: min(56vh, calc(380 * var(--dp)));
  border-radius: var(--radius-lg, 10px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 94%, transparent);
  border: 1px solid var(--border-color, #30363d);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
  z-index: 6;
  overflow: hidden;
}

.live-notes-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4, 8px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-bottom: 1px solid var(--border-color, #30363d);
}

.live-notes-title {
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--text-primary, #e6edf3);
}

.live-notes-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
}

.live-notes-close:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
}

.live-notes-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

.live-notes-hint {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #8b949e);
  line-height: 1.6;
}

.live-note-line {
  display: flex;
  gap: var(--space-4, 8px);
  font-size: var(--font-size-sm, 13px);
  line-height: 1.6;
}

.live-note-speaker {
  flex: 0 0 calc(64 * var(--dp));
  color: var(--text-muted, #8b949e);
  font-weight: 600;
  text-align: right;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.live-note-text {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--text-primary, #e6edf3);
  overflow-wrap: anywhere;
}

/* Voice meeting: no picture to show. The placeholder sits where the video
   would be, on the stage's own background, so it reads as "this is the
   meeting" rather than "the stream failed". */
.live-voice-stage {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3, 6px);
  color: var(--text-muted, #6c757d);
  pointer-events: none;
}
.live-voice-stage__title {
  font-size: var(--font-size-lg, 14px);
  font-weight: 600;
  color: var(--text-primary, #212529);
}
.live-voice-stage__desc { font-size: var(--font-size-xs, 11px); }
</style>
