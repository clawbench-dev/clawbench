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
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion'
import { getQuoteSource } from '@/utils/quoteQuestionUtils'
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

// Same double-click-to-copy + quote-reply pipeline as the markdown preview:
// a double-click copies the block and opens the shared quote bar so the user
// can turn the copied text into a chat message without re-selecting it.
//
// No `lineSelector`: the detail body is *rendered* markdown (the `.code-line`
// rows only exist in CodeMirror's raw view), so every double-click resolves to
// a block element. Issue/PR text has no file line numbers, so both line fields
// stay 0 and the quote fence carries no `:N` suffix — matching the selection
// path in useQuoteQuestion, which tags a `data-quote-source` region the same way.
const quoteQuestion = useQuoteQuestion()
const { handleDblClick } = useDoubleClickCopy({
  onCopy(target, text) {
    const el = target as HTMLElement | null
    // Reuse the selection path's label resolution: it walks up to the nearest
    // `[data-quote-source]` region (the issue/PR body) and reads its identity.
    const source = el ? getQuoteSource(el) : null
    quoteQuestion.showBar({
      text,
      filePath: source?.label || '',
      language: source?.language || '',
      startLine: 0,
      endLine: 0,
    })
  },
})

onMounted(() => {
  void detail.open(props.type, props.number)
})

// Re-render and re-verify whenever the loaded item or its comments change.
// renderId guards against a slow verification pass from a previous item
// mutating the container after the user has moved on.
//
// `loading` MUST be a dependency. The detail body only exists once loading is
// false (the template shows a spinner branch while it is true), and `open()`
// sets item/comments *before* clearing loading. Watching only item/comments
// therefore fired while bodyRef was still null — verifyAnnotations() bailed on
// its `if (!el) return` guard — and nothing ran again when the body finally
// mounted, so no path in an issue/PR body was ever verified and every
// annotation stayed data-path-type-less and unclickable.
let renderId = 0
watch(
  () => [detail.item.value, detail.comments.value, detail.loading.value] as const,
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
  if (linkOrBtn) {
    event.preventDefault()
    event.stopPropagation()
    codeLinkPreview.close()
    const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(linkOrBtn)
    if (!filePath) return
    if (lineRanges) void openFilePath(filePath, lineStart, lineEnd, 'forge', lineRanges)
    else void openFilePath(filePath, lineStart, lineEnd, 'forge')
    return
  }

  // Double-click on a paragraph/heading/etc. copies it and offers the quote
  // bar. Last in the chain so every interactive target above wins the click
  // first — handleDblClick also resolves anchor clicks, but those are already
  // consumed by the file-path / commit handlers above.
  handleDblClick(event)
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

<style scoped>.forge-detail-content {
  margin-top: 14px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border-color);
}

/* ── Comments ── */
.forge-comments {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
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
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  transition: border-color var(--duration-base) ease, background var(--duration-base) ease;
}
@media (hover: hover) {
  .forge-load-more-comments:hover:not(:disabled) {
    border-color: var(--accent-color);
    background: var(--bg-secondary);
  }
}
.forge-load-more-comments:disabled { opacity: var(--opacity-muted); cursor: not-allowed; }
.forge-comment {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  overflow: hidden;
  background: var(--bg-secondary);
}
.forge-comment-meta {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding:7px var(--space-6);
  border-bottom: 1px solid var(--border-color);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.forge-comment-author { font-weight: var(--font-weight-semibold); color: var(--text-primary); }
.forge-comment-body {
  padding: var(--space-5) var(--space-6);
  background: var(--bg-primary);
}

</style>
