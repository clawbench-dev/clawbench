<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <button class="meeting-icon-btn" type="button" :title="t('common.back')" @click="emit('back')">
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
        <div class="meeting-hint">{{ t('meeting.join.nickHint') }}</div>

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

        <div class="meeting-section">{{ t('meeting.create.source') }}</div>
        <div class="meeting-source-grid">
          <!-- Voice first: it is the option people look for, and it is the one
               that needs no permission. -->
          <button
            class="meeting-source"
            :class="{ 'meeting-source--on': selected === VOICE }"
            type="button"
            data-source="voice"
            @click="selected = VOICE"
          >
            <span class="meeting-source__thumb meeting-source__thumb--voice"><Mic :size="20" /></span>
            <span class="meeting-source__name">{{ t('meeting.create.voice') }}</span>
            <span class="meeting-source__note">{{ t('meeting.create.voiceNote') }}</span>
          </button>
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
        <div v-if="isVoice" class="meeting-hint">{{ t('meeting.create.voiceHint') }}</div>
        <div v-else-if="sources.length === 0 && !loadingSources" class="meeting-hint">{{ noSourceHint }}</div>

        <div class="meeting-section">{{ t('meeting.create.sound') }}</div>
        <label class="meeting-check">
          <input type="checkbox" :checked="microphone" @change="microphone = ($event.target as HTMLInputElement).checked" />
          <span class="meeting-check__icon"><Mic :size="13" /></span>
          {{ t('meeting.create.mic') }}
        </label>
        <div class="meeting-hint meeting-hint--indent">
          {{ microphone ? t('meeting.create.micOn') : t('meeting.create.micOff') }}
        </div>
        <label class="meeting-check" :class="{ 'meeting-check--disabled': isVoice }">
          <input
            type="checkbox"
            :checked="systemAudio && !isVoice"
            :disabled="isVoice"
            @change="systemAudio = ($event.target as HTMLInputElement).checked"
          />
          <span class="meeting-check__icon"><Volume2 :size="13" /></span>
          {{ t('meeting.create.systemAudio') }}
        </label>
        <div class="meeting-hint meeting-hint--indent">
          {{ isVoice ? t('meeting.create.systemAudioVoice') : t('meeting.create.systemAudioHint') }}
        </div>
        <label class="meeting-check">
          <input type="checkbox" :checked="notes" @change="notes = ($event.target as HTMLInputElement).checked" />
          <span class="meeting-check__icon"><NotebookPen :size="13" /></span>
          {{ t('meeting.create.notes') }}
        </label>
        <div class="meeting-hint meeting-hint--indent">
          {{ notes ? t('meeting.create.notesOn') : t('meeting.create.notesOff') }}
        </div>
      </template>
    </div>

    <div class="meeting-footer">
      <button class="meeting-btn" type="button" @click="emit('back')">{{ t('common.cancel') }}</button>
      <button
        class="meeting-btn meeting-btn--primary"
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
 * 创建会议: name (optional), what to share, and the three capture options.
 *
 * The options are the SAME refs the running meeting edits (useMeetingOptions),
 * so turning 听记 off here means it is off in the meeting too, and vice versa.
 *
 * The source grid's first tile is "不共享屏幕" — a voice meeting is a first-class
 * choice, not a checkbox hidden under the screen picker.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Mic, Monitor, NotebookPen, Volume2 } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { useMeetingOptions } from '@/composables/useMeetingOptions'
import { useMeetingNickname } from '@/composables/useMeetingNickname'
import { MEETING_NICK_MAX } from '@/share/meetingNickname'
import {
  getAudioCaptureSupport,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { getNative } from '@/utils/clawbenchNative'
import { browserScreenCaptureAvailable, isInsecureCaptureContext } from '@/utils/screenCaptureSupport'
import { appLog } from '@/utils/appLog'

const emit = defineEmits<{ back: []; started: [] }>()

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const { microphone, systemAudio, notes } = useMeetingOptions()
const { nickname, persist } = useMeetingNickname()

/** Sentinel source id for the voice meeting (no screen at all). */
const VOICE = 'voice'

const title = ref('')
const selected = ref('')
const sources = ref<CaptureSource[]>([])
const loadingSources = ref(false)
const starting = ref(false)
const systemAudioSupported = ref(false)

const hasNativeCapture = computed(() => Boolean(getNative()?.listCaptureSources))
const browserCapture = browserScreenCaptureAvailable()
const canCapture = computed(() => hasNativeCapture.value || browserCapture)
/** Android has no source list: the system dialog decides what is mirrored. */
const isAndroidCapture = computed(() => Boolean(getNative()?.startScreenCapture))

const supported = computed(() => canCapture.value || isAndroidCapture.value)
const isVoice = computed(() => selected.value === VOICE)

const unsupportedHint = computed(() => {
  if (isInsecureCaptureContext()) return t('liveShare.insecureContext')
  return t('liveShare.captureNeedsClient')
})

const noSourceHint = computed(() =>
  browserCapture ? t('liveShare.browserCaptureHint') : t('liveShare.noSources'),
)

const startLabel = computed(() => {
  if (isVoice.value) return t('meeting.create.startVoice')
  return selected.value ? t('meeting.create.startShare') : t('meeting.create.start')
})

const canStart = computed(() => {
  if (starting.value) return false
  if (!supported.value) return false
  // The nickname is required: viewers see it in chat, and an empty name would
  // fall back to the shared "User" label.
  return Boolean(selected.value) && Boolean(nickname.value.trim())
})

async function loadSources(): Promise<void> {
  loadingSources.value = true
  try {
    const [list, sysAudio] = await Promise.all([listCaptureSources(), getAudioCaptureSupport()])
    // Android has no source list: the consent dialog decides what gets
    // mirrored. One sentinel tile keeps "share my screen" selectable.
    sources.value = isAndroidCapture.value
      ? [{ id: 'android:screen', name: t('liveShare.entireScreen'), kind: 'screen', thumbnail: '' }]
      : list
    systemAudioSupported.value = sysAudio
    // Seed the toggle from the platform rather than from `false`: on Windows
    // sharing the system sound is the common case.
    if (sysAudio && !systemAudio.value) systemAudio.value = true
    if (!selected.value) {
      const screen = list.find((s) => s.kind === 'screen')
      // Default to voice only when there is nothing to share.
      selected.value = screen?.id ?? list[0]?.id ?? VOICE
    }
  } catch (e) {
    appLog.e('MeetingCreate', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

/** Speaker labels for the transcript channels (publisher-side, localized). */
function notesLabel(channel: string): string {
  if (channel === 'mic') return t('liveShare.notes.speakerMe')
  const id = channel.startsWith('viewer:') ? channel.slice('viewer:'.length) : ''
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
    const voice = isVoice.value
    const ok = await share.start({
      sourceId: voice ? '' : selected.value,
      systemAudio: voice ? false : systemAudio.value,
      microphone: microphone.value,
      notes: notes.value,
      video: !voice,
      shareTitle: title.value.trim(),
      notesLabel,
    })
    if (ok) {
      emit('started')
      return
    }
    if (share.error.value) toast.show(share.error.value, { type: 'error', icon: 'warning' })
  } finally {
    starting.value = false
  }
}

onMounted(() => {
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
  width: 100%; height: calc(32 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-secondary); color: var(--text-primary);
  font-size: var(--font-size-sm); font-family: inherit;
}
.meeting-input:focus { outline: none; border-color: var(--accent-color); }

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
.meeting-source__thumb--voice { background: color-mix(in srgb, var(--accent-color) 10%, transparent); color: var(--accent-color); }
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
  display: flex; justify-content: flex-end; gap: var(--space-3);
  padding: var(--space-4) var(--space-6);
  border-top: 1px solid var(--border-color); background: var(--bg-secondary); flex-shrink: 0;
}
.meeting-btn {
  display: inline-flex; align-items: center; gap: var(--space-2);
  height: calc(28 * var(--dp)); padding: 0 var(--space-6);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-primary); color: var(--text-primary);
  font-size: var(--font-size-sm); cursor: pointer;
}
.meeting-btn:hover:not(:disabled) { border-color: var(--accent-color); color: var(--accent-color); }
.meeting-btn--primary { border-color: var(--accent-color); background: var(--accent-color); color: #fff; }
.meeting-btn--primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); color: #fff; }
.meeting-btn:disabled { opacity: 0.45; cursor: not-allowed; }
</style>
