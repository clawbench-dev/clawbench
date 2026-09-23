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
          <MessageSquareQuote :size="15" />
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

            <!-- Linked change requests. A run that belongs to a PR is much more
                 useful when you can get to that PR, and the association is only
                 known here (the run list does not show it). Absent entirely when
                 there is no link, so a push-to-branch run shows nothing. -->
            <div v-if="linkedPullRequests.length" class="forge-pipeline-prs">
              <span class="forge-pipeline-prs-label">{{ t('forge.pipeline.linkedPrs') }}</span>
              <button
                v-for="pr in linkedPullRequests"
                :key="pr.number"
                class="forge-pipeline-pr"
                :title="pr.title || t('forge.pipeline.openPr', { number: pr.number })"
                @click="emit('open-pr', pr.number)"
              >
                <GitPullRequest :size="13" />
                <span class="forge-pipeline-pr-text">
                  <!-- GitHub supplies a title; GitLab's payload does not, so the
                       number alone is the honest label there. -->
                  <span v-if="pr.title" class="forge-pipeline-pr-title">{{ pr.title }}</span>
                  <span v-else class="forge-pipeline-pr-number">#{{ pr.number }}</span>
                </span>
                <ChevronRight :size="13" class="forge-pipeline-pr-chevron" />
              </button>
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
import {
  ChevronLeft, ChevronRight, ExternalLink, MessageSquareQuote, AlertCircle, ListChecks,
  GitPullRequest,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgePipelineDetail } from '@/composables/useForge'
import type { ForgePipelineRun, ForgePipelinePullRequest } from '@/utils/forgeApi'

const props = defineProps<{
  runId: number
}>()
const emit = defineEmits<{
  (e: 'back'): void
  (e: 'quote', run: ForgePipelineRun): void
  /**
   * Open a linked change request. The host owns navigation (this component only
   * knows the run), so it switches tabs and opens the item.
   */
  (e: 'open-pr', number: number): void
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

/**
 * The run's linked change requests.
 *
 * The field is absent (not empty) when there are none, so this normalizes both
 * to an empty list for the `v-if`.
 */
const linkedPullRequests = computed<ForgePipelinePullRequest[]>(
  () => detail.run.value?.pullRequests ?? [],
)

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
  // Matches ForgeDetail: the two forge drill-down views must render the same
  // timestamp the same way.
  return d.toLocaleString()
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
.forge-pipeline-duration {
  font-variant-numeric: tabular-nums;
}

/* ── Linked change requests ──
   Rendered as rows rather than inline links: they are navigation targets with
   the same weight as the run itself, and a long PR title needs to ellipsise
   rather than wrap the meta line. */
.forge-pipeline-prs {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-top: var(--space-4);
}
.forge-pipeline-prs-label {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.forge-pipeline-pr {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
  transition: border-color var(--duration-base) ease, color var(--duration-base) ease;
}
@media (hover: hover) {
  .forge-pipeline-pr:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
  .forge-pipeline-pr:hover .forge-pipeline-pr-chevron {
    color: var(--accent-color);
  }
}
.forge-pipeline-pr-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-pipeline-pr-chevron {
  flex-shrink: 0;
  color: var(--text-hint);
  transition: color var(--duration-base) ease;
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
.forge-pipeline-job-row .col-stage {
  color: var(--text-secondary, #666);
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
