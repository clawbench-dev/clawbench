<template>
  <div class="live-panel">
    <!-- Panel header, in the same shape as the port-forward / forge panels: an
         icon, the tab title, then the live state. The share is a workspace you
         come back to, so its state has to be visible from the tab, not only
         inside a dialog that has been closed. -->
    <div class="live-panel__header">
      <span class="live-panel__icon">
        <MonitorUp :size="14" />
      </span>
      <span class="live-panel__title">{{ t('nav.meeting') }}</span>
      <span v-if="isLive" class="live-panel__live" data-role="live-badge">
        <span class="live-panel__dot" />
        {{ t('liveShare.live') }}
      </span>
      <span class="live-panel__spacer" />
      <button
        v-if="share.state.value === 'publishing'"
        class="live-panel__stop"
        type="button"
        data-action="panel-stop"
        @click="onStop"
      >
        {{ t('liveShare.stop') }}
      </button>
    </div>

    <!-- While publishing the panel is three surfaces, not one long form: the
         setup, the live transcript and the chat. Stacked in a single scroll the
         transcript sat below a wall of controls, and the chat opened as a box
         wedged into the middle of it. -->
    <div v-if="share.state.value === 'publishing'" class="live-tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="live-tab"
        :class="{ 'live-tab--active': activeTab === tab.key }"
        type="button"
        :aria-pressed="activeTab === tab.key"
        :data-live-tab="tab.key"
        @click="selectTab(tab.key)"
      >
        {{ t(tab.labelKey) }}
        <span v-if="tab.key === 'chat' && unread > 0" class="live-count">{{ unread > 9 ? '9+' : unread }}</span>
        <!-- A red dot while the recorder runs: the tab bar is what the user
             looks at while reading the other tabs. -->
        <span
          v-else-if="tab.key === 'notes' && notesOn"
          class="live-tab__rec"
          :title="t('liveShare.notes.listening')"
        />
      </button>
    </div>

    <div class="live-panel__body live-dialog" :class="{ 'live-panel__body--fill': fillTab }">
      <!-- Publishing: three surfaces. 会议配置 holds what is set up once and
           then glanced at (link, tools, audience, permissions); the transcript
           and the chat get their own tabs so each can use the panel's height. -->
        <template v-if="activeTab === 'basic'">
          <!-- Every address the server is reachable on, primary first. A
               machine with Wi-Fi + Ethernet + Docker needs this: the address
               this browser used is not necessarily one a colleague can reach. -->
          <ShareAddressList :links="share.shareLinks.value" />
          <div class="meeting-hint">{{ t('liveShare.linkHint') }}</div>

          <!-- 画面与声音: the SAME heading and the SAME controls as 创建会议.
               The picture is a choice between "nothing" and the actual
               screens/windows — clicking a tile starts or swaps the capture,
               clicking 不共享 stops it. The checkbox that used to live here was
               the same decision split into two controls, and the "切换共享源"
               button opened a second copy of a grid the create page shows
               outright. -->
          <div class="meeting-section">{{ t('meeting.create.av') }}</div>
          <CaptureSourceGrid
            compact
            allow-off
            :sources="captureSources"
            :selected-id="screenSelected"
            :pick-label="pickLabel"
            :pick-busy="loadingSources"
            :disabled="screenBusy"
            @select="onSelectSource"
            @pick="loadSources"
          />
          <div v-if="sourceHint" class="meeting-hint" data-role="stage-sources">
            {{ sourceHint }}
          </div>
          <div v-if="screenHint" class="meeting-hint" data-role="stage-screen-hint">
            {{ screenHint }}
          </div>

          <!-- Camera. Shown whenever the machine has one; a camera the OS cannot
               provide is not something to offer. The self-view is mirrored, like
               every other video tool: you are looking in a mirror, not a photo.
               Same cards as the create form — the off state is a card, so there
               is no separate checkbox to keep in sync with the device choice. -->
          <template v-if="cameraAvailable">
            <div v-if="share.previewCameraStream.value" class="live-camera-self">
              <video
                ref="selfCameraRef"
                class="live-camera-self__video"
                :class="{ 'live-camera-self__video--off': !share.cameraEnabled.value }"
                autoplay
                playsinline
                muted
                data-role="camera-self"
              />
            </div>
            <DeviceChoiceGrid
              kind="video"
              :label="t('meeting.create.cameraDevices')"
              :off-label="t('meeting.create.cameraOffCard')"
              :selected-id="cameraSelected"
              @select="onSelectCamera"
            />
          </template>
          <div v-if="pictureHint" class="meeting-hint" data-role="stage-picture">
            {{ pictureHint }}
          </div>

          <!-- Same cards as the create form, live: putting a headset on mid-call
               should not require ending the meeting, and muting is the first
               card rather than a checkbox above the list. -->
          <DeviceChoiceGrid
            kind="audio"
            :label="t('meeting.create.micDevices')"
            :off-label="t('meeting.create.micOffCard')"
            :selected-id="micSelected"
            @select="onSelectMic"
          />

          <div class="meeting-checks">
            <label class="meeting-check">
              <input type="checkbox" :checked="notesOn" data-role="stage-notes"
                @change="onToggleNotes(($event.target as HTMLInputElement).checked)" />
              <span class="meeting-check__icon"><NotebookPen :size="13" /></span>
              {{ t('meeting.create.notes') }}
            </label>
          </div>
          <div class="meeting-hint meeting-hint--indent">
            {{ notesOn ? t('meeting.create.notesOn') : t('meeting.create.notesOff') }}
          </div>

          <!-- Tools that only make sense while publishing a SCREEN: annotate
               it. A camera picture has no desktop coordinates to draw on, so
               this stays hidden for camera-only meetings. Swapping the shared
               source is the picture grid above — one picker, not a second one
               behind a button. -->
          <template v-if="hasScreen">
            <div class="meeting-section">{{ t('liveShare.toolsSection') }}</div>
            <div class="live-tools">
              <button
                class="live-btn"
                :class="{ 'live-btn--on': isAnnotating }"
                type="button"
                data-action="annotate"
                @click="openAnnotate"
              >
                {{ t('liveShare.annotate') }}
              </button>
            </div>
          </template>

          <div class="meeting-section">
            {{ t('liveShare.audienceCount', { count: share.viewers.value.length }) }}
          </div>
          <div v-if="share.viewers.value.length === 0" class="meeting-hint">
            {{ t('liveShare.noViewers') }}
          </div>
          <ul v-else class="live-viewer-list">
            <li v-for="v in share.viewers.value" :key="v.id" class="live-viewer">
              <span class="live-viewer-id">{{ viewerLabel(v) }}</span>
              <span
                v-if="presence[v.id] === false"
                class="live-viewer-away"
                :title="t('liveShare.viewerAway')"
              >
                {{ t('liveShare.viewerAway') }}
              </span>
              <span class="live-viewer-talk" :class="`live-viewer-talk--${v.talk}`">
                {{ talkLabel(v.talk) }}
              </span>
              <!-- Only speaking viewers need an action: there is no approval
                   queue, so the publisher's controls are "mute this one" and
                   "mute everyone" (below). -->
              <template v-if="controlRequests.has(v.id)">
                <!-- Granting on a platform with no injector would silently do
                     nothing, so say so instead of showing the button. -->
                <span v-if="!controlSupported" class="live-viewer-note" data-role="control-unsupported">
                  {{ t('liveShare.controlUnsupported') }}
                </span>
                <button
                  v-else
                  class="live-btn live-btn--sm"
                  type="button"
                  data-action="control-allow"
                  @click="grantControl(v.id)"
                >
                  {{ t('liveShare.controlAllow') }}
                </button>
                <button class="live-btn live-btn--sm live-btn--danger" type="button" data-action="control-deny" @click="revokeControl(v.id)">
                  {{ t('liveShare.controlDeny') }}
                </button>
              </template>
              <button
                v-else-if="controlledBy === v.id"
                class="live-btn live-btn--sm live-btn--danger"
                type="button"
                data-action="control-stop"
                @click="revokeControl(v.id)"
              >
                {{ t('liveShare.controlStop') }}
              </button>
              <button
                v-if="v.talk === 'talking'"
                class="live-btn live-btn--sm"
                type="button"
                @click="share.revokeTalk(v.id)"
              >
                {{ t('liveShare.mute') }}
              </button>
            </li>
          </ul>

          <div class="meeting-checks">
            <label class="meeting-check">
              <input
                type="checkbox"
                :checked="share.talkEnabled.value"
                @change="onToggleTalk(($event.target as HTMLInputElement).checked)"
              />
              <span class="meeting-check__icon"><Mic :size="13" /></span>
              {{ t('liveShare.allowTalking') }}
            </label>
          </div>
          <div class="meeting-hint meeting-hint--indent">
            {{ t('liveShare.allowTalkingHint') }}
          </div>
          <!-- The publisher hears viewers through its speakers, so its own
               microphone can pick that up and send it back (twice). AEC covers
               part of it; headphones are the only reliable fix. -->
          <div v-if="share.talkEnabled.value" class="meeting-hint meeting-hint--indent">
            {{ t('liveShare.headphoneHint') }}
          </div>
          <button
            v-if="share.talkEnabled.value && hasTalkers"
            class="live-btn live-btn--danger"
            type="button"
            @click="share.muteAll()"
          >
            {{ t('liveShare.muteAll') }}
          </button>

          <!-- Audience screen sharing. Off by default: the token is a read
               credential, and a screen is a much bigger thing to hand out than
               a microphone. -->
          <div class="meeting-checks">
            <label class="meeting-check">
              <input
                type="checkbox"
                :checked="share.screenEnabled.value"
                @change="onToggleScreen(($event.target as HTMLInputElement).checked)"
              />
              <span class="meeting-check__icon"><MonitorUp :size="13" /></span>
              {{ t('liveShare.allowScreenShare') }}
            </label>
          </div>
          <div class="meeting-hint meeting-hint--indent">
            {{ t('liveShare.allowScreenShareHint') }}
          </div>

          <template v-if="share.screenSharer.value">
            <div class="meeting-section">
              {{ t('liveShare.memberSharing', { id: share.screenSharer.value }) }}
            </div>
            <div class="live-screen-preview">
              <video ref="remoteScreenRef" autoplay playsinline muted />
            </div>
            <div class="live-screen-actions">
              <button class="live-btn" type="button" data-action="open-screen-window" @click="openScreenWindow">
                {{ t('liveShare.openInWindow') }}
              </button>
              <button
                class="live-btn live-btn--danger"
                type="button"
                @click="share.revokeScreen(share.screenSharer.value)"
              >
                {{ t('liveShare.stopScreenShare') }}
              </button>
            </div>
          </template>
        </template>

        <!-- Live transcript. One line per speaker turn; the channels are
             recorded separately and merged by timestamp, so a remote
             participant and the publisher appear as different speakers. -->
        <div v-else-if="activeTab === 'notes'" class="live-tab-pane" data-role="notes-pane">
          <div v-if="notes.phase.value !== 'recording'" class="meeting-hint">
            {{ t('liveShare.notes.notEnabled') }}
          </div>
          <template v-else>
            <div v-if="noteBlocks.length === 0" class="meeting-hint">
              {{ t('liveShare.notes.listening') }}
            </div>
            <TranscriptList v-else :blocks="noteBlocks" fill />
          </template>
        </div>

        <div v-else class="live-tab-pane" data-role="chat-pane">
          <div class="live-chat-inline live-chat-inline--fill">
            <LiveChatPanel :messages="chatMessages" @send="sendChat" />
          </div>
        </div>

        <!-- An error belongs to the share, not to a tab: it stays visible
             whichever one is open. -->
        <div v-if="share.error.value" class="live-dialog-error">{{ share.error.value }}</div>

      <!-- Drawing surface lives in its OWN window, never in this panel: the
           ink and the shared picture must not be squeezed into the dialog, and
           an in-panel surface is also impossible to draw on while presenting.
           Hosts without Document PiP get no annotation at all (see
           openAnnotate) rather than a degraded in-panel version. -->
      <Teleport v-if="annotateWindowTarget && share.previewStream.value" :to="annotateWindowTarget">
        <LiveAnnotateOverlay
          :stream="share.previewStream.value"
          @close="closeAnnotate"
          @send="share.sendControl"
        />
      </Teleport>

      <!-- Fullscreen viewer for the member's screen. Rendered inside the dialog
           root but fixed to the viewport, so it is not clipped by the card. -->
      <div
        v-if="screenFullscreen && share.remoteScreenStream.value"
        class="live-screen-fullscreen"
        data-role="screen-fullscreen"
        @click.self="closeScreenFullscreen"
      >
        <video ref="fullscreenVideoRef" autoplay playsinline muted />
        <button
          class="live-screen-fullscreen-close"
          type="button"
          :aria-label="t('common.close')"
          @click="closeScreenFullscreen"
        >
          <X :size="18" />
        </button>
      </div>

      <!-- The member's screen in its own OS window. Document PiP keeps it in
           THIS renderer, which is the only way to show a live MediaStream
           outside the app window: a real BrowserWindow gets a separate JS
           context and cannot see the track at all. -->
      <Teleport v-if="screenWindowTarget" :to="screenWindowTarget">
        <div class="live-window">
          <video ref="windowVideoRef" class="live-window__video" autoplay playsinline muted />
          <div class="live-window__dock">
            <span class="live-window__who" :title="share.screenSharer.value">
              {{ t('liveShare.memberSharing', { id: share.screenSharer.value }) }}
            </span>
            <button class="live-window__btn" type="button" @click="toggleChat">
              {{ t('liveShare.chat') }}
              <span v-if="unread > 0" class="live-count">{{ unread > 9 ? '9+' : unread }}</span>
            </button>
            <button
              class="live-window__btn live-window__btn--danger"
              type="button"
              @click="share.revokeScreen(share.screenSharer.value)"
            >
              {{ t('liveShare.stopScreenShare') }}
            </button>
            <button
              class="live-window__btn live-window__btn--icon"
              type="button"
              :aria-label="t('common.close')"
              @click="closeScreenWindow"
            >
              <X :size="14" />
            </button>
          </div>
          <div v-if="windowChatOpen" class="live-window__chat">
            <LiveChatPanel :messages="chatMessages" @send="sendChat" />
          </div>
        </div>
      </Teleport>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 会议中: the running meeting — link, audience, transcript and chat.
 *
 * The capture session lives in useLiveShare (module-level, one per app
 * instance); this component only drives it and renders state. Leaving the tab
 * does NOT stop the meeting — the dock keeps a "in progress" dot.
 *
 * There is no meeting type. The picture can start or stop at any moment, so the
 * video-only tools (annotate, remote control, member screen share) follow the
 * CURRENT screen state rather than a mode chosen at creation: a button that can
 * only answer "not available" is worse than no button.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Mic, MonitorUp, NotebookPen, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import CaptureSourceGrid from '@/components/live/CaptureSourceGrid.vue'
import DeviceChoiceGrid from '@/components/live/DeviceChoiceGrid.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import TranscriptList from '@/components/live/TranscriptList.vue'
import { useToast } from '@/composables/useToast'
import {
  hasCameraDevice,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { groupNoteSegments, useLiveNotes, withMeetingFileEvents } from '@/composables/useLiveNotes'
import { meetingFileEntries, meetingFilesVersion } from '@/utils/meetingFileTracker'
import { DEFAULT_DEVICE, useMeetingOptions } from '@/composables/useMeetingOptions'
import { useScreenWindow } from '@/composables/useScreenWindow'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { getNative } from '@/utils/clawbenchNative'
import { resolveUserNickname } from '@/utils/userNickname'
import { resolveRemoteControlSupport } from '@/utils/remoteControlSupport'
import { browserScreenCaptureAvailable } from '@/utils/screenCaptureSupport'
import {
  ANDROID_CAPTURE_SOURCE,
  BROWSER_CAPTURE_SOURCE,
  buildCaptureChoices,
  captureSourceIdOf,
} from '@/utils/captureSourceChoices'
import LiveAnnotateOverlay from '@/share/LiveAnnotateOverlay.vue'
import LiveChatPanel, { type ChatMessage } from '@/share/LiveChatPanel.vue'
import { parseControlMessage, toInjectorEvent, type ControlMessage } from '@/share/liveControl'
import { chunkChatHistory } from '@/share/chatHistory'
import { appLog } from '@/utils/appLog'
// The same section / hint / checkbox primitives the create form uses.
import '@/assets/meeting-form.css'

const emit = defineEmits<{ ended: [token: string] }>()

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const notes = useLiveNotes()
const options = useMeetingOptions()
const { serverConfig } = useSettingsConfig()

/**
 * The host's chat name: the account nickname, or the role label when unset.
 * Viewers render the `name` carried on the wire, so this is what identifies the
 * host in their chat and in the archived transcript.
 */
const hostChatName = computed(
  () => resolveUserNickname(serverConfig.value?.user) || t('liveShare.hostName'),
)

/** A capture/publish session is running. */
const isLive = computed(
  () => share.state.value === 'publishing' || share.state.value === 'capturing',
)
/**
 * A SCREEN is being published right now.
 *
 * Everything that assumes desktop coordinates — annotation, remote control,
 * member screen share — hangs off this instead of "there is a picture": a
 * camera image has no desktop behind it, and the flag flips while the meeting
 * runs.
 */
const hasScreen = computed(() => share.captureKind.value !== null)
/** What the microphone cards show: muted is a choice, not a missing device. */
const micSelected = computed(() =>
  share.micEnabled.value ? options.micChoice.value || DEFAULT_DEVICE : '',
)
/** Same for the camera: the cards follow the LIVE state, not the stored one. */
const cameraSelected = computed(() =>
  share.cameraEnabled.value ? options.cameraChoice.value || DEFAULT_DEVICE : '',
)
const notesOn = computed(() => notes.phase.value === 'recording')
/** Whether this machine has a camera at all, so the group is worth showing. */
const cameraAvailable = ref(true)
/** Guards the async start/stop of a screen capture (it opens a picker). */
const screenBusy = ref(false)

const captureSources = ref<CaptureSource[]>([])
const loadingSources = ref(false)

/** A native host that can enumerate screens and windows. */
const hasNativeCapture = computed(() => Boolean(getNative()?.listCaptureSources))
/** Android mirrors through its own consent dialog: no list, one sentinel tile. */
const isAndroidCapture = computed(() => Boolean(getNative()?.startScreenCapture))
/** A plain browser's own picker IS the source choice. */
const browserCapture = browserScreenCaptureAvailable()
const canCapture = computed(() => hasNativeCapture.value || browserCapture)

/**
 * Which tile is on the wire.
 *
 * Read from the live capture rather than from a local "what did I click": the
 * meeting may have been started on the create page, and the grid must mark the
 * picture that is actually going out. The browser and Android hosts have no
 * source id, so their synthetic tile stands in.
 */
const screenSelected = computed(() => {
  if (!hasScreen.value) return ''
  const id = share.captureSourceId.value
  if (id) return id
  if (isAndroidCapture.value) return ANDROID_CAPTURE_SOURCE
  if (browserCapture && !hasNativeCapture.value) return BROWSER_CAPTURE_SOURCE
  return ''
})

/**
 * The tile that asks a host for a list it has not given yet.
 *
 * Only a native host can be in this state: a browser has the synthetic tile
 * above, and Android always has its one sentinel. Clicking re-runs the probe,
 * which is what makes the OS raise its own permission dialog (macOS screen
 * recording, Wayland portal).
 */
const pickLabel = computed(() => {
  if (captureSources.value.length > 0 || !canCapture.value) return ''
  return loadingSources.value ? t('meeting.create.picking') : t('meeting.create.pickSource')
})

/**
 * The one line under the picture grid, when there is nothing to show at all.
 *
 * The grid does NOT scroll: every tile is laid out inline and the panel body is
 * the only scroll container. A nested scroller also cost the grid its automatic
 * minimum size, so the body could squash it to a single clipped row.
 */
const sourceHint = computed(() => {
  if (captureSources.value.length === 0 && canCapture.value && !loadingSources.value) {
    return t('liveShare.noSources')
  }
  return ''
})

/** What the tiles cannot say: whether a screen is going out at all. */
const screenHint = computed(() => {
  if (captureSources.value.length === 0) return ''
  return hasScreen.value ? t('meeting.stage.screenOn') : t('meeting.stage.screenOff')
})

/**
 * The one line under the picture, when — and only when — it says something the
 * cards do not: the LAYOUT (the camera sits in the corner of the screen) and
 * the reason the camera group is missing altogether. Same rule as the create
 * form; "screen only" or "camera only" would just repeat the selection back.
 */
const pictureHint = computed(() => {
  if (!cameraAvailable.value) return t('meeting.create.cameraNoDevice')
  return share.cameraEnabled.value && hasScreen.value ? t('meeting.create.pictureBoth') : ''
})

const remoteScreenRef = ref<HTMLVideoElement | null>(null)
/** The publisher's own camera picture (mirrored by CSS). */
const selfCameraRef = ref<HTMLVideoElement | null>(null)
const fullscreenVideoRef = ref<HTMLVideoElement | null>(null)
const screenFullscreen = ref(false)
const screenWindow = useScreenWindow()
const screenWindowTarget = screenWindow.target
const windowVideoRef = ref<HTMLVideoElement | null>(null)
const annotateWindow = useScreenWindow()
const annotateWindowTarget = annotateWindow.target
const nativeAnnotating = ref(false)
const isAnnotating = computed(() => annotating.value || nativeAnnotating.value)

// ── Tabs ────────────────────────────────────────────────────────────────────
type LiveTab = 'basic' | 'notes' | 'chat'
const activeTab = ref<LiveTab>('basic')
const tabs: Array<{ key: LiveTab; labelKey: string }> = [
  { key: 'basic', labelKey: 'liveShare.tabs.basic' },
  { key: 'notes', labelKey: 'liveShare.tabs.transcript' },
  { key: 'chat', labelKey: 'liveShare.chat' },
]
const fillTab = computed(() => activeTab.value !== 'basic')

function selectTab(key: LiveTab): void {
  activeTab.value = key
  if (key === 'chat') unread.value = 0
}

// ── Tools: annotation, chat, source switch, presence ────────────────────────
const annotating = ref(false)
const windowChatOpen = ref(false)
const unread = ref(0)
const chatMessages = ref<ChatMessage[]>([])
const presence = ref<Record<string, boolean>>({})
const controlledBy = ref('')
const controlRequests = ref<Set<string>>(new Set())
const controlSupported = ref(false)
let controlUnsupportedWarned = false
let unsubscribeControl: (() => void) | null = null
let unsubscribeInk: (() => void) | undefined
let unsubscribeClosed: (() => void) | undefined

function toggleChat(): void {
  windowChatOpen.value = !windowChatOpen.value
  if (windowChatOpen.value) unread.value = 0
}

/**
 * Persist one chat line.
 *
 * Two writers, one function: the host's own messages never pass through
 * `onControl` (they are broadcast directly), so hanging persistence off the
 * inbound handler alone would silently drop every message the host typed.
 */
function persistChat(msg: ControlMessage): void {
  const token = share.token.value
  if (!token || msg.t !== 'chat') return
  const known = msg.from && msg.from !== 'host'
    ? share.viewers.value.find((v) => v.id === msg.from)?.name
    : ''
  const speaker = known || msg.name || (msg.from === 'host' ? hostChatName.value : msg.from || '')
  void fetch('/api/live-share/notes/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      token,
      id: msg.id,
      speaker,
      viewerId: msg.from === 'host' ? '' : msg.from,
      text: msg.text,
    }),
  }).catch((e) => appLog.w('MeetingStage', 'persist chat failed', e))
}

function sendChat(text: string): void {
  const msg: ControlMessage = {
    t: 'chat',
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    from: 'host',
    name: hostChatName.value,
    text,
    at: Date.now(),
  }
  share.sendControl(msg)
  chatMessages.value = [...chatMessages.value, { ...msg, self: true }]
  persistChat(msg)
}

function onControl(msg: ControlMessage): void {
  if (msg.t === 'chat-history-req') {
    // A viewer just opened its control channel. It holds no transcript of its
    // own, so hand it the tail of this meeting's — the same lines, in the same
    // frame shape, so they render identically to live ones.
    if (!msg.from) return
    for (const messages of chunkChatHistory(chatMessages.value)) {
      share.sendControlTo(msg.from, { t: 'chat-history', messages })
    }
    return
  }
  if (msg.t === 'chat') {
    chatMessages.value = [
      ...chatMessages.value,
      {
        id: msg.id,
        from: msg.from,
        // The wire `name` is untrusted (a viewer could label itself "主持人");
        // prefer the nickname the server accepted for that viewer.
        name:
          share.viewers.value.find((v) => v.id === msg.from)?.name ||
          t('share.live.viewerName', { id: msg.from ?? '' }),
        text: msg.text,
        at: msg.at,
        self: false,
      },
    ]
    if (activeTab.value !== 'chat' && !windowChatOpen.value) unread.value += 1
    persistChat(msg)
    return
  }
  if (msg.t === 'presence') {
    const id = msg.from ?? msg.viewerId
    if (id) presence.value = { ...presence.value, [id]: msg.visible }
    return
  }
  if (msg.t === 'ctrl-req') {
    if (msg.from) controlRequests.value = new Set([...controlRequests.value, msg.from])
    return
  }
  if (msg.t === 'ctrl-release') {
    if (!msg.from) return
    if (msg.from === controlledBy.value) revokeControl(msg.from)
    controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== msg.from))
    return
  }
  if (msg.t === 'ctrl-ev') {
    if (!controlledBy.value || msg.from !== controlledBy.value) return
    const bridge = getNative()
    if (!bridge?.remoteControl) {
      if (!controlUnsupportedWarned) {
        controlUnsupportedWarned = true
        toast.show(t('liveShare.controlUnsupported'), { type: 'error', icon: 'warning' })
      }
      return
    }
    const payload = toInjectorEvent(msg)
    if (!payload) return
    void Promise.resolve(bridge.remoteControl(payload)).then((res) => {
      if (res && typeof res === 'object' && !res.supported && !controlUnsupportedWarned) {
        controlUnsupportedWarned = true
        toast.show(t('liveShare.controlUnsupported'), { type: 'error', icon: 'warning' })
      }
    })
  }
}

function grantControl(viewerId: string): void {
  // Only a whole-screen capture has a desktop to map the pointer onto: a window
  // capture puts every click at the wrong place, and a camera picture has no
  // desktop at all. A viewer's request can still arrive from a stale page whose
  // button was hidden, so the refusal lives here and not only in the UI.
  if (share.captureKind.value !== 'screen') {
    toast.show(t('liveShare.controlWindowUnsupported'), { type: 'error', icon: 'warning' })
    return
  }
  controlledBy.value = viewerId
  controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== viewerId))
  share.sendControlTo(viewerId, { t: 'ctrl-grant', viewerId, granted: true })
  void getNative()?.remoteControlGrant?.(viewerId)
}

function revokeControl(viewerId: string): void {
  if (controlledBy.value === viewerId) controlledBy.value = ''
  controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== viewerId))
  share.sendControlTo(viewerId, { t: 'ctrl-grant', viewerId, granted: false })
  void getNative()?.remoteControlRevoke?.()
}

/**
 * Load the tiles the picture grid offers.
 *
 * Same builder as the create form, so the two screens cannot disagree about
 * what a host with no source list should see. The camera probe rides along:
 * it answers the same "what can this machine send" question.
 */
async function loadSources(): Promise<void> {
  if (loadingSources.value) return
  loadingSources.value = true
  try {
    const [list, hasCamera] = await Promise.all([listCaptureSources(), hasCameraDevice()])
    cameraAvailable.value = hasCamera
    captureSources.value = buildCaptureChoices({
      list,
      android: isAndroidCapture.value,
      browser: browserCapture && !hasNativeCapture.value,
      entireScreen: t('liveShare.entireScreen'),
      browserSource: t('meeting.create.browserSource'),
    })
  } catch (e) {
    appLog.e('MeetingStage', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

const hasTalkers = computed(() => share.viewers.value.some((v) => v.talk === 'talking'))

/**
 * Display name per viewer, with a `#2` suffix for duplicates.
 *
 * Nicknames are not unique (the server does not reserve them): two people may
 * both be "Alex". The list is keyed by viewerId, so the suffix only affects
 * what is shown, never who is who.
 */
const viewerLabels = computed(() => {
  const counts = new Map<string, number>()
  const labels: Record<string, string> = {}
  for (const v of share.viewers.value) {
    const base = v.name || v.id
    const n = (counts.get(base) ?? 0) + 1
    counts.set(base, n)
    labels[v.id] = n > 1 ? `${base} #${n}` : base
  }
  return labels
})

function viewerLabel(v: { id: string; name: string }): string {
  return viewerLabels.value[v.id] || v.name || v.id
}

/**
 * Viewer ids restart at v1 for every share, so a grant from the previous share
 * would otherwise authorise the first viewer of the NEXT one.
 */
watch(
  () => share.state.value,
  (next) => {
    if (next === 'publishing') return
    controlledBy.value = ''
    controlRequests.value = new Set()
    presence.value = {}
    chatMessages.value = []
    unread.value = 0
    annotating.value = false
    activeTab.value = 'basic'
    windowChatOpen.value = false
    annotateWindow.close()
    if (nativeAnnotating.value) {
      nativeAnnotating.value = false
      void getNative()?.closeAnnotateOverlay?.()
    }
    void getNative()?.remoteControlRevoke?.()
  },
)

/** Only the tail is rendered: a long meeting would otherwise rebuild thousands
 *  of nodes on every new line. The full transcript still reaches the minutes. */
const noteBlocks = computed(() => {
  // Read the version unconditionally: the tracker is a plain module, so this is
  // the only dependency that makes the panel re-render when a file is opened.
  void meetingFilesVersion.value
  return groupNoteSegments(withMeetingFileEvents(notes.segments.value, meetingFileEntries())).slice(-40)
})

function talkLabel(talk: string): string {
  return talk === 'talking' ? t('liveShare.talkTalking') : t('liveShare.talkMuted')
}

// ── Runtime capture switches ────────────────────────────────────────────────
/**
 * Mute, unmute, or publish from another input.
 *
 * The preference is only written after the capture actually moved: a device
 * that fails to open must leave both the running stream and the stored choice
 * on the microphone that still works.
 */
async function onSelectMic(choice: string): Promise<void> {
  if (!(await share.setMicChoice(choice))) {
    toast.show(share.error.value || t('liveShare.micSwitchFailed'), { type: 'error', icon: 'warning' })
    return
  }
  options.micChoice.value = choice
}

/**
 * One tile, one action — the same grid the create form draws, except that here
 * a click takes effect immediately: a source tile starts or swaps the capture,
 * 不共享 stops it.
 *
 * Clicking the tile that is already live is a no-op, EXCEPT for the browser's
 * synthetic tile: its source was chosen in the browser's own dialog, so asking
 * for "that one again" is the only way to pick a different tab or window. That
 * is what the old 切换共享源 button did.
 */
async function onSelectSource(id: string): Promise<void> {
  if (screenBusy.value) return
  if (id === screenSelected.value && id !== BROWSER_CAPTURE_SOURCE) return
  screenBusy.value = true
  const swapping = hasScreen.value
  try {
    if (id === '') {
      await share.stopScreen()
      return
    }
    const sourceId = captureSourceIdOf(id)
    const ok = swapping
      ? await share.switchSource(sourceId)
      : await share.startScreen(sourceId)
    if (!ok) {
      toast.show(
        share.error.value ||
          (swapping ? t('liveShare.switchSourceFailed') : t('liveShare.shareScreenFailed')),
        { type: 'error', icon: 'warning' },
      )
    }
  } finally {
    screenBusy.value = false
  }
}

/** Turn the camera on, off, or onto another one. */
async function onSelectCamera(choice: string): Promise<void> {
  const previous = options.cameraChoice.value
  if (await share.setCameraDevice(choice)) {
    options.cameraChoice.value = choice
    return
  }
  // A card must not stay selected for a camera that never started.
  options.cameraChoice.value = previous
  toast.show(share.error.value || t('meeting.create.cameraUnavailable'), { type: 'error', icon: 'warning' })
}

function onToggleNotes(on: boolean): void {
  options.notes.value = on
  if (on) {
    if (!share.startNotes()) {
      toast.show(t('meeting.stage.notesUnavailable'), { type: 'error', icon: 'warning' })
      options.notes.value = false
    }
    return
  }
  void share.stopNotes()
}

// ── Lifecycle ───────────────────────────────────────────────────────────────
onMounted(() => {
  unsubscribeControl = share.onControl(onControl)
  unsubscribeInk = getNative()?.onAnnotateInk?.((msg) => {
    const parsed = parseControlMessage(msg)
    if (parsed) share.sendControl(parsed)
  })
  unsubscribeClosed = getNative()?.onAnnotateClosed?.(() => {
    nativeAnnotating.value = false
  })
  void resolveRemoteControlSupport(getNative()).then((ok) => {
    controlSupported.value = ok
    // The room carries the capability too: viewers grey their "request
    // control" button on what the publisher advertises (injector present AND
    // a whole screen), never on their own platform.
    share.setControlSupported(ok)
  })
  if (notes.phase.value === 'recording') void notes.refresh()
  // The source list is what the picture grid draws; the camera probe rides
  // along in the same call, so a machine without a camera does not offer a
  // group that can only fail.
  void loadSources()
})

onBeforeUnmount(() => {
  unsubscribeControl?.()
  unsubscribeControl = null
  unsubscribeInk?.()
  unsubscribeInk = undefined
  unsubscribeClosed?.()
  unsubscribeClosed = undefined
  closeScreenWindow()
  closeAnnotate()
})

/** End the meeting and hand the token to the shell so it can open the record. */
async function onStop(): Promise<void> {
  const token = share.token.value
  await share.stop()
  emit('ended', token)
}

function onToggleTalk(on: boolean): void {
  share.setTalkEnabled(on)
}

function onToggleScreen(on: boolean): void {
  share.setScreenEnabled(on)
}

function openScreenFullscreen(): void {
  screenFullscreen.value = true
}

function closeScreenFullscreen(): void {
  screenFullscreen.value = false
}

async function openScreenWindow(): Promise<void> {
  const stream = share.remoteScreenStream.value
  if (!stream) return
  const result = await screenWindow.open()
  if (result === 'unsupported' || result === 'failed') {
    openScreenFullscreen()
    return
  }
  await nextTick()
  if (windowVideoRef.value) windowVideoRef.value.srcObject = stream
}

function closeScreenWindow(): void {
  screenWindow.close()
}

async function openAnnotate(): Promise<void> {
  const bridge = getNative()
  if (bridge?.openAnnotateOverlay && share.captureKind.value === 'screen') {
    try {
      const res = await bridge.openAnnotateOverlay()
      if (res?.opened) {
        nativeAnnotating.value = true
        return
      }
    } catch (e) {
      appLog.w('MeetingStage', 'annotate overlay failed, using a window', e)
    }
  }
  // No desktop overlay (browser, window source, Android): the drawing surface
  // still gets its OWN window. Never fall back to the panel — the user is
  // presenting, so a surface inside the dialog is both cramped and out of the
  // way of nothing; if no window can be opened, annotation is unavailable.
  const result = await annotateWindow.open({ width: 1200, height: 800 })
  if (result !== 'opened') {
    appLog.w('MeetingStage', 'annotate window unavailable', result)
    toast.show(t('liveShare.annotateNeedsWindow'), { type: 'error', icon: 'warning' })
    return
  }
  annotating.value = true
}

function closeAnnotate(): void {
  annotating.value = false
  annotateWindow.close()
  if (nativeAnnotating.value) {
    nativeAnnotating.value = false
    void getNative()?.closeAnnotateOverlay?.()
  }
}

watch(annotateWindowTarget, (target) => {
  if (!target && annotating.value) annotating.value = false
})

watch(
  () => share.remoteScreenStream.value,
  async (stream) => {
    await nextTick()
    if (remoteScreenRef.value) remoteScreenRef.value.srcObject = stream
    if (fullscreenVideoRef.value) fullscreenVideoRef.value.srcObject = stream
    if (windowVideoRef.value) windowVideoRef.value.srcObject = stream
    if (!stream) {
      screenFullscreen.value = false
      closeScreenWindow()
    }
  },
  { immediate: true },
)

watch(screenFullscreen, async (open) => {
  if (!open) return
  await nextTick()
  if (fullscreenVideoRef.value) {
    fullscreenVideoRef.value.srcObject = share.remoteScreenStream.value
  }
})

// The self-view element is inside a v-if, so it appears AFTER the stream does
// (and disappears with it). Watching both is the only way to catch the case
// where the stream is already there when the element mounts.
watch(
  [() => share.previewCameraStream.value, selfCameraRef],
  async ([stream]) => {
    await nextTick()
    const el = selfCameraRef.value
    if (!el) return
    if (el.srcObject !== stream) el.srcObject = stream
    if (stream) void el.play().catch((e) => appLog.w('MeetingStage', 'camera self-view play failed', e))
  },
  { immediate: true },
)

</script>

<style scoped>
/*
 * The shared .modal-body is padding-free by design (modal-card.css) — each
 * dialog supplies its own. This one did not, so the link field and the source
 * grid sat flush against the card edges.
 *
 * No max-width: the panel is a workspace, and the user asked for the tab pages
 * to use whatever width the dock gives them. A 760px column left the rest of a
 * wide dock empty while the source tiles scrolled inside their own box.
 */
.live-dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
  padding: var(--space-6) var(--space-7);
  /* A floor on the height keeps the card from jumping when the body switches
     between "pick a source" and the (shorter) publishing view. */
  min-height: calc(210 * var(--dp));
}

/* Section headings, hints and checkbox rows are the meeting form primitives —
   assets/meeting-form.css, shared with 创建会议. */

/* A blocking "please wait" state gets the whole card, centred. The dialog
   already reserves a min-height, so this only has to claim that space. */
.live-dialog-loading {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  min-height: calc(170 * var(--dp));
  color: var(--text-muted, #656d76);
}

.live-dialog-error {
  padding: var(--space-4, 8px) var(--space-6, 12px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #ffebe9);
  color: var(--color-red, #cf222e);
  font-size: var(--font-size-sm, 13px);
  /* overflow-wrap (not the deprecated word-break: break-word) so a long error
     message wraps instead of stretching the dialog. */
  overflow-wrap: anywhere;
}

/* ── Audience ──────────────────────────────────────────────────────────── */

.live-viewer-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  max-height: 180px;
  overflow-y: auto;
}

.live-viewer {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-2, 4px) var(--space-4, 8px);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-tertiary, #f6f8fa);
  font-size: var(--font-size-sm, 13px);
}

.live-viewer-id {
  font-family: var(--font-mono, monospace);
  color: var(--text-secondary, #424a53);
}

.live-viewer-note {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.live-viewer-talk {
  flex: 1;
  color: var(--text-muted, #656d76);
  font-size: var(--font-size-xs, 11px);
}

.live-viewer-talk--talking {
  color: var(--color-success, #1a7f37);
  font-weight: 600;
}

.live-viewer-talk--pending {
  color: var(--color-yellow, #9a6700);
}

/* ── In-body actions ───────────────────────────────────────────────────── */

/*
 * Only contextual, small actions live in the body (mute this viewer, mute
 * everyone). Primary/confirm actions use the shared .fbtn in ModalDialog's
 * footer, so they stay consistent with every other dialog.
 */
.live-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-5, 10px);
  border-radius: var(--radius-sm, 4px);
  border: 1px solid color-mix(in srgb, var(--text-secondary, #4b5563) 35%, transparent);
  background: var(--bg-secondary, #fff);
  color: var(--text-secondary, #4b5563);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.6;
  cursor: pointer;
  white-space: nowrap;
  transition:
    background var(--duration-fast, 0.1s) ease,
    border-color var(--duration-fast, 0.1s) ease,
    color var(--duration-fast, 0.1s) ease;
}

.live-btn:hover:not(:disabled) {
  background: var(--bg-hover, rgba(0, 0, 0, 0.04));
  color: var(--text-primary, #1f2328);
}

.live-btn:disabled {
  opacity: 0.55;
  cursor: default;
}

.live-btn--sm {
  padding: 0 var(--space-3, 6px);
  font-size: var(--font-size-xs, 11px);
}

.live-btn--danger {
  border-color: color-mix(in srgb, var(--color-red, #cf222e) 45%, transparent);
  color: var(--color-red, #cf222e);
  background: transparent;
}

.live-btn--danger:hover:not(:disabled) {
  background: color-mix(in srgb, var(--color-red, #cf222e) 10%, transparent);
  color: var(--color-red, #cf222e);
}

/* The "mute everyone" control sits under the audience block, so it needs its
   own left alignment rather than the flex-end the footer uses. */
.live-dialog > .live-btn {
  align-self: flex-start;
}

/* ── Join row ───────────────────────────────────────────────────────────── */

.live-join {
  display: flex;
  gap: var(--space-3, 6px);
}

.live-join__input {
  flex: 1 1 auto;
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border: 1px solid var(--border-color, #d0d7de);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-secondary, #fff);
  color: var(--text-primary, #1f2328);
  font-size: var(--font-size-sm, 13px);
}

/* ── Tool row ───────────────────────────────────────────────────────────── */

.live-tools {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3, 6px);
}

/* A tool that is currently on (annotation mode is running). */
.live-btn--on {
  border-color: color-mix(in srgb, var(--accent-color, #4a90d9) 55%, transparent);
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
}

/* Unread chat count, on the chat tab and in the member's-screen window dock.
   Accent, not red: red means an error or a destructive action in this UI, while
   an unread line is ordinary traffic. */
.live-count {
  min-width: 14px;
  padding: 0 3px;
  margin-left: 4px;
  border-radius: var(--radius-full, 999px);
  background: var(--accent-color, #4a90d9);
  color: #fff;
  /* Scales with the root font size like every other size in the app; there is
     no 10px token, so express it in --dp instead of a literal. */
  font-size: calc(10 * var(--dp));
  line-height: 14px;
  text-align: center;
}

.live-viewer-away {
  color: var(--text-muted, #656d76);
  font-size: var(--font-size-xs, 11px);
}

/* Inline chat inside the dialog: fixed height so the card does not grow with
   the conversation. In its own tab it takes the pane instead. */
/* Sizing wrapper only. It used to draw a bordered card around the chat, which
   stacked a border inside a bordered pane inside a tab — three frames for one
   conversation. The chat now sits on the pane like the transcript does. */
.live-chat-inline {
  height: calc(220 * var(--dp));
  overflow: hidden;
}

/* In the tab the wrapper hugs the chat exactly, so the chat's bottom edge IS
   the wrapper's bottom edge. Clipping there cut the focused input's ring (drawn
   1px outside its box) in half — the field looked like it was bleeding into the
   panel edge. The fixed-height card above is the only variant that needs the
   clip. */
.live-chat-inline--fill {
  flex: 1 1 auto;
  min-height: 0;
  height: auto;
  overflow: visible;
}

/* ── Member screen window (Document PiP) ────────────────────────────────────
   Rendered into a second document; the panel's stylesheet is copied there, so
   these scoped rules apply. Sizes use --dp like the rest of the app. */
.live-window {
  position: fixed;
  inset: 0;
  display: flex;
  flex-direction: column;
  background: #000;
}

.live-window__video {
  flex: 1;
  min-height: 0;
  width: 100%;
  object-fit: contain;
  background: #000;
}

/* Floating dock, mirroring the audience page: it must not eat picture area. */
.live-window__dock {
  position: absolute;
  left: 50%;
  bottom: calc(14 * var(--dp));
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  padding: calc(5 * var(--dp)) calc(8 * var(--dp));
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, #000 62%, transparent);
  border: 1px solid color-mix(in srgb, #fff 16%, transparent);
  box-shadow: 0 calc(6 * var(--dp)) calc(20 * var(--dp)) color-mix(in srgb, #000 45%, transparent);
}

.live-window__who {
  max-width: calc(220 * var(--dp));
  padding: 0 calc(6 * var(--dp));
  color: #fff;
  font-size: var(--font-size-xs, 11px);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.live-window__btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: calc(4 * var(--dp)) calc(10 * var(--dp));
  border: none;
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, #fff 14%, transparent);
  color: #fff;
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

.live-window__btn:hover {
  background: color-mix(in srgb, #fff 24%, transparent);
}

.live-window__btn--danger {
  background: color-mix(in srgb, var(--color-red, #cf222e) 80%, transparent);
}

.live-window__btn--icon {
  padding: calc(4 * var(--dp));
}

.live-window__chat {
  position: absolute;
  right: calc(14 * var(--dp));
  bottom: calc(60 * var(--dp));
  width: calc(280 * var(--dp));
  height: calc(320 * var(--dp));
  border-radius: var(--radius-md, 6px);
  overflow: hidden;
  background: var(--bg-primary, #fff);
  box-shadow: 0 calc(8 * var(--dp)) calc(24 * var(--dp)) color-mix(in srgb, #000 45%, transparent);
}

/* ── Member screen preview ──────────────────────────────────────────────── */

/* 16:9 box so the dialog does not jump when the first frame arrives. */
.live-screen-preview {
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-md, 6px);
  overflow: hidden;
  background: #000;
}

.live-screen-preview video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.live-screen-actions {
  display: flex;
  gap: var(--space-3, 6px);
}

/* Fullscreen overlay. Fixed (not absolute): the dialog card is small and would
   clip it, and the point is to use the whole window. */
.live-screen-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.92);
}

.live-screen-fullscreen video {
  max-width: 96vw;
  max-height: 92vh;
}

.live-screen-fullscreen-close {
  position: absolute;
  top: calc(16 * var(--dp));
  right: calc(16 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(36 * var(--dp));
  height: calc(36 * var(--dp));
  border: none;
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #fff) 20%, transparent);
  color: #fff;
  cursor: pointer;
}

.live-notes-minutes {
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  min-width: 0;
}

/* The report is the tallest thing this dialog ever shows; cap it and scroll
   rather than growing the card past the viewport. */
/* The saved path is long and machine-generated: wrap it instead of widening the
   dialog, and keep it in a monospace face so separators are readable. */
.live-notes-artifacts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.live-notes-file {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #f6f8fa);
  font-size: var(--font-size-xs, 11px);
}

.live-notes-file-label {
  flex: 0 0 auto;
  color: var(--text-muted, #656d76);
}

.live-notes-file-path {
  flex: 1 1 auto;
  min-width: 0;
  font-family: var(--font-mono, ui-monospace, monospace);
  color: var(--text-primary, #1f2328);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  direction: rtl;
  text-align: left;
}

.live-notes-minutes-body {
  max-height: calc(360 * var(--dp));
  overflow-y: auto;
  overscroll-behavior: contain;
  font-size: var(--font-size-sm, 13px);
  line-height: 1.7;
}
/* ── Panel shell ─────────────────────────────────────────────────────────────
   Same skeleton as the port-forward and forge panels: a fixed header strip, a
   scrolling body, a pinned action bar. The share used to be a modal card, where
   the header was the window chrome and the footer floated at the bottom of a
   fixed-height card; in a tab both have to be structural. */

.live-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.live-panel__header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 var(--space-5, 10px) 0 var(--space-6, 12px);
  gap: var(--space-3, 6px);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.live-panel__icon {
  display: flex;
  align-items: center;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.live-panel__title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.live-panel__spacer {
  flex: 1;
}

/* "On air" marker. A dot plus the word rather than the word alone: the tab is
   scanned, not read. Accent-coloured, NOT red — red in this UI means an error
   or a destructive action (the Stop button next to it), while a running share
   is normal ongoing work. */
.live-panel__live {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  height: calc(18 * var(--dp));
  padding: 0 var(--space-3, 6px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-xs, 11px);
  font-weight: var(--font-weight-semibold);
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

.live-panel__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: live-panel-pulse 1.4s ease-in-out infinite;
}

@keyframes live-panel-pulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}

.live-panel__state {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.live-panel__stop {
  height: calc(26 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: 1px solid color-mix(in srgb, var(--color-red, #cf222e) 40%, transparent);
  border-radius: var(--radius-lg, 8px);
  background: transparent;
  color: var(--color-red, #cf222e);
  font-size: var(--font-size-sm, 12px);
  cursor: pointer;
  flex-shrink: 0;
}

.live-panel__stop:hover {
  background: color-mix(in srgb, var(--color-red, #cf222e) 8%, transparent);
}

.live-panel__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

/* Tabs that own their scrolling (transcript, chat) hand the body's height down
   to the pane instead of scrolling it. */
.live-panel__body--fill {
  overflow: hidden;
}

.live-tab-pane {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--space-4, 8px);
  min-height: 0;
}

/* Full width on purpose: the meeting tabs are a workspace, and a centred
   760px column left the rest of a wide dock empty while the transcript and the
   source tiles scrolled inside it. The panel's own padding is the only inset. */
.live-panel__body.live-dialog {
  padding: var(--space-6) var(--space-7);
  min-height: 0;
}

/* ── Tabs ───────────────────────────────────────────────────────────────────
   Underline tabs, the same shape the git panel uses for its sub-tabs: three
   equal columns so the bar reads as a segmented switch rather than three
   unrelated links. */
.live-tabs {
  display: flex;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f6f8fa);
}

.live-tab {
  display: inline-flex;
  flex: 1 1 0;
  align-items: center;
  justify-content: center;
  gap: var(--space-2, 4px);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary, #666);
  font-size: var(--font-size-sm, 12px);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition:
    color var(--duration-fast, 0.1s) ease,
    border-color var(--duration-fast, 0.1s) ease;
}

@media (hover: hover) {
  .live-tab:hover {
    color: var(--text-primary, #1a1a1a);
  }
}

.live-tab--active {
  color: var(--accent-color, #4a90d9);
  border-bottom-color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-semibold);
}

/* Recording marker on the transcript tab: the tab bar is what stays visible
   while the user reads the other tabs, so "still transcribing" has to show
   there. Reuses the header badge's pulse and its accent colour — red means an
   error or a destructive action in this UI, while a running recorder is normal
   ongoing work. */
.live-tab__rec {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent-color, #4a90d9);
  animation: live-panel-pulse 1.4s ease-in-out infinite;
  flex-shrink: 0;
}

/* The publisher's own camera. Mirrored: people expect their self-view to move
   like a mirror, and an un-mirrored preview makes every gesture feel wrong.
   Viewers see the un-mirrored track, which is what a camera should look like. */
.live-camera-self {
  width: calc(200 * var(--dp));
  max-width: 100%;
  aspect-ratio: 4 / 3;
  border-radius: var(--radius-md, 8px);
  background: var(--bg-tertiary, #21262d);
  overflow: hidden;
  border: 1px solid var(--border-color, rgb(255 255 255 / 10%));
}
.live-camera-self__video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  transform: scaleX(-1);
}
/* Camera off: the track is still live but disabled, so the element shows a
   frozen last frame or black. Dimming says "this is off" without pretending
   the picture is still going out. */
.live-camera-self__video--off { opacity: 0.35; }

</style>