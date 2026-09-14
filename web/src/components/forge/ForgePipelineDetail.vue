<template>
  <div class="forge-detail forge-pipeline-detail">
    <!-- Standard drill-down header, matching every other detail page. -->
    <div class="forge-detail-header">
      <button class="forge-back" @click="emit('back')">
        <ChevronLeft :size="18" />
        <span>{{ t('forge.pipeline.detail.back') }}</span>
      </button>
      <div class="forge-detail-actions">
        <button
          v-if="detail.run.value"
          class="forge-icon-btn"
          :title="t('forge.pipeline.quote')"
          :aria-label="t('forge.pipeline.quote')"
          @mousedown.prevent
          @click="onQuote"
        >
          <MessageSquare :size="15" />
        </button>
        <a
          v-if="detail.run.value"
          class="forge-icon-btn"
          :href="detail.run.value.url"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('forge.pipeline.openRun')"
        >
          <ExternalLink :size="15" />
        </a>
      </div>
    </div>

    <div v-if="detail.loading.value" class="forge-loading">
      <LoadingIndicator size="md" :label="t('forge.loading')" />
    </div>

    <div v-else-if="detail.error.value" class="forge-error-card">
      <AlertCircle :size="18" class="forge-error-icon" />
      <div class="forge-error-text">
        <div class="forge-error-title">{{ errorTitle }}</div>
        <div class="forge-error-body">{{ detail.error.value.message }}</div>
      </div>
    </div>

    <template v-else-if="detail.run.value">
      <div class="forge-detail-body">
        <!-- Title + metadata. A run has no markdown body or comments, so the
             metadata IS the content; the jobs table below is the detail. -->
        <div class="forge-detail-title-row">
          <span class="forge-state-dot" :class="`pipeline-${detail.run.value.status}`"></span>
          <div class="forge-detail-title-main">
            <h2 class="forge-detail-title">{{ detail.run.value.name }}</h2>
            <div class="forge-detail-meta">
              <span class="forge-detail-number">#{{ detail.run.value.number }}</span>
              <span class="forge-meta-sep">·</span>
              <span>{{ detail.run.value.ref }}</span>
              <span v-if="detail.run.value.actor" class="forge-meta-sep">·</span>
              <span v-if="detail.run.value.actor">{{ detail.run.value.actor }}</span>
              <span class="forge-meta-sep">·</span>
              <span>{{ formatTime(detail.run.value.updatedAt) }}</span>
              <span class="forge-state-badge" :class="`pipeline-${detail.run.value.status}`">
                {{ t(`forge.pipeline.status.${detail.run.value.status}`) }}
              </span>
            </div>
            <div class="forge-pipeline-extra">
              <span v-if="detail.run.value.sha" class="forge-pipeline-sha-full">
                {{ detail.run.value.sha }}
              </span>
              <span v-if="detail.run.value.event" class="forge-pipeline-event">
                {{ t('forge.pipeline.event') }}: {{ detail.run.value.event }}
              </span>
              <span v-if="durationText" class="forge-pipeline-duration">
                {{ t('forge.pipeline.duration') }}: {{ durationText }}
              </span>
            </div>
          </div>
        </div>

        <!-- Jobs: the reason this view exists. A run's failure is located by
             which job failed, not by the run's own status. -->
        <div class="forge-pipeline-jobs">
          <div class="forge-pipeline-jobs-title">
            <ListChecks :size="14" />
            <span>{{ t('forge.pipeline.jobs') }}</span>
          </div>

          <div v-if="detail.jobs.value.length === 0" class="forge-pipeline-jobs-empty">
            {{ t('forge.pipeline.emptyJobs') }}
          </div>

          <div v-else class="forge-pipeline-jobs-table">
            <div class="forge-pipeline-job-row header">
              <span>{{ t('forge.pipeline.jobName') }}</span>
              <span class="col-stage">{{ t('forge.pipeline.jobStage') }}</span>
              <span class="col-status">{{ t('forge.pipeline.jobStatus') }}</span>
              <span class="col-duration">{{ t('forge.pipeline.jobDuration') }}</span>
            </div>
            <div
              v-for="job in detail.jobs.value"
              :key="job.id"
              class="forge-pipeline-job-row"
              :class="{ failed: job.status === 'failure' }"
            >
              <span class="forge-pipeline-job-name" :title="job.name">
                {{ job.name }}
                <!-- GitLab reports why a job failed; that reason is often the
                     most actionable thing on the page. -->
                <span v-if="job.failureReason" class="forge-pipeline-job-reason">
                  {{ job.failureReason }}
                </span>
              </span>
              <span class="col-stage">{{ job.stage || '—' }}</span>
              <span class="col-status">
                <span class="forge-state-dot" :class="`pipeline-${job.status}`"></span>
                {{ t(`forge.pipeline.status.${job.status}`) }}
              </span>
              <span class="col-duration">{{ formatDuration(job.durationSeconds) }}</span>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ExternalLink, MessageSquare, AlertCircle, ListChecks } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgePipelineDetail } from '@/composables/useForge'
import type { ForgePipelineRun } from '@/utils/forgeApi'

const props = defineProps<{
  runId: number
}>()
const emit = defineEmits<{
  (e: 'back'): void
  (e: 'quote', run: ForgePipelineRun): void
}>()

const { t } = useI18n()
const detail = useForgePipelineDetail()

function load() {
  if (props.runId > 0) void detail.open(props.runId)
}

onMounted(load)
watch(() => props.runId, load)

const errorTitle = computed(() => {
  const code = detail.error.value?.code
  if (code === 'ForgeNoPipelines') return t('forge.pipeline.noPlatform')
  if (code === 'ForgeNotFound') return t('forge.pipeline.loadFailed')
  return t('forge.error.generic')
})

/** Duration is only shown when the platform reported it. */
const durationText = computed(() => {
  const seconds = detail.run.value?.durationSeconds
  return seconds ? formatDuration(seconds) : ''
})

function onQuote() {
  const run = detail.run.value
  if (run) emit('quote', run)
}

/** Human-readable duration; empty for a missing or zero value. */
function formatDuration(seconds: number | undefined): string {
  if (!seconds || seconds <= 0) return '—'
  const m = Math.floor(seconds / 60)
  const s = Math.round(seconds % 60)
  if (m === 0) return `${s}s`
  return `${m}m ${s}s`
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString()
}
</script>

<style scoped>
.forge-pipeline-extra {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
}
.forge-pipeline-sha-full {
  font-family: var(--font-mono, monospace);
}
.forge-pipeline-jobs {
  margin-top: var(--space-5);
}
.forge-pipeline-jobs-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  margin-bottom: var(--space-3);
}
.forge-pipeline-jobs-empty {
  color: var(--text-secondary, #666);
  font-size: var(--font-size-sm);
  padding: var(--space-4) 0;
}
.forge-pipeline-jobs-table {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
}
.forge-pipeline-job-row {
  display: grid;
  grid-template-columns: 2fr 1fr 1.2fr 0.8fr;
  gap: var(--space-3);
  align-items: center;
  padding: var(--space-2) var(--space-3);
  font-size: var(--font-size-sm);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}
.forge-pipeline-job-row:last-child {
  border-bottom: none;
}
.forge-pipeline-job-row.header {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #666);
  background: var(--bg-secondary, #f8f9fa);
}
.forge-pipeline-job-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-pipeline-job-reason {
  margin-left: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--color-red);
  font-family: var(--font-mono, monospace);
}
.forge-pipeline-job-row .col-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.forge-pipeline-job-row .col-duration {
  text-align: right;
  color: var(--text-secondary, #666);
}
</style>
