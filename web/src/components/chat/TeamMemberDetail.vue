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

      <!-- Prompt: the member's full instructions. Rendered as Markdown (not a
           <pre> dump) so headings/lists/code in the prompt read as intended,
           with file paths annotated exactly like chat content. -->
      <div v-if="promptHtml" class="team-member-detail__section">
        <div class="team-member-detail__section-title">{{ t('chat.team.detail.prompt') }}</div>
        <div ref="promptRef" class="team-member-detail__prompt agent-call-prompt" v-html="promptHtml"></div>
      </div>
      <div v-else-if="promptState === 'loading'" class="team-member-detail__section team-member-detail__muted">
        <span class="li-spinner" />
        <span>{{ t('chat.team.detail.loadingPrompt') }}</span>
      </div>
      <div v-else-if="promptState === 'empty'" class="team-member-detail__section team-member-detail__muted">
        {{ t('chat.team.detail.noPrompt') }}
      </div>

      <!-- Result: the member's final answer, when one was recorded. -->
      <div v-if="outputHtml" class="team-member-detail__section">
        <div class="team-member-detail__section-title">{{ t('chat.team.detail.output') }}</div>
        <div ref="outputRef" class="team-member-detail__prompt agent-call-prompt" v-html="outputHtml"></div>
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
import { appLog } from '@/utils/appLog'
import type { TeamMember } from '@/composables/useTeamState'

const props = defineProps({
  open: { type: Boolean, default: false },
  member: { type: Object as () => TeamMember | null, default: null },
  /** Tool-call id of the Agent card that spawned this member. When known, the
   *  full spawn input (prompt, description) is fetched from the tool-call
   *  endpoint — the in-memory block is a slim one with no input. */
  toolId: { type: String, default: '' },
  /** Message that owns the Agent card, required by the tool-call endpoint. */
  msgId: { type: [String, Number], default: '' },
  sessionId: { type: String, default: '' },
})

defineEmits(['close'])

const { t } = useI18n()
const { openFilePath } = useFilePathAnnotation()
const bodyRef = ref<HTMLElement | null>(null)
const promptRef = ref<HTMLElement | null>(null)
const outputRef = ref<HTMLElement | null>(null)

const promptState = ref<'idle' | 'loading' | 'empty' | 'ready'>('idle')
const promptHtml = ref('')
const outputHtml = ref('')

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

/** Facts grid: only fields the wire actually reported. An empty value is
 *  omitted rather than rendered as a blank row. */
const facts = computed(() => {
  const m = props.member
  if (!m) return []
  const out: Array<{ label: string; value: string }> = []
  if (m.agentType) out.push({ label: t('chat.team.agentType'), value: m.agentType })
  if (m.description) out.push({ label: t('chat.team.detail.description'), value: m.description })
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
 * the verify step. Doing it here — scoped to this drawer's own element — means
 * a path in a member's prompt behaves exactly like one in a chat message:
 * clickable chip, correct icon, line-range support.
 */
function renderAnnotated(text: string): string {
  const { html, detectedPaths, detectedSHAs } = renderMarkdown(text, { sanitize: true, wrapTables: false })
  if (detectedPaths.length > 0 || detectedSHAs.length > 0) {
    nextTick(() => {
      const el = promptRef.value || outputRef.value || bodyRef.value
      if (!el) return
      if (detectedPaths.length > 0) void verifyFilePaths([...new Set(detectedPaths)], el)
      if (detectedSHAs.length > 0) void verifyCommitHashes([...new Set(detectedSHAs)], el)
    })
  }
  return html
}

/**
 * Load the member's full prompt.
 *
 * The chat block for an Agent card is a SLIM one — its `input` is empty on a
 * DB-loaded message, so the prompt only exists in the `chat_tool_calls` row.
 * The endpoint is the same one the tool-detail drawer uses, so a member's
 * prompt and its Agent card show identical content.
 */
async function loadPrompt() {
  const m = props.member
  if (!m) return
  promptHtml.value = ''
  outputHtml.value = ''

  if (!props.toolId || !props.msgId) {
    // No Agent card to read from. The wire's own `description` is the only
    // prompt-ish text available, and it is already shown in the facts grid.
    promptState.value = 'empty'
    return
  }

  promptState.value = 'loading'
  try {
    let url = `/api/ai/chat/tool-call?tool_id=${encodeURIComponent(props.toolId)}&message_id=${encodeURIComponent(String(props.msgId))}`
    if (props.sessionId) url += `&session_id=${encodeURIComponent(props.sessionId)}`
    const resp = await fetch(url)
    if (!resp.ok) {
      promptState.value = 'empty'
      return
    }
    const data = await resp.json()
    const rawInput = data.input
    const input = typeof rawInput === 'string' ? JSON.parse(rawInput) : rawInput
    const prompt = typeof input?.prompt === 'string' ? input.prompt : ''
    if (prompt) {
      promptHtml.value = renderAnnotated(prompt)
      promptState.value = 'ready'
    } else {
      promptState.value = 'empty'
    }
    if (typeof data.output === 'string' && data.output.trim() !== '') {
      outputHtml.value = renderAnnotated(data.output)
    }
  } catch (e) {
    appLog.w('TeamMemberDetail', 'failed to load member prompt:', e)
    promptState.value = 'empty'
  }
}

// Reload whenever the drawer opens on a (possibly different) member. Watching
// `open` rather than the member alone matters: the same member can be re-opened
// after a reconnect, and the cached HTML would otherwise be reused forever.
watch(
  () => [props.open, props.toolId, props.msgId] as const,
  ([open]) => { if (open) void loadPrompt() },
  { immediate: true },
)

// File-path chips are plain spans until verified, and verification is an async
// server round-trip; re-run it if the drawer is re-opened without a refetch.
watch(promptHtml, () => {
  nextTick(() => {
    if (!bodyRef.value) return
    const paths = [...bodyRef.value.querySelectorAll('.chat-file-path[data-file-path]:not([data-path-type])')]
      .map((el) => el.getAttribute('data-file-path'))
      .filter((p): p is string => !!p)
    if (paths.length > 0) void verifyFilePaths([...new Set(paths)], bodyRef.value)
  })
})

function handleBodyClick(e: MouseEvent) {
  // File-path chips inside v-html are normally wired by ChatMessageList's
  // delegated click handler, which never sees clicks inside this teleported
  // sheet. Route them through the SAME resolver + opener so a path in a
  // member's prompt behaves identically to one in a chat message (including
  // `:line` ranges and directory chips).
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
.team-member-detail__prompt {
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

.team-member-detail__prompt :deep(p:first-child) { margin-top: 0; }
.team-member-detail__prompt :deep(p:last-child) { margin-bottom: 0; }
.team-member-detail__prompt :deep(p) { margin: 0 0 var(--space-2); }

.team-member-detail__prompt :deep(h1),
.team-member-detail__prompt :deep(h2),
.team-member-detail__prompt :deep(h3),
.team-member-detail__prompt :deep(h4) {
  margin: var(--space-4) 0 var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.team-member-detail__prompt :deep(ul),
.team-member-detail__prompt :deep(ol) {
  margin: var(--space-2) 0;
  padding-left: var(--space-8);
}

.team-member-detail__prompt :deep(li) { margin: var(--space-1) 0; }

.team-member-detail__prompt :deep(code) {
  padding: 1px var(--space-2);
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  border-radius: var(--radius-xs);
}

.team-member-detail__prompt :deep(pre) {
  margin: var(--space-2) 0;
  padding: var(--space-3) var(--space-4);
  overflow-x: auto;
  background: var(--bg-secondary);
  border-radius: var(--radius-xs);
}

.team-member-detail__prompt :deep(pre code) {
  padding: 0;
  background: none;
  font-size: var(--font-size-xs);
}

.team-member-detail__prompt :deep(strong) {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.team-member-detail__prompt :deep(blockquote) {
  margin: var(--space-2) 0;
  padding-left: var(--space-3);
  border-left: 3px solid var(--border-color);
  color: var(--text-secondary);
}

/* Tables scroll inside the sheet instead of widening it. */
.team-member-detail__prompt :deep(table) {
  display: block;
  overflow-x: auto;
  margin: var(--space-2) 0;
  border-collapse: collapse;
}

.team-member-detail__prompt :deep(th),
.team-member-detail__prompt :deep(td) {
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--border-color);
  text-align: left;
}

.team-member-detail__prompt :deep(hr) {
  margin: var(--space-3) 0;
  border: none;
  border-top: 1px solid var(--border-color);
}

.team-member-detail__prompt :deep(img) {
  max-width: 100%;
  height: auto;
}

.team-member-detail__muted {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
</style>
