<template>
  <div class="task-overview">
    <!-- Scrollable content -->
    <div class="overview-scroll">
      <!-- Header section -->
      <div class="task-header">
        <div class="task-title-row">
          <span class="agent-icon">{{ getAgentIcon(task.agentId) }}</span>
          <h2 class="task-name">{{ task.name }}</h2>
          <span class="status-badge" :class="task.status">
            <span v-if="task.runningCount > 0" class="status-dot running"></span>
            <span v-else class="status-dot" :class="task.status"></span>
            {{ statusText }}
          </span>
        </div>
        <div class="task-meta-row">
          <span class="task-id-value" @click="copyId" :title="t('common.copy')">#{{ task.id }}</span>
          <span class="agent-name">{{ getAgentName(task.agentId) }}</span>
        </div>
      </div>

      <!-- Schedule card -->
      <div class="overview-card">
        <h3 class="card-title">
          <CalendarClock class="card-icon" :size="14" />
          {{ t('task.form.frequency') }}
        </h3>
        <div class="overview-row">
          <span class="overview-value font-mono">{{ task.cronExpr }}</span>
          <span class="overview-subtext">{{ humanizeCron(task.cronExpr) }}</span>
        </div>
        <div class="overview-divider"></div>
        <div class="overview-row">
          <span class="overview-label">{{ t('chat.contentBlocks.repeat') }}</span>
          <span class="overview-value">{{ repeatLabel(task.repeatMode, task.maxRuns) }}</span>
        </div>
        <div v-if="task.runCount > 0" class="overview-row">
          <span class="overview-label">{{ t('chat.contentBlocks.statusExecutions', { count: task.runCount }) }}</span>
        </div>
        <div v-if="task.nextRunAt" class="overview-row highlight">
          <span class="overview-label">{{ t('chat.contentBlocks.nextRun') }}</span>
          <span class="overview-value">{{ formatDateTime(task.nextRunAt) }}</span>
        </div>
      </div>

      <!-- Prompt preview card -->
      <div class="overview-card">
        <h3 class="card-title">
          <MessageSquare class="card-icon" :size="14" />
          {{ t('task.form.prompt') }}
        </h3>
        <div class="prompt-body markdown-body" ref="promptBodyRef" @click="handlePromptClick" v-html="renderedPrompt"></div>
      </div>
    </div>

    <!-- Fixed bottom action bar -->
    <div class="overview-actions">
      <button class="action-btn" @click="$emit('edit')" :title="t('common.edit')">
        <Pencil :size="14" />
        <span class="action-text">{{ t('common.edit') }}</span>
      </button>
      <button v-if="task.runCount > 0 || task.runningCount > 0" class="action-btn" :class="{ 'has-unread-flash': task.unreadCount > 0 }" @click="$emit('history')" :title="t('task.history')">
        <History :size="14" />
        <span class="action-text">{{ t('task.history') }}</span>
      </button>
      <span class="actions-spacer"></span>
      <template v-if="task.status === 'active'">
        <button class="action-btn accent" :disabled="actionLoading || task.runningCount > 0" @click="triggerTask" :title="task.runningCount > 0 ? t('chat.contentBlocks.statusRunning') : t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="action-btn warn icon-only" :disabled="actionLoading" @click="pauseTask" :title="t('task.pause')">
          <Pause :size="14" />
        </button>
        <button class="action-btn danger icon-only" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
        </button>
      </template>
      <template v-else-if="task.status === 'paused'">
        <button class="action-btn accent" :disabled="actionLoading || task.runningCount > 0" @click="triggerTask" :title="task.runningCount > 0 ? t('chat.contentBlocks.statusRunning') : t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="action-btn success icon-only" :disabled="actionLoading" @click="resumeTask" :title="t('task.resume')">
          <Play :size="14" />
        </button>
        <button class="action-btn danger icon-only" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
        </button>
      </template>
      <template v-else-if="task.status === 'completed'">
        <button class="action-btn danger icon-only" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { Pencil, Pause, Play, Zap, Trash2, History, CalendarClock, MessageSquare } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useTaskOverview } from '@/composables/useTaskOverview.ts'
import { useMarkdownRenderer } from '@/composables/useMarkdownRenderer'
import { useAgents } from '@/composables/useAgents'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useWorktreeAnnotation } from '@/composables/useWorktreeAnnotation.ts'
import { annotateCommitHashes, verifyCommitHashes } from '@/composables/useCommitHashAnnotation.ts'
import { annotateLocalhostUrls, useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { annotateCodeBlockHeaders, handleCodeBlockClick, annotateTableBlockHeaders, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { store } from '@/stores/app.ts'
import { humanizeCron, repeatLabel, formatDateTime } from '@/utils/format'

const { t } = useI18n()
const { renderMarkdown } = useMarkdownRenderer()
const { getAgentIcon, getAgentName } = useAgents()
const { annotateFilePaths, verifyFilePaths, openFilePath } = useFilePathAnnotation()
const { annotateWorktreePaths } = useWorktreeAnnotation()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

const props = defineProps<{
  task: any
}>()

const emit = defineEmits<{
  (e: 'deleted'): void
  (e: 'edit'): void
  (e: 'history'): void
}>()

// Task overview composable (ISS-011 + ISS-014)
const { actionLoading, triggerTask, pauseTask, resumeTask, deleteTask } = useTaskOverview({
  task: computed(() => props.task),
  emit: {
    deleted: () => emit('deleted'),
    edit: () => emit('edit'),
    history: () => emit('history'),
  },
})

const promptBodyRef = ref<HTMLElement | null>(null)
const renderedPrompt = ref('')
let promptRenderId = 0

function copyId() {
  if (props.task.id) {
    navigator.clipboard.writeText(String(props.task.id)).catch(() => {})
  }
}

const statusText = computed(() => {
  if (props.task.runningCount > 0) return t('chat.contentBlocks.statusRunning')
  const map: Record<string, string> = {
    active: t('chat.contentBlocks.statusActive'),
    paused: t('chat.contentBlocks.statusPaused'),
    completed: t('chat.contentBlocks.statusCompleted'),
  }
  return map[props.task.status] || props.task.status
})

watch(
  () => [props.task.prompt, store.state.projectRoot, store.state.homeDir] as const,
  ([prompt, projectRoot, homeDir]) => {
    const renderId = ++promptRenderId
    let html = renderMarkdown(prompt || '', { sanitize: true })
    // Add code block headers (language label + copy/wrap buttons)
    html = annotateCodeBlockHeaders(html)
    // Add table block headers (label + copy/wrap buttons)
    html = annotateTableBlockHeaders(html)
    // Annotate worktree paths BEFORE file paths — prevents partial matches
    const { html: worktreeHtml } = annotateWorktreePaths(html, { projectRoot })
    html = worktreeHtml
    // Annotate file paths
    const { html: annotatedHtml, detectedPaths } = annotateFilePaths(html, { projectRoot, homeDir })
    // Annotate commit hashes
    const { html: commitHtml, detectedSHAs } = annotateCommitHashes(annotatedHtml)
    // Annotate localhost URLs
    html = annotateLocalhostUrls(commitHtml)
    // Add lightbox-img class to all <img> tags for lightbox activation
    html = html.replace(/<img(\s+[^>]*?)>/gi, (_match, attrs) => {
      const clean = attrs.replace(/\s*class="[^"]*"/i, '')
      return `<img${clean} class="lightbox-img">`
    })

    renderedPrompt.value = html

    // Async verify file paths after DOM update
    if (detectedPaths.length > 0) {
      const uniquePaths = [...new Set(detectedPaths)]
      nextTick(() => {
        if (renderId !== promptRenderId) return
        if (promptBodyRef.value) {
          verifyFilePaths(uniquePaths, promptBodyRef.value)
        }
      })
    }
    // Async verify commit hashes after DOM update
    if (detectedSHAs.length > 0) {
      const uniqueSHAs = [...new Set(detectedSHAs)]
      nextTick(() => {
        if (renderId !== promptRenderId) return
        if (promptBodyRef.value) {
          verifyCommitHashes(uniqueSHAs, promptBodyRef.value)
        }
      })
    }
  },
  { immediate: true }
)

function handlePromptClick(event: MouseEvent) {
  // Handle localhost URL clicks
  if (handleLocalhostUrlClick(event)) return

  // Code block header buttons (copy/wrap)
  if (handleCodeBlockClick(event)) return

  // Table block header buttons (copy/wrap)
  if (handleTableBlockClick(event)) return

  const target = event.target as HTMLElement | null
  // Handle commit-hash clicks
  const commitEl = target?.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) {
      window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    }
    return
  }
  // Handle worktree action buttons
  const wtBtn = target?.closest('.chat-worktree-btn')
  if (wtBtn) {
    event.preventDefault()
    event.stopPropagation()
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    if (wtPath) {
      store.setProject(wtPath)
    }
    return
  }
  // Handle file-open buttons
  const btn = target?.closest('.chat-file-open-btn')
  if (btn) {
    event.preventDefault()
    event.stopPropagation()
    const filePath = btn.getAttribute('data-file-path')
    const lineStart = btn.getAttribute('data-line-start')
    const lineEnd = btn.getAttribute('data-line-end')
    if (filePath) {
      openFilePath(filePath, lineStart ? parseInt(lineStart, 10) : undefined, lineEnd ? parseInt(lineEnd, 10) : undefined)
    }
    return
  }
}
</script>

<style scoped>
.task-overview {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.overview-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Header section */
.task-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-bottom: 2px;
}

.task-title-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.agent-icon {
  font-size: 18px;
}

.task-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  margin: 0;
  flex: 1;
  word-break: break-word;
}

.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border-radius: 12px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}

.status-badge.active {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.status-badge.paused {
  background: rgba(234, 179, 8, 0.12);
  color: #ca8a04;
}

.status-badge.completed {
  background: rgba(156, 163, 175, 0.15);
  color: #6b7280;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.status-dot.active { background: #16a34a; }
.status-dot.paused { background: #ca8a04; }
.status-dot.completed { background: #6b7280; }
.status-dot.running {
  background: #16a34a;
  animation: task-running-pulse 0.8s ease-in-out infinite;
}

@keyframes task-running-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.5); }
  50% { opacity: 0.7; box-shadow: 0 0 8px 3px rgba(34, 197, 94, 0.3); }
}

.task-meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary, #666);
}

.task-id-value {
  font-family: 'SF Mono', 'Menlo', monospace;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-tertiary, #f1f3f5);
  transition: background 0.2s;
}

.task-id-value:hover {
  background: var(--border-color, #e5e5e5);
}

.task-id-value:active {
  background: var(--bg-tertiary, rgba(0, 0, 0, 0.06));
}

/* Cards */
.overview-card {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 8px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  margin: 0;
}

.card-icon {
  color: var(--text-muted, #999);
}

.overview-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: 2px 0;
}

.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.overview-row.highlight {
  background: rgba(0, 102, 204, 0.05);
  padding: 6px;
  border-radius: 6px;
  margin: -2px -6px;
}

.overview-row.highlight .overview-value {
  color: var(--accent-color, #0066cc);
  font-weight: 500;
}

.overview-label {
  font-size: 12px;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.overview-value {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  text-align: right;
  word-break: break-word;
}

.overview-value.font-mono {
  font-family: 'SF Mono', 'Menlo', monospace;
  background: var(--bg-primary, #fff);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #e5e5e5);
  font-size: 12px;
}

.overview-subtext {
  font-size: 11px;
  color: var(--text-muted, #999);
}

/* Prompt card */
/* Use global .markdown-body styles */
.prompt-body.markdown-body {
  overflow-y: visible;
  max-width: 100%;
  padding: 6px 0 0;
  margin: 0;
  background: transparent;
  font-size: 12px;
}

/* Fixed bottom action bar */
.overview-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.actions-spacer {
  flex: 1;
}

.action-btn {
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-primary, #1a1a1a);
  cursor: pointer;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 10px;
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}

/* Icon-only buttons */
.action-btn.icon-only {
  width: 28px;
  padding: 0;
}

.action-text {
  line-height: 1;
}

.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .action-btn:hover:not(:disabled) {
    background: var(--border-color, #e5e5e5);
    transform: translateY(-1px);
  }
}

.action-btn:active:not(:disabled) {
  transform: scale(0.96);
}

.action-btn.accent {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

@media (hover: hover) {
  .action-btn.accent:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
  }
}

.action-btn.warn {
  background: rgba(234, 179, 8, 0.15);
  color: #b47d00;
}

@media (hover: hover) {
  .action-btn.warn:hover:not(:disabled) {
    background: rgba(234, 179, 8, 0.25);
  }
}

.action-btn.success {
  background: rgba(34, 197, 94, 0.15);
  color: #15803d;
}

@media (hover: hover) {
  .action-btn.success:hover:not(:disabled) {
    background: rgba(34, 197, 94, 0.25);
  }
}

.action-btn.danger {
  background: rgba(239, 68, 68, 0.1);
  color: #b91c1c;
}

@media (hover: hover) {
  .action-btn.danger:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.2);
  }
}

/* Static indicator for history button when task has unread messages */
.action-btn.has-unread-flash {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, var(--bg-secondary, #f1f3f5));
}
</style>
