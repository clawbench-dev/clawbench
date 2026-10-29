<template>
  <ModalDialog :open="open" :title="t('liveShare.dialogTitle')" :z-index="2500" @close="onClose">
    <div class="live-dialog">
      <!-- Idle: pick a source and options, then start. -->
      <template v-if="share.state.value === 'idle'">
        <div v-if="!supported" class="live-dialog-warn">
          {{ t('liveShare.unsupported') }}
        </div>

        <template v-else>
          <div class="live-dialog-section">{{ t('liveShare.pickSource') }}</div>
          <div v-if="loadingSources" class="live-dialog-hint">
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

          <label class="live-check">
            <input v-model="withMicrophone" type="checkbox" />
            <span>{{ t('liveShare.includeMicrophone') }}</span>
          </label>
        </template>
      </template>

      <!-- Capturing: brief, but a spinner beats a frozen dialog. Centered in
           the card — as a plain hint it rendered as 11px text hugging the
           top-left corner of a mostly empty panel. -->
      <div v-else-if="share.state.value === 'capturing'" class="live-dialog-loading">
        <LoadingIndicator :label="t('liveShare.starting')" size="lg" />
      </div>

      <!-- Publishing: link + audience + speech controls. -->
      <template v-else>
        <!-- Every address the server is reachable on, primary first. A machine
             with Wi-Fi + Ethernet + Docker needs this: the address this browser
             used is not necessarily one a colleague can reach. -->
        <ShareAddressList :links="share.shareLinks.value" />
        <div class="live-dialog-hint">{{ t('liveShare.linkHint') }}</div>

        <div class="live-dialog-section">
          {{ t('liveShare.audienceCount', { count: share.viewers.value.length }) }}
        </div>
        <div v-if="share.viewers.value.length === 0" class="live-dialog-hint">
          {{ t('liveShare.noViewers') }}
        </div>
        <ul v-else class="live-viewer-list">
          <li v-for="v in share.viewers.value" :key="v.id" class="live-viewer">
            <span class="live-viewer-id">{{ v.id }}</span>
            <span class="live-viewer-talk" :class="`live-viewer-talk--${v.talk}`">
              {{ talkLabel(v.talk) }}
            </span>
            <!-- Only speaking viewers need an action: there is no approval
                 queue, so the publisher's controls are "mute this one" and
                 "mute everyone" (below). -->
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

        <div v-if="share.error.value" class="live-dialog-error">{{ share.error.value }}</div>
      </template>
    </div>
    <!-- Actions live in ModalDialog's footer, like every other dialog: it
         already supplies padding, a separating border and the tinted bar. In
         the body they were flush against the bottom edge with no divider. -->
    <template #footer>
      <button class="fbtn" type="button" @click="onClose">
        {{ t('common.cancel') }}
      </button>
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
  </ModalDialog>
</template>

<script setup lang="ts">
/**
 * Publisher dialog for a live screen/audio share.
 *
 * The capture session itself lives in useLiveShare (module-level, one per app
 * instance); this component only drives it and renders state. Closing the
 * dialog while publishing does NOT stop the share — the header button shows the
 * live indicator so the user can come back to it.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import { useToast } from '@/composables/useToast'
import {
  getAudioCaptureSupport,
  listCaptureSources,
  useLiveShare,
  type CaptureSource,
} from '@/composables/useLiveShare'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()

const captureSources = ref<CaptureSource[]>([])
const selectedSourceId = ref('')
const withSystemAudio = ref(false)
const withMicrophone = ref(true)
const systemAudioSupported = ref(false)
const supported = ref(false)
const loadingSources = ref(false)
const starting = ref(false)

const hasTalkers = computed(() => share.viewers.value.some((v) => v.talk === 'talking'))

function talkLabel(talk: string): string {
  return talk === 'talking' ? t('liveShare.talkTalking') : t('liveShare.talkMuted')
}

/** Load sources lazily: enumerating screens is not free and is only needed here. */
async function loadSources(): Promise<void> {
  loadingSources.value = true
  try {
    const [list, sysAudio] = await Promise.all([listCaptureSources(), getAudioCaptureSupport()])
    captureSources.value = list
    systemAudioSupported.value = sysAudio
    // Default the audio toggle to the platform's capability rather than off:
    // on Windows sharing the system sound is the common case.
    withSystemAudio.value = sysAudio
    supported.value = list.length > 0 || sysAudio || Boolean(navigator.mediaDevices?.getDisplayMedia)
    if (!selectedSourceId.value && list.length > 0) {
      // Prefer a full screen over a window: sharing a window is the exception.
      const screen = list.find((s) => s.kind === 'screen')
      selectedSourceId.value = (screen ?? list[0]).id
    }
  } catch (e) {
    appLog.e('LiveShareDialog', 'loadSources failed', e)
  } finally {
    loadingSources.value = false
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return
    if (share.state.value === 'idle') void loadSources()
  },
  { immediate: true },
)

async function onStart(): Promise<void> {
  starting.value = true
  try {
    const ok = await share.start({
      sourceId: selectedSourceId.value,
      systemAudio: withSystemAudio.value,
      microphone: withMicrophone.value,
    })
    if (!ok && share.error.value) toast.show(share.error.value, { type: 'error', icon: 'warning' })
  } finally {
    starting.value = false
  }
}

async function onStop(): Promise<void> {
  await share.stop()
  emit('close')
}

function onToggleTalk(on: boolean): void {
  share.setTalkEnabled(on)
}

/** Closing while publishing keeps the share running; the header shows it. */
function onClose(): void {
  emit('close')
}
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
</style>
