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
      <span class="live-panel__title">{{ t('nav.liveShare') }}</span>
      <span v-if="isLive" class="live-panel__live" data-role="live-badge">
        <span class="live-panel__dot" />
        {{ t('liveShare.live') }}
      </span>
      <span v-else-if="phase === 'minutes'" class="live-panel__state">
        {{ t('liveShare.notes.minutesTitle') }}
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
          v-else-if="tab.key === 'notes' && notes.phase.value === 'recording'"
          class="live-tab__rec"
          :title="t('liveShare.notes.listening')"
        />
      </button>
    </div>

    <div class="live-panel__body live-dialog" :class="{ 'live-panel__body--fill': fillTab }">
      <!-- Idle: pick a source and options, then start. -->
      <template v-if="share.state.value === 'idle' && phase !== 'minutes'">
        <div v-if="!supported" class="live-dialog-warn">
          {{ hasNativeCapture ? t('liveShare.unsupported') : t('liveShare.captureNeedsClient') }}
        </div>

        <template v-else>
          <div class="live-dialog-section">{{ t('liveShare.pickSource') }}</div>
          <!-- Android: the source is whatever the system dialog ends up
               mirroring, so there is nothing to pick. Showing a one-tile grid
               would suggest a choice that does not exist. -->
          <div v-if="isAndroidCapture" class="live-dialog-hint">
            {{ t('liveShare.androidCaptureHint') }}
          </div>
          <div v-else-if="loadingSources" class="live-dialog-hint">
            <LoadingIndicator size="sm" inline />
          </div>
          <div v-else-if="captureSources.length === 0" class="live-dialog-hint">
            {{ t('liveShare.noSources') }}
          </div>
          <div v-else class="live-source-grid">
            <button
              v-for="s in captureSources"
              :key="s.id"
              class="live-source"
              :class="{ 'live-source--active': s.id === selectedSourceId }"
              type="button"
              :data-source-id="s.id"
              @click="selectedSourceId = s.id"
            >
              <img v-if="s.thumbnail" class="live-source-thumb" :src="s.thumbnail" :alt="s.name" />
              <div v-else class="live-source-thumb live-source-thumb--empty" />
              <span class="live-source-name" :title="s.name">{{ s.name }}</span>
            </button>
          </div>

          <label class="live-check">
            <input v-model="withSystemAudio" type="checkbox" :disabled="!systemAudioSupported" />
            <span>{{ t('liveShare.includeSystemAudio') }}</span>
          </label>
          <div v-if="!systemAudioSupported" class="live-dialog-hint live-dialog-hint--indent">
            {{ t('liveShare.systemAudioUnavailable') }}
          </div>
          <div
            v-else-if="isAndroidCapture && withSystemAudio"
            class="live-dialog-hint live-dialog-hint--indent"
          >
            {{ t('liveShare.androidSystemAudioHint') }}
          </div>

          <label class="live-check">
            <input v-model="withMicrophone" type="checkbox" />
            <span>{{ t('liveShare.includeMicrophone') }}</span>
          </label>

          <!-- AI 听记. On by default: the user opened this dialog to share, and
               a meeting that loses its transcript because a box was unchecked
               is a worse outcome than one that has one. Viewers see that 听记 is
               running (the panel is offered to them), so nobody is recorded
               silently. -->
          <label class="live-check">
            <input v-model="withNotes" type="checkbox" />
            <span>{{ t('liveShare.notes.enable') }}</span>
          </label>
          <div class="live-dialog-hint live-dialog-hint--indent">
            {{ t('liveShare.notes.enableHint') }}
          </div>

          <!-- Joining someone else's share from the desktop client. In a
               browser the same link works, but without screen capture (no
               secure context over plain HTTP) and without remote control.
               Android has no `openShareLink` bridge: the entry would only ever
               answer "unsupported", so it is not offered there. -->
          <template v-if="!isAndroidCapture">
            <div class="live-dialog-section">{{ t('liveShare.joinTitle') }}</div>
            <div class="live-dialog-hint">{{ t('liveShare.joinHint') }}</div>
            <div class="live-join">
              <input
                v-model="joinUrl"
                class="live-join__input"
                type="url"
                :placeholder="t('liveShare.joinPlaceholder')"
                data-role="join-url"
                @keydown.enter="joinShare"
              />
              <button
                class="live-btn"
                type="button"
                :disabled="!joinUrl.trim()"
                data-action="join-share"
                @click="joinShare"
              >
                {{ t('liveShare.joinAction') }}
              </button>
            </div>
            <div v-if="joinError" class="live-dialog-error">{{ joinError }}</div>
          </template>
        </template>
      </template>

      <!-- Capturing: brief, but a spinner beats a frozen dialog. Centered in
           the card — as a plain hint it rendered as 11px text hugging the
           top-left corner of a mostly empty panel. -->
      <div v-else-if="share.state.value === 'capturing'" class="live-dialog-loading">
        <LoadingIndicator :label="t('liveShare.starting')" size="lg" />
      </div>

      <!-- Minutes: shown after stopping a share that had 听记 enabled. The
           dialog deliberately stays open — the transcript exists nowhere else
           in the UI, and closing it would throw the meeting away. -->
      <div v-else-if="phase === 'minutes'" class="live-notes-minutes">
        <div class="live-dialog-section">{{ t('liveShare.notes.minutesTitle') }}</div>
        <div v-if="notes.phase.value === 'finalizing'" class="live-dialog-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.pending.value > 0
              ? t('liveShare.notes.uploading', { n: notes.pending.value })
              : t('liveShare.notes.finalizing')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'generating'" class="live-dialog-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.revisePhase.value === 'generating'
              ? t('liveShare.notes.proofreading')
              : t('liveShare.notes.generating')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'failed'" class="live-dialog-error">
          {{ notes.error.value || t('liveShare.notes.generateFailed') }}
        </div>
        <div
          v-else-if="notes.minutes.value"
          class="live-notes-minutes-body markdown-body"
          v-html="renderedMinutes"
        ></div>
        <div v-else class="live-dialog-hint">{{ t('liveShare.notes.empty') }}</div>

        <!-- The proofread pass failed but the minutes still exist: say so,
             otherwise the summary silently describes a transcript nobody
             corrected. -->
        <div
          v-if="notes.minutesPhase.value === 'ready' && notes.revisePhase.value === 'failed'"
          class="live-dialog-warn"
          data-role="proofread-failed"
        >
          {{ t('liveShare.notes.proofreadFailed') }}
        </div>

        <!-- Where the durable copies live. The database rows expire; these files
             do not, and the user can edit or commit them. -->
        <div v-if="artifactRows.length" class="live-notes-artifacts" data-role="artifacts">
          <div class="live-notes-file-label">{{ t('liveShare.notes.savedTo') }}</div>
          <div v-for="row in artifactRows" :key="row.key" class="live-notes-file">
            <span class="live-notes-file-label">{{ row.label }}</span>
            <span class="live-notes-file-path" :title="row.path">{{ row.path }}</span>
            <button class="live-btn live-btn--sm" type="button" @click="copyArtifactPath(row.path)">
              {{ t('liveShare.notes.copyPath') }}
            </button>
          </div>
        </div>

        <!-- Never present a partial transcript as a complete meeting. -->
        <div v-if="notes.lost.value > 0" class="live-dialog-warn">
          {{ t('liveShare.notes.lostSegments', { n: notes.lost.value }) }}
        </div>
        <div v-else-if="notes.truncated.value" class="live-dialog-warn">
          {{ t('liveShare.notes.truncated') }}
        </div>
      </div>

      <!-- Publishing: three surfaces. 基础配置 holds what is set up once and
           then glanced at (link, tools, audience, permissions); the transcript
           and the chat get their own tabs so each can use the panel's height. -->
      <template v-else>
        <template v-if="activeTab === 'basic'">
          <!-- Every address the server is reachable on, primary first. A
               machine with Wi-Fi + Ethernet + Docker needs this: the address
               this browser used is not necessarily one a colleague can reach. -->
          <ShareAddressList :links="share.shareLinks.value" />
          <div class="live-dialog-hint">{{ t('liveShare.linkHint') }}</div>

          <!-- Tools that only make sense while publishing: annotate the shared
               screen or swap which window is being shared. Chat is a tab of its
               own, so it is not offered again here. -->
          <div class="live-dialog-section">{{ t('liveShare.toolsSection') }}</div>
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
            <button
              v-if="!isAndroidCapture"
              class="live-btn"
              type="button"
              data-action="switch-source"
              @click="openSwitch"
            >
              {{ t('liveShare.switchSource') }}
            </button>
          </div>

          <!-- Source picker for a mid-share swap. Hidden until asked for: the
               panel is already dense, and switching is rare. -->
          <div v-if="switching" class="live-source-grid live-source-grid--compact">
            <button
              v-for="src in captureSources"
              :key="src.id"
              class="live-source"
              :class="{ 'live-source--selected': src.id === selectedSourceId }"
              type="button"
              :disabled="switchingBusy"
              @click="applySwitch(src.id)"
            >
              {{ src.name }}
            </button>
          </div>

          <div class="live-dialog-section">
            {{ t('liveShare.audienceCount', { count: share.viewers.value.length }) }}
          </div>
          <div v-if="share.viewers.value.length === 0" class="live-dialog-hint">
            {{ t('liveShare.noViewers') }}
          </div>
          <ul v-else class="live-viewer-list">
            <li v-for="v in share.viewers.value" :key="v.id" class="live-viewer">
              <span class="live-viewer-id">{{ v.id }}</span>
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

          <label class="live-check">
            <input
              type="checkbox"
              :checked="share.talkEnabled.value"
              @change="onToggleTalk(($event.target as HTMLInputElement).checked)"
            />
            <span>{{ t('liveShare.allowTalking') }}</span>
          </label>
          <div class="live-dialog-hint live-dialog-hint--indent">
            {{ t('liveShare.allowTalkingHint') }}
          </div>
          <!-- The publisher hears viewers through its speakers, so its own
               microphone can pick that up and send it back (twice). AEC covers
               part of it; headphones are the only reliable fix. -->
          <div v-if="share.talkEnabled.value" class="live-dialog-hint live-dialog-hint--indent">
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
          <label class="live-check">
            <input
              type="checkbox"
              :checked="share.screenEnabled.value"
              @change="onToggleScreen(($event.target as HTMLInputElement).checked)"
            />
            <span>{{ t('liveShare.allowScreenShare') }}</span>
          </label>
          <div class="live-dialog-hint live-dialog-hint--indent">
            {{ t('liveShare.allowScreenShareHint') }}
          </div>

          <template v-if="share.screenSharer.value">
            <div class="live-dialog-section">
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
          <div v-if="notes.phase.value !== 'recording'" class="live-dialog-hint">
            {{ t('liveShare.notes.notEnabled') }}
          </div>
          <template v-else>
            <div v-if="noteBlocks.length === 0" class="live-dialog-hint">
              {{ t('liveShare.notes.listening') }}
            </div>
            <div v-else ref="notesListRef" class="live-notes-list live-notes-list--fill">
              <div v-for="b in noteBlocks" :key="b.key" class="live-note-line">
                <span class="live-note-speaker">{{ b.speaker }}</span>
                <span class="live-note-text">{{ b.text }}</span>
              </div>
            </div>
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
      </template>

      <!-- Drawing surface in its own window, so the ink and the shared picture
           are never squeezed into the panel. Hosts without Document PiP fall
           back to the in-panel overlay below. -->
      <Teleport v-if="annotateWindowTarget && annotating && share.previewStream.value" :to="annotateWindowTarget">
        <LiveAnnotateOverlay
          :stream="share.previewStream.value"
          @close="closeAnnotate"
          @send="share.sendControl"
        />
      </Teleport>
      <LiveAnnotateOverlay
        v-else-if="annotating && share.previewStream.value"
        :stream="share.previewStream.value"
        @close="closeAnnotate"
        @send="share.sendControl"
      />

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
    <!-- Pinned action bar, same treatment the dialog footer used to give them:
         a tinted bar with a separating border, so the buttons never float in
         the middle of a long scroll. -->
    <div class="live-panel__footer">
      <template v-if="phase === 'minutes'">
        <button
          v-if="notes.minutesPhase.value === 'failed'"
          class="fbtn"
          type="button"
          @click="notes.generateMinutes(true)"
        >
          {{ t('common.retry') }}
        </button>
        <button
          class="fbtn"
          type="button"
          :disabled="!notes.minutes.value"
          @click="copyMinutes"
        >
          {{ t('liveShare.notes.copy') }}
        </button>
        <button
          class="fbtn"
          type="button"
          :disabled="!notes.minutes.value"
          @click="downloadMinutes"
        >
          {{ t('liveShare.notes.download') }}
        </button>
        <button class="fbtn fbtn-primary" type="button" data-action="new-share" @click="resetReport">
          {{ t('liveShare.newShare') }}
        </button>
      </template>
      <template v-else>
        <button
          v-if="share.state.value === 'idle' && supported"
          class="fbtn fbtn-primary"
          type="button"
          :disabled="!selectedSourceId || starting"
          @click="onStart"
        >
          {{ starting ? t('liveShare.starting') : t('liveShare.start') }}
        </button>
        <button
          v-else-if="share.state.value === 'publishing'"
          class="fbtn fbtn-danger"
          type="button"
          @click="onStop"
        >
          {{ t('liveShare.stop') }}
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The desktop-sharing dock panel: start a share, hand out the link, watch the
 * audience, and read the transcript / minutes afterwards.
 *
 * The capture session lives in useLiveShare (module-level, one per app
 * instance); this component only drives it and renders state. Leaving the tab
 * while publishing does NOT stop the share — the panel header keeps a LIVE
 * badge so it is obvious from the dock that something is still on air.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MonitorUp, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import { useToast } from '@/composables/useToast'
import {
  getAudioCaptureSupport,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { groupNoteSegments, useLiveNotes } from '@/composables/useLiveNotes'
import { useScreenWindow } from '@/composables/useScreenWindow'
import { getNative } from '@/utils/clawbenchNative'
import { resolveRemoteControlSupport } from '@/utils/remoteControlSupport'
import LiveAnnotateOverlay from '@/share/LiveAnnotateOverlay.vue'
import LiveChatPanel, { type ChatMessage } from '@/share/LiveChatPanel.vue'
import { parseControlMessage, toInjectorEvent, type ControlMessage } from '@/share/liveControl'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const notes = useLiveNotes()

/** True while a capture/publish session is running (not while idle or reporting). */
const isLive = computed(
  () => share.state.value === 'publishing' || share.state.value === 'capturing',
)

const captureSources = ref<CaptureSource[]>([])
const selectedSourceId = ref('')
const withSystemAudio = ref(false)
const withMicrophone = ref(true)
const withNotes = ref(true)
const systemAudioSupported = ref(false)
const supported = ref(false)
/**
 * Whether this host can enumerate capture sources at all. The desktop shell
 * can; a browser cannot, and neither can a desktop build older than the capture
 * IPC — for those the empty list is not a permission problem and saying so would
 * send the user looking in the wrong place.
 */
const hasNativeCapture = computed(() =>
  Boolean(getNative()?.listCaptureSources || getNative()?.startScreenCapture),
)
/**
 * Android captures through MediaProjection rather than getDisplayMedia, which
 * changes the UI in two places: there is exactly one (system-chosen) source, so
 * the picker and the mid-share swap are meaningless.
 */
const isAndroidCapture = computed(() => Boolean(getNative()?.startScreenCapture))
/** Sentinel source id for the Android path; never rendered as a tile. */
const ANDROID_SOURCE_ID = 'android:screen'
const loadingSources = ref(false)
const starting = ref(false)
/** 'publishing' = the normal dialog; 'minutes' = the post-share report. */
const phase = ref<'publishing' | 'minutes'>('publishing')
const notesListRef = ref<HTMLElement | null>(null)
const remoteScreenRef = ref<HTMLVideoElement | null>(null)
const fullscreenVideoRef = ref<HTMLVideoElement | null>(null)
const screenFullscreen = ref(false)
const screenWindow = useScreenWindow()
const screenWindowTarget = screenWindow.target
const windowVideoRef = ref<HTMLVideoElement | null>(null)
const annotateWindow = useScreenWindow()
const annotateWindowTarget = annotateWindow.target
/** True while the desktop overlay window owns the annotation surface. */
const nativeAnnotating = ref(false)
const isAnnotating = computed(() => annotating.value || nativeAnnotating.value)

// ── Tabs ────────────────────────────────────────────────────────────────────
/** Which of the three publishing surfaces is showing. */
type LiveTab = 'basic' | 'notes' | 'chat'
const activeTab = ref<LiveTab>('basic')
/**
 * The label keys are data, not three literal `t()` calls in the template: the
 * tab bar renders them in a loop so the badge and the recording dot have one
 * place to attach.
 */
const tabs: Array<{ key: LiveTab; labelKey: string }> = [
  { key: 'basic', labelKey: 'liveShare.tabs.basic' },
  { key: 'notes', labelKey: 'liveShare.tabs.transcript' },
  { key: 'chat', labelKey: 'liveShare.chat' },
]
/**
 * The transcript and the chat own their scrolling, so they take the body's
 * whole height instead of sitting in a fixed box inside a scrolling form.
 */
const fillTab = computed(
  () => share.state.value === 'publishing' && activeTab.value !== 'basic',
)

function selectTab(key: LiveTab): void {
  activeTab.value = key
  if (key === 'chat') unread.value = 0
}

// ── Tools: annotation, chat, source switch, presence ────────────────────────
const annotating = ref(false)
/**
 * The chat opened from the member's-screen window (Document PiP). It is a
 * separate surface: that window lives outside the app and its chat floats over
 * the picture, so it must not be tied to the panel's chat tab.
 */
const windowChatOpen = ref(false)
const unread = ref(0)
const chatMessages = ref<ChatMessage[]>([])
const switching = ref(false)
const joinUrl = ref('')
const joinError = ref('')
const switchingBusy = ref(false)
/** viewerId → currently visible. Absent means "no report yet". */
const presence = ref<Record<string, boolean>>({})
/** Viewer currently allowed to drive the pointer, '' when nobody is. */
const controlledBy = ref('')
/** Viewers that asked for control and are waiting for an answer. */
const controlRequests = ref<Set<string>>(new Set())
/**
 * Whether this machine can inject input. Starts false and is filled in on
 * mount: the panel lives on the desktop only, but "unknown" must never look
 * like "yes" — the allow button would promise something the OS cannot do.
 */
const controlSupported = ref(false)
let controlUnsupportedWarned = false
let unsubscribeControl: (() => void) | null = null
let unsubscribeInk: (() => void) | undefined
let unsubscribeClosed: (() => void) | undefined

function toggleChat(): void {
  windowChatOpen.value = !windowChatOpen.value
  if (windowChatOpen.value) unread.value = 0
}

function sendChat(text: string): void {
  const msg: ControlMessage = {
    t: 'chat',
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    from: 'host',
    name: t('liveShare.hostName'),
    text,
    at: Date.now(),
  }
  share.sendControl(msg)
  chatMessages.value = [...chatMessages.value, { ...msg, self: true }]
}

function onControl(msg: ControlMessage): void {
  if (msg.t === 'chat') {
    chatMessages.value = [
      ...chatMessages.value,
      {
        id: msg.id,
        // The wire `name` is untrusted (a viewer could label itself "主持人");
        // derive the label from the relay-stamped id instead.
        name: t('share.live.viewerName', { id: msg.from ?? '' }),
        text: msg.text,
        at: msg.at,
        self: false,
      },
    ]
    // Not unread while the message is on screen: either the panel's chat tab
    // or the floating chat in the member's-screen window counts as "seen".
    if (activeTab.value !== 'chat' && !windowChatOpen.value) unread.value += 1
    return
  }
  if (msg.t === 'presence') {
    // `from` is stamped by the relay; the wire `viewerId` is whatever the peer
    // typed. Trusting it would let viewer A mark viewer B as away.
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
    // Also covers "requested but not answered yet": the request disappears
    // instead of sitting there forever.
    controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== msg.from))
    return
  }
  if (msg.t === 'ctrl-ev') {
    // Only the viewer we granted may move the pointer; anything else is a stale
    // grant or a peer that never asked.
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
      // The bridge answers `{supported:false}` when the native input module is
      // not installed; say so once instead of swallowing every click.
      if (res && typeof res === 'object' && !res.supported && !controlUnsupportedWarned) {
        controlUnsupportedWarned = true
        toast.show(t('liveShare.controlUnsupported'), { type: 'error', icon: 'warning' })
      }
    })
  }
}

function grantControl(viewerId: string): void {
  // The pointer is mapped over the desktop, which is only correct when the
  // picture IS the desktop. A window capture would put every click at the same
  // fraction of the screen instead of inside the window.
  if (share.captureKind.value === 'window') {
    toast.show(t('liveShare.controlWindowUnsupported'), { type: 'error', icon: 'warning' })
    return
  }
  controlledBy.value = viewerId
  controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== viewerId))
  // Addressed to that viewer only: a broadcast would put everyone into
  // "controlling" and swallow their typing.
  share.sendControlTo(viewerId, { t: 'ctrl-grant', viewerId, granted: true })
  void getNative()?.remoteControlGrant?.(viewerId)
}

function revokeControl(viewerId: string): void {
  if (controlledBy.value === viewerId) controlledBy.value = ''
  controlRequests.value = new Set([...controlRequests.value].filter((id) => id !== viewerId))
  share.sendControlTo(viewerId, { t: 'ctrl-grant', viewerId, granted: false })
  void getNative()?.remoteControlRevoke?.()
}

async function joinShare(): Promise<void> {
  const url = joinUrl.value.trim()
  if (!url) return
  joinError.value = ''
  const bridge = getNative()
  if (!bridge?.openShareLink) {
    joinError.value = t('liveShare.joinUnsupported')
    return
  }
  const ok = await bridge.openShareLink(url)
  if (!ok) {
    joinError.value = t('liveShare.joinInvalid')
    return
  }
  joinUrl.value = ''
}

async function openSwitch(): Promise<void> {
  switching.value = !switching.value
  if (switching.value && captureSources.value.length === 0) await loadSources()
}

async function applySwitch(sourceId: string): Promise<void> {
  switchingBusy.value = true
  selectedSourceId.value = sourceId
  const ok = await share.switchSource(sourceId)
  switchingBusy.value = false
  if (!ok) toast.show(t('liveShare.switchSourceFailed'), { type: 'error', icon: 'warning' })
  else switching.value = false
}

const hasTalkers = computed(() => share.viewers.value.some((v) => v.talk === 'talking'))

/**
 * Viewer ids restart at v1 for every share, so a grant from the previous share
 * would otherwise authorise the first viewer of the NEXT one. Clearing on the
 * transition out of `publishing` is what closes that hole.
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
    // A new share starts on the setup tab: the chat and the transcript of the
    // previous one are gone, so landing on either would show an empty pane.
    activeTab.value = 'basic'
    windowChatOpen.value = false
    // The share is over: a drawing surface over a dead picture helps nobody.
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
const noteBlocks = computed(() => groupNoteSegments(notes.segments.value).slice(-40))
const renderedMinutes = computed(() =>
  notes.minutes.value ? renderMarkdownHtml(notes.minutes.value) : '',
)

/** Localised speaker label for one recording channel. */
function notesLabel(channel: string): string {
  if (channel === 'mic') return t('liveShare.notes.speakerMe')
  const id = channel.startsWith('viewer:') ? channel.slice('viewer:'.length) : ''
  const n = Number(id.replace(/^v/, '')) || 0
  return t('liveShare.notes.speakerViewer', { n })
}

function talkLabel(talk: string): string {
  return talk === 'talking' ? t('liveShare.talkTalking') : t('liveShare.talkMuted')
}

/** Load sources lazily: enumerating screens is not free and is only needed here. */
async function loadSources(): Promise<void> {
  loadingSources.value = true
  try {
    const [list, sysAudio] = await Promise.all([listCaptureSources(), getAudioCaptureSupport()])
    // Android has no source enumeration: the system consent dialog decides what
    // gets mirrored. Keep one sentinel entry so "start" is enabled, and let the
    // template show a sentence instead of a one-tile picker.
    captureSources.value = isAndroidCapture.value
      ? [{ id: ANDROID_SOURCE_ID, name: t('liveShare.entireScreen'), kind: 'screen', thumbnail: '' }]
      : list
    systemAudioSupported.value = sysAudio
    // Default the audio toggle to the platform's capability rather than off:
    // on Windows sharing the system sound is the common case.
    withSystemAudio.value = sysAudio
    supported.value =
      hasNativeCapture.value &&
      (captureSources.value.length > 0 ||
        sysAudio ||
        Boolean(navigator.mediaDevices?.getDisplayMedia))
    if (!selectedSourceId.value && captureSources.value.length > 0) {
      // Prefer a full screen over a window: sharing a window is the exception.
      const screen = captureSources.value.find((s) => s.kind === 'screen')
      selectedSourceId.value = (screen ?? captureSources.value[0]).id
    }
  } catch (e) {
    appLog.e('LiveSharePanel', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

// The panel is mounted the first time its tab is opened and then kept alive, so
// "entered the panel" == "mounted". A share that is already publishing gets its
// transcript refreshed: the capture lives in the module singleton and kept
// transcribing while the tab was closed.
onMounted(() => {
  void resolveRemoteControlSupport(getNative()).then((ok) => {
    controlSupported.value = ok
  })
  if (share.state.value === 'idle') void loadSources()
  if (share.state.value === 'publishing' && notes.phase.value === 'recording') {
    void notes.refresh()
  }
})

async function onStart(): Promise<void> {
  starting.value = true
  try {
    const ok = await share.start({
      sourceId: selectedSourceId.value,
      systemAudio: withSystemAudio.value,
      microphone: withMicrophone.value,
      notes: withNotes.value,
      notesLabel,
    })
    if (!ok && share.error.value) toast.show(share.error.value, { type: 'error', icon: 'warning' })
  } finally {
    starting.value = false
  }
}

async function onStop(): Promise<void> {
  const hadNotes = notes.phase.value === 'recording'
  await share.stop()
  if (hadNotes) {
    // The minutes are generated in the background; the panel switches to the
    // report view and stays there until the user starts a new share.
    phase.value = 'minutes'
  }
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
 * Show the member's screen in its own window, with a dock like the audience's:
 * who is sharing, chat, and stop-sharing.
 */
async function openScreenWindow(): Promise<void> {
  const stream = share.remoteScreenStream.value
  if (!stream) return
  const result = await screenWindow.open()
  // No Document PiP on this host: the in-app overlay is the only way to show
  // the picture large. Falling through silently would look like a dead button.
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

/**
 * Open the drawing surface in its own window.
 *
 * The canvas has to be as large as the shared picture: drawn inline it is
 * squeezed into the panel, and a stroke lands somewhere other than where the
 * publisher aimed. Hosts without Document PiP keep the in-panel overlay.
 */
async function openAnnotate(): Promise<void> {
  // Preferred: draw straight on the screen through the desktop shell. The
  // window is transparent and covers the shared display, so what the publisher
  // marks is where the audience looks — no preview, no second window to place.
  const bridge = getNative()
  // A single-window share is not the desktop: the overlay would draw over the
  // whole screen while the audience only sees that one window, so the ink would
  // either be outside the picture or land in the wrong place. The preview
  // surface is the only correct target there.
  if (bridge?.openAnnotateOverlay && share.captureKind.value === 'screen') {
    try {
      const res = await bridge.openAnnotateOverlay()
      if (res?.opened) {
        nativeAnnotating.value = true
        return
      }
    } catch (e) {
      appLog.w('LiveSharePanel', 'annotate overlay failed, falling back', e)
    }
  }
  // Web / Android hosts have no transparent overlay; the in-app surface is the
  // only place a canvas can live there.
  const result = await annotateWindow.open({ width: 1200, height: 800 })
  if (result !== 'opened') appLog.w('LiveSharePanel', 'annotate window unavailable', result)
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

// Closing the window with its own title-bar button must end the drawing mode
// too, otherwise the inline fallback would pop up behind it.
watch(annotateWindowTarget, (target) => {
  if (!target && annotating.value) annotating.value = false
})

/**
 * Bind the relayed screen to whichever <video> elements exist.
 *
 * Two elements (inline preview + fullscreen) share one MediaStream; both can
 * play it, and re-binding on every change keeps them in sync without moving a
 * single element around the DOM.
 */
// The control channel exists for the lifetime of the share, not of the dialog:
// subscribe once so chat keeps arriving while the dialog is closed.
onMounted(() => {
  unsubscribeControl = share.onControl(onControl)
  // Strokes from the desktop overlay. The main process only forwards them on
  // platforms where the overlay is hidden from the capture; elsewhere the ink
  // is already in the picture and relaying it would draw it twice.
  unsubscribeInk = getNative()?.onAnnotateInk?.((msg) => {
    const parsed = parseControlMessage(msg)
    if (parsed) share.sendControl(parsed)
  })
  unsubscribeClosed = getNative()?.onAnnotateClosed?.(() => {
    nativeAnnotating.value = false
  })
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

watch(
  () => share.remoteScreenStream.value,
  async (stream) => {
    await nextTick()
    if (remoteScreenRef.value) remoteScreenRef.value.srcObject = stream
    if (fullscreenVideoRef.value) fullscreenVideoRef.value.srcObject = stream
    if (windowVideoRef.value) windowVideoRef.value.srcObject = stream
    if (!stream) {
      screenFullscreen.value = false
      // The member stopped sharing: a window showing a dead picture is worse
      // than none, and nothing else would close it.
      closeScreenWindow()
    }
  },
  { immediate: true },
)

// Opening the fullscreen element after the ref exists still needs the stream
// attached (the watcher ran before the element was mounted).
watch(screenFullscreen, async (open) => {
  if (!open) return
  await nextTick()
  if (fullscreenVideoRef.value) {
    fullscreenVideoRef.value.srcObject = share.remoteScreenStream.value
  }
})

/** Leaves the report and goes back to the setup view, ready for a new share. */
function resetReport(): void {
  notes.reset()
  phase.value = 'publishing'
}

async function copyMinutes(): Promise<void> {
  try {
    await navigator.clipboard.writeText(notes.minutes.value)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('LiveSharePanel', 'copy minutes failed', e)
  }
}

/**
 * The files this meeting left in the project, in reading order.
 *
 * Built from the server's artifact report rather than from a fixed list: a
 * failed proofread has no revised file, and showing an empty row would suggest
 * the file exists.
 */
const artifactRows = computed(() => {
  const a = notes.artifacts.value
  const rows: Array<{ key: string; label: string; path: string }> = []
  if (a.minutes) {
    rows.push({ key: 'minutes', label: t('liveShare.notes.artifactMinutes'), path: a.minutes })
  }
  if (a.transcript) {
    rows.push({
      key: 'transcript',
      label: t('liveShare.notes.artifactTranscript'),
      path: a.transcript,
    })
  }
  if (a.revised) {
    rows.push({ key: 'revised', label: t('liveShare.notes.artifactRevised'), path: a.revised })
  }
  if (a.audio) {
    rows.push({
      key: 'audio',
      label: t('liveShare.notes.artifactAudio', { n: a.audioFiles ?? 0 }),
      path: a.audio,
    })
  }
  return rows
})

async function copyArtifactPath(path: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(path)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('LiveSharePanel', 'copy path failed', e)
  }
}

function downloadMinutes(): void {
  const text = notes.minutes.value
  if (!text) return
  const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${t('liveShare.notes.fileName')}.md`
  document.body.appendChild(a)
  a.click()
  a.remove()
  // Revoke on the next task: Safari aborts the download if the URL is released
  // in the same tick as the click.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

// Follow the transcript while it grows. Runs after the DOM update so the new
// line is already measured.
watch(
  () => noteBlocks.value.length,
  async () => {
    await nextTick()
    const el = notesListRef.value
    if (el) el.scrollTop = el.scrollHeight
  },
)
</script>

<style scoped>
/*
 * The shared .modal-body is padding-free by design (modal-card.css) — each
 * dialog supplies its own. This one did not, so the link field and the source
 * grid sat flush against the card edges. The values match SessionShareDialog so
 * the two share dialogs line up when opened side by side.
 */
.live-dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  min-width: 0;
  padding: var(--space-6, 12px) var(--space-7, 16px);
  /* A floor on the height keeps the card from jumping when the body switches
     between "pick a source" and the (shorter) publishing view. */
  min-height: calc(210 * var(--dp));
}

.live-dialog-section {
  font-size: var(--font-size-sm, 13px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
  /* Air above a heading only: the flex gap already separates it from the
     content it introduces. */
  margin-top: var(--space-2, 4px);
}

/* A hairline above every section except the first turns a flat list of lines
   into blocks. The first one is skipped because a rule directly under the
   dialog's top padding reads as a stray border. */
.live-dialog-section:not(:first-child) {
  margin-top: var(--space-3, 6px);
  padding-top: var(--space-5, 10px);
  border-top: 1px solid var(--border-color, #e5e5e5);
}

.live-dialog-hint {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
  line-height: 1.6;
}

/* Indented hints belong to the checkbox above them. The indent matches the
   checkbox box plus its gap so the text starts under the label, not the box. */
.live-dialog-hint--indent {
  padding-left: calc(16 * var(--dp));
}

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

.live-dialog-warn {
  padding: var(--space-4, 8px) var(--space-6, 12px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #f6f8fa);
  color: var(--text-muted, #656d76);
  font-size: var(--font-size-sm, 13px);
  line-height: 1.6;
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

/* ── Source picker ─────────────────────────────────────────────────────── */

.live-source-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: var(--space-4, 8px);
  max-height: 240px;
  overflow-y: auto;
  /* Room for the active card's ring, which would otherwise be clipped by the
     scroll container on the first and last column. */
  padding: 2px;
}

.live-source {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  padding: var(--space-3, 6px);
  border-radius: var(--radius-md, 6px);
  border: 1px solid var(--border-color, #d0d7de);
  background: var(--bg-secondary, #fff);
  cursor: pointer;
  min-width: 0;
  text-align: left;
  transition:
    border-color var(--duration-fast, 0.1s) ease,
    box-shadow var(--duration-fast, 0.1s) ease,
    background var(--duration-fast, 0.1s) ease;
}

.live-source:hover {
  border-color: var(--accent-color, #0969da);
}

.live-source--active {
  border-color: var(--accent-color, #0969da);
  box-shadow: 0 0 0 1px var(--accent-color, #0969da);
}

.live-source-thumb {
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-tertiary, #f6f8fa);
}

.live-source-thumb--empty {
  display: block;
}

.live-source-name {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary, #424a53);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Checkbox rows ─────────────────────────────────────────────────────── */

.live-check {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  font-size: var(--font-size-sm, 13px);
  color: var(--text-primary, #1f2328);
  cursor: pointer;
  /* The checkbox is the click target; the label is part of it. Padding gives
     the row a comfortable hit area without shifting the text off the grid. */
  padding: var(--space-1, 2px) 0;
}

.live-check input[type='checkbox'] {
  margin: 0;
  flex-shrink: 0;
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

/* ── AI 听记 ─────────────────────────────────────────────────────────────── */

/*
 * Fixed height with its own scroll: the dialog's height must not jump every
 * ten seconds as a new line lands, and an hour-long meeting must not push the
 * footer off-screen. Inside its own tab it takes the pane instead.
 */
.live-notes-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
  max-height: calc(150 * var(--dp));
  overflow-y: auto;
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #f6f8fa);
  overscroll-behavior: contain;
}

.live-notes-list--fill {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
}

.live-note-line {
  display: flex;
  gap: var(--space-4, 8px);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.6;
}

/* A fixed column keeps every line's text starting at the same x, which is what
   makes a multi-speaker transcript scannable. */
.live-note-speaker {
  flex: 0 0 calc(64 * var(--dp));
  color: var(--text-muted, #656d76);
  font-weight: 600;
  text-align: right;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.live-note-text {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--text-primary, #1f2328);
  overflow-wrap: anywhere;
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

/* Unread chat count, on the chat tab and in the member's-screen window dock. */
.live-count {
  min-width: 14px;
  padding: 0 3px;
  margin-left: 4px;
  border-radius: var(--radius-full, 999px);
  background: var(--color-red, #cf222e);
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
.live-chat-inline {
  height: calc(220 * var(--dp));
  border: 1px solid var(--border-color, #d0d7de);
  border-radius: var(--radius-md, 6px);
  overflow: hidden;
}

.live-chat-inline--fill {
  flex: 1 1 auto;
  min-height: 0;
  height: auto;
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

.live-source-grid--compact {
  max-height: calc(140 * var(--dp));
  overflow-y: auto;
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

/* The dialog body used to be centred in a card; in a tab the content is a
   column, and a max width keeps long lines (transcripts, paths) readable on a
   wide dock panel. */
.live-panel__body.live-dialog {
  padding: var(--space-6, 12px) var(--space-7, 16px);
  max-width: 760px;
  width: 100%;
  margin: 0 auto;
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
   there. Reuses the header badge's pulse so the two read as the same state. */
.live-tab__rec {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-red, #cf222e);
  animation: live-panel-pulse 1.4s ease-in-out infinite;
  flex-shrink: 0;
}

.live-panel__footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3, 6px);
  flex-shrink: 0;
  padding: var(--space-4, 8px) var(--space-6, 12px);
  border-top: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f6f8fa);
}

</style>