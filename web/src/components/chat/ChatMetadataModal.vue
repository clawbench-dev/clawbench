<template>
  <ModalDialog :open="show" size="wide" :zIndex="2500" @close="$emit('close')">
    <template #header>
      <FileText :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('chat.metadata.title') }}</span>
    </template>
    <div class="md-content">
      <!-- Errors are the one thing that must not be buried at the bottom of a
           grouped list: the user opened the modal *because* the turn failed. -->
      <div v-if="data && data.isError" class="md-error">
        <TriangleAlert :size="15" class="md-error-icon" />
        <div class="md-error-body">
          <span class="md-error-label">{{ t('chat.metadata.error') }}</span>
          <span class="md-error-text">{{ data.errorMessage || t('chat.metadata.unknownError') }}</span>
        </div>
      </div>

      <section v-for="section in sections" :key="section.id" class="md-section">
        <div class="md-section-head">
          <component :is="section.icon" :size="13" class="md-section-icon" />
          <span class="md-section-title">{{ t(section.titleKey) }}</span>
        </div>

        <!-- Token counts get tile treatment: they are the numbers people
             actually read, and three short labels in a row scan far faster
             than three more "label: value" lines. -->
        <div v-if="section.tiles.length" class="md-tiles">
          <div v-for="tile in section.tiles" :key="tile.key" class="md-tile">
            <span class="md-tile-label">{{ tile.label }}</span>
            <span class="md-tile-value">{{ tile.value }}</span>
          </div>
        </div>

        <div v-if="section.rows.length" class="md-rows">
          <div
            v-for="row in section.rows"
            :key="row.key"
            class="md-row"
            :class="{ 'md-row--copy': row.copy, 'md-row--stacked': row.stacked }"
            @click="handleRowClick(row, $event)"
          >
            <span class="md-label">{{ row.label }}</span>
            <div class="md-value-box">
              <template v-if="row.chips">
                <span v-for="chip in row.chips" :key="chip.key" class="md-chip">{{ chip.text }}</span>
              </template>
              <template v-else>
                <span
                  class="md-value"
                  :class="{
                    'md-mono': row.mono,
                    'md-value--copyable': row.copy,
                    'md-value--error': row.tone === 'error',
                    'md-value--ok': row.tone === 'ok',
                    'md-value--muted': row.tone === 'muted',
                  }"
                >{{ row.value }}</span>
                <span v-if="row.hint" class="md-hint">{{ row.hint }}</span>
              </template>
              <CopyButton
                v-if="row.copy"
                :text="row.value"
                :size="13"
                title-key="chat.metadata.copy"
                class="md-copy-btn"
                @click.stop
              />
            </div>
          </div>
        </div>
      </section>
    </div>
  </ModalDialog>
</template>

<script setup>
import { computed } from 'vue'
import { FileText, TriangleAlert, Layers, ChartPie, Timer, Database, Fingerprint, Search } from 'lucide-vue-next'
import CopyButton from '@/components/common/CopyButton.vue'
import { copyText } from '@/utils/clipboard'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { formatDuration, formatRelativeTime } from '@/utils/format.ts'

const { t } = useI18n()

const props = defineProps({
  show: Boolean,
  data: { type: Object, default: () => ({}) },
  backend: String,
  createdAt: String,
  relatedFile: String,
  messageId: Number,
  sessionId: String,
  ftsIndexed: Boolean,
  vecIndexed: Boolean,
  formatDetailTime: Function,
})

defineEmits(['close'])

// Map a usageByCategory key (CodeBuddy codebuddy.ai/usageByCategory) to a
// localized label. Unknown future keys fall back to the raw key.
function catLabel(key) {
  const map = {
    conversation: 'chat.sessionInfo.catConversation',
    tools: 'chat.sessionInfo.catTools',
    systemPrompt: 'chat.sessionInfo.catSystemPrompt',
    skills: 'chat.sessionInfo.catSkills',
    mcp: 'chat.sessionInfo.catMCP',
  }
  const i18nKey = map[key]
  return i18nKey ? t(i18nKey) : key
}

// Cache hit rate, same formula as the context panel and the stats page:
// hit / FULL PROMPT, because inputTokens already includes the cached and the
// cache-write portions. Dividing by (hit + miss) would omit the write side and
// report a higher rate than the agent itself does. Null when nothing is
// reported; falls back to hit+miss for a row whose input does not cover the
// split.
const metadataCacheHitRate = computed(() => {
  const hit = Number(props.data?.cacheHitTokens) || 0
  const miss = Number(props.data?.cacheMissTokens) || 0
  if (hit <= 0 && miss <= 0) return null
  const split = hit + miss
  const input = Number(props.data?.inputTokens) || 0
  const denominator = input > 0 && input >= split ? input : split
  if (denominator <= 0) return 0
  return (hit / denominator) * 100
})

/** `1,234` for counts. Every token figure in this modal is a count. */
function count(value) {
  return Number(value).toLocaleString()
}

/**
 * The modal's whole layout is derived here rather than hand-written in the
 * template, so "which section does this field belong to" is a single
 * ordered list instead of 30 scattered `v-if` blocks.
 *
 * Ordering contract (the reason this file was reworked): sections that EVERY
 * backend populates come first — overview, token usage, runtime — and the
 * agent-specific extras (CodeBuddy's cache/credit split, trace identity)
 * follow. A section whose rows are all absent is dropped entirely, so a plain
 * CLI backend renders a short, clean modal instead of a wall of empty labels.
 */
const sections = computed(() => {
  const d = props.data || {}
  const out = []

  // ── 1. Overview — what/when, then how. Reported by every backend. ──
  const overview = []
  if (props.createdAt) {
    overview.push({
      key: 'time',
      label: t('chat.metadata.time'),
      value: props.formatDetailTime ? props.formatDetailTime(props.createdAt) : props.createdAt,
      hint: formatRelativeTime(props.createdAt),
    })
  }
  if (props.messageId) {
    overview.push({ key: 'messageId', label: t('chat.metadata.messageId'), value: String(props.messageId), mono: true, copy: true })
  }
  if (props.sessionId) {
    overview.push({ key: 'sessionId', label: t('chat.metadata.sessionId'), value: props.sessionId, mono: true, copy: true })
  }
  if (d.sessionId && d.sessionId !== props.sessionId) {
    overview.push({ key: 'externalSessionId', label: t('chat.metadata.externalSessionId'), value: d.sessionId, mono: true, copy: true })
  }
  if (props.backend) {
    overview.push({ key: 'backend', label: t('chat.metadata.backend'), value: props.backend })
  }
  if (d.transport) {
    overview.push({ key: 'transport', label: t('chat.metadata.transport'), value: d.transport === 'cli' ? 'CLI' : 'ACP' })
  }
  if (d.model) {
    overview.push({ key: 'model', label: t('chat.metadata.model'), value: d.model })
  }
  if (d.requestModelName) {
    overview.push({ key: 'requestModelName', label: t('chat.metadata.requestModelName'), value: d.requestModelName })
  }
  if (d.mode) {
    overview.push({ key: 'mode', label: t('chat.metadata.mode'), value: d.mode })
  }
  if (d.thinkingEffort) {
    overview.push({ key: 'thinkingEffort', label: t('chat.metadata.thinkingEffort'), value: d.thinkingEffort })
  }
  if (props.relatedFile) {
    overview.push({ key: 'relatedFile', label: t('chat.metadata.relatedFile'), value: props.relatedFile, copy: true, stacked: true })
  }
  out.push({ id: 'overview', titleKey: 'chat.metadata.sectionOverview', icon: Layers, tiles: [], rows: overview })

  // ── 2. Token usage — input/output/total exist for every backend. ──
  const tiles = []
  if (d.inputTokens) tiles.push({ key: 'input', label: t('chat.metadata.inputTokens'), value: count(d.inputTokens) })
  if (d.outputTokens) tiles.push({ key: 'output', label: t('chat.metadata.outputTokens'), value: count(d.outputTokens) })
  if (d.totalTokens) tiles.push({ key: 'total', label: t('chat.metadata.totalTokens'), value: count(d.totalTokens) })
  const tokenRows = []
  if (d.thoughtTokens) {
    tokenRows.push({ key: 'thoughtTokens', label: t('chat.metadata.thoughtTokens'), value: count(d.thoughtTokens) })
  }
  if (d.cachedReadTokens) {
    tokenRows.push({ key: 'cachedReadTokens', label: t('chat.metadata.cachedReadTokens'), value: count(d.cachedReadTokens) })
  }
  if (d.cachedWriteTokens) {
    tokenRows.push({ key: 'cachedWriteTokens', label: t('chat.metadata.cachedWriteTokens'), value: count(d.cachedWriteTokens) })
  }
  out.push({ id: 'tokens', titleKey: 'chat.metadata.sectionTokens', icon: ChartPie, tiles, rows: tokenRows })

  // ── 3. Runtime — how long the turn took and how it ended. ──
  const runtime = []
  if (d.wallMs) {
    runtime.push({ key: 'wallDuration', label: t('chat.metadata.wallDuration'), value: formatDuration(d.wallMs) })
  }
  if (d.durationMs) {
    runtime.push({ key: 'duration', label: t('chat.metadata.duration'), value: formatDuration(d.durationMs) })
  }
  if (d.costUsd) {
    runtime.push({ key: 'cost', label: t('chat.metadata.cost'), value: `$${d.costUsd.toFixed(2)}` })
  }
  if (d.finishReason) {
    runtime.push({ key: 'finishReason', label: t('chat.metadata.finishReason'), value: d.finishReason })
  }
  if (d.stopReason) {
    runtime.push({ key: 'stopReason', label: t('chat.metadata.stopReason'), value: d.stopReason })
  }
  if (d.outcome) {
    runtime.push({ key: 'outcome', label: t('chat.metadata.outcome'), value: d.outcome })
  }
  if (d.agentPhase) {
    runtime.push({ key: 'agentPhase', label: t('chat.metadata.agentPhase'), value: d.agentPhase })
  }
  out.push({ id: 'runtime', titleKey: 'chat.metadata.sectionRuntime', icon: Timer, tiles: [], rows: runtime })

  // ── 4. Cache & credit — CodeBuddy-only `_meta.usage` extras. ──
  const cache = []
  if (d.cacheHitTokens) {
    cache.push({ key: 'cacheHitTokens', label: t('chat.metadata.cacheHitTokens'), value: count(d.cacheHitTokens) })
  }
  if (d.cacheMissTokens) {
    cache.push({ key: 'cacheMissTokens', label: t('chat.metadata.cacheMissTokens'), value: count(d.cacheMissTokens) })
  }
  if (d.cacheCreationTokens) {
    cache.push({ key: 'cacheCreationTokens', label: t('chat.metadata.cacheCreationTokens'), value: count(d.cacheCreationTokens) })
  }
  if (metadataCacheHitRate.value !== null) {
    cache.push({ key: 'cacheHitRate', label: t('chat.metadata.cacheHitRate'), value: `${metadataCacheHitRate.value.toFixed(1)}%` })
  }
  if (d.credit) {
    cache.push({ key: 'credit', label: t('chat.metadata.credit'), value: Number(d.credit).toFixed(4) })
  }
  if (d.usageByCategory && Object.keys(d.usageByCategory).length) {
    cache.push({
      key: 'usageByCategory',
      label: t('chat.metadata.usageByCategory'),
      value: '',
      chips: Object.entries(d.usageByCategory).map(([key, val]) => ({
        key,
        text: `${catLabel(key)}: ${Number(val).toLocaleString()}`,
      })),
      stacked: true,
    })
  }
  out.push({ id: 'cache', titleKey: 'chat.metadata.sectionCache', icon: Database, tiles: [], rows: cache })

  // ── 5. Trace & identity — per-agent `_meta` extensions. ──
  const trace = []
  if (d.requestId) {
    trace.push({ key: 'requestId', label: t('chat.metadata.requestId'), value: d.requestId, mono: true, copy: true })
  }
  if (d.traceId) {
    trace.push({ key: 'traceId', label: t('chat.metadata.traceId'), value: d.traceId, mono: true, copy: true })
  }
  if (d.messageRequestId) {
    trace.push({ key: 'messageRequestId', label: t('chat.metadata.messageRequestId'), value: d.messageRequestId, mono: true, copy: true })
  }
  if (d.responseModelId) {
    trace.push({ key: 'responseModelId', label: t('chat.metadata.responseModelId'), value: d.responseModelId })
  }
  out.push({ id: 'trace', titleKey: 'chat.metadata.sectionTrace', icon: Fingerprint, tiles: [], rows: trace })

  // ── 6. Index status — always rendered: "is this message searchable" is a
  //       diagnostic answer that a missing row would make ambiguous. ──
  out.push({
    id: 'index',
    titleKey: 'chat.metadata.sectionIndex',
    icon: Search,
    tiles: [],
    rows: [
      {
        key: 'ftsIndexed',
        label: t('chat.metadata.ftsIndexed'),
        value: props.ftsIndexed ? t('chat.metadata.indexedYes') : t('chat.metadata.indexedNo'),
        tone: props.ftsIndexed ? 'ok' : 'muted',
      },
      {
        key: 'vecIndexed',
        label: t('chat.metadata.vecIndexed'),
        value: props.vecIndexed ? t('chat.metadata.indexedYes') : t('chat.metadata.indexedNo'),
        tone: props.vecIndexed ? 'ok' : 'muted',
      },
    ],
  })

  return out.filter(s => s.tiles.length || s.rows.length)
})

function handleRowClick(row, event) {
  if (!row.copy) return
  copyValue(row.value, event)
}

/**
 * Copy one metadata value from a CLICKABLE TEXT value (the row / the value
 * itself). The dedicated copy button next to it handles its own state via
 * CopyButton; this path only needs to flash the text it was clicked on.
 *
 * No toast: the flash is the feedback (see the copy-button unification).
 * `copyText` is used rather than a hand-rolled `navigator.clipboard` call so
 * the non-secure-context fallback applies here too.
 */
function copyValue(value, event) {
  const el = event.currentTarget
  if (!el || !value) return
  copyText(value, () => {
    el.classList.add('copied')
    setTimeout(() => el.classList.remove('copied'), 800)
  })
}
</script>

<style scoped>
/* `.modal-body` is already the scroll container (flex:1 + overflow-y:auto);
   adding a second one here would nest scrollbars. */
.md-content {
    /* Horizontal inset is wider than the vertical one: the label/value rows
       already carry a negative inline margin (so their hover fill can bleed
       past the text), which ate most of a symmetric 8px and left the values
       flush against the card edge. 16px matches the other modal bodies
       (.session-share-dialog-body / .session-tags-dialog). */
    padding: var(--space-4) var(--space-7);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
}

/* ── Error banner ── */
.md-error {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-md);
    border: 1px solid color-mix(in srgb, var(--color-red) 35%, transparent);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
}

.md-error-icon {
    flex-shrink: 0;
    margin-top: 1px;
    color: var(--color-red);
}

.md-error-body {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
}

.md-error-label {
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--color-red);
}

.md-error-text {
    font-size: var(--font-size-md);
    color: var(--text-primary);
    word-break: break-all;
    line-height: 1.5;
}

/* ── Group ──
   Groups are SEPARATORS, not containers: no border, no fill, no radius. The
   modal body is already the one surface; a tinted box inside it would be a
   card inside a card, which costs a full border+padding ring on every group
   for no information. The heading's trailing rule is the only decoration —
   it carries the grouping without drawing a box around the content. */
.md-section {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
}

.md-section-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) 0;
}

/* Rule fills whatever the title leaves, so the heading reads as a section
   break rather than a floating label. */
.md-section-head::after {
    content: '';
    flex: 1;
    height: 1px;
    margin-left: var(--space-2);
    background: color-mix(in srgb, var(--text-primary) 12%, transparent);
}

.md-section-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.md-section-title {
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--text-secondary);
    letter-spacing: 0.02em;
}

/* ── Metric tiles ──
   Flat: no box either. The monospace, larger figure is what sets these apart
   from ordinary rows; a background would make them the third nested surface
   on the screen. */
.md-tiles {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-5);
    padding: var(--space-1) 0 var(--space-2);
}

.md-tile {
    flex: 1 1 calc(84 * var(--dp));
    min-width: calc(84 * var(--dp));
    display: flex;
    flex-direction: column;
    gap: calc(2 * var(--dp));
}

.md-tile-label {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    white-space: nowrap;
}

.md-tile-value {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    font-size: var(--font-size-xl);
    color: var(--text-primary);
}

/* ── Rows ── */
.md-row {
    display: grid;
    grid-template-columns: minmax(calc(76 * var(--dp)), 34%) 1fr;
    align-items: start;
    gap: var(--space-4);
    /* Negative inline margin + matching padding: the row's hover fill reaches
       slightly past the text (so it reads as a row, not a text highlight)
       without shifting the label/value columns. */
    margin: 0 calc(-1 * var(--space-2));
    padding: calc(2 * var(--dp)) var(--space-2);
    border-radius: var(--radius-xs);
}

/* Long ids/files get the label on its own line — squeezing a 60-char path into
   a 1fr column next to a 34% label leaves ~3 characters per line. */
.md-row--stacked {
    grid-template-columns: 1fr;
    gap: var(--space-2);
}

.md-label {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    line-height: 1.5;
}

.md-value-box {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: var(--space-3);
    min-width: 0;
}

.md-value {
    font-size: var(--font-size-md);
    color: var(--text-primary);
    line-height: 1.5;
    word-break: break-all;
}

.md-mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
}

.md-hint {
    font-size: var(--font-size-xs);
    color: var(--text-hint);
}

.md-value--ok {
    color: var(--color-green);
}

.md-value--muted {
    color: var(--text-muted);
}

.md-value--error {
    color: var(--color-red);
}

.md-value--copyable {
    cursor: pointer;
}

.md-row--copy {
    cursor: pointer;
    user-select: none;
}

@media (hover: hover) {
  .md-row--copy:hover {
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }

  .md-row--copy:hover .md-value--copyable {
    color: var(--accent-color);
  }
}

.md-row--copy.copied .md-value--copyable,
.md-row--copy .md-value--copyable.copied {
    color: var(--color-green);
}

.md-copy-btn {
    flex-shrink: 0;
    align-self: center;
    display: flex;
    align-items: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-hint);
    padding: var(--space-1);
    border-radius: var(--radius-xs);
    transition: color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .md-copy-btn:hover {
    color: var(--accent-color);
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
}

.md-chip {
    display: inline-block;
    padding: 1px var(--space-4);
    border-radius: var(--radius-xs);
    /* 10% (not the 6–7% used for row separators): the chip has to read as a
       distinct object inside an already-tinted section card, and at 7% it
       dissolved into the card in the darker themes. */
    background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-secondary));
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    white-space: nowrap;
}
</style>
