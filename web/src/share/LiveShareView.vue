<template>
  <!-- The meeting is over: the token now addresses the permanent RECORD, so
       the page opens the minutes / transcript / proofread / chat instead of a
       gate that would dial a room which no longer exists. -->
  <MeetingShareDetail v-if="recordView" :title="title || metaTitle" />
  <!-- The nickname gate runs BEFORE any connection: an empty name must not take
       an audience slot. It also carries the meeting title the server already
       resolved from the share link. -->
  <LiveJoinGate v-else-if="!joined" :title="title" :error="gateError" @join="onJoin" />
  <!-- Shell: the picture column plus the side panel, side by side. The panel is
       a SIBLING of the picture, never an overlay: opening it must squeeze the
       stage, so nothing the presenter draws ends up hidden behind it. -->
  <LiveStageLayout
    v-else
    ref="layoutRef"
    :side-open="sideOpen"
    :active-tab="sideTab"
    :notes-available="notesAvailable"
    participants-available
    :participant-count="roster.length"
    @update:active-tab="setSideTab"
    @close="closeSide"
    @resize="syncInkRect"
  >
    <div
      ref="rootRef"
      class="live-share-view"
      :style="dockHeight > 0 ? { '--live-dock-height': `${dockHeight}px` } : undefined"
    >
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
               and waiting states from rendering a black rectangle.

               `!videoEnabled` is part of that: the publisher stopping its screen
               (or turning the camera off) leaves the m-line in place and only
               stops sending frames, so the element keeps painting its LAST one.
               The stage is still `playing` — the meeting goes on with audio —
               so without this the final screen stays behind the transparent
               "no picture" placeholder. Hiding (not clearing `srcObject`) is
               deliberate: the same element carries the mixed audio. -->
          <video
            ref="videoRef"
            class="live-video"
            :class="{
              'live-video--hidden': state !== 'playing' || viewMode === 'member' || !videoEnabled,
            }"
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
             would read as a broken stream.

             Gated on `playing` as well: this and `.live-overlay` below are the
             SAME slot (both `inset: 0`, centred), so their conditions must be
             mutually exclusive. "Not sharing yet" only means anything while the
             meeting is live — the host stopping its picture leaves
             `videoEnabled` false, and if the room then ends (or the viewer
             retries and lands in connecting/failed) both blocks would render at
             once, superimposing two sets of copy. -->
        <div
          v-if="!videoEnabled && state === 'playing'"
          class="live-voice-stage"
          data-role="no-picture"
        >
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
          data-local-ui
        >
          <span class="live-quality__dot" />
          {{ quality.label }}
        </div>

        <!-- Remote control state. Loud on purpose: while it is on, this viewer's
             pointer is moving someone else's mouse. `data-local-ui` keeps the
             release button pressable: without it the stage forwards (and
             captures) the press, so the host receives a click and the viewer
             never gets one — the mouse is "hijacked" with no way out. -->
        <div
          v-if="controlState !== 'none'"
          class="live-screen-banner live-control-banner"
          data-role="control-banner"
          data-local-ui
        >
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
        <div v-if="screenBanner" class="live-screen-banner" data-role="screen-banner" data-local-ui>
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

        <div v-if="state !== 'playing'" class="live-overlay" data-local-ui>
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

      <!-- Floating control dock. The pill and its buttons are the shared
           LiveDock / LiveDockButton the host's picture of a member's screen
           uses too: same object, different actions. -->
      <LiveDock brand :live="state === 'playing'" :title="title || t('share.live.title')">
        <!-- Autoplay with audio is blocked without a user gesture, so playback
             starts muted and the viewer opts in here. A switch, labelled by
             STATE exactly like the microphone next to it (声音已关 / 声音已开),
             so the way back to silence is the same control. Only shown when the
             stream actually carries audio — there is nothing to switch on a
             silent share. -->
        <LiveDockButton
          v-if="hasAudio"
          :live="!soundMuted"
          role="switch"
          :aria-checked="!soundMuted"
          :aria-label="t('share.live.soundToggle')"
          :title="soundMuted ? t('share.live.soundOffHint') : t('share.live.soundOnHint')"
          data-action="toggle-sound"
          @click="toggleSound"
        >
          <template #icon>
            <VolumeX v-if="soundMuted" :size="16" />
            <Volume2 v-else :size="16" />
          </template>
          {{ soundMuted ? t('share.live.soundOff') : t('share.live.soundOn') }}
        </LiveDockButton>

        <!-- One toggle: on = live, off = muted. No approval step — the
             publisher's room-level switch is the whole authorisation model, and
             it can be turned off at any moment (arrives as talk-revoked, which
             flips this back to off). -->
        <LiveDockButton
          v-if="talkEnabled"
          :live="talking"
          role="switch"
          :aria-checked="talking"
          :aria-label="t('share.live.micToggle')"
          :disabled="!canTalk || mutedByHost"
          :title="
            mutedByHost
              ? t('share.live.micMutedByHost')
              : !canTalk
                ? t('share.live.micNeedsHttps')
                : talking
                  ? t('share.live.micOnHint')
                  : t('share.live.micOffHint')
          "
          data-action="mic-toggle"
          @click="toggleMic"
        >
          <template #icon>
            <Mic v-if="talking" :size="16" />
            <MicOff v-else :size="16" />
          </template>
          {{ mutedByHost ? t('share.live.micMutedByHost') : talking ? t('share.live.micOn') : t('share.live.micOff') }}
        </LiveDockButton>

        <!-- Shown whenever the host shares a screen, even when THIS page cannot
             drive it: hiding it entirely leaves a member wondering why the button
             their colleague sees is missing. Disabled + a title explains it. -->
        <LiveDockButton
          v-if="controlState === 'none' && hostScreen"
          :disabled="!canRequestControl"
          :aria-label="t('share.live.requestControl')"
          :title="
            canRequestControl ? t('share.live.requestControlHint') : t('share.live.controlUnavailable')
          "
          data-action="request-control"
          @click="requestControl"
        >
          <template #icon><MousePointer2 :size="16" /></template>
          {{ t('share.live.requestControl') }}
        </LiveDockButton>

        <LiveDockButton
          v-if="screenEnabled"
          :live="sharingScreen"
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
          <template #icon>
            <ScreenShare v-if="!sharingScreen" :size="16" />
            <ScreenShareOff v-else :size="16" />
          </template>
          {{ sharingScreen ? t('share.live.stopSharing') : t('share.live.shareScreen') }}
        </LiveDockButton>

        <LiveDockButton
          :live="chatOpen"
          :badge="unread"
          :aria-pressed="chatOpen"
          :aria-label="t('share.live.chatTitle')"
          :title="t('share.live.chatTitle')"
          data-action="chat"
          @click="toggleSide('chat')"
        >
          <template #icon><MessageSquare :size="16" /></template>
          {{ t('share.live.chat') }}
        </LiveDockButton>

        <LiveDockButton
          v-if="notesAvailable"
          :live="notesOpen"
          :aria-pressed="notesOpen"
          :aria-label="t('share.live.notesTitle')"
          :title="t('share.live.notesTitle')"
          data-action="notes"
          @click="toggleSide('notes')"
        >
          <template #icon><FileText :size="16" /></template>
          {{ t('share.live.notes') }}
        </LiveDockButton>

        <LiveDockButton
          :aria-pressed="isFullscreen"
          :aria-label="isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen')"
          :title="isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen')"
          data-action="fullscreen"
          @click="toggleFullscreen"
        >
          <template #icon>
            <Minimize v-if="isFullscreen" :size="16" />
            <Maximize v-else :size="16" />
          </template>
          {{ isFullscreen ? t('share.live.exitFullscreen') : t('share.live.fullscreen') }}
        </LiveDockButton>
      </LiveDock>
    </div>

    <template #chat>
      <LiveChatPanel :messages="chatMessages" @send="sendChat" />
    </template>

    <template #notes>
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
    </template>

    <template #participants>
      <!-- The audience's view of the room. No actions here on purpose: only the
           host can mute or remove somebody, so a member list that offered
           buttons would be a set of controls that always fail. -->
      <!-- Headcount first, then the rows: the same number the tab carries, so
           the pane answers "how many" on its own after the tab bar scrolls out
           of view. -->
      <p v-if="roster.length > 0" class="live-members-total" data-role="members-total">
        {{ t('share.live.memberCount', { n: roster.length }) }}
      </p>
      <ul v-if="roster.length > 0" class="live-members" data-role="members-list">
        <li v-for="m in roster" :key="m.id" class="live-member" :data-member-id="m.id">
          <SpeakerAvatar class="live-member__avatar" :name="memberName(m)" />
          <span class="live-member__name" :title="memberName(m)">{{ memberName(m) }}</span>
          <span v-if="m.host" class="live-member__chip live-member__chip--role">
            {{ t('share.live.hostRole') }}
          </span>
          <span
            v-else-if="m.id === myViewerId"
            class="live-member__chip live-member__chip--you"
            data-role="member-you"
          >
            {{ t('share.live.memberYou') }}
          </span>
          <span
            v-if="screenSharer === m.id"
            class="live-member__chip"
            data-role="member-sharing"
          >
            {{ t('share.live.memberSharing') }}
          </span>
          <span
            v-if="m.talking"
            class="live-member__chip live-member__chip--live"
            data-role="member-talking"
          >
            {{ t('share.live.memberTalking') }}
          </span>
          <span
            v-else-if="m.muted"
            class="live-member__chip live-member__chip--muted"
            data-role="member-muted"
          >
            {{ t('share.live.memberMuted') }}
          </span>
        </li>
      </ul>
      <p v-else class="live-members-empty" data-role="members-empty">
        {{ t('share.live.membersEmpty') }}
      </p>
    </template>

    <template #overlay>
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
    </template>
  </LiveStageLayout>
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
  Volume2,
  VolumeX,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import { LiveViewer, type LiveStats, type LiveViewerState, type RosterMember } from './liveViewer'
import TranscriptList from '@/components/live/TranscriptList.vue'
import LiveJoinGate from './LiveJoinGate.vue'
import LiveInkLayer from './LiveInkLayer.vue'
import LiveChatPanel, { type ChatMessage } from './LiveChatPanel.vue'
import LiveDock from './LiveDock.vue'
import LiveDockButton from './LiveDockButton.vue'
import LiveStageLayout from './LiveStageLayout.vue'
import MeetingShareDetail from './MeetingShareDetail.vue'
import { useInkBoard } from './useInkBoard'
import { contentRectOf, toNormalized, type ContentRect, type ControlMessage } from './liveControl'
import { appendChatMessage, mergeChatHistory } from './chatHistory'
import { ShareNotesPoller, fetchNotesSnapshot } from './liveNotesClient'
import { getShareToken } from './shareMode'
import { groupNoteSegments, withMeetingFileEvents, type LiveNoteSegment, type NoteFileEvent } from '@/composables/liveNotes'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { useWakeLock } from '@/composables/useWakeLock'

const TAG = 'LiveShareView'

/**
 * What `/api/share/{token}/meta` told the dispatcher about this token.
 *
 * Only `ended` matters here: a live room and a finished meeting share the same
 * kind, and the record view is what a finished one opens. `title` is used while
 * the record is still loading, so the header never shows a blank name.
 */
const props = withDefaults(
  defineProps<{ meta?: { ended?: boolean; title?: string } | null }>(),
  { meta: null },
)
const metaTitle = computed(() => props.meta?.title ?? '')
/**
 * The meeting is over and this page shows its record.
 *
 * Seeded from meta (a link opened after the meeting) and flipped when the
 * publisher stops while we are watching (see `applyViewerState`).
 */
const recordView = ref(props.meta?.ended === true)

const { t } = useI18n()

/** The layout component — its root is the shell, which fullscreen targets. */
const layoutRef = ref<InstanceType<typeof LiveStageLayout> | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
/** The picture-in-picture tile carrying the publisher's camera. */
const cameraVideoRef = ref<HTMLVideoElement | null>(null)
const state = ref<LiveViewerState>('connecting')
/** The nickname gate has been passed; only then is a socket opened. */
const joined = ref(false)

/**
 * Keep the screen on while this member is actually in the meeting.
 *
 * Same reasoning as the host side: a phone that dims out pauses the WebView and
 * the viewer drops out of the room. Scoped to the live states — once the meeting
 * has ended the record is a page to read, not a call to attend.
 */
const wakeLock = useWakeLock()
const inMeeting = computed(
  () =>
    joined.value &&
    (state.value === 'connecting' || state.value === 'negotiating' || state.value === 'playing'),
)
watch(
  inMeeting,
  (on) => {
    if (on) void wakeLock.acquire('meeting-viewer')
    else wakeLock.release('meeting-viewer')
  },
  { immediate: true },
)

const nick = ref('')
/**
 * Why the last join attempt was refused, shown under the gate's name field.
 *
 * Only the duplicate-name refusal lands here: it is the one refusal the reader
 * can fix without leaving the page, so it must come back to the field rather
 * than to a terminal "connection failed" screen.
 */
const gateError = ref('')
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
/**
 * The host has blocked our microphone.
 *
 * The switch stays visible but disabled: hiding it would look like the room
 * never allowed speech, while the reason here is one person's decision that the
 * host can undo at any moment.
 */
const mutedByHost = ref(false)
/**
 * Sound is a two-state toggle, not a one-shot prompt.
 *
 * `hasAudio` decides whether the button exists at all (a silent share has
 * nothing to toggle); `soundMuted` mirrors the <video> element, which is the
 * real owner of the mute state.
 */
const hasAudio = ref(false)
const soundMuted = ref(true)
/** Mirrors the browser's fullscreen state so the icon can follow Esc / F11. */
const isFullscreen = ref(false)

// ── Side panel: 聊天 + 听记 + 成员 ───────────────────────────────────────────
/**
 * One full-height column beside the picture, three tabs.
 *
 * Deliberately a flex SIBLING of the stage rather than an overlay. The panels
 * used to float over the video with a 50vh / 56vh cap, which on a laptop left
 * room for about six chat lines and hid part of the presentation. Opening this
 * squeezes the stage instead: nothing the presenter shows is ever covered, and
 * the chat gets the whole height of the window.
 */
const sideOpen = ref(false)
const sideTab = ref<'chat' | 'notes' | 'participants'>('chat')
/** Derived, so the dock can keep using the old names for its active tint. */
const chatOpen = computed(() => sideOpen.value && sideTab.value === 'chat')
const notesOpen = computed(() => sideOpen.value && sideTab.value === 'notes')

/**
 * Who is in the meeting, as the server reports it.
 *
 * Full snapshots only — the server re-sends the list on every join, leave and
 * speech change, so there is nothing to merge here.
 */
const roster = ref<RosterMember[]>([])
/** Our own viewer id, so the list can mark "you" instead of just a name. */
const myViewerId = ref('')

/**
 * What to print for one participant.
 *
 * The host may not have sent a nickname (an older client, or a page that never
 * asked for one), and a viewer's name is whatever they typed at the gate. The
 * role label is the honest fallback for the host; a numbered viewer is the same
 * fallback the chat uses, so the two lists agree.
 */
function memberName(m: RosterMember): string {
  if (m.name) return m.name
  return m.host ? t('share.live.hostRole') : t('share.live.viewerName', { id: m.id })
}

/**
 * Display name for an identity as it arrives on the wire ('host' or a viewer
 * slot id).
 *
 * The level frames and screen-state carry only the slot id; the nickname lives
 * in the roster. Resolving it here is what keeps the floating "is speaking"
 * chip and the sharing banner naming a person the same way the 成员 list does.
 * The numbered fallback covers a frame that arrives before that member's
 * roster entry (or after they left).
 */
function identityName(id: string): string {
  const m = roster.value.find((x) => x.id === id)
  if (m) return memberName(m)
  return id === 'host' ? t('share.live.hostRole') : t('share.live.viewerName', { id })
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
/** The picture column — the containing block of the dock and the control pill. */
const rootRef = ref<HTMLElement | null>(null)
/**
 * How much height the floating dock takes, in CSS px.
 *
 * MEASURED, not assumed. It used to be a constant (`34dp`, or `66dp` under a
 * `(max-width: 520px)` breakpoint) describing "one row" / "two rows". That
 * cannot survive interface scaling: the buttons are 28dp, so at 1.33× the seven
 * of them no longer fit a window the breakpoint still calls wide, the dock
 * wraps, and the control pill above it — positioned with this number — lands on
 * top of the second row. A breakpoint can only guess where the wrap happens;
 * the dock itself knows.
 *
 * 0 means "not measured yet": the stylesheet's own default then applies, so the
 * first paint is never worse than before.
 */
const dockHeight = ref(0)
let dockResizeObserver: ResizeObserver | null = null
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

const speakerLabel = computed(() => (speaking.value ? identityName(speaking.value) : ''))

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
  // The server's copy, sent only after the peer accepted the request: it is
  // what lets the host be notified while they are not on the meeting page.
  // Sending it first would raise a notification for a request the publisher
  // never received.
  viewer.requestControl()
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

/** The pointer we captured for the open press, so it can be given back. */
let controlPointerId: number | null = null

/**
 * Chrome that belongs to the viewer, not to the host's machine.
 *
 * Remote control forwards every pointer event to the host and captures the
 * pointer on the stage. Anything inside the stage that is really UI (the
 * "结束控制" pill sits there) would then never see its own click — the viewer
 * drives the host's mouse while being unable to press a single local button.
 * Every block of chrome carries `data-local-ui`; interactive elements are
 * covered by the tag selectors so a new button cannot reintroduce the trap.
 */
const LOCAL_UI_SELECTOR =
  '[data-local-ui], button, a, input, textarea, select, label, [role="button"], [role="switch"]'

function isLocalUi(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(LOCAL_UI_SELECTOR) !== null
}

/** Give the pointer back to the page (a stuck capture swallows every click). */
function releaseStageCapture(): void {
  const id = controlPointerId
  controlPointerId = null
  if (id === null) return
  try {
    const el = stageRef.value
    if (el?.hasPointerCapture?.(id)) el.releasePointerCapture(id)
  } catch (e) {
    appLog.d(TAG, 'releasePointerCapture failed', e)
  }
}

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
  releaseStageCapture()
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
  releaseStageCapture()
  const p = normalizedPoint(ev) ?? press
  viewer?.sendControl({ t: 'ctrl-ev', kind: 'up', x: p.x, y: p.y, button: press.button })
}

function onStageDown(ev: PointerEvent): void {
  if (controlState.value === 'granted') {
    // The page's own chrome wins: a press on the "结束控制" pill (or the dock,
    // or the retry button) must stay local. Forwarding it would move the host's
    // mouse instead of pressing the button — and because the stage captures the
    // pointer, the button would never receive the click either, which is how a
    // viewer ends up stuck driving someone else's machine with no way out.
    if (isLocalUi(ev.target)) return
    const p = normalizedPoint(ev)
    if (p) controlPress = { ...p, button: ev.button }
    // Keep the pointer events coming once the cursor leaves the picture, and
    // make sure the matching `up` is not lost: the window listener below is the
    // fallback for a capture that never happened or was lost.
    try {
      stageRef.value?.setPointerCapture?.(ev.pointerId)
      controlPointerId = ev.pointerId
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
    // A drag that started on the picture keeps driving the host even across our
    // chrome; a free move over the chrome is the viewer pointing at OUR UI.
    if (!controlPress && isLocalUi(ev.target)) return
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
    releaseStageCapture()
    sendControlEvent(ev, 'up')
    return
  }
  onPanEnd()
}

function onStageWheel(ev: WheelEvent): void {
  if (controlState.value === 'granted') {
    // Scrolling over our own chrome is not scrolling the host's window.
    if (isLocalUi(ev.target)) return
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
function setSideTab(tab: 'chat' | 'notes' | 'participants'): void {
  if (tab === 'notes' && !notesAvailable.value) return
  sideTab.value = tab
  if (tab === 'notes') startNotesPolling()
  else stopNotesPolling()
  // Reading the chat clears its badge; merely opening the panel on 听记 does not.
  if (tab === 'chat') unread.value = 0
}

function openSide(tab: 'chat' | 'notes' | 'participants'): void {
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
function toggleSide(tab: 'chat' | 'notes' | 'participants'): void {
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
  chatMessages.value = appendChatMessage(chatMessages.value, { ...msg, self: true })
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
    chatMessages.value = appendChatMessage(chatMessages.value, {
      id: msg.id,
      from: msg.from,
      name: msg.name || t('share.live.viewerName', { id: msg.from }),
      text: msg.text,
      at: msg.at,
      self: msg.from === viewer?.viewerId,
    })
    if (!chatOpen.value) unread.value += 1
    return
  }
  if (msg.t === 'ctrl-grant') {
    // The grant is addressed to ONE viewer. Without this check a broadcast
    // grant would put every viewer into "controlling" — their keydown handler
    // would then swallow typing in the chat box. A grant with no id is not
    // "everybody": it is a malformed frame, and treating it as a broadcast is
    // exactly the failure mode this guard exists for.
    if (!msg.viewerId || msg.viewerId !== viewer?.viewerId) return
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
  if (screenSharer.value) {
    return t('share.live.viewingSharer', { name: identityName(screenSharer.value) })
  }
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
      return failReason.value === 'kicked'
        ? t('share.live.kickedTitle')
        : t('share.live.endedTitle')
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
      // Being removed is not the meeting ending: say which one happened, or the
      // viewer will think the host's connection dropped.
      return failReason.value === 'kicked'
        ? t('share.live.kickedDesc')
        : t('share.live.endedDesc')
    case 'full':
      return t('share.live.fullDesc')
    case 'failed':
      // Both of these mean "the peer link was never built" — the socket worked
      // and the media path did not — so they share the advice. 'negotiation' is
      // the watchdog giving up on an answer that never came.
      return failReason.value === 'ice' || failReason.value === 'negotiation'
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
  // The mute state lives on the ELEMENT (imperative, not tracked by Vue), so a
  // re-attach — the publisher swapping source, or a renegotiation — must read it
  // back instead of assuming: the viewer may already have turned the sound on,
  // and re-muting them would be a silent regression.
  hasAudio.value = stream.getAudioTracks().length > 0
  soundMuted.value = el.muted
  void el.play().catch((e) => {
    appLog.w(TAG, 'autoplay blocked', e)
  })
}

/**
 * Flip the sound on or off.
 *
 * Reading `el.muted` back keeps the element the single source of truth: an
 * autoplay block or a browser-side mute can change it without going through
 * this function.
 */
function toggleSound(): void {
  const el = videoRef.value
  if (!el) return
  el.muted = !el.muted
  soundMuted.value = el.muted
  if (el.muted) return
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

/**
 * Mirror one viewer state into the page and run its terminal cleanup.
 *
 * One implementation for both sources of a state change: the viewer's own
 * callback, and the negotiation watchdog (which fails the page without the
 * viewer reporting anything). Doing the cleanup in only one of them left the
 * timed-out path with a live transcript poll and a stale side tab.
 */
function applyViewerState(s: LiveViewerState, reason?: string): void {
  state.value = s
  if (reason) failReason.value = reason
  // The meeting ended — not "you were removed", which is a different fact and
  // keeps its own explanation. The record survives the room, so this is where
  // the member lands: the same four surfaces the host sees.
  if (s === 'ended' && reason !== 'kicked') recordView.value = true
  // The name was refused: back to the gate, with the name still in the field
  // and the reason under it. The fix is a different name, so an overlay with a
  // retry button would send the reader in a circle — retrying the same name is
  // refused identically.
  if (s === 'name-taken') {
    viewer = null
    gateError.value = t('share.live.nameTaken')
    joined.value = false
  }
  if (s === 'ended' || s === 'full' || s === 'failed' || s === 'name-taken') {
    talking.value = false
    sharingScreen.value = false
    screenSharer.value = ''
    memberStream.value = null
    // The room is gone, so the live transcript endpoint stops advancing.
    // Fall back to the chat tab rather than closing the panel: the
    // conversation is still worth reading after the stream drops.
    stopNotesPolling()
    if (sideTab.value === 'notes') sideTab.value = 'chat'
  }
}

/** Gate callback: remember the name and connect. */
function onJoin(name: string): void {
  nick.value = name
  gateError.value = ''
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
  mutedByHost.value = false
  roster.value = []
  // Armed here rather than only in the state watcher: the very first
  // `connecting` is the value the ref was created with, so no watcher callback
  // ever fires for it — and a socket that never opens would then wait forever.
  armNegotiationWatchdog()

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
      onStateChange: (s, detail) => applyViewerState(s, detail?.reason),
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
      onTalkMuted: (muted) => {
        mutedByHost.value = muted
      },
      onJoined: (id) => {
        myViewerId.value = id
      },
      onRoster: (list) => {
        roster.value = list
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
  // The host's block is enforced by the server too; checking here keeps the
  // optimistic flip from flashing "on" for a frame before the refusal arrives.
  if (mutedByHost.value) return
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
 * Track the floating dock's real height.
 *
 * The control pill is positioned above the dock, so it needs the dock's height
 * — and that height is not a constant: the pill wraps to a second row whenever
 * the seven buttons stop fitting, which depends on the window width AND on the
 * interface scale (the buttons are 28dp, so a bigger UI wraps on a wider
 * window). A media query can only guess; observing the element is exact, and it
 * also covers a label appearing or disappearing inside a button.
 *
 * The dock only exists AFTER the viewer joins (before that the page shows the
 * nickname gate), so this cannot run once at mount — it is driven by the same
 * `joined` flag that renders the shell, and it tears down its observer when the
 * shell goes away.
 *
 * `ResizeObserver` is used rather than a window `resize` listener because the
 * wrap can change without the window changing at all (a scale change, a font
 * loading, the brand logo rendering).
 */
function stopObservingDock(): void {
  dockResizeObserver?.disconnect()
  dockResizeObserver = null
  dockHeight.value = 0
}

function observeDock(): void {
  stopObservingDock()
  const dock = rootRef.value?.querySelector<HTMLElement>('.live-dock')
  if (!dock) return
  const sync = () => {
    dockHeight.value = dock.getBoundingClientRect().height
  }
  sync()
  if (typeof ResizeObserver !== 'function') return // jsdom / very old engines
  dockResizeObserver = new ResizeObserver(sync)
  dockResizeObserver.observe(dock)
}

// The shell (and therefore the dock) is created by the join and torn down when
// the meeting ends into the record view.
//
// The teardown must NOT call observeDock: this watcher runs before the DOM is
// patched, so the shell is still in place and observing again would re-attach
// to a subtree that is about to be removed (and write the stale height back
// after it was reset). `post` makes the mount case run after the patch, which
// is when the dock actually exists.
watch(
  joined,
  (isJoined) => {
    if (isJoined) observeDock()
    else stopObservingDock()
  },
  { flush: 'post' },
)

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
    const el = layoutRef.value?.rootEl
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

/**
 * How long a viewer waits for media before giving up on the negotiation.
 *
 * The publisher answers an offer only when it can wire the m-line layout it
 * expects. Anything else — a bundle from a different generation, an SDP the
 * browser refuses, a publisher that silently dropped the frame — leaves the
 * viewer waiting for an `answer` that never comes. There is no failure frame on
 * that path, so without a watchdog the page spins on "协商中…" forever with no
 * retry: the only way out is a reload, which usually reproduces it.
 *
 * 15s is comfortably longer than a LAN handshake (sub-second) plus ICE, and
 * short enough that a stuck viewer is not left guessing.
 */
const NEGOTIATION_TIMEOUT_MS = 15_000
let negotiationTimer: ReturnType<typeof setTimeout> | null = null

function clearNegotiationWatchdog(): void {
  if (negotiationTimer) clearTimeout(negotiationTimer)
  negotiationTimer = null
}

function armNegotiationWatchdog(): void {
  clearNegotiationWatchdog()
  negotiationTimer = setTimeout(() => {
    negotiationTimer = null
    if (state.value !== 'connecting' && state.value !== 'negotiating') return
    // Release the half-open session first: its socket and peer connection will
    // never carry media, and leaving them open means the retry races a second
    // offer on the same room slot.
    viewer?.close()
    viewer = null
    // `negotiation`, not `ice`: nothing was established, so the advice that
    // fits is "the peer link could not be built", not "the network failed".
    applyViewerState('failed', 'negotiation')
  }, NEGOTIATION_TIMEOUT_MS)
}

watch(state, (s) => {
  if (s !== 'playing') talking.value = false
  if (s === 'playing') {
    clearNegotiationWatchdog()
    startStatsPolling()
    onVisibility()
    return
  }
  if (s === 'connecting' || s === 'negotiating') {
    armNegotiationWatchdog()
    return
  }
  // Terminal (or idle): stop both timers. Leaving the stats interval running
  // after the picture ended kept polling a closed peer connection every 3s for
  // as long as the page stayed open.
  clearNegotiationWatchdog()
  stopStatsPolling()
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

function stopStatsPolling(): void {
  if (statsTimer) clearInterval(statsTimer)
  statsTimer = null
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
  // The lock lives in the composable, not in this component: unmounting without
  // releasing it would leave the screen on for the rest of the session.
  wakeLock.release('meeting-viewer')
  stopObservingDock()
  stopStatsPolling()
  clearNegotiationWatchdog()
  // The "is speaking" chip clears itself on a timer; leaving it armed would
  // touch a ref after the view is gone.
  if (speakerTimer) clearTimeout(speakerTimer)
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
  /* Fallback for the dock's height, used only until the measurement in
     `observeDock()` lands (and in jsdom, where nothing is laid out). The real
     value is written inline by the component: the dock wraps to a second row
     once its seven 28dp buttons stop fitting, and that point moves with both
     the window width and the interface scale, so no constant can describe it.
     The control pill is positioned above the dock with this number — see
     `.live-screen-banner.live-control-banner`. */
  --live-dock-height: calc(34 * var(--dp));
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
  /* Opaque on purpose. This covers the stage, and the stage can hold other
     absolutely-positioned content in the same slot (the "no picture" placeholder,
     a stale last frame). A transparent overlay lets that show through and the two
     texts interleave. Same surface as the column behind it, so nothing changes
     visually — it just stops being see-through. */
  background: var(--bg-primary, #0d1117);
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
  padding: calc(6 * var(--dp)) calc(14 * var(--dp));
  border-radius: var(--radius-md, 6px);
  border: 1px solid var(--border-color, #30363d);
  background: var(--bg-secondary, #161b22);
  color: var(--text-primary, #e6edf3);
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
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
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
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
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px) + var(--live-dock-height));
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
  padding: 0 calc(2 * var(--dp));
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
  width: min(calc(360 * var(--dp)), calc(100vw - calc(32 * var(--dp))));
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

/* "Transcription stopped" note above a transcript that outlived it. One muted
   line, no card: the content below is the subject, this only explains why it
   stops growing. */
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
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
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

.live-notes-stopped {
  flex: 0 0 auto;
  padding-bottom: var(--space-3, 6px);
  color: var(--text-muted, #6c757d);
  font-size: var(--font-size-xs, 11px);
}

/* ── 成员 tab ──────────────────────────────────────────────────────────────── */

/* Headcount above the rows: a caption, aligned with the first row's text. The
   pane is a plain flex column with no gap, so the space lives here. */
.live-members-total {
  margin: 0 0 var(--space-3, 6px);
  padding: 0 var(--space-3, 6px);
  color: var(--text-muted, #8b949e);
  font-size: var(--font-size-xs, 11px);
}

.live-members {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.live-member {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-radius: var(--radius-sm, 6px);
  min-width: 0;
}

/* One avatar size for the whole list, through the shared component's own
   variable — the same icon the transcript and the chat draw for that name. */
.live-member__avatar {
  --sa-size: calc(24 * var(--dp));
  flex: 0 0 auto;
}

.live-member__name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary, #e6edf3);
  font-size: var(--font-size-md, 13px);
}

.live-member__chip {
  flex: 0 0 auto;
  padding: 0 var(--space-3, 6px);
  border-radius: var(--radius-full, 999px);
  background: var(--bg-tertiary, #21262d);
  color: var(--text-muted, #8b949e);
  font-size: var(--font-size-2xs, 10px);
  line-height: calc(16 * var(--dp));
}

/* Tinted from the chip's own text colour rather than a fixed background step:
   the 36 themes do not agree on what "one step darker" is. */
.live-member__chip--role {
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 16%, transparent);
}

.live-member__chip--you {
  color: var(--text-primary, #e6edf3);
  background: var(--bg-tertiary, #21262d);
}

.live-member__chip--live {
  color: var(--color-success, #3fb950);
  background: color-mix(in srgb, var(--color-success, #3fb950) 16%, transparent);
}

.live-member__chip--muted {
  color: var(--color-red, #f85149);
  background: color-mix(in srgb, var(--color-red, #f85149) 14%, transparent);
}

.live-members-empty {
  margin: 0;
  color: var(--text-muted, #8b949e);
  font-size: var(--font-size-sm, 12px);
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
