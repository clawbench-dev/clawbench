<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <button class="meeting-icon-btn" type="button" :title="t('common.back')" data-action="meeting-create-back" @click="emit('back')">
        <ArrowLeft :size="14" />
      </button>
      <span class="meeting-header__title">{{ t('meeting.create.title') }}</span>
      <span class="meeting-header__spacer" />
    </div>

    <div class="meeting-body">
      <template v-if="!supported">
        <div class="meeting-warn">{{ unsupportedHint }}</div>
      </template>

      <template v-else>
        <!-- Identity first: it is required (viewers see it in chat), unlike the
             meeting name below. Pre-filled from the account nickname when set,
             but the host may use a different name for this meeting. -->
        <div class="meeting-section">{{ t('meeting.join.nick') }}</div>
        <input
          v-model="nickname"
          class="meeting-input"
          type="text"
          :maxlength="MEETING_NICK_MAX"
          data-role="meeting-nick"
          :placeholder="t('meeting.join.nickPlaceholder')"
        />
        <div class="meeting-hint">{{ t('meeting.create.nickHint') }}</div>

        <!-- Name next: optional — an unnamed meeting is named after its start
             time. -->
        <div class="meeting-section">{{ t('meeting.create.name') }}</div>
        <input
          v-model="title"
          class="meeting-input"
          type="text"
          maxlength="80"
          data-role="meeting-title"
          :placeholder="t('meeting.create.namePlaceholder')"
          @keydown.enter="start"
        />

        <!-- Two meeting types, not a screen picker: "voice" and "video" is the
             choice people come here to make. What the video is made of (camera,
             screen, or both) is a detail underneath. -->
        <div class="meeting-section">{{ t('meeting.create.type') }}</div>
        <div class="meeting-type-grid">
          <button
            class="meeting-type"
            :class="{ 'meeting-type--on': !isVideo }"
            type="button"
            data-type="voice"
            @click="kind = 'voice'"
          >
            <span class="meeting-type__icon"><Mic :size="18" /></span>
            <span class="meeting-type__text">
              <span class="meeting-type__name">{{ t('meeting.create.voiceMeeting') }}</span>
              <span class="meeting-type__note">{{ t('meeting.create.voiceMeetingNote') }}</span>
            </span>
          </button>
          <button
            class="meeting-type"
            :class="{ 'meeting-type--on': isVideo }"
            type="button"
            data-type="video"
            @click="kind = 'video'"
          >
            <span class="meeting-type__icon"><Video :size="18" /></span>
            <span class="meeting-type__text">
              <span class="meeting-type__name">{{ t('meeting.create.videoMeeting') }}</span>
              <span class="meeting-type__note">{{ t('meeting.create.videoMeetingNote') }}</span>
            </span>
          </button>
        </div>

        <!-- Camera and screen are independent toggles, but what the picture
             will be is their COMBINATION. One description underneath both says
             which of the four states is selected; repeating a line under each
             toggle said the same thing twice and read as two separate modes. -->
        <template v-if="isVideo">
          <label class="meeting-check" :class="{ 'meeting-check--disabled': !cameraAvailable }">
            <input
              type="checkbox"
              :checked="camera && cameraAvailable"
              :disabled="!cameraAvailable"
              data-role="meeting-camera"
              @change="camera = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><Video :size="13" /></span>
            {{ t('meeting.create.camera') }}
          </label>

          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="shareScreen"
              data-role="meeting-screen"
              @change="shareScreen = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><Monitor :size="13" /></span>
            {{ t('meeting.create.shareScreen') }}
          </label>

          <!-- "Neither" is the only combination that cannot start a video
               meeting, so it is the only one that reads as a warning. -->
          <div v-if="!cameraOn && !shareScreen" class="meeting-warn" data-role="meeting-picture">
            {{ noPictureHint }}
          </div>
          <div v-else class="meeting-hint meeting-hint--indent" data-role="meeting-picture">
            {{ pictureHint }}
          </div>

          <div v-if="shareScreen" class="meeting-source-grid">
            <button
              v-for="src in sources"
              :key="src.id"
              class="meeting-source"
              :class="{ 'meeting-source--on': selected === src.id }"
              type="button"
              :data-source="src.id"
              @click="selected = src.id"
            >
              <span class="meeting-source__thumb">
                <img v-if="src.thumbnail" :src="src.thumbnail" :alt="src.name" />
                <Monitor v-else :size="20" />
              </span>
              <span class="meeting-source__name" :title="src.name">{{ src.name }}</span>
              <span class="meeting-source__note">{{ src.kind === 'window' ? t('meeting.create.window') : t('meeting.create.screen') }}</span>
            </button>
          </div>
          <div v-if="shareScreen && sources.length === 0 && !loadingSources" class="meeting-hint">
            {{ noSourceHint }}
          </div>
        </template>

        <!-- These three toggles name themselves; a sentence under each was
             noise. Only the camera/screen pair needs a description, because
             what they produce depends on how they combine. -->
        <div class="meeting-section">{{ t('meeting.create.sound') }}</div>
        <label class="meeting-check">
          <input
            type="checkbox"
            :checked="microphone"
            data-role="meeting-mic"
            @change="microphone = ($event.target as HTMLInputElement).checked"
          />
          <span class="meeting-check__icon"><Mic :size="13" /></span>
          {{ t('meeting.create.mic') }}
        </label>
        <label class="meeting-check" :class="{ 'meeting-check--disabled': !hasScreen }">
          <input
            type="checkbox"
            :checked="systemAudio && hasScreen"
            :disabled="!hasScreen"
            @change="systemAudio = ($event.target as HTMLInputElement).checked"
          />
          <span class="meeting-check__icon"><Volume2 :size="13" /></span>
          {{ t('meeting.create.systemAudio') }}
        </label>
        <label class="meeting-check">
          <input type="checkbox" :checked="notes" @change="notes = ($event.target as HTMLInputElement).checked" />
          <span class="meeting-check__icon"><NotebookPen :size="13" /></span>
          {{ t('meeting.create.notes') }}
        </label>
      </template>
    </div>

    <div class="meeting-footer">
      <button class="fbtn" type="button" @click="emit('back')">{{ t('common.cancel') }}</button>
      <button
        class="fbtn fbtn-primary"
        type="button"
        data-action="meeting-start"
        :disabled="!canStart"
        @click="start"
      >
        {{ starting ? t('meeting.create.starting') : startLabel }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 创建会议: name (optional), meeting type, and the capture options.
 *
 * The options are the SAME refs the running meeting edits (useMeetingOptions),
 * so turning 听记 off here means it is off in the meeting too, and vice versa.
 *
 * The type is a first-class choice (voice / video), not a "share nothing" tile
 * hidden in the screen picker: what the video is made of — camera, screen, or
 * both — is a detail underneath it.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Mic, Monitor, NotebookPen, Video, Volume2 } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { useMeetingOptions } from '@/composables/useMeetingOptions'
import { useMeetingNickname } from '@/composables/useMeetingNickname'
import { MEETING_NICK_MAX } from '@/share/meetingNickname'
import {
  getAudioCaptureSupport,
  hasCameraDevice,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { getNative } from '@/utils/clawbenchNative'
import { browserScreenCaptureAvailable, isInsecureCaptureContext } from '@/utils/screenCaptureSupport'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const emit = defineEmits<{ back: []; started: [] }>()

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const { microphone, systemAudio, notes, camera } = useMeetingOptions()
const { nickname, persist } = useMeetingNickname()

type MeetingKind = 'voice' | 'video'
const kind = ref<MeetingKind>('video')
/** Share a screen alongside the camera (or on its own). */
const shareScreen = ref(false)

const title = ref('')
const selected = ref('')
const sources = ref<CaptureSource[]>([])
const loadingSources = ref(false)
const starting = ref(false)
const systemAudioSupported = ref(false)
/** Whether this machine has a camera at all. */
const cameraAvailable = ref(true)

const hasNativeCapture = computed(() => Boolean(getNative()?.listCaptureSources))
const browserCapture = browserScreenCaptureAvailable()
const canCapture = computed(() => hasNativeCapture.value || browserCapture)
/** Android has no source list: the system dialog decides what is mirrored. */
const isAndroidCapture = computed(() => Boolean(getNative()?.startScreenCapture))

const supported = computed(() => canCapture.value || isAndroidCapture.value || cameraAvailable.value)
const isVideo = computed(() => kind.value === 'video')
/**
 * Whether the user asked for a screen.
 *
 * Not the same as "a source is selected": a plain browser picks its source in
 * the browser's own dialog, so its id list is empty while it still shares.
 */
const hasScreen = computed(() => isVideo.value && shareScreen.value)

const unsupportedHint = computed(() => {
  if (isInsecureCaptureContext()) return t('liveShare.insecureContext')
  return t('liveShare.captureNeedsClient')
})

const noSourceHint = computed(() =>
  browserCapture ? t('liveShare.browserCaptureHint') : t('liveShare.noSources'),
)

/** Camera on AND usable — a camera the OS cannot provide is not a picture. */
const cameraOn = computed(() => camera.value && cameraAvailable.value)

/**
 * One line for the camera/screen combination — four states, not two.
 *
 * The toggles are independent, so the picture is their combination: both,
 * camera only, screen only, or neither. "Neither" cannot start a video meeting
 * and is rendered as a warning instead (see `noPictureHint`).
 */
const pictureHint = computed(() => {
  // A machine without a camera can only be in the screen-only states; say so
  // once here rather than under a permanently disabled checkbox.
  if (!cameraAvailable.value) return t('meeting.create.cameraNoDevice')
  if (cameraOn.value) {
    return hasScreen.value ? t('meeting.create.pictureBoth') : t('meeting.create.pictureCamera')
  }
  return t('meeting.create.pictureScreen')
})

/** The "neither" state: why it cannot start, and what to do instead. */
const noPictureHint = computed(() =>
  cameraAvailable.value ? t('meeting.create.noPicture') : t('meeting.create.noPictureNoCamera'),
)

const startLabel = computed(() => {
  if (!isVideo.value) return t('meeting.create.startVoice')
  return t('meeting.create.startVideo')
})

const canStart = computed(() => {
  if (starting.value) return false
  if (!supported.value) return false
  // The nickname is required: viewers see it in chat, and an empty name would
  // fall back to the shared "User" label.
  if (!nickname.value.trim()) return false
  if (!isVideo.value) return true
  // A video meeting must actually have a picture; "video" with no camera and
  // no screen would start a voice meeting under a false name.
  return cameraOn.value || hasScreen.value
})

async function loadSources(): Promise<void> {
  loadingSources.value = true
  try {
    const [list, sysAudio, hasCamera] = await Promise.all([
      listCaptureSources(),
      getAudioCaptureSupport(),
      hasCameraDevice(),
    ])
    // Android has no source list: the consent dialog decides what gets
    // mirrored. One sentinel tile keeps "share my screen" selectable.
    sources.value = isAndroidCapture.value
      ? [{ id: 'android:screen', name: t('liveShare.entireScreen'), kind: 'screen', thumbnail: '' }]
      : list
    cameraAvailable.value = hasCamera
    systemAudioSupported.value = sysAudio
    // Seed the toggle from the platform rather than from `false`: on Windows
    // sharing the system sound is the common case.
    if (sysAudio && !systemAudio.value) systemAudio.value = true
    if (!selected.value) {
      const screen = list.find((s) => s.kind === 'screen')
      selected.value = screen?.id ?? list[0]?.id ?? ''
    }
    // Screen sharing stays the default (it is what this feature has always
    // been), including in a plain browser, where the id list is empty because
    // the browser's own picker chooses the source. The camera is opt-IN —
    // turning somebody's camera on because they opened a form is exactly the
    // kind of surprise this setting must not give — unless there is no screen
    // capture at all, where it is the only picture left.
    const canPickScreen = sources.value.length > 0 || (browserCapture && !hasNativeCapture.value)
    shareScreen.value = canPickScreen
    if (!canPickScreen) camera.value = hasCamera
  } catch (e) {
    appLog.e('MeetingCreate', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

/**
 * Speaker labels for the transcript channels (publisher-side, localized).
 *
 * A viewer's real nickname is used when the join frame has delivered it, so the
 * transcript — and the minutes generated from it — name people instead of
 * numbering them. Called once per uploaded segment, not once per channel: the
 * name can arrive after that viewer's first audio track.
 */
function notesLabel(channel: string): string {
  if (channel === 'mic') return t('liveShare.notes.speakerMe')
  const id = channel.startsWith('viewer:') ? channel.slice('viewer:'.length) : ''
  const name = share.viewers.value.find((v) => v.id === id)?.name.trim()
  if (name) return name
  const n = Number(id.replace(/^v/, '')) || 0
  return t('liveShare.notes.speakerViewer', { n })
}

async function start(): Promise<void> {
  if (!canStart.value) return
  starting.value = true
  try {
    // The nickname is the account identity (user.nickname), not a per-meeting
    // field: persist an edit before the room exists, so the name viewers see in
    // chat is the one saved here. A rejected name (e.g. collides with an agent)
    // must stop the meeting rather than start it under a different label.
    try {
      await persist()
    } catch (e) {
      toast.show(e instanceof Error ? e.message : String(e), { type: 'error', icon: 'warning' })
      return
    }
    const voice = !isVideo.value
    const ok = await share.start({
      sourceId: hasScreen.value ? selected.value : '',
      screen: hasScreen.value,
      systemAudio: hasScreen.value ? systemAudio.value : false,
      microphone: microphone.value,
      camera: !voice && camera.value && cameraAvailable.value,
      notes: notes.value,
      video: !voice,
      shareTitle: title.value.trim(),
      notesLabel,
    })
    if (ok) {
      // A camera that failed while the screen still works does not fail the
      // meeting, so the error is surfaced here rather than by a false return.
      if (share.error.value) toast.show(share.error.value, { type: 'error', icon: 'warning' })
      emit('started')
      return
    }
    if (share.error.value) toast.show(share.error.value, { type: 'error', icon: 'warning' })
  } finally {
    starting.value = false
  }
}

onMounted(() => {
  // The host always starts with the microphone on. The option is a shared
  // singleton (the running stage mutes/unmutes the same ref), so a previous
  // meeting — or a previous visit to this form — may have left it off. It is a
  // DEFAULT, not a lock: the host can still uncheck it here, and mute later in
  // the meeting.
  microphone.value = true
  if (supported.value) void loadSources()
})
</script>

<style scoped>
.meeting-panel { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow: hidden; background: var(--bg-primary); }
.meeting-header {
  display: flex; align-items: center; gap: var(--space-3);
  height: var(--header-height); padding: 0 var(--space-5) 0 var(--space-6);
  border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.meeting-header__title { font-size: var(--font-size-md); font-weight: 600; color: var(--text-primary); }
.meeting-header__spacer { flex: 1; }
.meeting-icon-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: calc(26 * var(--dp)); height: calc(26 * var(--dp));
  border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-muted); cursor: pointer;
}
.meeting-icon-btn:hover { background: var(--bg-hover); color: var(--text-primary); }

.meeting-body {
  flex: 1; min-height: 0; overflow-y: auto;
  display: flex; flex-direction: column; gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
.meeting-section {
  font-size: var(--font-size-sm); font-weight: 600; color: var(--text-primary);
  margin-top: var(--space-2);
}
.meeting-section:not(:first-child) {
  margin-top: var(--space-3); padding-top: var(--space-5);
  border-top: 1px solid var(--border-color);
}
.meeting-input {
  width: 100%; height: calc(38 * var(--dp));
  /* The body is a scrolling flex column, and an input is a replaced element
     with no content-derived minimum — without this it is the ONLY child that
     shrinks when the form overflows, which squashed both fields to about half
     height on a phone. */
  flex-shrink: 0;
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-secondary); color: var(--text-primary);
  font-size: var(--font-size-lg); font-family: inherit;
}
.meeting-input:focus { outline: none; border-color: var(--accent-color); }

.meeting-type-grid {
  display: grid; grid-template-columns: 1fr 1fr;
  gap: var(--space-4); padding: 2px;
}
.meeting-type {
  display: flex; align-items: center; gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  background: var(--bg-secondary); text-align: left; cursor: pointer; min-width: 0;
}
.meeting-type:hover { border-color: var(--accent-color); }
.meeting-type--on { border-color: var(--accent-color); box-shadow: 0 0 0 1px var(--accent-color); }
.meeting-type__icon {
  display: inline-flex; align-items: center; justify-content: center;
  width: calc(30 * var(--dp)); height: calc(30 * var(--dp)); flex-shrink: 0;
  border-radius: var(--radius-sm); background: var(--bg-tertiary); color: var(--text-muted);
}
.meeting-type--on .meeting-type__icon {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}
.meeting-type__text { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.meeting-type__name {
  display: block; font-size: var(--font-size-sm); font-weight: 600; color: var(--text-primary);
}
.meeting-type__note {
  display: block; margin-top: 2px;
  font-size: var(--font-size-2xs); color: var(--text-muted);
}

.meeting-source-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(calc(150 * var(--dp)), 1fr));
  gap: var(--space-4); padding: 2px;
}
.meeting-source {
  display: flex; flex-direction: column; gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  background: var(--bg-secondary); text-align: left; cursor: pointer; min-width: 0;
}
.meeting-source:hover { border-color: var(--accent-color); }
.meeting-source--on { border-color: var(--accent-color); box-shadow: 0 0 0 1px var(--accent-color); }
.meeting-source__thumb {
  display: flex; align-items: center; justify-content: center;
  width: 100%; aspect-ratio: 16 / 9;
  border-radius: var(--radius-sm); background: var(--bg-tertiary); color: var(--text-hint);
  overflow: hidden;
}
.meeting-source__thumb img { width: 100%; height: 100%; object-fit: cover; }
.meeting-source__name {
  font-size: var(--font-size-xs); color: var(--text-secondary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.meeting-source__note { font-size: var(--font-size-2xs); color: var(--text-muted); }

.meeting-check {
  display: flex; align-items: center; gap: var(--space-2);
  padding: var(--space-1) 0;
  font-size: var(--font-size-sm); color: var(--text-primary); cursor: pointer;
}
.meeting-check input { margin: 0; accent-color: var(--accent-color); }
.meeting-check__icon { display: inline-flex; color: var(--text-muted); }
.meeting-check input:checked + .meeting-check__icon { color: var(--accent-color); }
.meeting-check--disabled { color: var(--text-hint); cursor: not-allowed; }
.meeting-check--disabled .meeting-check__icon { color: var(--text-hint); }

.meeting-hint {
  display: flex; align-items: center; gap: var(--space-2);
  font-size: var(--font-size-xs); line-height: 1.6; color: var(--text-muted);
}
.meeting-hint--indent { padding-left: calc(16 * var(--dp)); }
.meeting-warn {
  padding: var(--space-4) var(--space-5); border-radius: var(--radius-md);
  background: var(--bg-tertiary); color: var(--text-muted);
  font-size: var(--font-size-sm); line-height: 1.6;
}

.meeting-footer {
  display: flex; flex-wrap: nowrap; justify-content: flex-end;
  justify-content: safe flex-end; gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color); background: var(--bg-primary); flex-shrink: 0;
  overflow-x: auto; overflow-y: hidden;
}
</style>
