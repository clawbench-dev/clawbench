<template>
  <!-- The nickname gate runs BEFORE any connection: an empty name must not take
       an audience slot. It also carries the meeting title the server already
       resolved from the share link. -->
  <LiveJoinGate v-if="!joined" :title="title" @join="onJoin" />
  <!-- Shell: the picture column plus the side panel, side by side. The panel is
       a SIBLING of the picture, never an overlay: opening it must squeeze the
       stage, so nothing the presenter draws ends up hidden behind it. -->
  <div v-else ref="rootRef" class="live-shell">
    <div class="live-share-view">
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
               at 0.5/0.5 must land in the middle of the CONTENT on every screen.
               Screens only: a stroke over a camera image has nothing to annotate. -->
          <div v-if="hostScreen" class="live-ink-wrap" :style="inkWrapStyle">
            <LiveInkLayer :strokes="board.strokes.value" :laser="board.laser.value" />
          </div>
        </div>

        <!-- The publisher's camera, when the meeting shows a screen AND a camera.
             Outside the zoom wrapper on purpose: the screen may be zoomed and
             panned, the face must not be. -->
        <video
          v-show="hostScreen && cameraMode && cameraStream"
          ref="cameraVideoRef"
          class="live-camera-pip"
          autoplay
          playsinline
          muted
          data-role="camera-pip"
        />

        <!-- No picture right now. Meetings have no type: the host may start a
             screen share or turn the camera on at any moment, so this says "not
             yet" instead of naming a kind of meeting. A black rectangle here
             would read as a broken stream. -->
        <div v-if="!videoEnabled" class="live-voice-stage" data-role="no-picture">
          <Mic :size="34" />
          <div class="live-voice-stage__title">{{ t('share.live.noPictureTitle') }}</div>
          <div class="live-voice-stage__desc">{{ t('share.live.noPictureDesc') }}</div>
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

        <!-- Shown whenever the host shares a screen, even when THIS page cannot
             drive it: hiding it entirely leaves a member wondering why the button
             their colleague sees is missing. Disabled + a title explains it. -->
        <button
          v-if="controlState === 'none' && hostScreen"
          class="live-dock-btn"
          type="button"
          :disabled="!canRequestControl"
          :aria-label="t('share.live.requestControl')"
          :title="
            canRequestControl ? t('share.live.requestControlHint') : t('share.live.controlUnavailable')
          "
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
          @click="toggleSide('chat')"
        >
          <MessageSquare :size="16" />
          <span class="live-dock-label">{{ t('share.live.chat') }}</span>
          <span v-if="unread > 0" class="live-dock-badge">{{ unread > 9 ? '9+' : unread }}</span>
        </button>

        <button
          v-if="notesAvailable"
          class="live-dock-btn"
          :class="{ 'live-dock-btn--live': notesOpen }"
          type="button"
          :aria-pressed="notesOpen"
          :aria-label="t('share.live.notesTitle')"
          :title="t('share.live.notesTitle')"
          data-action="notes"
          @click="toggleSide('notes')"
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

    <!-- The same divider the app's split panes use (App.vue, the file manager):
         one shared implementation of the 1px line, the accent hover, the
         pointer capture that survives a fast drag off the line, and the touch
         grab band. Only the geometry of the panes on either side is ours. -->
    <SplitDivider
      v-if="sideOpen"
      class="live-side-divider"
      orientation="horizontal"
      @dragmove="onSideDrag"
    />

    <!-- Side panel: 聊天 + 听记, full height, on the right.
         It is a flex SIBLING of the picture, so opening it shrinks the stage
         instead of covering it — and `v-show` (not `v-if`) keeps the chat list,
         its scroll position and an unsent draft alive while it is closed. -->
    <aside
      v-show="sideOpen"
      class="live-side"
      :style="sideWidth > 0 ? { '--live-side-w': `${sideWidth}px` } : undefined"
      data-panel="side"
    >
      <div class="live-side-tabs" role="tablist">
        <button
          class="live-side-tab"
          :class="{ 'live-side-tab--on': sideTab === 'chat' }"
          type="button"
          role="tab"
          :aria-selected="sideTab === 'chat'"
          data-tab="chat"
          @click="setSideTab('chat')"
        >
          {{ t('share.live.chat') }}
        </button>
        <button
          v-if="notesAvailable"
          class="live-side-tab"
          :class="{ 'live-side-tab--on': sideTab === 'notes' }"
          type="button"
          role="tab"
          :aria-selected="sideTab === 'notes'"
          data-tab="notes"
          @click="setSideTab('notes')"
        >
          {{ t('share.live.notes') }}
        </button>
        <span class="live-side-gap" />
        <button
          class="live-side-close"
          type="button"
          :aria-label="t('common.close')"
          data-action="side-close"
          @click="closeSide"
        >
          <X :size="16" />
        </button>
      </div>

      <div
        v-show="sideTab === 'chat'"
        class="live-chat-panel__body"
        role="tabpanel"
        data-panel="chat"
      >
        <LiveChatPanel :messages="chatMessages" @send="sendChat" />
      </div>

      <div
        v-if="notesAvailable"
        v-show="sideTab === 'notes'"
        class="live-notes-body"
        role="tabpanel"
        data-panel="notes"
      >
        <!-- Transcription is over, but the lines it already produced stay. The
             hint is what stops "nothing new is appearing" from reading as a
             broken panel. -->
        <div v-if="!notesEnabled && noteBlocks.length > 0" class="live-notes-stopped" data-role="notes-stopped">
          {{ t('share.live.notesStoppedHint') }}
        </div>
        <!-- Same shape as the record's 转写 tab: centred glyph, title, one line
             of context. "Waiting" only makes sense while 听记 is on; once it is
             off, the honest line is that nothing was recorded. -->
        <MeetingEmpty
          v-if="noteBlocks.length === 0"
          :icon="Captions"
          :title="t(notesEnabled ? 'share.live.notesWaiting' : 'share.live.notesOffTitle')"
          :desc="t(notesEnabled ? 'share.live.notesWaitingDesc' : 'share.live.notesOffDesc')"
          data-role="notes-empty"
        />
        <!-- The same rows the publisher's 转写 tab draws: one implementation. -->
        <TranscriptList v-else :blocks="noteBlocks" fill />
      </div>
    </aside>

    <!-- Client-mode source picker. Electron refuses an undeclared capture
         source, so the choice has to happen before getDisplayMedia. It is a
         shell-level modal, not a picture-level one: the choice concerns the
         whole page, so it must also cover an open side panel. -->
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
  Captions,
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
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import { LiveViewer, type LiveStats, type LiveViewerState } from './liveViewer'
import TranscriptList from '@/components/live/TranscriptList.vue'
import LiveJoinGate from './LiveJoinGate.vue'
import LiveInkLayer from './LiveInkLayer.vue'
import LiveChatPanel, { type ChatMessage } from './LiveChatPanel.vue'
import SplitDivider from '@/components/common/SplitDivider.vue'
import { useInkBoard } from './useInkBoard'
import { contentRectOf, toNormalized, type ContentRect, type ControlMessage } from './liveControl'
import { mergeChatHistory } from './chatHistory'
import { ShareNotesPoller, fetchNotesSnapshot } from './liveNotesClient'
import { getShareToken } from './shareMode'
import { groupNoteSegments, withMeetingFileEvents, type LiveNoteSegment, type NoteFileEvent } from '@/composables/liveNotes'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { designToCss } from '@/utils/uiScale'

const TAG = 'LiveShareView'

const { t } = useI18n()

const rootRef = ref<HTMLElement | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
/** The picture-in-picture tile carrying the publisher's camera. */
const cameraVideoRef = ref<HTMLVideoElement | null>(null)
const state = ref<LiveViewerState>('connecting')
/** The nickname gate has been passed; only then is a socket opened. */
const joined = ref(false)
const nick = ref('')
/** Whether the publisher sends a picture (false = voice meeting). */
const videoEnabled = ref(true)
/**
 * Whether that picture is a screen (vs a camera).
 *
 * Remote control and annotation hang off this, not off `videoEnabled`: a
 * camera image has no desktop behind it, so a pointer mapped onto it would
 * inject clicks at nonsense coordinates.
 */
const hostScreen = ref(true)
/**
 * Whether the publisher can accept a remote-control request.
 *
 * Comes from the server, never from this page's own platform: injection happens
 * on the host machine, so only the host knows whether it can take the request.
 * Starts false — a viewer must not offer a button whose press can only be
 * refused.
 */
const controlEnabled = ref(false)
/** Whether the publisher's camera tile should be shown. */
const cameraMode = ref(false)
/** The publisher's camera picture (picture-in-picture tile). */
const cameraStream = ref<MediaStream | null>(null)
const title = ref('')
const failReason = ref('')
const talkEnabled = ref(false)
const talking = ref(false)
const needsSoundUnlock = ref(false)
/** Mirrors the browser's fullscreen state so the icon can follow Esc / F11. */
const isFullscreen = ref(false)

// ── Side panel: 聊天 + 听记 ──────────────────────────────────────────────────
/**
 * One full-height column beside the picture, two tabs.
 *
 * Deliberately a flex SIBLING of the stage rather than an overlay. The panels
 * used to float over the video with a 50vh / 56vh cap, which on a laptop left
 * room for about six chat lines and hid part of the presentation. Opening this
 * squeezes the stage instead: nothing the presenter shows is ever covered, and
 * the chat gets the whole height of the window.
 */
const sideOpen = ref(false)
const sideTab = ref<'chat' | 'notes'>('chat')
/** Derived, so the dock can keep using the old names for its active tint. */
const chatOpen = computed(() => sideOpen.value && sideTab.value === 'chat')
const notesOpen = computed(() => sideOpen.value && sideTab.value === 'notes')

/**
 * Panel width after a drag, in CSS px. `0` = the stylesheet's clamp decides.
 *
 * CSS px (not a ratio) because a side panel is a width, not a share: widening
 * the window must not widen the chat. The stylesheet still caps it — `max-width`
 * keeps the picture above its own minimum when the window shrinks after a wide
 * drag, which is why there is no resize listener here.
 */
const sideWidth = ref(0)

/** Design-px bounds for the drag. */
const SIDE_MIN_DP = 260
/** A very wide window must not turn the chat into half the screen. */
const SIDE_MAX_DP = 560
/** The picture keeps at least this much, however far the divider is dragged. */
const STAGE_MIN_DP = 280

function clampSideWidth(px: number, shellPx: number): number {
  const min = designToCss(SIDE_MIN_DP)
  const max = Math.max(min, Math.min(designToCss(SIDE_MAX_DP), shellPx - designToCss(STAGE_MIN_DP)))
  return Math.min(max, Math.max(min, px))
}

/**
 * The divider rides the panel's left edge, so the distance from the pointer to
 * the shell's right edge IS the panel's new width — no need to know where the
 * drag started.
 */
function onSideDrag(clientX: number): void {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  sideWidth.value = clampSideWidth(rect.right - clientX, rect.width)
}

// ── AI 听记 (viewer side) ────────────────────────────────────────────────────
/** Whether the publisher is recording 听记 right now. */
const notesEnabled = ref(false)
/**
 * Whether this meeting has any 听记 to show — either because it is running, or
 * because it ran and was switched off. Sticky on purpose: turning 听记 off
 * mid-meeting must not make the lines already transcribed disappear.
 */
const notesSeen = ref(false)
const notesAvailable = computed(() => notesEnabled.value || notesSeen.value)
const notesSegments = ref<LiveNoteSegment[]>([])
/** Files the host opened, as the public endpoint reports them (name only). */
const notesFiles = ref<NoteFileEvent[]>([])

// ── Control channel: ink, chat, quality, presence ───────────────────────────
const board = useInkBoard()
const stageRef = ref<HTMLElement | null>(null)
const inkRect = ref<ContentRect>({ left: 0, top: 0, width: 0, height: 0 })
const chatMessages = ref<ChatMessage[]>([])
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
  if (controlState.value === 'none') return
  // Escape is the local escape hatch: it must never be forwarded, or a viewer
  // could not stop controlling without reaching for the mouse.
  //
  // Checked BEFORE the editable-target guard, and it also covers a pending
  // request: Escape is never "typing", so a viewer whose focus happens to sit
  // in the chat box (or who has not been approved yet) still has a way out.
  if (ev.key === 'Escape' || ev.key === 'Esc') {
    if (ev.ctrlKey || ev.altKey) return
    releaseControl()
    ev.preventDefault()
    return
  }
  if (controlState.value !== 'granted') return
  // Local typing is not remote control: leave the key alone (no forward, no
  // preventDefault) so the chat box keeps working.
  if (isEditableTarget(ev.target)) return
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

/**
 * Show a tab of the side panel, starting/stopping the transcript poll with it.
 *
 * The poll is scoped to the 听记 tab, not to the panel: a viewer reading the
 * chat must not keep hitting the transcript endpoint every 3 seconds.
 */
function setSideTab(tab: 'chat' | 'notes'): void {
  if (tab === 'notes' && !notesAvailable.value) return
  sideTab.value = tab
  if (tab === 'notes') startNotesPolling()
  else stopNotesPolling()
  // Reading the chat clears its badge; merely opening the panel on 听记 does not.
  if (tab === 'chat') unread.value = 0
}

function openSide(tab: 'chat' | 'notes'): void {
  // A tab that does not exist yet must not open the panel on an empty pane.
  if (tab === 'notes' && !notesAvailable.value) return
  sideOpen.value = true
  setSideTab(tab)
}

function closeSide(): void {
  sideOpen.value = false
  stopNotesPolling()
}

/** The dock buttons toggle: same tab again closes the panel. */
function toggleSide(tab: 'chat' | 'notes'): void {
  if (sideOpen.value && sideTab.value === tab) closeSide()
  else openSide(tab)
}

function sendChat(text: string): void {
  const msg: ControlMessage = {
    t: 'chat',
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    from: viewer?.viewerId ?? '',
    // The nickname the viewer chose at the gate, not the slot id: the local
    // echo of this same frame is what the sender reads, so labelling it
    // "viewer 1" would show the sender a different name than the publisher
    // (who resolves the name from the room roster).
    name: nick.value,
    text,
    at: Date.now(),
  }
  if (!viewer?.sendControl(msg)) return
  chatMessages.value = [...chatMessages.value, { ...msg, self: true }]
}

function applyControl(msg: ControlMessage): void {
  if (msg.t === 'chat-history') {
    // Catch-up for this (re)join. Merged rather than replaced: lines sent while
    // the replay was in flight are already here, and the frames may be split.
    chatMessages.value = mergeChatHistory(
      chatMessages.value,
      msg.messages,
      viewer?.viewerId ?? '',
      (from) => t('share.live.viewerName', { id: from }),
    )
    return
  }
  if (msg.t === 'chat') {
    chatMessages.value = [
      ...chatMessages.value,
      {
        id: msg.id,
        from: msg.from,
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

/**
 * Whether this page may ask for control of the host screen.
 *
 * Purely the publisher's advertised capability (`control-enabled` / `joined`).
 * The viewer's own platform is deliberately not part of this: a browser tab can
 * watch, chat and annotate, and whether a request can be served depends on the
 * host's injector and on it sharing a whole screen, not on what the viewer runs.
 */
const canRequestControl = computed(() => controlEnabled.value)

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
const noteBlocks = computed(() =>
  groupNoteSegments(withMeetingFileEvents(notesSegments.value, notesFiles.value)).slice(-60),
)
let notesPoller: ShareNotesPoller | null = null

/**
 * Ask once whether this meeting recorded anything.
 *
 * A viewer who joins after 听记 was switched off never saw the tab, and the
 * publisher's state alone says nothing about what is already on the server. One
 * read is enough to keep that content reachable; failure keeps the tab hidden
 * rather than promising a pane that may be empty.
 */
async function probeNotes(): Promise<void> {
  if (notesSeen.value) return
  const snapshot = await fetchNotesSnapshot()
  if (!snapshot) return
  if (snapshot.segments.length === 0 && snapshot.files.length === 0) return
  notesSegments.value = snapshot.segments
  notesFiles.value = snapshot.files.map((f) => ({
    path: '',
    name: f.name,
    openedAt: f.openedAt,
    closedAt: f.closedAt,
  }))
  notesSeen.value = true
}

function startNotesPolling(): void {
  if (!notesPoller) {
    notesPoller = new ShareNotesPoller((snapshot) => {
      notesSegments.value = snapshot.segments
      notesFiles.value = snapshot.files.map((f) => ({ path: '', name: f.name, openedAt: f.openedAt, closedAt: f.closedAt }))
    })
  }
  notesPoller.start()
}

/**
 * Stop polling and drop the instance.
 *
 * A stopped poller cannot be restarted (`stopped` is sticky), so it must not be
 * kept around: the next 听记 visit builds a fresh one.
 */
function stopNotesPolling(): void {
  notesPoller?.stop()
  notesPoller = null
}

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

/**
 * Attach the publisher's camera to its own element.
 *
 * Deliberately separate from `attachStream`: the camera is a tile, so it must
 * never touch the main <video> (which would replace the screen) nor the sound
 * unlock state (the camera carries no audio, and clearing the prompt would
 * leave a muted meeting with no way to unmute it).
 */
function attachCamera(stream: MediaStream | null): void {
  cameraStream.value = stream
  const el = cameraVideoRef.value
  if (!el) return
  el.srcObject = stream
  if (stream) void el.play().catch((e) => appLog.w(TAG, 'camera play failed', e))
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
      onHostScreen: (screen) => {
        hostScreen.value = screen
      },
      onControlEnabled: (v) => {
        controlEnabled.value = v
      },
      onCameraMode: (on) => {
        cameraMode.value = on
      },
      onCameraStream: attachCamera,
      onStateChange: (s, detail) => {
        state.value = s
        if (detail?.reason) failReason.value = detail.reason
        if (s === 'ended' || s === 'full' || s === 'failed') {
          talking.value = false
          sharingScreen.value = false
          screenSharer.value = ''
          memberStream.value = null
          // The room is gone, so the transcript endpoint 404s from here on.
          // Fall back to the chat tab rather than closing the panel: the
          // conversation is still worth reading after the stream drops.
          stopNotesPolling()
          if (sideTab.value === 'notes') sideTab.value = 'chat'
        }
      },
      onTalkEnabled: (v) => {
        talkEnabled.value = v
      },
      onNotesEnabled: (v) => {
        notesEnabled.value = v
        if (v) {
          notesSeen.value = true
          // Switched back on while the reader is already on the pane: resume
          // without making them leave the tab and come back.
          if (notesOpen.value) startNotesPolling()
          return
        }
        // 听记 off mid-meeting: what it already transcribed stays readable, and
        // the pane keeps its tab instead of dumping the reader into the chat.
        // The transcript cannot grow any more, so the poll stops with it.
        if (notesSeen.value) {
          stopNotesPolling()
          return
        }
        // A viewer who never saw it on: one probe decides whether this meeting
        // recorded before they arrived, so that content is not lost to them.
        void probeNotes()
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
    window.addEventListener('compositionend', onControlComposition)
    // A release outside the picture never reaches the stage, so the window has
    // to be listened to as well.
    window.addEventListener('pointerup', onWindowPointerUp)
    window.addEventListener('pointercancel', onWindowPointerUp)
  } else {
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

// Opening or closing the side panel changes the stage's WIDTH without a window
// resize, so `resize` never fires. The ink layer is positioned to the picture's
// rectangle, and a stale rect puts every stroke half a panel off the pixel it
// was drawn on. Re-measure after the DOM has taken the new width — and again on
// every drag step, which moves the edge without changing `sideOpen`.
watch([sideOpen, sideWidth], () => {
  void nextTick(syncInkRect)
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
  // Attached for the page's whole life, not only while control is granted:
  // Escape has to work in the pending state too. Capture phase, because it is
  // the local way OUT of control — a component that stops the event on its way
  // up must not be able to trap the viewer in someone else's screen.
  window.addEventListener('keydown', onControlKey, true)
})

onBeforeUnmount(() => {
  if (statsTimer) clearInterval(statsTimer)
  statsTimer = null
  document.removeEventListener('visibilitychange', onVisibility)
  window.removeEventListener('keydown', onControlKey, true)
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
  attachCamera(null)
  stopNotesPolling()
  viewer?.close()
  viewer = null
  const el = videoRef.value
  if (el) el.srcObject = null
})
</script>

<style scoped>
/* Row shell: the picture column and the side panel share the width.
   `position: relative` makes it the containing block for the source picker,
   which is a page-level modal and must cover the open panel too. */
.live-shell {
  position: relative;
  display: flex;
  height: 100%;
  background: var(--bg-primary, #0d1117);
}

/* The picture column. `min-width: 0` is what lets it actually shrink when the
   side panel opens: a flex item's default `min-width: auto` refuses to go below
   its content, and the video would push the panel off the screen instead. */
.live-share-view {
  display: flex;
  flex-direction: column;
  flex: 1 1 0;
  min-width: 0;
  min-height: 0;
  /* Positioning context for the floating dock and the banners. */
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

/* The publisher's camera, as a picture-in-picture tile over the screen.
   Bottom-right on purpose: the dock sits bottom-centre and the quality chip
   top-right, so this corner is the only one that never covers a control. */
.live-camera-pip {
  position: absolute;
  right: calc(12 * var(--dp));
  bottom: calc(12 * var(--dp));
  width: min(calc(180 * var(--dp)), 38%);
  aspect-ratio: 4 / 3;
  object-fit: cover;
  background: #000;
  border: 1px solid var(--border-color, rgb(255 255 255 / 12%));
  border-radius: var(--radius-md, 8px);
  box-shadow: 0 4px 16px rgb(0 0 0 / 45%);
  /* A tile, not a control: taps and drags belong to the stage underneath. */
  pointer-events: none;
  z-index: 2;
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

/* Unavailable on this platform (plain-http microphone, browser-side screen
   capture, remote control outside the client). The button keeps its place —
   removing it would read as "the host did not allow this" — but must look
   unusable, otherwise a click that does nothing looks broken. */
.live-dock-btn:disabled {
  opacity: var(--opacity-disabled, 0.4);
  cursor: not-allowed;
}

.live-dock-btn:disabled:hover {
  background: transparent;
  color: var(--text-secondary, #8b949e);
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

/*
 * Control state pill, sitting just above the dock instead of at the top.
 *
 * The compound selector is required, not stylistic: `.live-screen-banner` is
 * declared LATER in this file and sets `top`. At equal specificity the later
 * rule wins, so `top: auto` alone lost and the pill stretched from `top` to
 * `bottom` — a full-height strip whose flex content landed mid-screen, over
 * the picture, swallowing pointer events there.
 */
.live-screen-banner.live-control-banner {
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

/* ── Side panel: 聊天 + 听记 ─────────────────────────────────────────────── */

/*
 * Full-height column beside the picture. A flex SIBLING of the stage, never an
 * absolutely positioned card: `position: absolute` is exactly what made the old
 * panels cover the presentation. Width is a range so a wide window does not
 * hand the chat half the screen, and a narrow one still gets a readable column.
 */
.live-side {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  /* `--live-side-w` is written by the divider drag; without a drag the range
     below decides, so a wide window does not hand the chat half the screen and
     a narrow one still gets a readable column. */
  width: var(--live-side-w, clamp(calc(280 * var(--dp)), 26vw, calc(400 * var(--dp))));
  /* A drag must never starve the picture: the cap is applied by CSS, so a
     window that shrinks AFTER a wide drag is handled without a resize listener
     (the inline width would otherwise win and the picture would collapse). */
  max-width: calc(100% - calc(280 * var(--dp)));
  min-height: 0;
  /* No border-left: the divider draws the boundary (and thickens on hover). */
  background: var(--bg-secondary, #161b22);
  padding-right: env(safe-area-inset-right, 0);
  padding-bottom: env(safe-area-inset-bottom, 0);
}

.live-side-tabs {
  display: flex;
  align-items: stretch;
  flex-shrink: 0;
  height: calc(38 * var(--dp));
  border-bottom: 1px solid var(--border-color, #30363d);
  background: var(--bg-secondary, #161b22);
}

.live-side-tab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2, 4px);
  padding: 0 var(--space-5, 10px);
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary, #8b949e);
  font-size: var(--font-size-sm, 13px);
  cursor: pointer;
}

.live-side-tab:hover {
  color: var(--text-primary, #e6edf3);
}

.live-side-tab--on {
  color: var(--accent-color, #4a90d9);
  border-bottom-color: var(--accent-color, #4a90d9);
  font-weight: 600;
}

/* Pushes the close button to the far edge, so the tabs stay left-aligned like
   every other tab strip in the app. */
.live-side-gap {
  flex: 1 1 auto;
}

.live-side-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  align-self: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  margin-right: var(--space-3, 6px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
}

.live-side-close:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
}

/*
 * The chat body carries no inset of its own: it is flat so it can sit on a
 * pane that already insets its content (the meeting panel does). The side
 * panel is not such a pane, so the inset is supplied here, otherwise the
 * bubbles, the input and the send button run into the border.
 */
.live-chat-panel__body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-4, 8px) var(--space-5, 10px);
}

/* Unread counter on the dock button. Accent, not red: red means an error or a
   destructive action in this UI, while an unread line is ordinary traffic. */
.live-dock-badge {
  min-width: 16px;
  padding: 0 4px;
  border-radius: var(--radius-full, 999px);
  background: var(--accent-color, #4a90d9);
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

/* ── AI 听记 pane (inside the side panel) ────────────────────────────────── */

/* The pane's own inset; the rows themselves are `TranscriptList` (the same
   component the publisher's 转写 tab uses), so nothing about the line language
   is repeated here. */
.live-notes-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-4, 8px) var(--space-5, 10px);
}

/* The empty state is `MeetingEmpty` — the same block the record's tabs draw —
   so there is no hint styling of its own here. */

/* "Transcription stopped" note above a transcript that outlived it. One muted
   line, no card: the content below is the subject, this only explains why it
   stops growing. */
.live-notes-stopped {
  flex: 0 0 auto;
  padding-bottom: var(--space-3, 6px);
  color: var(--text-muted, #6c757d);
  font-size: var(--font-size-xs, 11px);
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

/*
 * Narrow windows (a phone, a split-screen tablet): a right-hand column would
 * leave the picture a sliver. Stack instead — the panel keeps a real height by
 * taking the bottom share, and the stage gives up height rather than width.
 * Still a squeeze, still no overlap.
 */
@media (max-width: 720px) {
  .live-shell {
    flex-direction: column;
  }

  /* A horizontal divider has nothing to separate once the panes are stacked. */
  .live-side-divider {
    display: none;
  }

  .live-side {
    width: 100%;
    /* The wide-layout cap would keep the stacked panel narrower than the
       screen; the drag that wrote `--live-side-w` cannot happen here anyway. */
    max-width: none;
    flex: 0 0 46%;
    border-top: 1px solid var(--border-color, #30363d);
  }
}
</style>
