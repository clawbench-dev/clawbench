<template>
  <BottomSheet :open="open" auto maximized panel-class="team-member-sheet" @close="$emit('close')">
    <template #header>
      <div class="team-member-detail__header">
        <span class="team-member-detail__dot" :style="{ background: accentColor }" aria-hidden="true" />
        <span class="team-member-detail__name">{{ member?.name || '' }}</span>
        <span v-if="statusKind" class="team-member-detail__status" :data-status="statusKind">{{ statusLabel }}</span>
      </div>
    </template>

    <div ref="bodyRef" class="team-member-detail__body" @click="handleBodyClick">
      <!-- Fact grid: everything the wire reports about the member, in a
           definition list so a screen reader reads "label, value" pairs. -->
      <dl class="team-member-detail__facts">
        <template v-for="f in facts" :key="f.label">
          <dt class="team-member-detail__fact-label">{{ f.label }}</dt>
          <dd class="team-member-detail__fact-value">{{ f.value }}</dd>
        </template>
      </dl>

      <!-- Role / instructions.
           This is the roster's `description` rendered as Markdown — see
           `roleHtml` for why it is the description and not a separately
           fetched prompt. -->
      <div v-if="roleHtml" class="team-member-detail__section">
        <div class="team-member-detail__section-title">{{ t('chat.team.detail.description') }}</div>
        <div ref="roleRef" class="team-member-detail__markdown agent-call-prompt" v-html="roleHtml"></div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import { verifyFilePaths, readLineTargetFromEl, useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { verifyCommitHashes } from '@/composables/useCommitHashAnnotation.ts'
import { teamMemberColorVar } from '@/utils/teamMemberColor'
import type { TeamMember } from '@/composables/useTeamState'

const props = defineProps({
  open: { type: Boolean, default: false },
  member: { type: Object as () => TeamMember | null, default: null },
})

defineEmits(['close'])

const { t } = useI18n()
const { openFilePath } = useFilePathAnnotation()
const bodyRef = ref<HTMLElement | null>(null)
const roleRef = ref<HTMLElement | null>(null)

const accentColor = computed(() => teamMemberColorVar(props.member?.color))

// ── Status ──
// Same mapping as the roster so the drawer and the row never disagree.
const statusKind = computed(() => {
  const m = props.member
  if (!m) return ''
  switch (m.status) {
    case 'running': return 'running'
    case 'completed': return 'completed'
    case 'failed': return 'failed'
    case 'killed': return 'terminated'
    default: return m.activity === 'working' ? 'running' : 'pending'
  }
})

const statusLabel = computed(() =>
  statusKind.value ? t(`chat.team.status.${statusKind.value}`) : '',
)

/**
 * The member's instructions, rendered as Markdown.
 *
 * This is the roster's `description` — NOT a separately fetched prompt. That
 * choice is deliberate and was verified against a real `codebuddy --acp`
 * process (internal/ai/codebuddy_team_description_probe_integration_test.go):
 * the roster's `description` is byte-for-byte the spawn frame's `prompt`
 * (measured: both 196 bytes, `description == prompt` true), so fetching the
 * prompt from `chat_tool_calls` returned the exact same text.
 *
 * Worse, the fetch was also fragile: after an interruption/reconnect the Agent
 * card's slim block has no `input`, and the member's `sessionId` may no longer
 * resolve, so the prompt silently came back empty while `description` kept
 * working — the sheet would go blank exactly when the user was recovering from
 * a dropped session.
 *
 * `description` is what the roster always carries, so the sheet now renders it
 * directly: no request, no loading state, and nothing to lose on reconnect.
 */
const roleHtml = computed(() => {
  const text = props.member?.description
  if (!text) return ''
  return renderAnnotated(text)
})

/** Facts grid: only fields the wire actually reported. An empty value is
 *  omitted rather than rendered as a blank row.
 *
 *  `description` is NOT a fact row — it is the Markdown body below, which would
 *  otherwise be shown twice. */
const facts = computed(() => {
  const m = props.member
  if (!m) return []
  const out: Array<{ label: string; value: string }> = []
  if (m.agentType) out.push({ label: t('chat.team.agentType'), value: m.agentType })
  if (m.activity) out.push({ label: t('chat.team.detail.activity'), value: m.activity })
  if (m.lifecycle) out.push({ label: t('chat.team.detail.lifecycle'), value: m.lifecycle })
  if (typeof m.toolCallCount === 'number') {
    out.push({ label: t('chat.team.detail.toolCalls'), value: String(m.toolCallCount) })
  }
  const u = m.tokenUsage
  if (u) {
    out.push({
      label: t('chat.team.tokens'),
      value: `${u.inputTokens ?? 0} in / ${u.outputTokens ?? 0} out`,
    })
    if (u.lastContextWindow) {
      out.push({ label: t('chat.team.contextWindow'), value: String(u.lastContextWindow) })
    }
  }
  if (m.taskId) out.push({ label: t('chat.team.detail.taskId'), value: m.taskId })
  if (m.sessionId) out.push({ label: t('chat.team.detail.sessionId'), value: m.sessionId })
  return out
})

/**
 * Render a Markdown payload and annotate the file paths / commit hashes it
 * mentions.
 *
 * `renderMarkdown` returns the detected paths instead of annotating inline
 * (annotation needs an existence check against the server), so the caller owns
 * the verify step. Doing it here — scoped to this sheet's own element — means
 * a path in a member's instructions behaves exactly like one in a chat message:
 * clickable chip, correct icon, line-range support.
 */
function renderAnnotated(text: string): string {
  const { html, detectedPaths, detectedSHAs } = renderMarkdown(text, { sanitize: true, wrapTables: false })
  if (detectedPaths.length > 0 || detectedSHAs.length > 0) {
    nextTick(() => annotate())
  }
  return html
}

/** Verify the path/commit chips currently in the body. */
function annotate() {
  const el = bodyRef.value
  if (!el) return
  const paths = [...el.querySelectorAll('.chat-file-path[data-file-path]:not([data-path-type])')]
    .map((n) => n.getAttribute('data-file-path'))
    .filter((p): p is string => !!p)
  const shas = [...el.querySelectorAll('.chat-commit-hash-pending[data-commit-sha]')]
    .map((n) => n.getAttribute('data-commit-sha'))
    .filter((s): s is string => !!s)
  if (paths.length > 0) void verifyFilePaths([...new Set(paths)], el)
  if (shas.length > 0) void verifyCommitHashes([...new Set(shas)], el)
}

// Re-verify on every re-render of the Markdown body. The HTML is written with
// v-html, so the chips exist as plain spans until verification marks them —
// and a re-open (or a member switch) rebuilds them from scratch.
watch(roleHtml, () => nextTick(annotate), { immediate: true })

function handleBodyClick(e: MouseEvent) {
  // File-path chips inside v-html are normally wired by ChatMessageList's
  // delegated click handler, which never sees clicks inside this teleported
  // sheet. Route them through the SAME resolver + opener so a path in a
  // member's instructions behaves identically to one in a chat message
  // (including `:line` ranges and directory chips).
  const el = (e.target as HTMLElement)?.closest(
    '.chat-file-open-btn, .chat-file-path[data-path-type="dir"]',
  ) as HTMLElement | null
  if (!el) return
  e.preventDefault()
  e.stopPropagation()
  const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(el)
  if (!filePath) return
  void (lineRanges
    ? openFilePath(filePath, lineStart, lineEnd, 'chat', lineRanges)
    : openFilePath(filePath, lineStart, lineEnd, 'chat'))
}
</script>

<style scoped>
.team-member-detail__header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

/* The member's colour, same swatch as the roster name. */
.team-member-detail__dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: var(--radius-full);
}

.team-member-detail__name {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.team-member-detail__status {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.team-member-detail__status[data-status='running'] { color: var(--color-info); }
.team-member-detail__status[data-status='completed'] { color: var(--color-green); }
.team-member-detail__status[data-status='failed'] { color: var(--color-red); }

.team-member-detail__body {
  padding: var(--space-2) var(--space-1) var(--space-6);
  overflow-y: auto;
}

/* ── Facts grid ──
   Two columns: a fixed label track and a value track that absorbs the rest.
   `minmax(0, 1fr)` on the value column is what lets long ids (taskId,
   sessionId) wrap instead of pushing the sheet wider than the viewport. */
.team-member-detail__facts {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: var(--space-2) var(--space-4);
  margin: 0 0 var(--space-5);
  padding: var(--space-4);
  background: var(--bg-secondary);
  border-radius: var(--radius-md);
}

.team-member-detail__fact-label {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  white-space: nowrap;
}

.team-member-detail__fact-value {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  overflow-wrap: break-word;
}

.team-member-detail__section {
  margin-bottom: var(--space-5);
}

.team-member-detail__section-title {
  margin-bottom: var(--space-3);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* Markdown body.
 *
 * Deliberately mirrors the Agent card's own prompt styling in ContentBlocks
 * (`.content-blocks .tool-detail .agent-call-prompt`): the same text must not
 * look different depending on whether it is read in the card's detail drawer or
 * in the team member sheet. Those rules are scoped under `.tool-detail`, so they
 * cannot be inherited here — the values are duplicated on purpose, and this
 * block is the one to update if that styling ever changes.
 *
 * `:deep()` is required: the HTML comes from v-html, whose elements carry no
 * scoped attribute (design-guide 红线 1). */
.team-member-detail__markdown {
  padding: var(--space-3) var(--space-4);
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-relaxed);
  white-space: normal;
  overflow-wrap: break-word;
}

.team-member-detail__markdown :deep(p:first-child) { margin-top: 0; }
.team-member-detail__markdown :deep(p:last-child) { margin-bottom: 0; }
.team-member-detail__markdown :deep(p) { margin: 0 0 var(--space-2); }

.team-member-detail__markdown :deep(h1),
.team-member-detail__markdown :deep(h2),
.team-member-detail__markdown :deep(h3),
.team-member-detail__markdown :deep(h4) {
  margin: var(--space-4) 0 var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.team-member-detail__markdown :deep(ul),
.team-member-detail__markdown :deep(ol) {
  margin: var(--space-2) 0;
  padding-left: var(--space-8);
}

.team-member-detail__markdown :deep(li) { margin: var(--space-1) 0; }

.team-member-detail__markdown :deep(code) {
  padding: 1px var(--space-2);
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  border-radius: var(--radius-xs);
}

.team-member-detail__markdown :deep(pre) {
  margin: var(--space-2) 0;
  padding: var(--space-3) var(--space-4);
  overflow-x: auto;
  background: var(--bg-secondary);
  border-radius: var(--radius-xs);
}

.team-member-detail__markdown :deep(pre code) {
  padding: 0;
  background: none;
  font-size: var(--font-size-xs);
}

.team-member-detail__markdown :deep(strong) {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.team-member-detail__markdown :deep(blockquote) {
  margin: var(--space-2) 0;
  padding-left: var(--space-3);
  border-left: 3px solid var(--border-color);
  color: var(--text-secondary);
}

/* Tables scroll inside the sheet instead of widening it. */
.team-member-detail__markdown :deep(table) {
  display: block;
  overflow-x: auto;
  margin: var(--space-2) 0;
  border-collapse: collapse;
}

.team-member-detail__markdown :deep(th),
.team-member-detail__markdown :deep(td) {
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--border-color);
  text-align: left;
}

.team-member-detail__markdown :deep(hr) {
  margin: var(--space-3) 0;
  border: none;
  border-top: 1px solid var(--border-color);
}

.team-member-detail__markdown :deep(img) {
  max-width: 100%;
  height: auto;
}
</style>

<style>
/* Wide-screen: the member sheet carries the member's full instructions, which
   are long-form Markdown (headings, lists, tables, code fences). The default
   624px card wraps those badly. Mirrors .tool-detail-sheet / .session-search-sheet.
   Only takes effect in BottomSheet's wide-screen card mode (the narrow-mode
   bottom sheet ignores --modal-max-width).
   Non-scoped because the class is bound inside BottomSheet's own template. */
.team-member-sheet {
  --modal-max-width: 900px;
}
</style>
