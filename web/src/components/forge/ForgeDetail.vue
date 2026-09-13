<template>
  <div class="forge-detail">
    <!-- Standard drill-down header: var(--header-height) with a round icon
         button, matching every other detail page. -->
    <div class="forge-detail-header">
      <button class="forge-back" @click="emit('back')">
        <ChevronLeft :size="18" />
        <span>{{ t('forge.detail.back') }}</span>
      </button>
      <div class="forge-detail-actions">
        <button
          v-if="detail.item.value"
          class="forge-icon-btn"
          :title="t('forge.detail.quote')"
          :aria-label="t('forge.detail.quote')"
          @mousedown.prevent
          @click="onQuote"
        >
          <MessageSquare :size="15" />
        </button>
        <a
          v-if="detail.item.value"
          class="forge-icon-btn"
          :href="detail.item.value.url"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('forge.detail.openBrowser')"
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
        <div class="forge-error-title">{{ t('forge.error.generic') }}</div>
        <div class="forge-error-body">{{ detail.error.value.message }}</div>
      </div>
    </div>

    <template v-else-if="detail.item.value">
      <!-- `data-quote-source` labels text selected inside this region so the
           quote pipeline can tag the fence with the issue/PR identity. It is
           deliberately NOT `data-file-path`: that attribute marks a markdown
           body as an attachable file and would grow "add to chat" buttons on
           every code block in the issue body. -->
      <div
        ref="bodyRef"
        class="forge-detail-body"
        :data-quote-source="quoteSourceLabel"
        :data-quote-language="detail.item.value.type"
        @click="handleContentClick"
      >
        <!-- Title + meta -->
        <div class="forge-detail-title-row">
          <span class="forge-state-dot" :class="`state-${detail.item.value.state}`"></span>
          <div class="forge-detail-title-main">
            <h2 class="forge-detail-title">{{ detail.item.value.title }}</h2>
            <div class="forge-detail-meta">
              <span class="forge-detail-number">#{{ detail.item.value.number }}</span>
              <span class="forge-meta-sep">·</span>
              <span>{{ detail.item.value.author }}</span>
              <span class="forge-meta-sep">·</span>
              <span>{{ formatTime(detail.item.value.createdAt) }}</span>
              <span class="forge-state-badge" :class="`state-${detail.item.value.state}`">
                {{ t(`forge.state.${stateKey(detail.item.value.state)}`) }}
              </span>
            </div>
          </div>
        </div>

        <!-- Body rendered through the shared markdown pipeline -->
        <div
          v-if="detail.item.value.body"
          class="forge-detail-content markdown-body"
          v-html="renderedBody"
        ></div>

        <!-- Comments -->
        <div class="forge-comments">
          <div v-if="detail.comments.value.length" class="forge-comments-title">
            <MessageSquare :size="14" />
            <span>{{ detail.comments.value.length }}</span>
          </div>
          <button
            v-if="detail.hasMoreComments.value"
            class="forge-load-more-comments"
            :disabled="detail.loadingComments.value"
            @click="detail.loadOlderComments()"
          >
            {{ detail.loadingComments.value ? t('forge.loading') : t('forge.detail.loadOlder') }}
          </button>
          <div v-for="c in detail.comments.value" :key="c.id" class="forge-comment">
            <div class="forge-comment-meta">
              <span class="forge-comment-author">{{ c.author }}</span>
              <span class="forge-comment-time">{{ formatTime(c.createdAt) }}</span>
            </div>
            <div class="forge-comment-body markdown-body" v-html="renderComment(c.body)"></div>
          </div>
        </div>
      </div>

    </template>

    <!-- Floating code preview for annotated file paths (same card as chat/task). -->
    <CodeLinkPreview v-if="codeLinkPreview.enabled.value" :preview="codeLinkPreview" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ExternalLink, MessageSquare, AlertCircle } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgeDetail } from '@/composables/useForge'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation'
import { verifyCommitHashes } from '@/composables/useCommitHashAnnotation'
import { useCodeLinkPreview, handleVerifiedFilePathClick } from '@/composables/useCodeLinkPreview'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgeDetail'
const props = defineProps<{
  type: 'issue' | 'pr'
  number: number
}>()
const emit = defineEmits<{
  (e: 'back'): void
  (e: 'quote', payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string } }): void
}>()

const { t } = useI18n()
const detail = useForgeDetail()
const { verifyFilePaths, openFilePath, readLineTargetFromEl } = useFilePathAnnotation()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

// The detail body renders outside every container that runs path verification
// (chat verifies against #aiChatMessages, tasks against their own refs), so
// annotations produced by the shared markdown pipeline were never checked
// against disk — an issue body mentioning a path that does not exist (an
// example, or a file in another repository) kept a live-looking but dead chip,
// and there was no click handler to open even a real one. This component now
// owns both halves: verifyAnnotations() below, and handleContentClick().
const bodyRef = ref<HTMLElement | null>(null)
const codeLinkPreview = useCodeLinkPreview({ containerRef: bodyRef, source: 'forge' })

onMounted(() => {
  void detail.open(props.type, props.number)
})

// Re-render and re-verify whenever the loaded item or its comments change.
// renderId guards against a slow verification pass from a previous item
// mutating the container after the user has moved on.
let renderId = 0
watch(
  () => [detail.item.value, detail.comments.value] as const,
  async () => {
    const id = ++renderId
    await nextTick()
    if (id !== renderId) return
    verifyAnnotations()
  },
  { immediate: true },
)

function verifyAnnotations() {
  const el = bodyRef.value
  if (!el) return
  // Collect what the pipeline actually annotated rather than re-deriving from
  // the body text: the annotation step resolves paths (baseDir, ~, Windows
  // separators) in ways a second parse here would have to duplicate.
  const paths = [...el.querySelectorAll('.chat-file-open-btn[data-file-path]')]
    .map(btn => btn.getAttribute('data-file-path'))
    .filter((p): p is string => !!p)
  if (paths.length > 0) void verifyFilePaths([...new Set(paths)], el)

  const shas = [...el.querySelectorAll('.chat-commit-open-btn[data-commit-sha], .chat-commit-hash-pending[data-commit-sha]')]
    .map(node => node.getAttribute('data-commit-sha'))
    .filter((s): s is string => !!s)
  if (shas.length > 0) void verifyCommitHashes([...new Set(shas)], el)
}

function handleContentClick(event: MouseEvent) {
  // Code / table block header buttons (copy, wrap).
  if (handleCodeBlockClick(event)) return
  if (handleTableBlockClick(event)) return

  // localhost URLs are App-mode only; a no-op on the web.
  if (handleLocalhostUrlClick(event)) return

  // Verified file paths open the code link preview, matching chat/task.
  if (handleVerifiedFilePathClick(event, codeLinkPreview)) return

  const target = event.target as HTMLElement | null

  const commitEl = target?.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    return
  }

  const btn = target?.closest<HTMLElement>('.chat-file-open-btn[data-file-path]')
  const dirEl = target?.closest<HTMLElement>('.chat-file-path[data-file-path][data-path-type="dir"]')
  const linkOrBtn = btn || dirEl
  if (!linkOrBtn) return
  event.preventDefault()
  event.stopPropagation()
  codeLinkPreview.close()
  const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(linkOrBtn)
  if (!filePath) return
  if (lineRanges) void openFilePath(filePath, lineStart, lineEnd, 'forge', lineRanges)
  else void openFilePath(filePath, lineStart, lineEnd, 'forge')
}

const renderedBody = computed(() => {
  const body = detail.item.value?.body ?? ''
  return body ? renderMarkdownHtml(body) : ''
})

// Comments carry the same annotations, so they render through the pipeline too;
// verification runs once per comment render via the shared watcher above.
function renderComment(body: string): string {
  try {
    return renderMarkdownHtml(body || '')
  } catch (err) {
    appLog.w(TAG, 'render comment failed', err)
    return ''
  }
}

/**
 * Label quoted text with the issue/PR it came from, e.g. "acme/widgets#123".
 * Empty when no item is loaded, which makes getQuoteSource() treat the region
 * as unlabelled and fall back to normal file handling.
 */
const quoteSourceLabel = computed(() => {
  const it = detail.item.value
  if (!it) return ''
  return `${it.slug}#${it.number}`
})

function onQuote() {
  const it = detail.item.value
  if (!it) return
  emit('quote', {
    item: { type: it.type, number: it.number, title: it.title, url: it.url, slug: it.slug },
  })
}

// Map the normalized state to its i18n key. "merged" only exists under
// forge.state; open/closed are shared with the list filter labels.
function stateKey(state: string): string {
  return state === 'merged' ? 'merged' : state === 'closed' ? 'closed' : 'open'
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString()
}
</script>

<style scoped>
.forge-detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary);
}

/* ── Header — same 36px bar as every other drill-down page ── */
.forge-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--header-height);
  padding: 0 4px 0 6px;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
  flex-shrink: 0;
  gap: 8px;
}
.forge-back {
  display: flex;
  align-items: center;
  gap: 2px;
  background: transparent;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: var(--font-size-md);
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  transition: background 0.15s ease;
}
@media (hover: hover) {
  .forge-back:hover { background: var(--bg-secondary); }
}
.forge-back:active { background: var(--bg-tertiary); }
.forge-detail-actions { display: flex; align-items: center; gap: 4px; }
/* Round icon button, styled after the per-component .header-btn used across the
   app (there is no shared global class — each panel defines its own).
   `border: none` is required: this class is used by BOTH <a> and <button>, and
   a bare <button> keeps the UA's default border, which shows up as a stray ring
   around the round icon. (<a> has no default border, which is why the gap only
   appeared once a <button> used this class.) */
.forge-icon-btn {
  width: 28px;
  height: 28px;
  border-radius: 14px;
  border: none;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  cursor: pointer;
  text-decoration: none;
  transition: background 0.2s ease, color 0.2s ease;
}
@media (hover: hover) {
  .forge-icon-btn:hover {
    background: var(--bg-tertiary);
    color: var(--accent-color);
  }
}

.forge-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* ── Error ── */
.forge-error-card {
  margin: 12px;
  padding: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px solid color-mix(in srgb, var(--color-red) 35%, var(--border-color));
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 6%, var(--bg-secondary));
}
.forge-error-icon { color: var(--color-red); flex-shrink: 0; }
.forge-error-text { flex: 1; min-width: 0; }
.forge-error-title { font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); margin-bottom: 2px; }
.forge-error-body { color: var(--text-secondary); font-size: var(--font-size-sm); }

/* ── Body ── */
.forge-detail-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px;
}
.forge-detail-title-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.forge-detail-title-main { flex: 1; min-width: 0; }
.forge-detail-title {
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-semibold);
  margin: 0;
  line-height: 1.35;
  color: var(--text-primary);
}
.forge-detail-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.forge-detail-number {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.forge-meta-sep { opacity: 0.5; }
/* State badge — tinted pill instead of bare coloured text. */
.forge-state-badge {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  padding: 1px 8px;
  border-radius: 999px;
  border: 1px solid transparent;
}
.forge-state-badge.state-open {
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success) 35%, transparent);
}
.forge-state-badge.state-closed {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 35%, transparent);
}
.forge-state-badge.state-merged {
  color: var(--color-purple);
  background: color-mix(in srgb, var(--color-purple) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-purple) 35%, transparent);
}
.forge-detail-content {
  margin-top: 14px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border-color);
}

/* ── Comments ── */
.forge-comments {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.forge-comments-title {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
}
.forge-load-more-comments {
  align-self: center;
  padding: 5px 14px;
  border: 1px solid var(--border-color);
  border-radius: 999px;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}
@media (hover: hover) {
  .forge-load-more-comments:hover:not(:disabled) {
    border-color: var(--accent-color);
    background: var(--bg-secondary);
  }
}
.forge-load-more-comments:disabled { opacity: 0.5; cursor: not-allowed; }
.forge-comment {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  overflow: hidden;
  background: var(--bg-secondary);
}
.forge-comment-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  border-bottom: 1px solid var(--border-color);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.forge-comment-author { font-weight: var(--font-weight-semibold); color: var(--text-primary); }
.forge-comment-body {
  padding: 10px 12px;
  background: var(--bg-primary);
}

/* Status dot — baseline-aligned with the title's first line. */
.forge-state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  display: inline-block;
  margin-top: 6px;
}
.forge-state-dot.state-open { background: var(--color-success); }
.forge-state-dot.state-closed { background: var(--color-red); }
.forge-state-dot.state-merged { background: var(--color-purple); }
</style>
