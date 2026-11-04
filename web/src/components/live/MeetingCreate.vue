<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <button class="meeting-icon-btn" type="button" :title="t('common.back')" data-action="meeting-create-back" @click="emit('back')">
        <ArrowLeft :size="14" />
      </button>
      <span class="meeting-header__title">{{ t('meeting.create.title') }}</span>
      <span class="meeting-header__spacer" />
    </div>

    <!-- The desktop shell can capture on http (its own origin is promoted), but
         the link it hands out cannot. Say so before the form is filled in — the
         same notice the meeting list carries, so the two cannot drift. -->
    <HttpsNotice v-if="desktopInsecure" variant="banner" />

    <div class="meeting-body">
      <template v-if="!supported">
        <!-- An insecure origin is not a stale client: the page has no capture
             API at all. Offer the one action that actually fixes it instead of
             sending the user to update an app that is already up to date. -->
        <HttpsNotice v-if="insecure" variant="block" />
        <div v-else class="meeting-warn">{{ unsupportedHint }}</div>
      </template>

      <template v-else>
        <!-- Name first: it is optional (an unnamed meeting is named after its
             start time), so the required field is the one the host fills in
             last and sees the reason for — the footer button is disabled until
             then. -->
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

        <!-- Identity second, and the only required field. Pre-filled from the
             account nickname when set, but the host may use a different name for
             this meeting. Empty is a blocked state, not a silent one: the hint
             under the field says why the footer button is disabled. -->
        <div class="meeting-section">{{ t('meeting.join.nick') }}</div>
        <input
          v-model="nickname"
          class="meeting-input"
          type="text"
          :maxlength="MEETING_NICK_MAX"
          data-role="meeting-nick"
          :placeholder="t('meeting.join.nickPlaceholder')"
          :aria-invalid="!nicknameValid"
        />
        <!-- The only line the field needs: why the footer button is disabled.
             Nothing explains what a 称谓 is — the placeholder already does, and
             a filled-in field needs no commentary at all. -->
        <div
          v-if="!nicknameValid"
          class="meeting-hint meeting-hint--required"
          data-role="meeting-nick-hint"
        >
          {{ t('meeting.create.nickRequired') }}
        </div>

        <!-- No meeting type: the picture is a CHOICE between "nothing" and the
             actual screens/windows, so it is a grid of frames rather than a
             checkbox followed by a list. Camera, microphone and 听记 are the
             independent parts of the same setup and share one group. -->
        <div class="meeting-section">{{ t('meeting.create.av') }}</div>
        <div class="meeting-subsection">{{ t('meeting.create.picture') }}</div>
        <CaptureSourceGrid
          compact
          allow-off
          :sources="sources"
          :selected-id="screenChoice"
          :pick-label="pickLabel"
          :pick-busy="loadingSources"
          @select="screenChoice = $event"
          @pick="loadSources"
        />
        <div v-if="sourceHint" class="meeting-hint" data-role="meeting-sources">
          {{ sourceHint }}
        </div>

        <!-- Camera and microphone are card choices too, with the off state as
             the first card: one control per decision instead of a checkbox plus
             a list underneath. -->
        <!-- Camera, in the same frames as the screens above: it is a picture
             too. The group stays on screen with no camera at all — 「不共享摄像头」
             is still the answer to "which camera", and a missing group reads as
             a question the form forgot to ask. -->
        <DeviceChoiceGrid
          kind="video"
          variant="tile"
          :available="cameraAvailable"
          :label="t('meeting.create.cameraDevices')"
          :off-label="t('meeting.create.cameraOffCard')"
          :selected-id="cameraChoice"
          @select="cameraChoice = $event"
        />

        <div v-if="pictureHint" class="meeting-hint" data-role="meeting-picture">
          {{ pictureHint }}
        </div>

        <DeviceChoiceGrid
          kind="audio"
          :label="t('meeting.create.micDevices')"
          :off-label="t('meeting.create.micOffCard')"
          :selected-id="micChoice"
          @select="micChoice = $event"
        />

        <!-- The room-level switches, under their own heading: they are not
             part of "which picture / which microphone", they are what the
             meeting allows. -->
        <div class="meeting-section">{{ t('meeting.create.other') }}</div>
        <div class="meeting-checks">
          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="notes"
              :disabled="!notesSupported"
              data-role="meeting-notes"
              @change="notes = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><NotebookPen :size="13" /></span>
            {{ t('meeting.create.notes') }}
            <span v-if="!notesSupported" class="meeting-check__note">
              {{ t('meeting.create.notesUnsupported') }}
            </span>
          </label>
          <!-- Pictures are independent of the transcript: they are a persistent
               artifact of this machine, and wanting one does not imply wanting
               the other. Always shown, including for a voice meeting — somebody
               can share a screen later and the frames should follow. -->
          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="shots"
              data-role="meeting-shots"
              @change="shots = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><ImageIcon :size="13" /></span>
            {{ t('meeting.create.shots') }}
          </label>
          <!-- Audience permissions start ON. They are the same two room-level
               switches the 会议管理 tab shows during the meeting, so they travel
               with the create request: a meeting where nobody may speak and
               nobody may share would otherwise need the host to hunt for two
               switches mid-call. -->
          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="allowTalk"
              data-role="meeting-allow-talk"
              @change="allowTalk = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><Mic :size="13" /></span>
            {{ t('liveShare.allowTalking') }}
          </label>
          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="allowMemberScreen"
              data-role="meeting-allow-member-screen"
              @change="allowMemberScreen = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><MonitorUp :size="13" /></span>
            {{ t('liveShare.allowScreenShare') }}
          </label>
        </div>

        <!-- 中继服务: the only option here that needs a second deployment.
             The switch stays visible so the feature can be found at all; the
             endpoint fields only appear once it is on, because a small meeting
             must not have to scroll past three inputs it will never use. -->
        <div class="meeting-section" data-role="meeting-relay-section">
          {{ t('meeting.create.relay') }}
        </div>
        <div class="meeting-hint">{{ t('meeting.create.relayHint') }}</div>
        <div class="meeting-checks">
          <label class="meeting-check">
            <input
              type="checkbox"
              :checked="useRelay"
              data-role="meeting-relay"
              @change="useRelay = ($event.target as HTMLInputElement).checked"
            />
            <span class="meeting-check__icon"><Radio :size="13" /></span>
            {{ t('meeting.create.relayEnable') }}
          </label>
        </div>

        <template v-if="useRelay">
          <!-- Said out loud because the fields sit in a per-meeting form while
               they apply to every meeting on this server. -->
          <div class="meeting-hint" data-role="meeting-relay-scope">
            {{ t('meeting.create.relayScope') }}
          </div>
          <input
            v-model="relayUrl"
            class="meeting-input"
            type="text"
            data-role="meeting-relay-url"
            :placeholder="t('meeting.create.relayUrlPlaceholder')"
            spellcheck="false"
          />
          <input
            v-model="relayKey"
            class="meeting-input"
            type="text"
            data-role="meeting-relay-key"
            :placeholder="t('meeting.create.relayKey')"
            spellcheck="false"
          />
          <input
            v-model="relaySecret"
            class="meeting-input"
            type="password"
            data-role="meeting-relay-secret"
            :placeholder="relayHasSecret ? t('meeting.create.relaySecretStored') : t('meeting.create.relaySecret')"
            spellcheck="false"
          />
          <div class="meeting-checks">
            <button
              class="fbtn"
              type="button"
              data-action="meeting-relay-test"
              :disabled="relayTesting"
              @click="testRelay"
            >
              {{ relayTesting ? t('meeting.create.relayTesting') : t('meeting.create.relayTest') }}
            </button>
          </div>
          <div
            v-if="relayMessage"
            class="meeting-hint"
            :class="{ 'meeting-hint--required': !relayOk }"
            data-role="meeting-relay-result"
          >
            {{ relayMessage }}
          </div>
        </template>
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
        {{ starting ? t('meeting.create.starting') : t('meeting.create.start') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 创建会议: name (optional) and the capture options.
 *
 * The options are the SAME refs the running meeting edits (useMeetingOptions),
 * so turning 听记 off here means it is off in the meeting too, and vice versa.
 *
 * There is no meeting type. What the host picks here is just the starting
 * state: the picture is a choice of frame, camera/microphone/听记 are
 * independent, every one of them can be changed again during the meeting, and
 * sharing no picture at all is allowed.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ImageIcon, Mic, MonitorUp, NotebookPen, Radio } from 'lucide-vue-next'
import CaptureSourceGrid from '@/components/live/CaptureSourceGrid.vue'
import DeviceChoiceGrid from '@/components/live/DeviceChoiceGrid.vue'
import HttpsNotice from '@/components/live/HttpsNotice.vue'
import { useToast } from '@/composables/useToast'
import { DEFAULT_DEVICE, applyMeetingDefaults, useMeetingOptions } from '@/composables/useMeetingOptions'
import { useMeetingNickname } from '@/composables/useMeetingNickname'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { notesRecordingSupported } from '@/composables/liveNotes'
import { useLiveSFUConfig } from '@/composables/useLiveSFUConfig'
import { MEETING_NICK_MAX } from '@/share/meetingNickname'
import {
  hasCameraDevice,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { getNative } from '@/utils/clawbenchNative'
import { browserScreenCaptureAvailable, isInsecureCaptureContext } from '@/utils/screenCaptureSupport'
import { buildCaptureChoices, captureSourceIdOf } from '@/utils/captureSourceChoices'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'
// The section / field / hint / checkbox primitives, shared with the running
// meeting's 会议管理 tab so one form has one look.
import '@/assets/meeting-form.css'

const emit = defineEmits<{ back: []; started: [] }>()

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const { micChoice, cameraChoice, notes, shots } = useMeetingOptions()
const { localConfig, serverConfig } = useSettingsConfig()
// A new create flow starts from the per-device defaults (设置 → 会议). The stage
// never re-applies them, so a choice made during the meeting survives.
applyMeetingDefaults({
  notes: localConfig.meetingDefaultNotes !== false,
  shots: localConfig.meetingDefaultShots !== false,
})
const { nickname, persist } = useMeetingNickname()
/**
 * 听记 needs `MediaRecorder`, which not every engine has. The switch is shown
 * disabled with the reason rather than left on: an enabled switch that produces
 * no transcript is worse than an honest "not here".
 */
const notesSupported = notesRecordingSupported()

/**
 * What the host chose: '' for no picture, the browser tile for "let the browser
 * ask", otherwise a source id from the list.
 */
const screenChoice = ref('')

const title = ref('')
const sources = ref<CaptureSource[]>([])
const loadingSources = ref(false)
const starting = ref(false)
/** Whether this machine has a camera at all. */
const cameraAvailable = ref(true)

/**
 * The two audience permissions, seeded from the per-device defaults (both ON
 * out of the box).
 *
 * They are room-level switches rather than local capture choices, so they are
 * not part of useMeetingOptions: the running meeting reads them back from the
 * room, and the create response is what seeds that state. Turning one off here
 * is the same decision as turning it off in the 会议管理 tab, only earlier.
 */
const allowTalk = ref(localConfig.meetingDefaultAllowTalk !== false)
const allowMemberScreen = ref(localConfig.meetingDefaultAllowMemberScreen !== false)

/**
 * Whether the client should ask for minutes when the meeting ends.
 *
 * Server config, read at start time: the trigger is client-side, so the
 * server's job is only to expose the preference. The stage still shows the
 * "生成" action, which is the manual path when this is off.
 */
const autoMinutes = computed(() => {
  const meeting = serverConfig.value.meeting as { auto_minutes?: boolean } | undefined
  return meeting?.auto_minutes !== false
})

/**
 * 中继服务: a per-meeting choice backed by installation-wide credentials.
 *
 * The switch is what decides the topology (and with it the viewer ceiling);
 * the three fields are the endpoint it points at. Default OFF: a meeting that
 * never asked for a relay must not start paying a hop through one.
 */
const useRelay = ref(false)
const relayUrl = ref('')
const relayKey = ref('')
const relaySecret = ref('')
const relayMessage = ref('')
const relayOk = ref(false)
const relayTesting = ref(false)
const { state: relayState, load: loadRelay, saveAndTest: saveAndTestRelay } = useLiveSFUConfig()
/** Whether a credential is already stored — the form never receives its value. */
const relayHasSecret = computed(() => relayState.value.hasSecret)
/** The relay can only be selected once a working endpoint is stored. */
const relayReady = computed(() => relayState.value.configured)

const hasNativeCapture = computed(() => Boolean(getNative()?.listCaptureSources))
const browserCapture = browserScreenCaptureAvailable()
const canCapture = computed(() => hasNativeCapture.value || browserCapture)
/** Android has no source list: the system dialog decides what is mirrored. */
const isAndroidCapture = computed(() => Boolean(getNative()?.startScreenCapture))

const supported = computed(() => canCapture.value || isAndroidCapture.value || cameraAvailable.value)
/** Whether the user asked for a screen. */
const hasScreen = computed(() => screenChoice.value !== '')

/**
 * The tile that asks a host for a list it has not given yet.
 *
 * Only a native host can be in this state: a browser has the synthetic tile
 * above, and Android always has its one sentinel. Clicking re-runs the probe,
 * which is what makes the OS raise its own permission dialog (macOS screen
 * recording, Wayland portal).
 */
const pickLabel = computed(() => {
  if (sources.value.length > 0 || !canCapture.value) return ''
  return loadingSources.value ? t('meeting.create.picking') : t('meeting.create.pickSource')
})

/**
 * The one line under the picture grid, when there is nothing to show at all.
 *
 * The grid does NOT scroll and does not need a "scroll for more" hint: every
 * tile is laid out inline and the form body is the only scroll container.
 */
const sourceHint = computed(() => {
  if (sources.value.length === 0 && canCapture.value && !loadingSources.value) {
    return t('liveShare.noSources')
  }
  return ''
})

const unsupportedHint = computed(() => {
  return t('liveShare.captureNeedsClient')
})

/**
 * An insecure origin has no capture API at all. That case renders the shared
 * HTTPS notice (with its one-click fix) instead of the generic "update your
 * client" line, which would send the user looking in the wrong place.
 */
const insecure = isInsecureCaptureContext()

/**
 * The desktop shell promotes its own origin to a secure context, so this form
 * still works on http — but a guest opening the link cannot talk or share a
 * screen. The same banner the meeting list shows, repeated here because this is
 * where the link is about to be created.
 *
 * Gated on `canCapture`: when this machine cannot capture either, the block
 * notice below already says everything, and two notices with the same button
 * would be noise. (`canCapture` rather than `supported` because the latter also
 * waits on the camera probe — the banner would flash in and out on a host that
 * has no camera.)
 */
const desktopInsecure = computed(
  () => canCapture.value && insecure && getNative()?.isDesktopApp?.() === true,
)

/** Camera on AND usable — a camera the OS cannot provide is not a picture. */
const cameraOn = computed(() => cameraChoice.value !== '' && cameraAvailable.value)

/**
 * The one line under the picture, when — and only when — it says something the
 * cards do not.
 *
 * The two card groups already show what is being sent, so "screen only",
 * "camera only" or "camera in the corner" would just repeat the selection back.
 * What they cannot show is why the camera group is missing altogether.
 */
const pictureHint = computed(() => {
  if (!cameraAvailable.value) return t('meeting.create.cameraNoDevice')
  return ''
})

/** The one required field: viewers see it in chat, and an empty name would fall
 *  back to the shared "User" label. */
const nicknameValid = computed(() => Boolean(nickname.value.trim()))

const canStart = computed(() => {
  if (starting.value) return false
  if (!supported.value) return false
  // Asking for the relay without a tested endpoint would create a room that
  // cannot carry media. Refusing here is the honest failure: the alternative is
  // a meeting that silently falls back to 8 seats.
  if (useRelay.value && !relayReady.value) return false
  return nicknameValid.value
})

/** Which id to publish: a browser pick has no id, the dialog decides. */
const selectedSourceId = computed(() => captureSourceIdOf(screenChoice.value))

async function loadSources(): Promise<void> {
  if (loadingSources.value) return
  loadingSources.value = true
  try {
    const [list, hasCamera] = await Promise.all([listCaptureSources(), hasCameraDevice()])
    cameraAvailable.value = hasCamera
    // Android has no source list: the consent dialog decides what gets
    // mirrored. One sentinel tile keeps "share my screen" selectable.
    sources.value = buildCaptureChoices({
      list,
      android: isAndroidCapture.value,
      browser: browserCapture && !hasNativeCapture.value,
      entireScreen: t('liveShare.entireScreen'),
      browserSource: t('meeting.create.browserSource'),
    })

    // Keep the current choice when it still exists; otherwise default to the
    // first screen. Screen sharing stays the default (it is what this feature
    // has always been) and the camera stays opt-IN — turning somebody's camera
    // on because they opened a form is exactly the surprise this must not give
    // — unless there is no screen capture at all, where it is the only picture
    // left.
    const stillThere = sources.value.some((s) => s.id === screenChoice.value)
    if (!stillThere) {
      const screen = sources.value.find((s) => s.kind === 'screen')
      screenChoice.value = screen?.id ?? sources.value[0]?.id ?? ''
    }
    // No screen to share: the camera is the only picture left, so it starts on.
    if (sources.value.length === 0 && hasCamera && cameraChoice.value === '') {
      cameraChoice.value = DEFAULT_DEVICE
    }
  } catch (e) {
    appLog.e('MeetingCreate', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

/**
 * The publisher's own label, frozen when the meeting starts.
 *
 * Read live from the account instead and a rename mid-meeting (this account
 * edited on another device) would split one person into two speakers, which the
 * transcript then merges as if they were different people.
 */
let hostLabel = ''

/**
 * Speaker labels for the transcript channels (publisher-side, localized).
 *
 * Every channel is labelled with the 称谓 the person entered, the publisher's
 * own included. A line like `[00:08] 泷泽：…` needs no separate explanation of
 * who "我" is, and the 称谓 is the spelling viewers use when they address the
 * host in the body — so the proofreader has something to check it against.
 *
 * A viewer's name is only known once the join frame has delivered it, hence the
 * ordinal fallback: an unattributable line is still better than a mislabelled
 * one. Called once per uploaded segment, not once per channel, so a name that
 * arrives after that viewer's first audio track still applies to the next one.
 */
function notesLabel(channel: string): string {
  if (channel === 'mic') return hostLabel
  const id = channel.startsWith('viewer:') ? channel.slice('viewer:'.length) : ''
  const name = share.viewers.value.find((v) => v.id === id)?.name.trim()
  if (name) return name
  const n = Number(id.replace(/^v/, '')) || 0
  return t('liveShare.notes.speakerViewer', { n })
}

/**
 * Probe the draft values, and store them only if the probe passes.
 *
 * A relay that "saves fine" but cannot be reached is worse than one that
 * refuses: every later meeting would fail at start with no indication of which
 * of the three fields is wrong.
 */
async function testRelay(): Promise<void> {
  if (relayTesting.value) return
  if (!relayUrl.value.trim() || (!relayKey.value.trim() && !relayHasSecret.value)) {
    relayOk.value = false
    relayMessage.value = t('meeting.create.relayMissing')
    return
  }
  relayTesting.value = true
  relayMessage.value = ''
  try {
    const res = await saveAndTestRelay({
      url: relayUrl.value,
      apiKey: relayKey.value,
      apiSecret: relaySecret.value,
    })
    relayOk.value = res.ok
    relayMessage.value = res.message
    // The secret is now stored server-side; keeping it in the field would send
    // it again on the next save for no reason.
    if (res.ok) relaySecret.value = ''
  } finally {
    relayTesting.value = false
  }
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
    // Freeze the label now: `persist()` has just made it the account's name, and
    // every segment of this meeting must carry the same one.
    hostLabel = nickname.value.trim()
    const ok = await share.start({
      sourceId: selectedSourceId.value,
      screen: hasScreen.value,
      microphone: micChoice.value !== '',
      micDeviceId: micChoice.value === DEFAULT_DEVICE ? '' : micChoice.value,
      cameraDeviceId: cameraOn.value ? cameraChoice.value : '',
      notes: notes.value,
      shots: shots.value,
      // When off, the audio is still merged on finish (so "重新识别转写" keeps
      // its source); only the automatic minutes request is skipped.
      autoMinutes: autoMinutes.value,
      allowTalk: allowTalk.value,
      allowMemberScreen: allowMemberScreen.value,
      // Fixed for the life of the room: the topology cannot change mid-meeting.
      useRelay: useRelay.value,
      shareTitle: title.value.trim(),
      // The audience's member list names the host with this; without it the
      // host row can only say "主持人".
      hostName: hostLabel,
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
  // A disabled switch must not be left ON (it is a shared singleton, so the
  // running stage would read it back as "notes wanted").
  if (!notesSupported) notes.value = false
  // Prefill the relay URL from what is stored, so enabling the switch on a
  // configured install needs no retyping. The secret never comes back, which is
  // why the placeholder says "已保存" instead.
  void loadRelay().then(() => {
    if (!relayUrl.value) relayUrl.value = relayState.value.url
  })
  // The host always starts with the microphone on. The choice is a shared
  // singleton (the running stage mutes/unmutes the same ref), so a previous
  // meeting — or a previous visit to this form — may have left it muted. A
  // chosen DEVICE is kept; only "muted" is reset. It stays a default, not a
  // lock: the mute card is right there.
  if (micChoice.value === '') micChoice.value = DEFAULT_DEVICE
  if (supported.value) {
    void loadSources()
  }
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
/* .meeting-section / .meeting-input / .meeting-hint / .meeting-check live in
   assets/meeting-form.css — the running meeting's 会议管理 tab draws the same
   controls and must not grow a second copy. */
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
