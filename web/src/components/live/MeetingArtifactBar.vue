<template>
  <div class="artifact-bar" :class="{ 'artifact-bar--warn': stale, 'artifact-bar--error': failed }">
    <div class="artifact-bar__status">
      <LoadingIndicator v-if="running || waiting" size="sm" inline />
      <span v-if="blocked" class="artifact-bar__text">{{ blocked }}</span>
      <template v-else-if="running">
        <span class="artifact-bar__text">
          {{ runningAsr ? t('meeting.detail.artifact.runningAsr') : t('meeting.detail.artifact.running', { kind: label }) }}
          <template v-if="detail"> · {{ detail }}</template>
          <template v-if="elapsed"> · {{ t('meeting.detail.artifact.elapsed', { time: elapsed }) }}</template>
        </span>
      </template>
      <template v-else-if="waiting">
        <span class="artifact-bar__text">{{ t('meeting.detail.artifact.waiting', { kind: label }) }}</span>
      </template>
      <template v-else-if="failed">
        <span class="artifact-bar__text">{{ errorText }}</span>
        <span v-if="state.hasContent" class="artifact-bar__hint">{{ t('meeting.detail.artifact.failedKeepOld') }}</span>
      </template>
      <template v-else-if="state.hasContent">
        <span class="artifact-bar__text">{{ generatedLabel }}</span>
        <span v-if="stale" class="artifact-bar__hint">{{ staleLabel }}</span>
      </template>
      <span v-else class="artifact-bar__text">{{ t('meeting.detail.artifact.notGenerated') }}</span>
    </div>

    <button
      v-if="showSettingsAction && !running && !waiting"
      class="meeting-text-btn"
      type="button"
      data-action="meeting-artifact-settings"
      @click="openSettings"
    >
      {{ t('meeting.detail.artifact.goSettings') }}
    </button>
    <button
      v-if="running || waiting"
      class="meeting-text-btn"
      type="button"
      data-action="meeting-artifact-cancel"
      @click="emit('cancel')"
    >
      {{ t('meeting.detail.artifact.cancel') }}
    </button>
    <button
      v-else
      class="meeting-text-btn"
      type="button"
      :disabled="Boolean(blocked)"
      data-action="meeting-artifact-regenerate"
      @click="emit('regenerate')"
    >
      {{ actionLabel }}
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * The one-line status/action strip at the top of a text tab's pane.
 *
 * It reads the per-artifact state and the current generation summary: a tab
 * whose step is queued says "waiting", the tab whose step is running shows the
 * progress, and every other tab keeps its own content state. While a chain is
 * running the old content stays visible — the bar is the only thing that
 * changes, so reading is not interrupted.
 *
 * The strip belongs to the tab that owns the artifact, so it always speaks for
 * that tab's own product (纪要 / 转写 / 校对). Tabs without an artifact (资源 /
 * 聊天) do not render it at all.
 */
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'
import '@/assets/meeting-text-btn.css'
import type {
  MeetingArtifactKind,
  MeetingArtifactState,
  MeetingGeneration,
} from '@/composables/useMeetingHistory'

const props = defineProps<{
  kind: MeetingArtifactKind
  state: MeetingArtifactState
  generation: MeetingGeneration | null
  hasAudio: boolean
  /** A human-readable reason the action is unavailable (no source). */
  blocked?: string
}>()

const emit = defineEmits<{ regenerate: []; cancel: [] }>()

const { t, locale } = useI18n()
const switchTab = inject<(tab: string) => void>('switchTab')

const label = computed(() => t(`meeting.detail.tab${props.kind === 'minutes' ? 'Minutes' : props.kind === 'revised' ? 'Revised' : 'Transcript'}`))

const isCurrentStep = computed(() => props.generation?.step === props.kind)
const isWaiting = computed(() => Boolean(props.generation && !isCurrentStep.value && props.generation.remaining.includes(props.kind)))
// A settled row whose job is gone (a refresh between the last heartbeat and the
// terminal write) still counts as running until the next poll settles it.
const running = computed(() => isCurrentStep.value || (props.state.jobStatus === 'running' && !props.generation))
const waiting = computed(() => isWaiting.value)
const runningAsr = computed(() => props.kind === 'transcript' && props.generation?.step === 'transcript' && props.hasAudio)
const failed = computed(() => props.state.jobStatus === 'failed' && !running.value)
const stale = computed(() => props.state.stale && !failed.value && !running.value)

const detail = computed(() => (isCurrentStep.value ? props.generation?.detail ?? '' : ''))
const elapsed = computed(() => {
  if (!isCurrentStep.value || !props.generation?.startedAt) return ''
  const seconds = Math.max(0, Math.round((Date.now() - props.generation.startedAt) / 1000))
  const minutes = Math.floor(seconds / 60)
  return `${minutes}:${String(seconds % 60).padStart(2, '0')}`
})

const generatedLabel = computed(() => {
  if (!props.state.contentAt) return t('meeting.detail.artifact.generated')
  const d = new Date(props.state.contentAt)
  const stamp = d.toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
  return t('meeting.detail.artifact.generatedAt', { time: stamp })
})

const staleLabel = computed(() => {
  const upstream = props.kind === 'minutes'
    ? t('meeting.detail.tabRevised')
    : t('meeting.detail.tabTranscript')
  return t('meeting.detail.artifact.stale', { upstream })
})

const errorText = computed(() => {
  const error = props.state.error || ''
  switch (props.state.errorCode) {
    case 'interrupted':
      return t('meeting.detail.artifact.interrupted')
    case 'stt':
      return t('meeting.detail.artifact.sttFailed', { error })
    case 'audio':
      return t('meeting.detail.artifact.audioUnavailable')
    case 'import':
      return t('meeting.detail.artifact.importFailed', { error })
    case 'timeout':
      return t('meeting.detail.artifact.timeout')
    case 'io':
      return t('meeting.detail.artifact.ioFailed', { error })
    case 'budget':
      return t('meeting.detail.artifact.budgetFailed')
    case 'no_source':
      // The server's `error` here is an internal English string ("live notes:
      // transcript is empty"); the bar must never surface it.
      return props.kind === 'transcript'
        ? t('meeting.detail.artifact.noSourceTranscript')
        : t('meeting.detail.artifact.noSourceUpstream')
    case 'model':
      return t('meeting.detail.artifact.failedWith', { error })
    default:
      return error || t('meeting.detail.artifact.failed')
  }
})

/**
 * Where "去设置" should land for a failure the settings page can actually fix.
 *
 * STT failures belong to 语音识别; model/budget failures belong to 会议. Other
 * codes (io/audio/timeout/interrupted) are not configuration problems, so the
 * bar offers no misleading link.
 */
const settingsCategory = computed(() => {
  if (props.state.errorCode === 'stt') return 'stt'
  if (props.state.errorCode === 'model' || props.state.errorCode === 'budget') return 'meeting'
  return ''
})
const showSettingsAction = computed(() => settingsCategory.value !== '')

function openSettings() {
  const category = settingsCategory.value
  if (!category) return
  setPendingSettingsCategory(category)
  switchTab?.('settings')
}

const actionLabel = computed(() => {
  if (props.blocked) return t('meeting.detail.artifact.generate')
  if (failed.value) return t('meeting.detail.artifact.retry')
  if (stale.value) return t('meeting.detail.artifact.update')
  if (props.kind === 'transcript' && props.hasAudio) {
    return t('meeting.detail.artifact.reasr')
  }
  return props.state.hasContent
    ? t('meeting.detail.artifact.regenerate')
    : t('meeting.detail.artifact.generate')
})
</script>

<style scoped>
.artifact-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-shrink: 0;
  padding: var(--space-2) var(--space-7);
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  min-height: calc(26 * var(--dp));
}
.artifact-bar--warn { color: var(--color-orange); }
.artifact-bar--error { color: var(--color-red); }
.artifact-bar__status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex: 1;
}
.artifact-bar__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.artifact-bar__hint { opacity: 0.85; white-space: nowrap; }
</style>
