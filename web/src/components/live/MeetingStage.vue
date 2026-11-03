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
        <!-- An accent count on 参会者 while somebody is waiting for an answer:
             the request lives in a tab the host is not looking at, and a
             request nobody notices is the same as a refusal. -->
        <span
          v-if="tab.key === 'participants' && pendingApprovals > 0"
          class="live-count"
          data-role="participants-badge"
        >{{ pendingApprovals > 9 ? '9+' : pendingApprovals }}</span>
        <span v-if="tab.key === 'chat' && unread > 0" class="live-count">{{ unread > 9 ? '9+' : unread }}</span>
        <!-- A red dot while the recorder runs: the tab bar is what the user
             looks at while reading the other tabs. -->
        <span
          v-if="tab.key === 'notes' && notesOn"
          class="live-tab__rec"
          :title="t('liveShare.notes.listening')"
        />
      </button>
    </div>

    <div class="live-panel__body live-dialog" :class="{ 'live-panel__body--fill': fillTab }">
      <!-- Publishing: four surfaces. 会议管理 holds what is set up once and
           then glanced at (link, picture, devices, room switches); 参会者 holds
           the people and the actions on them; the transcript and the chat get
           their own tabs so each can use the panel's height. -->
        <template v-if="activeTab === 'basic'">
          <!-- Every address the server is reachable on, primary first. A
               machine with Wi-Fi + Ethernet + Docker needs this: the address
               this browser used is not necessarily one a colleague can reach. -->
          <div class="meeting-section">{{ t('meeting.create.link') }}</div>
          <ShareAddressList :links="share.shareLinks.value" />

          <!-- 画面与声音: the SAME heading and the SAME controls as 创建会议.
               The picture is a choice between "nothing" and the actual
               screens/windows — clicking a tile starts or swaps the capture,
               clicking 不共享 stops it. The checkbox that used to live here was
               the same decision split into two controls, and the "切换共享源"
               button opened a second copy of a grid the create page shows
               outright. -->
          <div class="meeting-section">{{ t('meeting.create.av') }}</div>
          <!-- A member's picture owns a row of its own, above the grid: it is
               not one of the host's sources, it is what is already going out.
               No selected state on it — the accent ring belongs to the host's
               own choice below, and two highlighted boxes would fight. -->
          <div v-if="share.screenSharer.value" class="meeting-subsection">
            {{ t('meeting.create.memberShare') }}
          </div>
          <MemberScreenCard
            v-if="share.screenSharer.value"
            :name="sharerName"
            :stream="share.remoteScreenStream.value"
            :window-available="screenWindowAvailable"
            :window-open="screenWindowOpen"
            @fullscreen="openScreenFullscreen"
            @window="openScreenWindow"
            @stop="share.revokeScreen(share.screenSharer.value)"
          />
          <div
            v-if="share.screenSharer.value && screenWindowOpen"
            class="meeting-hint"
            data-role="screen-in-window"
          >
            {{ t('liveShare.screenInWindow') }}
          </div>
          <div class="meeting-subsection">{{ t('meeting.create.picture') }}</div>
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

          <!-- Camera. The self-view is mirrored, like every other video tool:
               you are looking in a mirror, not a photo. Same frames as the
               create form, and the group stays on screen with no camera at all
               — the off card is still an answer. -->
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
            variant="tile"
            :available="cameraAvailable"
            :label="t('meeting.create.cameraDevices')"
            :off-label="t('meeting.create.cameraOffCard')"
            :selected-id="cameraSelected"
            @select="onSelectCamera"
          />
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

          <!-- What the room allows, in one group: 听记, audience speech and
               audience screen sharing are the same kind of decision ("what may
               happen in this meeting"), so they sit together instead of being
               scattered between the device grids and the audience list. -->
          <div class="meeting-section">{{ t('meeting.create.other') }}</div>
          <div class="meeting-checks">
            <label class="meeting-check">
              <input type="checkbox" :checked="notesOn" data-role="stage-notes"
                @change="onToggleNotes(($event.target as HTMLInputElement).checked)" />
              <span class="meeting-check__icon"><NotebookPen :size="13" /></span>
              {{ t('meeting.create.notes') }}
            </label>
            <!-- Key frames are a separate decision from the transcript: the
                 pictures are stored in this project, and a host may want one
                 without the other. -->
            <label class="meeting-check">
              <input type="checkbox" :checked="shotsOn" data-role="stage-shots"
                @change="onToggleShots(($event.target as HTMLInputElement).checked)" />
              <span class="meeting-check__icon"><ImageIcon :size="13" /></span>
              {{ t('meeting.create.shots') }}
            </label>
            <label class="meeting-check">
              <input
                type="checkbox"
                :checked="share.talkEnabled.value"
                data-role="stage-talk"
                @change="onToggleTalk(($event.target as HTMLInputElement).checked)"
              />
              <span class="meeting-check__icon"><Mic :size="13" /></span>
              {{ t('liveShare.allowTalking') }}
            </label>
            <!-- Off by default: the token is a read credential, and a screen is
                 a much bigger thing to hand out than a microphone. -->
            <label class="meeting-check">
              <input
                type="checkbox"
                :checked="share.screenEnabled.value"
                data-role="stage-screen-share"
                @change="onToggleScreen(($event.target as HTMLInputElement).checked)"
              />
              <span class="meeting-check__icon"><MonitorUp :size="13" /></span>
              {{ t('liveShare.allowScreenShare') }}
            </label>
          </div>
          <button
            v-if="share.talkEnabled.value && hasTalkers"
            class="live-btn live-btn--danger"
            type="button"
            @click="share.muteAll()"
          >
            {{ t('liveShare.muteAll') }}
          </button>
        </template>

        <!-- 参会者: managing people is a different task from setting the meeting
             up, and it is the one surface that may need an answer at any
             moment — which is why its tab carries the pending-approval badge.
             The list scrolls with the panel body (no fill): it is at most nine
             rows, and a nested scroller would only add a second scrollbar. -->
        <div
          v-else-if="activeTab === 'participants'"
          class="live-participants-pane"
          data-role="participants-pane"
        >
          <MeetingParticipants
            :host-name="hostChatName"
            :viewers="share.viewers.value"
            :presence="presence"
            :talk-enabled="share.talkEnabled.value"
            :sharing-id="share.screenSharer.value"
            :control-requests="controlRequests"
            :controlled-by="controlledBy"
            :control-supported="controlSupported"
            @mute="share.setViewerMuted($event, true)"
            @unmute="share.setViewerMuted($event, false)"
            @kick="onKickViewer"
            @stop-screen="share.revokeScreen($event)"
            @allow-control="grantControl"
            @deny-control="revokeControl"
            @revoke-control="revokeControl"
          />
        </div>

        <!-- Live transcript. One line per speaker turn; the channels are
             recorded separately and merged by timestamp, so a remote
             participant and the publisher appear as different speakers. -->
        <div v-else-if="activeTab === 'notes'" class="live-tab-pane" data-role="notes-pane">
          <!-- Two different empty states, both drawn as the shared card (the
               same `MeetingEmpty` the record's tabs and the viewer's panel use):
               "听记 is off" is a setting to change before the next share, while
               "listening" is a live state that fills itself. As one muted line
               in the corner they both read as a pane that failed to render. -->
          <MeetingEmpty
            v-if="notes.phase.value !== 'recording'"
            :icon="Captions"
            :title="t('liveShare.notes.notEnabledTitle')"
            :desc="t('liveShare.notes.notEnabled')"
            data-role="notes-off"
          />
          <template v-else>
            <MeetingEmpty
              v-if="noteBlocks.length === 0"
              :icon="Captions"
              :title="t('liveShare.notes.listening')"
              :desc="t('liveShare.notes.listeningHint')"
              data-role="notes-listening"
            />
            <TranscriptList v-else :blocks="noteBlocks" fill />
          </template>
          <!-- What the record will contain, visible while it happens. Shown
               only once frames exist (or were lost): an empty bar under every
               voice meeting would just be noise. -->
          <div
            v-if="shots.active.value && (shots.count.value > 0 || shots.lost.value > 0)"
            class="live-shot-bar"
            data-role="shots-bar"
          >
            <ImageIcon :size="13" />
            <span>{{ t('meeting.shots.count', { n: shots.count.value }) }}</span>
            <span v-if="shots.lost.value > 0" class="live-shot-bar__lost">
              {{ t('meeting.shots.lost', { n: shots.lost.value }) }}
            </span>
            <button class="meeting-link live-shot-bar__toggle" type="button" data-role="shots-toggle"
              @click="onToggleShots(!shotsOn)">
              {{ shotsOn ? t('meeting.shots.pause') : t('meeting.shots.resume') }}
            </button>
          </div>
        </div>

        <div v-else class="live-tab-pane" data-role="chat-pane">
          <div class="live-chat-inline live-chat-inline--fill">
            <LiveChatPanel :messages="chatMessages" @send="sendChat" />
          </div>
        </div>

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

      <!-- The member's screen, full screen. Teleported to <body> rather than
           rendered inside the dialog: it must cover the whole app, and the
           dock tab it was opened from is hidden the moment the host switches
           tabs. -->
      <Teleport to="body">
        <div
          v-if="screenFullscreen && share.remoteScreenStream.value"
          class="live-screen-fullscreen"
          data-role="screen-fullscreen"
        >
          <MemberScreenSurface
            variant="fullscreen"
            :stream="share.remoteScreenStream.value"
            :sharer="share.screenSharer.value"
            :unread="unread"
            :chat-open="screenChatOpen"
            :messages="chatMessages"
            :note-blocks="noteBlocks"
            :notes-enabled="notesOn"
            :can-switch="screenWindowAvailable"
            @switch="openScreenWindowFromFullscreen"
            @stop="share.revokeScreen(share.screenSharer.value)"
            @close="closeScreenFullscreen"
            @toggle-chat="toggleChat"
            @open-panel="openScreenChat"
            @send-chat="sendChat"
          />
        </div>
      </Teleport>

      <!-- The same picture in its own OS window. Document PiP keeps it in THIS
           renderer, which is the only way to show a live MediaStream outside
           the app window: a real BrowserWindow gets a separate JS context and
           cannot see the track at all. -->
      <Teleport v-if="screenWindowTarget" :to="screenWindowTarget">
        <MemberScreenSurface
          variant="window"
          :stream="share.remoteScreenStream.value"
          :sharer="share.screenSharer.value"
          :unread="unread"
          :chat-open="screenChatOpen"
          :messages="chatMessages"
          :note-blocks="noteBlocks"
          :notes-enabled="notesOn"
          :can-switch="screenWindowAvailable"
          @switch="returnScreenToFullscreen"
          @stop="share.revokeScreen(share.screenSharer.value)"
          @close="closeScreenWindow"
          @toggle-chat="toggleChat"
          @open-panel="openScreenChat"
          @send-chat="sendChat"
        />
      </Teleport>
    </div>

    <!-- The running meeting's actions sit in ONE bar at the foot, like every
         other meeting screen. The header is status only (title + 共享中):
         结束会议 is destructive and global, and 标注画面 is a tool for the
         whole meeting rather than a field of the 会议管理 form it used to live
         in — a host who is presenting should not have to open a setup tab to
         draw on the picture. -->
    <div v-if="share.state.value === 'publishing'" class="meeting-footer" data-role="stage-footer">
      <button class="fbtn fbtn-danger" type="button" data-action="panel-end" @click="onStop">
        <PhoneOff :size="14" />{{ t('meeting.stage.end') }}
      </button>
      <span class="live-panel__spacer" />
      <!-- The only way to keep a frame the triggers missed. Whole-meeting tool,
           like 标注: it belongs in the footer, not in a setup tab. -->
      <button
        v-if="shotsOn"
        class="fbtn"
        type="button"
        data-action="shot-now"
        @click="captureShot"
      >
        <ImageIcon :size="14" />{{ t('meeting.shots.capture') }}
      </button>
      <!-- Drawing only exists for a SCREEN picture: a camera image has no
           desktop coordinates to map the ink onto, so the tool follows the
           current capture rather than a type chosen at creation. -->
      <button
        v-if="hasScreen"
        class="fbtn"
        :class="{ 'fbtn-primary': isAnnotating }"
        type="button"
        data-action="annotate"
        :aria-pressed="isAnnotating"
        @click="openAnnotate"
      >
        <PenLine :size="14" />{{ t('liveShare.annotate') }}
      </button>
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
import { Captions, ImageIcon, Mic, MonitorUp, NotebookPen, PenLine, PhoneOff } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import CaptureSourceGrid from '@/components/live/CaptureSourceGrid.vue'
import MemberScreenCard from '@/components/live/MemberScreenCard.vue'
import DeviceChoiceGrid from '@/components/live/DeviceChoiceGrid.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import TranscriptList from '@/components/live/TranscriptList.vue'
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import MeetingParticipants from '@/components/live/MeetingParticipants.vue'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import {
  hasCameraDevice,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { groupNoteSegments, useLiveNotes, withMeetingFileEvents } from '@/composables/useLiveNotes'
import { useMeetingHistory } from '@/composables/useMeetingHistory'
import { meetingFileEntries, meetingFilesVersion } from '@/utils/meetingFileTracker'
import { DEFAULT_DEVICE, useMeetingOptions } from '@/composables/useMeetingOptions'
import { useMeetingShots } from '@/composables/useMeetingShots'
import { useScreenWindow, ANNOTATE_WINDOW_NAME, SCREEN_WINDOW_NAME } from '@/composables/useScreenWindow'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { getNative, isDesktopApp } from '@/utils/clawbenchNative'
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
import MemberScreenSurface from './MemberScreenSurface.vue'
import { parseControlMessage, toInjectorEvent, type ControlMessage } from '@/share/liveControl'
import { chunkChatHistory, appendChatMessage, trimChat } from '@/share/chatHistory'
import { viewerLabel } from '@/components/live/viewerLabels'
import { appLog } from '@/utils/appLog'
// The same section / hint / checkbox primitives the create form uses.
import '@/assets/meeting-form.css'
// The footer's pills: this panel is not inside ModalDialog, so it loads the
// shared footer-button sheet itself.
import '@/assets/modal-footer-btn.css'

const emit = defineEmits<{ ended: [token: string] }>()
/**
 * Whether this stage is the surface the host is looking at.
 *
 * The shell keeps the component MOUNTED even when it shows the record list (a
 * meeting you leave is still running, and its chat and pending approvals must
 * survive the trip), so "is this rendered" is no longer the same question as
 * "is this visible". Anything that grabs the screen on its own — the member's
 * picture going full screen — has to ask this instead.
 */
const props = withDefaults(defineProps<{ active?: boolean }>(), { active: true })

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const share = useLiveShare()
const notes = useLiveNotes()
const options = useMeetingOptions()
const shots = useMeetingShots()
const { serverConfig } = useSettingsConfig()

/**
 * The host's chat name: the account nickname, or the role label when unset.
 * Viewers render the `name` carried on the wire, so this is what identifies the
 * host in their chat and in the archived transcript.
 */
const hostChatName = computed(
  () => resolveUserNickname(serverConfig.value?.user) || t('liveShare.hostName'),
)

/**
 * Who is sharing, named the way the participant list names them — including the
 * `#2` suffix when two viewers picked the same nickname. The raw viewer id is
 * the fallback: the sharer can already be gone from the roster.
 */
const sharerName = computed(() =>
  viewerLabel(share.viewers.value, share.screenSharer.value),
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
/** Whether frames are being kept right now (running and not paused). */
const shotsOn = computed(() => shots.active.value && !shots.paused.value)
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

/**
 * The one line under the picture, when — and only when — it says something the
 * cards do not.
 *
 * Same rule as the create form: the card groups already show what is being
 * sent, so only the reason a whole group is missing is worth a line.
 */
const pictureHint = computed(() => {
  if (!cameraAvailable.value) return t('meeting.create.cameraNoDevice')
  return ''
})

/** The publisher's own camera picture (mirrored by CSS). */
const selfCameraRef = ref<HTMLVideoElement | null>(null)
const screenFullscreen = ref(false)
const screenWindow = useScreenWindow(SCREEN_WINDOW_NAME)
const screenWindowTarget = screenWindow.target
/** True while the member's screen has a window of its own. */
const screenWindowOpen = computed(() => Boolean(screenWindowTarget.value))
/**
 * Whether this host can hand the picture to an OS window at all.
 *
 * Resolved on mount rather than at import time: the API is a global the host
 * either has or does not, and a button that silently does nothing is worse than
 * no button (Android WebView, Firefox).
 */
const screenWindowAvailable = ref(false)
/**
 * The stream whose picture was opened automatically.
 *
 * Auto-opening happens once per share: a host who closed the picture on purpose
 * must not get it back on its own — the panel keeps the actions for that.
 */
let autoOpenedStream: MediaStream | null = null
const annotateWindow = useScreenWindow(ANNOTATE_WINDOW_NAME)
const annotateWindowTarget = annotateWindow.target
const nativeAnnotating = ref(false)
const isAnnotating = computed(() => annotating.value || nativeAnnotating.value)

// ── Tabs ────────────────────────────────────────────────────────────────────
type LiveTab = 'basic' | 'participants' | 'notes' | 'chat'
const activeTab = ref<LiveTab>('basic')
const tabs: Array<{ key: LiveTab; labelKey: string }> = [
  { key: 'basic', labelKey: 'liveShare.tabs.basic' },
  { key: 'participants', labelKey: 'liveShare.tabs.participants' },
  { key: 'notes', labelKey: 'liveShare.tabs.transcript' },
  { key: 'chat', labelKey: 'liveShare.chat' },
]
/** Panes that own their own scrolling; 会议管理 and 参会者 scroll as a form. */
const fillTab = computed(() => activeTab.value === 'notes' || activeTab.value === 'chat')

/**
 * Pending approvals shown on the 参会者 tab.
 *
 * Only remote-control requests need an answer today; keeping the count derived
 * from the request set means a future approval type joins the badge for free.
 */
const pendingApprovals = computed(() => controlRequests.value.size)

function selectTab(key: LiveTab): void {
  activeTab.value = key
  if (key === 'chat') unread.value = 0
}

// ── Tools: annotation, chat, source switch, presence ────────────────────────
const annotating = ref(false)
const screenChatOpen = ref(false)
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
  screenChatOpen.value = !screenChatOpen.value
  if (screenChatOpen.value) unread.value = 0
}

/** A pane button in the member-screen dock asked for the panel (e.g. 听记). */
function openScreenChat(): void {
  screenChatOpen.value = true
  unread.value = 0
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
  })
    .then((resp) => {
      // A refused write is not a transport error, so `.catch` alone never saw
      // it: a chat row lost to a busy database or an expired token looked
      // exactly like one that was saved.
      if (!resp.ok) throw new Error(`status ${resp.status}`)
    })
    .catch((e) => appLog.w('MeetingStage', 'persist chat failed', e))
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
  // Bounded: a meeting runs for hours and every line stays in this array until
  // the stage unmounts.
  chatMessages.value = appendChatMessage(chatMessages.value, { ...msg, self: true })
  persistChat(msg)
}

/**
 * Bring back the lines this client never saw arrive.
 *
 * Live lines only exist in this component's array, so anything archived before
 * it mounted (the app was reloaded mid-meeting) or by another host device would
 * be missing from the pane forever. The stored rows carry the same client id
 * the live ones use, which is what makes the merge idempotent.
 */
let chatHistoryGen = 0
async function loadChatHistory(): Promise<void> {
  const token = share.token.value
  if (!token) return
  const gen = ++chatHistoryGen
  try {
    const { messages } = await useMeetingHistory().chat(token)
    // The share may have ended, or another one started, while this was in
    // flight: its rows are not ours to append.
    if (gen !== chatHistoryGen || share.token.value !== token) return
    const known = new Set(chatMessages.value.map((m) => m.id))
    const restored = messages
      .filter((m) => m.clientId && !known.has(m.clientId))
      .map((m) => ({
        id: m.clientId,
        from: m.viewerId || 'host',
        name: m.speaker,
        text: m.text,
        at: m.at,
        // Only the host's own lines are mirrored to the right; `viewerId` is
        // empty exactly for those.
        self: !m.viewerId,
      }))
    if (restored.length === 0) return
    chatMessages.value = trimChat([...restored, ...chatMessages.value].sort((a, b) => a.at - b.at))
  } catch (e) {
    appLog.d('MeetingStage', 'chat history load failed', e)
  }
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
    chatMessages.value = appendChatMessage(chatMessages.value, {
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
    })
    if (activeTab.value !== 'chat' && !screenChatOpen.value) unread.value += 1
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
    const inject = bridge.remoteControl
    // The injection runs in the shell's input helper, which can refuse (no
    // permission, security software, a helper that failed to start). Every
    // mouse move would otherwise become an unhandled rejection and the control
    // would fail silently.
    void (async () => {
      const res = await inject(payload)
      if (res && typeof res === 'object' && !res.supported && !controlUnsupportedWarned) {
        controlUnsupportedWarned = true
        toast.show(t('liveShare.controlUnsupported'), { type: 'error', icon: 'warning' })
      }
    })().catch((e) => {
      appLog.w('MeetingStage', 'remote control injection failed', e)
      if (!controlUnsupportedWarned) {
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
 * Remove a viewer from the meeting.
 *
 * Confirmed first: the action is not undoable (the link works again, but they
 * lose their place in the chat and any pending control request). The name is in
 * the question so a mis-aimed click on a narrow row is caught before it lands.
 */
async function onKickViewer(viewerId: string): Promise<void> {
  const name = share.viewers.value.find((v) => v.id === viewerId)?.name || viewerId
  const ok = await dialog.confirm(t('liveShare.participant.kickConfirm', { name }), {
    dangerous: true,
  })
  if (!ok) return
  if (controlledBy.value === viewerId) controlledBy.value = ''
  controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== viewerId))
  share.kickViewer(viewerId)
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
 * Viewer ids restart at v1 for every share, so a grant from the previous share
 * would otherwise authorise the first viewer of the NEXT one.
 */
watch(
  () => share.state.value,
  (next) => {
    if (next === 'publishing') return
    // A history request that was in flight must not repopulate a finished
    // meeting's chat.
    chatHistoryGen += 1
    controlledBy.value = ''
    controlRequests.value = new Set()
    presence.value = {}
    chatMessages.value = []
    unread.value = 0
    annotating.value = false
    activeTab.value = 'basic'
    screenChatOpen.value = false
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

/**
 * Turning frames off PAUSES rather than tears down: the count and the
 * de-duplication state survive, so turning it back on does not restart the
 * hourly quota or forget the last frame.
 */
function onToggleShots(on: boolean): void {
  options.shots.value = on
  if (!on) {
    shots.pause()
    return
  }
  if (shots.active.value) shots.resume()
  else share.startShots()
}

/** Keep the picture on screen right now (the triggers may have missed it). */
function captureShot(): void {
  void shots.captureNow('manual')
}

// ── Lifecycle ───────────────────────────────────────────────────────────────
onMounted(() => {
  // The desktop shell can always give the picture a real window (a popup is an
  // Electron BrowserWindow); browsers need Document PiP for it.
  screenWindowAvailable.value =
    isDesktopApp() ||
    Boolean(
      (globalThis as unknown as { documentPictureInPicture?: { requestWindow?: unknown } })
        .documentPictureInPicture?.requestWindow,
    )
  unsubscribeControl = share.onControl(onControl)
  unsubscribeInk = getNative()?.onAnnotateInk?.((msg) => {
    const parsed = parseControlMessage(msg)
    if (parsed) share.sendControl(parsed)
  })
  unsubscribeClosed = getNative()?.onAnnotateClosed?.(() => {
    nativeAnnotating.value = false
    // The strokes are gone with the window; a mirror left behind would be
    // composited into the next key frame.
    share.clearInkMirror()
  })
  void resolveRemoteControlSupport(getNative()).then((ok) => {
    controlSupported.value = ok
    // The room carries the capability too: viewers grey their "request
    // control" button on what the publisher advertises (injector present AND
    // a whole screen), never on their own platform.
    share.setControlSupported(ok)
  })
  if (notes.phase.value === 'recording') void notes.refresh()
  // The stage stays mounted across the meeting list, so this only matters for
  // the lines archived before it existed (a reload, or another host device).
  void loadChatHistory()
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

/**
 * Detach the member's screen into its own OS window.
 *
 * Only ever called from a click: `requestWindow` requires transient user
 * activation, and a member starting to share is not one. When the host cannot
 * open a window at all, the fullscreen picture (where these buttons live) is
 * already the fallback, so there is nothing to do.
 */
async function openScreenWindow(): Promise<void> {
  if (!share.remoteScreenStream.value) return
  const result = await screenWindow.open()
  if (result !== 'opened') {
    // Say so instead of leaving a button that appears to do nothing: the
    // picture stays fullscreen, which is where the host already is.
    toast.show(t('liveShare.screenWindowFailed'), { type: 'error', icon: 'warning' })
    openScreenFullscreen()
  }
}

/** The dock's "open in a window": hand the picture over, then drop the overlay. */
async function openScreenWindowFromFullscreen(): Promise<void> {
  await openScreenWindow()
  // openScreenWindow falls back to the overlay when no window came up, so the
  // overlay only goes away if there is something else showing the picture.
  if (screenWindowOpen.value) closeScreenFullscreen()
}

/** The window dock's "full screen": the mirror of the move above. */
function returnScreenToFullscreen(): void {
  closeScreenWindow()
  openScreenFullscreen()
}

function closeScreenWindow(): void {
  screenWindow.close()
}

/**
 * Open the drawing surface — or bring it back.
 *
 * This button is the host's only handle on a surface that lives outside the
 * panel (an OS overlay, or the surface's own window), so a click always has to
 * end with that surface visible. Re-clicking while it is open is therefore not
 * a no-op: the desktop overlay is asked to show its toolbar again (it hides
 * itself after a couple of seconds on the platforms that capture it), and the
 * window is raised. The same call re-opens the window when it was closed behind
 * our back, instead of returning "already open" for a document that is gone.
 */
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
  share.clearInkMirror()
  if (nativeAnnotating.value) {
    nativeAnnotating.value = false
    void getNative()?.closeAnnotateOverlay?.()
  }
}

watch(annotateWindowTarget, (target) => {
  if (!target && annotating.value) annotating.value = false
})

// The drawing surface has nothing to draw on once the picture is gone: close it
// instead of leaving an empty window behind (the button that could reopen it is
// hidden by then, so a blank window would be a dead end).
watch(
  () => share.previewStream.value,
  (stream) => {
    if (!stream && isAnnotating.value) closeAnnotate()
  },
)

/**
 * The member's picture goes full screen as soon as it arrives — but only while
 * this stage is what the host is looking at. The component now stays mounted
 * behind the record list, and an overlay there would cover the list the host is
 * reading.
 */
function maybeOpenRemoteScreen(): void {
  const stream = share.remoteScreenStream.value
  if (!stream) {
    autoOpenedStream = null
    screenFullscreen.value = false
    closeScreenWindow()
    return
  }
  if (!props.active) return
  // The picture comes to the host, not the other way round: fill the screen as
  // soon as the member starts sharing instead of waiting for a click.
  if (autoOpenedStream === stream) return
  autoOpenedStream = stream
  openScreenFullscreen()
}

watch(() => share.remoteScreenStream.value, maybeOpenRemoteScreen, { immediate: true })
// The full-screen overlay is a `Teleport to="body"`, so `v-show` cannot hide it:
// leaving the stage for the record list would leave a black full-screen picture
// on top of it, with only its own close button as a way out (the Android back
// gesture lands on the list, not on the overlay). Closing it here is what the
// old unmount used to do by accident.
//
// `autoOpenedStream` is cleared with it so coming back re-opens the picture the
// host was watching: keeping the mark would make the "already auto-opened"
// guard skip the reopen.
watch(
  () => props.active,
  (active) => {
    if (active) {
      maybeOpenRemoteScreen()
      return
    }
    if (screenFullscreen.value) closeScreenFullscreen()
    autoOpenedStream = null
  },
)

// A different meeting is a different chat: pull its archived lines once the
// token exists (the stage mounts before the room does).
watch(
  () => share.token.value,
  (token) => {
    if (token) void loadChatHistory()
  },
)


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

/* Unread chat count, on the chat tab and in the member's-screen window dock.
   Accent, not red: red means an error or a destructive action in this UI, while
   an unread line is ordinary traffic. */
.live-count {
  /* Every geometry in --dp: the font size already scaled, so a bare px box
     overflowed the pill as soon as the interface scale went above 1. */
  min-width: calc(14 * var(--dp));
  padding: 0 calc(3 * var(--dp));
  margin-left: calc(4 * var(--dp));
  border-radius: var(--radius-full, 999px);
  background: var(--accent-color, #4a90d9);
  color: #fff;
  /* Scales with the root font size like every other size in the app; there is
     no 10px token, so express it in --dp instead of a literal. */
  font-size: calc(10 * var(--dp));
  line-height: calc(14 * var(--dp));
  text-align: center;
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

/* ── Member screen ───────────────────────────────────────────────────────── */

/* The picture itself is MemberScreenSurface — the same component fills the
   screen and the detached window. The panel's handle on it is
   MemberScreenCard, which draws its own row above the source grid. */

/* Fullscreen frame for the shared picture: the whole viewport, above the app.
   Teleported to <body> because a dock tab is hidden the moment the host
   switches tabs, and the picture must survive that. */
.live-screen-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  background: #000;
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
   or a destructive action (结束会议 in the footer), while a running share is
   normal ongoing work. */
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

/* The running meeting's action bar: the same flat pill bar every other meeting
   screen uses (design-guide §按钮 — a footer is not a second panel colour).
   Spacer-separated like 记录详情: the destructive action sits at the left edge,
   the tool at the right. It scrolls sideways rather than wrapping, because the
   left dock can be dragged down to 320px. */
.meeting-footer {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  justify-content: safe flex-end;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color);
  background: var(--bg-primary);
  flex-shrink: 0;
  overflow-x: auto;
  overflow-y: hidden;
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

/* The frame counter under the live transcript: one quiet line, not a panel. */
.live-shot-bar {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  gap: var(--space-2, 4px);
  padding-top: var(--space-2, 4px);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.live-shot-bar__lost {
  color: var(--color-orange, #d97706);
}

.live-shot-bar__toggle {
  margin-left: auto;
}

/* The participant list scrolls with the panel body rather than owning a
   scroller: `flex: none` keeps it at its natural height so the body scrolls
   instead of the row list being squashed into an invisible overflow. */
.live-participants-pane {
  display: flex;
  flex: none;
  flex-direction: column;
  gap: var(--space-4, 8px);
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
  /* Four labels do not fit a 320px dock in every language. The bar scrolls
     sideways instead of wrapping a label onto a second line (the tab is a
     fixed 34dp tall), and the scrollbar itself stays hidden. */
  overflow-x: auto;
  scrollbar-width: none;
}

.live-tabs::-webkit-scrollbar {
  display: none;
}

.live-tab {
  display: inline-flex;
  /* Grow to share the width when there is room, never shrink below the label:
     a squeezed label wraps out of the fixed-height bar. */
  flex: 1 1 auto;
  align-items: center;
  justify-content: center;
  gap: var(--space-2, 4px);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5, 10px);
  white-space: nowrap;
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