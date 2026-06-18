<template>
  <BottomSheet :open="show" auto @close="$emit('close')">
    <template #header>
      <div class="tool-detail-header" :data-category="category">
        <component :is="headerIcon" :size="14" class="tool-detail-header-icon" />
        <span class="tool-detail-header-name">{{ displayName }}</span>
        <span v-if="toolSummary" class="tool-detail-header-summary">{{ toolSummary }}</span>
        <span v-if="!toolDone" class="tool-detail-spinner"></span>
        <XCircle v-else-if="toolStatus === 'error'" :size="14" color="#ef4444" class="tool-detail-status" />
        <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-detail-status" />
      </div>
    </template>
    <div class="tool-detail-body" @click="handleBodyClick">
      <div v-html="toolInputHtml"></div>
      <!-- Tool output section -->
      <div v-if="toolOutputHtml" class="tool-output-section">
        <div class="tool-output-header">
          <span class="tool-output-label">output</span>
          <span v-if="toolStatus === 'error'" class="tool-output-status tool-output-error">error</span>
          <span v-else class="tool-output-status tool-output-success">ok</span>
        </div>
        <div class="tool-output-body" v-html="toolOutputHtml"></div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { computed } from 'vue'
import { CheckCircle2, XCircle } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { getToolIcon, toolDisplayName } from '@/utils/icons'
import { handleToolAction } from '@/utils/renderToolDetail.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { store } from '@/stores/app.ts'

const props = defineProps({
  show: { type: Boolean, default: false },
  toolName: { type: String, default: '' },
  toolSubagentType: { type: String, default: '' },
  toolSummary: { type: String, default: '' },
  toolInputHtml: { type: String, default: '' },
  toolOutputHtml: { type: String, default: '' },
  toolStatus: { type: String, default: '' },
  toolDone: { type: Boolean, default: true },
})

const emit = defineEmits(['close', 'file-open', 'send-message'])

const category = computed(() => getToolIcon(props.toolName).category)
const headerIcon = computed(() => getToolIcon(props.toolName).icon)
const displayName = computed(() => {
  if (props.toolSubagentType) {
    const raw = props.toolSubagentType
    return raw.charAt(0).toUpperCase() + raw.slice(1)
  }
  return props.toolName
})

const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

function handleBodyClick(event) {
  if (props.toolName && handleToolAction(props.toolName, event, emit)) return

  // Handle localhost URL open buttons — bottom sheet is teleported to <body>,
  // ChatMessageList's handleChatClick won't see these clicks.
  if (handleLocalhostUrlClick(event)) return

  // Handle commit-hash clicks (span or button) — bottom sheet is teleported to <body>,
  // ChatMessageList's handleChatClick won't see these clicks.
  const commitEl = event.target.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) {
      window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    }
    return
  }
  // Handle file-open buttons
  const fileBtn = event.target.closest('.chat-file-open-btn')
  if (fileBtn) {
    const filePath = fileBtn.getAttribute('data-file-path')
    const lineStart = fileBtn.getAttribute('data-line-start')
    const lineEnd = fileBtn.getAttribute('data-line-end')
    if (filePath) emit('file-open', { path: filePath, lineStart: lineStart ? parseInt(lineStart, 10) : undefined, lineEnd: lineEnd ? parseInt(lineEnd, 10) : undefined })
    return
  }
  // Handle worktree action buttons
  const wtBtn = event.target.closest('.chat-worktree-btn')
  if (wtBtn) {
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    if (wtPath) store.setProject(wtPath)
    return
  }
  event.stopPropagation()
}
</script>

<style scoped>
/* Header — tool-specific accent colors */
.tool-detail-header {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1;
  --tool-accent: var(--text-muted);
}

.tool-detail-header[data-category="file"]     { --tool-accent: var(--accent-color); }
.tool-detail-header[data-category="bash"]     { --tool-accent: #10b981; }
.tool-detail-header[data-category="search"]   { --tool-accent: #8b5cf6; }
.tool-detail-header[data-category="task"]     { --tool-accent: #f59e0b; }
.tool-detail-header[data-category="plan"]     { --tool-accent: var(--accent-color); }
.tool-detail-header[data-category="agent"]    { --tool-accent: #ec4899; }
.tool-detail-header[data-category="skill"]    { --tool-accent: #06b6d4; }
.tool-detail-header[data-category="ask"]      { --tool-accent: #f97316; }
.tool-detail-header[data-category="fallback"] { --tool-accent: var(--text-muted); }

:root[data-theme="dark"] .tool-detail-header[data-category="bash"]   { --tool-accent: #34d399; }
:root[data-theme="dark"] .tool-detail-header[data-category="search"] { --tool-accent: #a78bfa; }
:root[data-theme="dark"] .tool-detail-header[data-category="task"]   { --tool-accent: #fbbf24; }
:root[data-theme="dark"] .tool-detail-header[data-category="agent"]  { --tool-accent: #f472b6; }
:root[data-theme="dark"] .tool-detail-header[data-category="skill"]  { --tool-accent: #22d3ee; }
:root[data-theme="dark"] .tool-detail-header[data-category="ask"]    { --tool-accent: #fb923c; }

.tool-detail-header-icon {
  color: color-mix(in srgb, var(--tool-accent) 80%, transparent);
  flex-shrink: 0;
}

.tool-detail-header-name {
  font-weight: 600;
  color: var(--tool-accent);
  font-size: 13px;
  flex-shrink: 0;
}

.tool-detail-header-summary {
  color: var(--text-tertiary, #888);
  font-size: 12px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.tool-detail-status {
  flex-shrink: 0;
  margin-left: auto;
}

.tool-detail-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid var(--border-color);
  border-top-color: var(--tool-accent);
  border-radius: 50%;
  animation: tool-spin 0.6s linear infinite;
  flex-shrink: 0;
  margin-left: auto;
}

@keyframes tool-spin {
  to { transform: rotate(360deg); }
}

/* Body */
.tool-detail-body {
  padding: 12px 14px;
  overflow-y: auto;
  overflow-x: hidden;
  font-size: 12px;
  line-height: 1.5;
  flex: 1;
  cursor: default;
}

/* Tint the bottom sheet header with tool accent color */
:deep(.bs-header) {
  --tool-accent: var(--text-muted);
  background: color-mix(in srgb, var(--tool-accent) 5%, transparent);
  border-bottom-color: color-mix(in srgb, var(--tool-accent) 15%, var(--border-color));
}
</style>

<style>
/* Non-scoped styles for v-html penetration — tool detail rendering in bottom sheet */
.tool-detail-body .tool-output-section {
  margin-top: 8px;
  border-top: 1px solid var(--border-color);
  padding-top: 8px;
}

.tool-detail-body .tool-output-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}

.tool-detail-body .tool-output-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
  font-weight: 600;
}

:root[data-theme="dark"] .tool-detail-body .tool-output-label {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.tool-detail-body .tool-output-status {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 600;
}

.tool-detail-body .tool-output-success {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

:root[data-theme="dark"] .tool-detail-body .tool-output-success {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.tool-detail-body .tool-output-error {
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}

:root[data-theme="dark"] .tool-detail-body .tool-output-error {
  background: rgba(248, 113, 113, 0.15);
  color: #fca5a5;
}

.tool-detail-body .tool-output-body {
  overflow-y: auto;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .tool-output-body pre {
  margin: 0;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.tool-detail-body .tool-output-default pre {
  background: var(--bg-tertiary);
  border-radius: 4px;
  padding: 8px 10px;
}

/* File header */
.tool-detail-body .tool-file-header {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-bottom: 6px;
  padding-bottom: 6px;
  padding-right: 22px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.tool-detail-body .tool-file-header .chat-file-open-btn {
  position: absolute;
  top: 0;
  right: 0;
  flex-shrink: 0;
}

/* Base style for file-open buttons in tool detail */
.tool-detail-body .chat-file-open-btn {
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted, #999);
  border-radius: 3px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s;
}

.tool-detail-body .chat-file-open-btn:hover {
  color: var(--accent-color, #4a90d9);
  background: var(--bg-tertiary, #f0f0f0);
}

.tool-detail-body .tool-file-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-color);
  word-break: break-all;
  flex: 1;
  min-width: 0;
}

/* Edit diff */
.tool-detail-body .edit-diff-view {
  display: flex;
  flex-direction: column;
  font-size: 12px;
  line-height: 1.6;
}

.tool-detail-body .edit-diff-replace-all {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(245, 158, 11, 0.12);
  color: #d97706;
  font-weight: 600;
  white-space: nowrap;
}

.tool-detail-body .edit-diff-scroll {
  overflow-x: auto;
}

.tool-detail-body .edit-diff-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  min-width: max-content;
}

.tool-detail-body .edit-diff-del {
  background: rgba(239, 68, 68, 0.08);
  color: #dc2626;
  white-space: pre;
}

.tool-detail-body .edit-diff-add {
  background: rgba(34, 197, 94, 0.08);
  color: #16a34a;
  white-space: pre;
}

:root[data-theme="dark"] .tool-detail-body .edit-diff-del {
  background: rgba(248, 113, 113, 0.1);
  color: #fca5a5;
}

:root[data-theme="dark"] .tool-detail-body .edit-diff-add {
  background: rgba(74, 222, 128, 0.1);
  color: #86efac;
}

:root[data-theme="dark"] .tool-detail-body .edit-diff-replace-all {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}

/* File preview */
.tool-detail-body .file-preview-view {
  display: flex;
  flex-direction: column;
  font-size: 12px;
  line-height: 1.6;
}

.tool-detail-body .file-preview-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
}

.tool-detail-body .file-preview-line {
  white-space: pre;
  color: var(--text-primary);
}

.tool-detail-body .file-preview-meta {
  white-space: normal;
  color: var(--text-muted, #999);
  font-style: italic;
  padding: 4px 0;
}

/* File write */
.tool-detail-body .file-write-view {
  display: flex;
  flex-direction: column;
  font-size: 12px;
  line-height: 1.6;
}

.tool-detail-body .file-write-badge {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(59, 130, 246, 0.12);
  color: #2563eb;
  font-weight: 600;
  white-space: nowrap;
}

:root[data-theme="dark"] .tool-detail-body .file-write-badge {
  background: rgba(96, 165, 250, 0.15);
  color: #93c5fd;
}

.tool-detail-body .file-write-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
}

.tool-detail-body .file-write-line {
  white-space: pre;
  color: var(--text-primary);
}

/* JSON fallback */
.tool-detail-body .tool-json-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.5;
  overflow-x: auto;
}

.tool-detail-body .tool-json-body code {
  font-family: inherit;
}

/* Bash terminal */
.tool-detail-body .bash-terminal-view {
  white-space: normal;
}

.tool-detail-body .bash-terminal-desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 6px;
  white-space: pre-wrap;
  word-break: break-word;
}

.tool-detail-body .bash-terminal-body {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 8px 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

.tool-detail-body .bash-prompt {
  color: #16a34a;
  font-weight: 700;
  margin-right: 4px;
}

:root[data-theme="dark"] .tool-detail-body .bash-prompt {
  color: #4ade80;
}

.tool-detail-body .bash-command {
  color: var(--text-primary);
}

/* Bash output */
.tool-detail-body .bash-output-body pre {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 8px 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

/* Grep search */
.tool-detail-body .grep-search-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .grep-pattern-row,
.tool-detail-body .grep-path-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.tool-detail-body .grep-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .tool-detail-body .grep-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.tool-detail-body .grep-pattern-text,
.tool-detail-body .grep-path-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.tool-detail-body .grep-tags-row,
.tool-detail-body .bash-tags-row,
.tool-detail-body .web-search-tags-row,
.tool-detail-body .web-fetch-tags-row,
.tool-detail-body .glob-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 2px;
}

.tool-detail-body .grep-mode-tag {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.08);
  color: #8b5cf6;
  font-weight: 500;
}

:root[data-theme="dark"] .tool-detail-body .grep-mode-tag {
  background: rgba(167, 139, 250, 0.12);
  color: #a78bfa;
}

/* Glob pattern */
.tool-detail-body .glob-pattern-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .glob-pattern-row,
.tool-detail-body .glob-path-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.tool-detail-body .glob-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .tool-detail-body .glob-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.tool-detail-body .glob-pattern-text,
.tool-detail-body .glob-path-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

/* WebSearch */
.tool-detail-body .web-search-view {
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .web-search-query {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  color: var(--text-primary);
}

.tool-detail-body .web-search-icon {
  flex-shrink: 0;
  font-size: 14px;
  line-height: 1.4;
}

.tool-detail-body .web-search-text {
  white-space: pre-wrap;
  word-break: break-word;
}

/* WebFetch */
.tool-detail-body .web-fetch-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .web-fetch-url-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.tool-detail-body .web-fetch-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .tool-detail-body .web-fetch-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.tool-detail-body .web-fetch-link {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--accent-color);
  text-decoration: none;
  word-break: break-all;
}

.tool-detail-body .web-fetch-link:hover {
  text-decoration: underline;
}

.tool-detail-body .web-fetch-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.tool-detail-body .web-fetch-prompt {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}

/* Agent call */
.tool-detail-body .agent-call-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .agent-call-header {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.tool-detail-body .agent-type-badge {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(236, 72, 153, 0.12);
  color: #db2777;
  font-weight: 600;
  white-space: nowrap;
}

:root[data-theme="dark"] .tool-detail-body .agent-type-badge {
  background: rgba(244, 114, 182, 0.15);
  color: #f472b6;
}

.tool-detail-body .agent-call-desc {
  color: var(--text-primary);
  font-weight: 500;
}

.tool-detail-body .agent-call-prompt {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: normal;
  word-break: break-word;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
  font-family: inherit;
  line-height: 1.6;
}
.tool-detail-body .agent-call-prompt p:first-child {
  margin-top: 0;
}
.tool-detail-body .agent-call-prompt p:last-child {
  margin-bottom: 0;
}
.tool-detail-body .agent-call-prompt h1,
.tool-detail-body .agent-call-prompt h2,
.tool-detail-body .agent-call-prompt h3,
.tool-detail-body .agent-call-prompt h4 {
  font-size: 13px;
  font-weight: 600;
  margin: 8px 0 4px;
  color: var(--text-primary);
}
.tool-detail-body .agent-call-prompt ul,
.tool-detail-body .agent-call-prompt ol {
  margin: 4px 0;
  padding-left: 20px;
}
.tool-detail-body .agent-call-prompt li {
  margin: 2px 0;
}
.tool-detail-body .agent-call-prompt code {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  padding: 1px 4px;
  border-radius: 3px;
}
.tool-detail-body .agent-call-prompt pre {
  margin: 4px 0;
  padding: 6px 8px;
  background: var(--bg-secondary);
  border-radius: 4px;
  overflow-x: auto;
}
.tool-detail-body .agent-call-prompt pre code {
  background: none;
  padding: 0;
  font-size: 12px;
}
.tool-detail-body .agent-call-prompt strong {
  font-weight: 600;
  color: var(--text-primary);
}
.tool-detail-body .agent-call-prompt hr {
  border: none;
  border-top: 1px solid var(--border-color);
  margin: 6px 0;
}

/* Skill call */
.tool-detail-body .skill-call-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}

.tool-detail-body .skill-call-header {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tool-detail-body .skill-call-icon {
  font-size: 14px;
  flex-shrink: 0;
}

.tool-detail-body .skill-call-name {
  font-weight: 600;
  color: #0891b2;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
}

:root[data-theme="dark"] .tool-detail-body .skill-call-name {
  color: #22d3ee;
}

.tool-detail-body .skill-call-args {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  line-height: 1.5;
}

/* Thinking content in overlay — plain text (legacy) */
.tool-detail-body .thinking-overlay-text {
  margin: 0;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-secondary);
}

/* Thinking content in overlay — markdown rendered */
.tool-detail-body .thinking-overlay-md {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary);
  word-break: break-word;
}
.tool-detail-body .thinking-overlay-md p {
  margin: 0 0 0.5em;
}
.tool-detail-body .thinking-overlay-md p:last-child {
  margin-bottom: 0;
}
.tool-detail-body .thinking-overlay-md pre {
  margin: 0.5em 0;
  padding: 8px;
  background: var(--bg-tertiary, rgba(0,0,0,0.04));
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
}
.tool-detail-body .thinking-overlay-md code {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
}
.tool-detail-body .thinking-overlay-md :not(pre) > code {
  padding: 1px 4px;
  background: var(--bg-tertiary, rgba(0,0,0,0.06));
  border-radius: 3px;
}
.tool-detail-body .thinking-overlay-md ul,
.tool-detail-body .thinking-overlay-md ol {
  margin: 0.3em 0;
  padding-left: 1.5em;
}
.tool-detail-body .thinking-overlay-md li {
  margin: 0.15em 0;
}
.tool-detail-body .thinking-overlay-md blockquote {
  margin: 0.5em 0;
  padding-left: 0.8em;
  border-left: 3px solid var(--border-color, rgba(0,0,0,0.12));
  color: var(--text-secondary);
}
.tool-detail-body .thinking-overlay-md h1,
.tool-detail-body .thinking-overlay-md h2,
.tool-detail-body .thinking-overlay-md h3 {
  margin: 0.6em 0 0.3em;
  font-size: 1em;
  font-weight: 600;
}
.tool-detail-body .thinking-overlay-md a {
  color: var(--accent-color, #0066cc);
}
.tool-detail-body .thinking-overlay-md table {
  border-collapse: collapse;
  margin: 0.5em 0;
  font-size: 12px;
}
.tool-detail-body .thinking-overlay-md th,
.tool-detail-body .thinking-overlay-md td {
  border: 1px solid var(--border-color, rgba(0,0,0,0.12));
  padding: 4px 8px;
}
.tool-detail-body .thinking-overlay-md th {
  background: var(--bg-tertiary, rgba(0,0,0,0.04));
  font-weight: 600;
}

/* ── Localhost URL open button in tool output (same pattern as ChatMessageItem) ── */
.tool-detail-body .chat-url-open-btn {
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted, #999);
  border-radius: 3px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s;
  font-size: 12px;
  line-height: 1;
  vertical-align: baseline;
}

.tool-detail-body .chat-url-open-btn:hover {
  color: var(--accent-color, #4a90d9);
  background: var(--bg-tertiary, #f0f0f0);
}

.tool-detail-body .chat-worktree-btn {
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted, #999);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 3px;
  font-size: 12px;
  line-height: 1;
  vertical-align: baseline;
  transition: color 0.15s, background 0.15s;
}

.tool-detail-body .chat-worktree-switch-btn:hover {
  color: var(--accent-color, #4a90d9);
  background: var(--bg-tertiary, #f0f0f0);
}

.tool-detail-body .chat-url-open-btn.loading {
  opacity: 0.5;
  pointer-events: none;
}

.tool-detail-body .chat-url-open-btn.loading::after {
  content: '';
  width: 8px;
  height: 8px;
  border: 1.5px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: url-btn-spin 0.6s linear infinite;
  margin-left: 2px;
  display: inline-block;
}

/* Localhost <a> links in tool output pre blocks */
.tool-detail-body pre a[href] {
  color: var(--accent-color, #4a90d9);
  text-decoration: none;
}

.tool-detail-body pre a[href]:hover {
  text-decoration: underline;
}

/* ── New tool renderer styles ── */

/* LS directory view */
.tool-detail-body .ls-dir-view {
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .ls-dir-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tool-detail-body .ls-dir-icon {
  font-size: 14px;
  flex-shrink: 0;
}
.tool-detail-body .ls-dir-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-color);
  word-break: break-all;
}

/* Todo write */
.tool-detail-body .todo-write-view {
  font-size: 12px;
  line-height: 1.6;
}
.tool-detail-body .todo-write-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.tool-detail-body .todo-item {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 2px 0;
}
.tool-detail-body .todo-icon {
  flex-shrink: 0;
  font-size: 12px;
  line-height: 1.6;
}
.tool-detail-body .todo-content {
  word-break: break-word;
  color: var(--text-primary);
}
.tool-detail-body .todo-done .todo-icon { color: #16a34a; }
.tool-detail-body .todo-active .todo-icon { color: #f59e0b; }
.tool-detail-body .todo-pending .todo-icon { color: var(--text-muted); }
.tool-detail-body .todo-done .todo-content { text-decoration: line-through; color: var(--text-muted); }
.tool-detail-body .todo-active .todo-content { font-weight: 500; }

/* Todo read */
.tool-detail-body .todo-read-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
.tool-detail-body .todo-read-icon { font-size: 14px; }
.tool-detail-body .todo-read-label {
  font-weight: 500;
  color: var(--text-secondary);
}

/* Task tool */
.tool-detail-body .task-tool-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .task-tool-field {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.tool-detail-body .task-field-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(245, 158, 11, 0.12);
  color: #d97706;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}
:root[data-theme="dark"] .tool-detail-body .task-field-label {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}
.tool-detail-body .task-field-value {
  color: var(--text-primary);
  word-break: break-word;
}
.tool-detail-body .task-tool-empty {
  color: var(--text-muted);
  font-style: italic;
}

/* Mode switch */
.tool-detail-body .mode-switch-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .mode-switch-icon { font-size: 14px; }
.tool-detail-body .mode-switch-mode {
  font-weight: 600;
  color: var(--accent-color);
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
}

/* Worktree switch */
.tool-detail-body .worktree-switch-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .worktree-switch-icon { font-size: 14px; }
.tool-detail-body .worktree-switch-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-color);
  word-break: break-all;
}

/* Send message */
.tool-detail-body .send-message-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .send-message-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tool-detail-body .send-message-icon { font-size: 14px; }
.tool-detail-body .send-message-recipient {
  font-weight: 500;
  color: var(--text-primary);
}
.tool-detail-body .send-message-content {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}

/* Computer use */
.tool-detail-body .computer-use-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .computer-use-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tool-detail-body .computer-use-icon { font-size: 14px; }
.tool-detail-body .computer-use-action {
  font-weight: 600;
  color: var(--text-primary);
  text-transform: uppercase;
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(236, 72, 153, 0.12);
  color: #db2777;
}
.tool-detail-body .computer-use-desc {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}

/* Team tool */
.tool-detail-body .team-tool-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .team-tool-icon { font-size: 14px; }
.tool-detail-body .team-tool-name {
  font-weight: 600;
  color: var(--text-primary);
}

/* Chat reply */
.tool-detail-body .chat-reply-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .chat-reply-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tool-detail-body .chat-reply-icon { font-size: 14px; }
.tool-detail-body .chat-reply-recipient {
  font-weight: 500;
  color: var(--text-primary);
}
.tool-detail-body .chat-reply-message {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}

/* Save memory */
.tool-detail-body .save-memory-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .save-memory-icon { font-size: 14px; }
.tool-detail-body .save-memory-key {
  font-weight: 600;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: #0891b2;
}
:root[data-theme="dark"] .tool-detail-body .save-memory-key {
  color: #22d3ee;
}
.tool-detail-body .save-memory-value {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}

/* Deep think */
.tool-detail-body .deep-think-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .deep-think-icon { font-size: 14px; }
.tool-detail-body .deep-think-topic {
  font-weight: 500;
  color: var(--text-primary);
  word-break: break-word;
}

/* Structured output */
.tool-detail-body .structured-output-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .structured-output-icon { font-size: 14px; }
.tool-detail-body .structured-output-prompt {
  font-weight: 500;
  color: var(--text-primary);
  word-break: break-word;
}

/* Skill manage */
.tool-detail-body .skill-manage-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .skill-manage-icon { font-size: 14px; }
.tool-detail-body .skill-manage-action {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(6, 182, 212, 0.12);
  color: #0891b2;
  font-weight: 600;
}
:root[data-theme="dark"] .tool-detail-body .skill-manage-action {
  background: rgba(34, 211, 238, 0.15);
  color: #22d3ee;
}
.tool-detail-body .skill-manage-name {
  font-weight: 600;
  color: #0891b2;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
}
:root[data-theme="dark"] .tool-detail-body .skill-manage-name {
  color: #22d3ee;
}

/* Monitor */
.tool-detail-body .monitor-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .monitor-icon { font-size: 14px; }
.tool-detail-body .monitor-target {
  font-weight: 500;
  color: var(--text-primary);
}
.tool-detail-body .monitor-command-body {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 8px 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

/* Image gen */
.tool-detail-body .image-gen-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  flex-wrap: wrap;
}
.tool-detail-body .image-gen-icon { font-size: 14px; }
.tool-detail-body .image-gen-prompt {
  font-weight: 500;
  color: var(--text-primary);
  word-break: break-word;
}
.tool-detail-body .image-gen-size {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(6, 182, 212, 0.12);
  color: #0891b2;
  font-weight: 600;
}
:root[data-theme="dark"] .tool-detail-body .image-gen-size {
  background: rgba(34, 211, 238, 0.15);
  color: #22d3ee;
}

/* LSP */
.tool-detail-body .lsp-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  flex-wrap: wrap;
}
.tool-detail-body .lsp-icon { font-size: 14px; }
.tool-detail-body .lsp-method {
  font-weight: 600;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: #0891b2;
}
:root[data-theme="dark"] .tool-detail-body .lsp-method {
  color: #22d3ee;
}
.tool-detail-body .lsp-file-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--accent-color);
  word-break: break-all;
}

/* Git tool */
.tool-detail-body .git-tool-view {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.tool-detail-body .git-tool-icon { font-size: 14px; }
.tool-detail-body .git-tool-body {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 8px 10px;
  white-space: pre-wrap;
  word-break: break-word;
  flex: 1;
  min-width: 0;
}

/* AskUserQuestion */
.tool-detail-body .ask-question-view {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .ask-question-empty {
  color: var(--text-muted);
  font-style: italic;
}
.tool-detail-body .ask-question-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}
.tool-detail-body .ask-question-header {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.tool-detail-body .ask-question-text {
  color: var(--text-primary);
  font-weight: 500;
}
.tool-detail-body .ask-question-options {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.tool-detail-body .ask-question-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  border: 1px solid var(--border-color);
  transition: background 0.15s, border-color 0.15s;
}
.tool-detail-body .ask-question-option:hover {
  background: color-mix(in srgb, var(--accent-color) 5%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 30%, var(--border-color));
}
.tool-detail-body .ask-question-option.selected {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
  border-color: var(--accent-color);
}
.tool-detail-body .ask-option-indicator {
  flex-shrink: 0;
  font-size: 14px;
  line-height: 1.4;
  color: var(--text-muted);
}
.tool-detail-body .ask-question-option.selected .ask-option-indicator {
  color: var(--accent-color);
}
.tool-detail-body .ask-option-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.tool-detail-body .ask-option-label {
  font-weight: 500;
  color: var(--text-primary);
}
.tool-detail-body .ask-option-desc {
  font-size: 11px;
  color: var(--text-muted);
}
.tool-detail-body .ask-question-supplementary {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 4px;
}
.tool-detail-body .ask-supplementary-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.tool-detail-body .ask-supplementary-input {
  width: 100%;
  padding: 6px 8px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s;
  box-sizing: border-box;
}
.tool-detail-body .ask-supplementary-input:focus {
  border-color: var(--accent-color);
}
.tool-detail-body .ask-supplementary-input::placeholder {
  color: var(--text-muted);
}
.tool-detail-body .ask-question-submit {
  align-self: flex-end;
  padding: 6px 16px;
  border-radius: 4px;
  border: none;
  background: var(--accent-color);
  color: white;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
}
.tool-detail-body .ask-question-submit:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.tool-detail-body .ask-question-submit:not(:disabled):hover {
  opacity: 0.9;
}

/* PermissionApproval */
.tool-detail-body .permission-approval-view {
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 12px;
  line-height: 1.5;
}
.tool-detail-body .permission-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tool-detail-body .permission-icon {
  font-size: 14px;
  flex-shrink: 0;
}
.tool-detail-body .permission-title {
  font-weight: 600;
  color: #dc2626;
}
:root[data-theme="dark"] .tool-detail-body .permission-title {
  color: #fca5a5;
}
.tool-detail-body .permission-tool-name {
  font-weight: 600;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--text-primary);
}
.tool-detail-body .permission-tool-detail {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 4px 8px;
  background: var(--bg-tertiary);
  border-radius: 4px;
}
.tool-detail-body .permission-detail-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}
:root[data-theme="dark"] .tool-detail-body .permission-detail-label {
  background: rgba(248, 113, 113, 0.12);
  color: #fca5a5;
}
.tool-detail-body .permission-tool-detail code {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--text-primary);
  word-break: break-all;
}
.tool-detail-body .permission-options {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.tool-detail-body .permission-btn {
  padding: 6px 14px;
  border-radius: 4px;
  border: 1px solid var(--border-color);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s, background 0.15s;
  background: var(--bg-secondary);
  color: var(--text-primary);
}
.tool-detail-body .permission-btn:hover {
  opacity: 0.85;
}
.tool-detail-body .permission-btn:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}
.tool-detail-body .permission-btn-allow {
  background: rgba(34, 197, 94, 0.1);
  border-color: rgba(34, 197, 94, 0.3);
  color: #16a34a;
}
:root[data-theme="dark"] .tool-detail-body .permission-btn-allow {
  background: rgba(74, 222, 128, 0.12);
  border-color: rgba(74, 222, 128, 0.25);
  color: #4ade80;
}
.tool-detail-body .permission-btn-reject {
  background: rgba(239, 68, 68, 0.08);
  border-color: rgba(239, 68, 68, 0.2);
  color: #dc2626;
}
:root[data-theme="dark"] .tool-detail-body .permission-btn-reject {
  background: rgba(248, 113, 113, 0.1);
  border-color: rgba(248, 113, 113, 0.2);
  color: #fca5a5;
}

/* Permission/ask question categories for overlay header */
.tool-detail-header[data-category="permission"] { --tool-accent: #ef4444; }
:root[data-theme="dark"] .tool-detail-header[data-category="permission"] { --tool-accent: #f87171; }

.tool-detail-body .permission-result {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 600;
  margin-top: 6px;
}

.tool-detail-body .permission-result-approved {
  background: #dcfce7;
  color: #166534;
}

.tool-detail-body .permission-result-denied {
  background: #fee2e2;
  color: #991b1b;
}

:root[data-theme="dark"] .tool-detail-body .permission-result-approved {
  background: #166534;
  color: #dcfce7;
}

:root[data-theme="dark"] .tool-detail-body .permission-result-denied {
  background: #991b1b;
  color: #fee2e2;
}

.tool-detail-body .permission-auto-approved .permission-header {
  opacity: 0.85;
}

.tool-detail-body .permission-result-auto-approved {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 500;
  background: #dcfce7;
  color: #15803d;
  border: 1px solid #bbf7d0;
}

:root[data-theme="dark"] .tool-detail-body .permission-result-auto-approved {
  background: #166534;
  color: #dcfce7;
  border-color: #15803d;
}

/* Tool output status badge (for Write/Edit etc. that return short status) */
.tool-detail-body .tool-output-status-msg {
  padding: 6px 0;
}
.tool-detail-body .tool-output-ok-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}
:root[data-theme="dark"] .tool-detail-body .tool-output-ok-badge {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}
</style>
