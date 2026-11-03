<template>
  <BottomSheet :open="open" :title="sheetTitle" size="narrow" @close="emit('close')">
    <div class="regen-body">
      <div v-if="loading" class="regen-loading">
        <LoadingIndicator size="sm" inline />
        {{ t('meeting.detail.regen.preparing') }}
      </div>
      <div v-else-if="error" class="regen-error">{{ error }}</div>
      <template v-else-if="plan">
        <div v-if="plan.blocked" class="regen-error">{{ blockedText }}</div>
        <template v-else>
          <div class="regen-section-title">{{ t('meeting.detail.regen.plan') }}</div>
          <ol class="regen-steps">
            <li v-for="(step, index) in plan.steps" :key="step.kind" class="regen-step">
              <span class="regen-step__index">{{ index + 1 }}</span>
              <span class="regen-step__kind">{{ kindLabel(step.kind) }}</span>
              <span v-if="step.kind === 'transcript'" class="regen-step__mode">
                {{ step.mode === 'asr' ? t('meeting.detail.regen.modeAsr') : t('meeting.detail.regen.modeRender') }}
              </span>
              <span class="regen-step__reason">{{ reasonLabel(step.reason) }}</span>
            </li>
          </ol>

          <label v-if="target !== 'minutes'" class="regen-check">
            <input
              type="checkbox"
              :checked="includeDownstream"
              data-role="regen-downstream"
              @change="onDownstreamChange"
            />
            <span>{{ t('meeting.detail.regen.downstream', { kinds: downstreamKinds }) }}</span>
          </label>
          <div v-if="target !== 'minutes'" class="regen-hint">
            {{ t('meeting.detail.regen.downstreamHint') }}
          </div>

          <div v-for="warning in plan.warnings" :key="warning" class="regen-warning">
            {{ warningText(warning) }}
          </div>

          <div class="regen-hint regen-cost">{{ t('meeting.detail.regen.cost') }}</div>
        </template>
      </template>
    </div>

    <template #footer>
      <button class="fbtn" type="button" data-action="regen-cancel" @click="emit('close')">
        {{ t('meeting.detail.artifact.cancel') }}
      </button>
      <button
        class="fbtn fbtn-primary"
        type="button"
        data-action="regen-start"
        :disabled="startDisabled"
        @click="emit('confirm')"
      >
        {{ startLabel }}
      </button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
/**
 * The confirmation sheet for a regeneration.
 *
 * It renders the server's plan verbatim: the user sees that "regenerate
 * minutes" really runs transcript → proofread → minutes, why each upstream
 * level is included, and what will be overwritten. The POST recomputes the
 * same plan, so this is a preview rather than a contract the client can edit.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import type { MeetingArtifactKind, RegenPlan, RegenReason } from '@/composables/useMeetingHistory'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{
  open: boolean
  target: MeetingArtifactKind
  plan: RegenPlan | null
  loading: boolean
  error: string
  includeDownstream: boolean
}>()

const emit = defineEmits<{
  close: []
  confirm: []
  'update:includeDownstream': [value: boolean]
}>()

const { t } = useI18n()

const kindLabel = (kind: MeetingArtifactKind): string => t(
  kind === 'minutes'
    ? 'meeting.detail.tabMinutes'
    : kind === 'revised'
      ? 'meeting.detail.tabRevised'
      : 'meeting.detail.tabTranscript',
)

const targetAsr = computed(() => props.plan?.steps.some((step) => step.kind === 'transcript' && step.mode === 'asr') ?? false)
const sheetTitle = computed(() => (
  targetAsr.value
    ? t('meeting.detail.regen.titleAsr')
    : t('meeting.detail.regen.title', { kind: kindLabel(props.target) })
))

const downstreamKinds = computed(() => (
  props.target === 'transcript'
    ? t('meeting.detail.regen.downstreamTranscript')
    : t('meeting.detail.regen.downstreamRevised')
))

const startDisabled = computed(() => props.loading || !props.plan || Boolean(props.plan.blocked))
const startLabel = computed(() => {
  if (props.plan?.blocked) return t('meeting.detail.artifact.generate')
  return targetAsr.value ? t('meeting.detail.regen.startAsr') : t('meeting.detail.regen.start')
})

const blockedText = computed(() => {
  const block = props.plan?.blocked
  if (!block) return ''
  if (block.code === 'no_source') return t('meeting.detail.regen.blockedNoSource')
  return block.message
})

function reasonLabel(reason: RegenReason): string {
  switch (reason) {
    case 'missing': return t('meeting.detail.regen.reasonMissing')
    case 'stale': return t('meeting.detail.regen.reasonStale')
    case 'pending': return t('meeting.detail.regen.reasonPending')
    case 'downstream': return t('meeting.detail.regen.reasonDownstream')
    default: return t('meeting.detail.regen.reasonTarget')
  }
}

function warningText(warning: string): string {
  switch (warning) {
    case 'overwrite_transcript': return t('meeting.detail.regen.overwriteTranscript')
    case 'audio_unavailable': return t('meeting.detail.regen.audioUnavailable')
    case 'audio_incomplete': return t('meeting.detail.regen.audioIncomplete')
    case 'mixed_audio_only': return t('meeting.detail.regen.mixedAudioOnly')
    case 'audio_base_estimated': return t('meeting.detail.regen.audioBaseEstimated')
    default: return warning
  }
}

function onDownstreamChange(event: Event): void {
  const input = event.target as HTMLInputElement
  emit('update:includeDownstream', input.checked)
}
</script>

<style scoped>
.regen-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-primary);
}
.regen-loading {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--text-muted);
}
.regen-error {
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
.regen-section-title {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.regen-steps {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.regen-step {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: calc(24 * var(--dp));
}
.regen-step__index {
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  line-height: calc(18 * var(--dp));
  text-align: center;
  flex-shrink: 0;
}
.regen-step__kind { font-weight: var(--font-weight-medium); }
.regen-step__mode {
  padding: 0 var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
}
.regen-step__reason {
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.regen-check {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
  cursor: pointer;
}
.regen-check input { accent-color: var(--accent-color); }
.regen-hint {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}
.regen-cost { margin-top: var(--space-1); }
.regen-warning {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-orange) 10%, transparent);
  color: var(--color-orange);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}
</style>
