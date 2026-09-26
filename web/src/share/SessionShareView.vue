<template>
  <div class="share-view">
    <!-- Full-width topbar (shared .share-topbar chrome), stacked into two rows:
         the title, then the byline. The conversation column below keeps its own
         900px measure, so the chrome can span the viewport without widening the
         thread. -->
    <div class="share-topbar share-topbar--stacked">
      <div class="share-topbar-main">
        <h1 class="share-topbar-title" :title="title">{{ title }}</h1>
        <div class="share-top-actions">
          <!-- Conversation TOC toggle. Hidden until the snapshot lands, since
               the entries are derived from the messages. -->
          <button
            v-if="!loading && !error && tocItems.length > 0"
            class="share-btn share-toc-toggle"
            type="button"
            :title="t('share.toggleToc')"
            :aria-label="t('share.toggleToc')"
            :aria-expanded="tocOpen"
            @click="tocOpen = !tocOpen"
          >
            <List :size="16" />
          </button>
          <!-- Export the snapshot verbatim (client-side; no extra request). -->
          <button
            v-if="!loading && !error && snapshot"
            class="share-btn session-share-export"
            type="button"
            :title="t('share.exportJson')"
            :aria-label="t('share.exportJson')"
            @click="onExportJson"
          >
            <Download :size="16" />
          </button>
        </div>
      </div>
      <!-- Byline: which CLI produced this conversation, how long it is, and
           how long the agent actually spent on it. The model is deliberately
           NOT shown — it is per-message, so a single session-level label
           would misreport a thread that switched models midway. Each message
           keeps its own metadata modal. -->
      <div class="session-share-byline">
        <span v-if="loading" class="share-status">{{ t('share.loading') }}</span>
        <span v-else-if="error" class="share-status share-error">{{ error }}</span>
        <template v-else>
          <span v-if="backendLabel" class="session-share-agent">
            <AgentIcon :backend="backendLabel" :name="agentName" :size="14" />
            <span class="session-share-agent-name">{{ agentName }}</span>
          </span>
          <span v-if="backendLabel && messageCount > 0" class="session-share-dot" aria-hidden="true">·</span>
          <span v-if="messageCount > 0" class="session-share-count">
            {{ t('share.messageCount', { count: messageCount }) }}
          </span>
          <!-- Total agent time. Summed from each assistant turn's wallMs
               (the same value its meta bar shows); omitted when no turn
               carries one, rather than rendering "0ms". -->
          <template v-if="totalDurationMs > 0">
            <span v-if="messageCount > 0" class="session-share-dot" aria-hidden="true">·</span>
            <span class="session-share-duration">
              {{ t('share.totalDuration', { duration: formatDuration(totalDurationMs) }) }}
            </span>
          </template>
        </template>
      </div>
    </div>

    <div class="share-body">
      <div class="share-content session-share-content" ref="contentRef">
        <div v-if="loading" class="share-center-hint">
          <LoadingIndicator size="md" />
        </div>

        <div v-else-if="error" class="share-error-state">
          <FileX2 :size="40" />
          <div class="share-error-title">{{ t('share.invalidTitle') }}</div>
          <div class="share-error-desc">{{ error }}</div>
        </div>

        <!-- Conversation column. Mirrors the chat area's 900px measure, with
             no side rules: the messages sit directly on the page background. -->
        <div v-else class="session-share-column">
          <div class="session-share-messages">
            <ChatMessageItem
              v-for="(msg, i) in messages"
              :key="msg.id"
              :msg="msg"
              :index="i"
              :expanded-tools="expandedTools"
              :block-tasks="blockTasks"
              :block-ask-questions="blockAskQuestions"
              :agents="[]"
              :static-block-cache="staticBlockCache"
              :active="false"
              :is-last-assistant="isLastAssistantMessage(messages, msg)"
              :is-last-message="i === messages.length - 1"
              :hide-session-actions="true"
              :read-only="true"
              @toggle-tool="onToggleTool"
              @show-tool-detail="onShowToolDetail"
              @show-metadata="onShowMetadata"
              @toggle-summary="onToggleSummary"
              @render-flush="() => {}"
              @file-tag-click="() => {}"
            />
          </div>
        </div>
      </div>

      <!-- TOC rail (wide screens). Entries are every message in the thread,
           rendered with the same row component the in-app conversation index
           uses. Narrow screens get the slide-in drawer below. -->
      <aside v-if="tocItems.length > 0 && tocOpen && !isNarrow" class="share-toc share-toc--wide">
        <div class="share-toc-head">
          <div class="share-toc-title">{{ t('share.toc') }}</div>
        </div>
        <div class="share-toc-list">
          <MessageIndexRow
            v-for="(item, i) in tocItems"
            :key="item.id"
            :msg="item"
            :index="i + 1"
            :active="activeTocId === item.id"
            :show-time="true"
            @select="scrollToMessage(item.id)"
          />
        </div>
      </aside>
    </div>

    <!-- Narrow-screen TOC drawer: backdrop + slide-in panel -->
    <Teleport to="body">
      <div v-if="isNarrow && tocItems.length > 0 && tocOpen" class="share-toc-drawer">
        <div class="share-toc-backdrop" @click="tocOpen = false" />
        <aside class="share-toc share-toc--wide share-toc-panel">
          <div class="share-toc-head">
            <div class="share-toc-title">{{ t('share.toc') }}</div>
            <button class="share-toc-close" type="button" :aria-label="t('common.close')" @click="tocOpen = false">
              <X :size="16" />
            </button>
          </div>
          <div class="share-toc-list">
            <MessageIndexRow
              v-for="(item, i) in tocItems"
              :key="item.id"
              :msg="item"
              :index="i + 1"
              :active="activeTocId === item.id"
              :show-time="true"
              @select="scrollToMessage(item.id); tocOpen = false"
            />
          </div>
        </aside>
      </div>
    </Teleport>

    <!-- Tool detail overlay: reads input/output from the inlined snapshot, so no
         authenticated request is made. -->
    <ToolDetailDrawer
      :show="toolDetailDrawer.effectiveOpen.value"
      :toolName="toolDetailOverlay.name"
      :toolSubagentType="toolDetailOverlay.subagentType"
      :toolSummary="toolDetailOverlay.summary"
      :toolInputHtml="toolDetailOverlay.inputHtml"
      :toolOutputHtml="toolDetailOverlay.outputHtml"
      :toolStatus="toolDetailOverlay.status"
      :toolDone="toolDetailOverlay.done"
      :toolDuration="toolDetailOverlay.duration"
      :displayNameOverride="toolDetailOverlay.displayNameOverride"
      @close="closeOverlay"
      @file-open="() => {}"
      @click="handleOverlayRetryClick"
    />

    <!-- Message metadata modal (model / tokens / cost / duration). -->
    <ChatMetadataModal
      :show="metadataDrawer.effectiveOpen.value"
      :data="metadataModal.data"
      :backend="metadataModal.backend"
      :createdAt="metadataModal.createdAt"
      :relatedFile="metadataModal.relatedFile"
      :messageId="metadataModal.messageId"
      :sessionId="metadataModal.sessionId"
      :ftsIndexed="metadataModal.ftsIndexed"
      :vecIndexed="metadataModal.vecIndexed"
      :formatDetailTime="chatRender.formatDetailTime"
      @close="metadataDrawer.close()"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, provide, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, FileX2, List, X } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import MessageIndexRow from '@/components/chat/MessageIndexRow.vue'
import ToolDetailDrawer from '@/components/chat/ToolDetailDrawer.vue'
import ChatMetadataModal from '@/components/chat/ChatMetadataModal.vue'
import { useChatRender } from '@/composables/useChatRender'
import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { isLastAssistantMessage, isShowingSummary, normalizeDisplayMode, parseMessages } from '@/utils/chatSessionUtils'
import { localConfig } from '@/composables/useSettingsConfig'
import { getBackendDisplayName } from '@/utils/backendNames'
import { flashElement } from '@/utils/domFlash'
import { setShareSessionData, shareApiUrl, type ShareToolCallData } from './shareMode'
import { downloadBlob } from '@/utils/download.ts'
import { formatDuration } from '@/utils/format.ts'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'SessionShareView'

const { t } = useI18n()

const loading = ref(true)
const error = ref('')
const title = ref('')
const backendLabel = ref('')
/** Agent display name for the byline. Derived from the static backend-id map so
 *  the public share SPA never calls the authenticated /api/agents endpoint. */
const agentName = computed(() => getBackendDisplayName(backendLabel.value))
const messageCount = ref(0)
const messages = ref<Record<string, unknown>[]>([])

/**
 * The snapshot payload as served, kept for the JSON export.
 *
 * This must be a DEEP COPY taken before `parseMessages` runs. That function
 * mutates every message IN PLACE — it assigns `blocks`, `metadata` and
 * `showingSummary` onto the very objects the payload owns — and the view then
 * flips `showingSummary` on toggle. Re-serializing the live payload would
 * therefore export a mutated, view-state-dependent document in which each
 * message carries its content twice (the original `content` JSON string plus
 * the derived `blocks`), which is not what the link serves.
 *
 * The round-trip through JSON also preserves key order (JS keeps insertion
 * order for string keys), so the exported document matches the API's field
 * order as well as its data.
 */
const snapshot = ref<Record<string, unknown> | null>(null)

// ── Conversation TOC ──
// Every message is an entry (both roles), rendered with the same row component
// the in-app conversation index uses. Entries ARE the parsed messages: the row
// component derives its own preview text, so no separate projection is needed.
const tocOpen = ref(true)
/** Narrow layout (<900px): the TOC moves to a slide-in drawer over the content. */
const isNarrow = ref(false)
let tocMq: MediaQueryList | null = null
function syncNarrow() {
  if (typeof window.matchMedia !== 'function') return // jsdom / non-browser
  isNarrow.value = window.matchMedia('(max-width: 899px)').matches
}
const contentRef = ref<HTMLElement | null>(null)
/** Row shape consumed by MessageIndexRow (a structural subset of a parsed
 *  message). The messages array stays loosely typed because it is also fed to
 *  useChatRender, which works on generic records. */
interface TocMessage {
  id: number | string
  role?: string
  summary?: string
  content?: string
  createdAt?: string
  blocks?: Array<{ type?: string; text?: string }>
  files?: Array<string | { path?: string }>
}
const tocItems = computed<TocMessage[]>(() => messages.value as unknown as TocMessage[])
/** Message id currently in view (scroll-spy); null until the observer fires. */
const activeTocId = ref<number | string | null>(null)

/**
 * Jump to a message by id.
 *
 * Scrolls ONLY the content container (never the outer page / topbar), matching
 * ShareView.scrollToHeading — a bare scrollIntoView would also move the
 * topbar and every other scrollable ancestor.
 */
function scrollToMessage(msgId: number | string) {
  const root = contentRef.value
  if (!root) return
  // Match on the dataset rather than building a selector with the id: ids are
  // free-form (string ids may contain quotes), and CSS.escape is not available
  // in every environment the view is rendered in (jsdom).
  const key = `db-${msgId}`
  const el = Array.from(root.querySelectorAll<HTMLElement>('[data-msg-key]'))
    .find((node) => node.dataset.msgKey === key)
  if (!el) return
  const targetTop = el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop
  root.scrollTo({ top: targetTop, behavior: 'smooth' })
  flashElement(el, { className: 'chat-message-highlight' })
  activeTocId.value = msgId
  // Hold the clicked entry's highlight while the smooth scroll runs, so the
  // scroll-spy does not immediately steal it mid-flight.
  holdActiveUntil = Date.now() + TOC_ACTIVE_HOLD_MS
}

// ── Scroll-spy ──
// Highlights the message currently in view. IntersectionObserver with a root
// margin biased to the top: the first message whose top has passed the upper
// band wins, which matches "what am I reading" better than raw intersection.
const TOC_ACTIVE_HOLD_MS = 1200
let holdActiveUntil = 0
let tocObserver: IntersectionObserver | null = null
/** Ids currently intersecting the spy band. */
const visibleIds = new Set<number | string>()

function setupTocObserver() {
  teardownTocObserver()
  const root = contentRef.value
  if (!root || typeof IntersectionObserver !== 'function') return
  tocObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      const id = (entry.target as HTMLElement).dataset.msgId
      if (id == null) continue
      const key = /^\d+$/.test(id) ? Number(id) : id
      if (entry.isIntersecting) visibleIds.add(key)
      else visibleIds.delete(key)
    }
    if (Date.now() < holdActiveUntil) return
    // tocItems is in document order, so the first visible entry is the
    // topmost one. Keep the last known id when nothing is in the band
    // (between messages, or scrolled past the end).
    const topmost = tocItems.value.find((m) => visibleIds.has(m.id))
    if (topmost) activeTocId.value = topmost.id
  }, {
    root,
    rootMargin: '-60px 0px -70% 0px',
    threshold: 0,
  })
  for (const el of root.querySelectorAll<HTMLElement>('[data-msg-key]')) {
    el.dataset.msgId = String(el.dataset.msgKey).replace(/^db-/, '')
    tocObserver.observe(el)
  }
}

function teardownTocObserver() {
  if (tocObserver) {
    tocObserver.disconnect()
    tocObserver = null
  }
  visibleIds.clear()
}

/**
 * Total assistant wall-clock time, summed across the thread.
 *
 * Sums `metadata.wallMs` from each assistant message's parsed content — the
 * same per-turn value the chat meta bar shows. Missing values are skipped
 * rather than treated as 0 (older turns predate the field), so a thread with
 * partial data reports the sum of what is known instead of under-reporting.
 */
const totalDurationMs = computed(() => {
  let sum = 0
  for (const msg of messages.value) {
    if (msg.role !== 'assistant') continue
    const meta = msg.metadata as { wallMs?: number } | undefined
    if (typeof meta?.wallMs === 'number' && meta.wallMs > 0) sum += meta.wallMs
  }
  return sum
})

// ── Render chain ──
// One shared instance drives every message, exactly as the chat panel does.
// useChatRender's task-block store stays inert because the snapshot carries no
// TaskIDs (the Go builder strips them).
const chatRender = useChatRender({ messages, theme: ref(''), currentSessionId: ref('') })
const { expandedTools, blockTasks, blockAskQuestions, staticBlockCache, toggleToolDetail } = chatRender

// ── Provides mirrored from TaskExecDetail (the other standalone chat host) ──
// getAgentBackend/getAgentName are synthesized from the snapshot instead of
// useAgents(), which would call the authenticated /api/agents endpoint.
function getAgentBackend(): string {
  return backendLabel.value
}
function getAgentName(): string {
  return backendLabel.value || t('share.sharedConversation')
}

provide('chatRender', {
  renderTextBlock: chatRender.renderTextBlock,
  formatMessageTime: chatRender.formatMessageTime,
  toolCallSummary: chatRender.toolCallSummary,
  formatToolInput: chatRender.formatToolInput,
  truncate: chatRender.truncate,
  hasImagesInContent: chatRender.hasImagesInContent,
})
provide('chatSession', { getAgentBackend, getAgentName })
provide('chatUI', { navigateToFileViewer: () => {} })
// ChatMessageItem injects autoSpeech without a default and reads it during
// render, so a stub is required. TTS needs auth and is out of scope here.
provide('autoSpeech', {
  isActive: () => false,
  isGeneratingText: () => false,
  getPhaseLabel: () => '',
  isPlayingAudio: () => false,
  speakText: () => {},
  stopAudio: () => {},
})

// ── Tool detail drawer ──
// input/output are inlined in the snapshot, so the drawer renders without any
// authenticated fetch (see fetchToolCallDetail's share-provider branch).
const toolDetailDrawer = useToolDetailDrawer({
  chatRender,
  tabId: 'chat',
  onFileOpen: () => {},
})
const { toolDetailOverlay, closeOverlay, handleShowToolDetail, handleOverlayRetryClick } = toolDetailDrawer

// ── Message metadata modal ──
// Mirrors ChatPanelContent's local modal state. The RAG index-status lookup is
// deliberately omitted: it needs auth and is not part of the share contract.
const metadataModal = ref({
  data: {} as Record<string, unknown>,
  backend: '',
  createdAt: '',
  relatedFile: '',
  messageId: null as string | number | null,
  sessionId: '',
  ftsIndexed: false,
  vecIndexed: false,
})
const metadataDrawer = useTabDrawer('chat')

/**
 * Flip one message between its summary and its original blocks.
 *
 * Mirrors ChatPanelContent.handleToggleSummary minus the lazy-fetch branch:
 * the snapshot always carries both the summary and the full blocks, so there
 * is nothing to fetch. The decision uses the same helper the app does, so the
 * share page and the chat panel never disagree about what is showing.
 */
function onToggleSummary(msgId: string | number) {
  const msg = messages.value.find((m) => m.id === msgId)
  if (!msg) return
  // No summary in the snapshot: there is nothing to toggle to.
  if (msg.summary == null || msg.summary === '') return

  const mode = normalizeDisplayMode(localConfig.messageDisplayMode)
  const showingNow = isShowingSummary(msg, mode, {
    isLastAssistant: isLastAssistantMessage(messages.value, msg),
  })
  // Record the explicit preference; the render decision derives from it.
  msg.showingSummary = !showingNow
}
/**
 * Turn the session title into a safe download filename.
 *
 * Titles are free text: they routinely contain '/' (paths), ':' and quotes, any
 * of which produce a broken or silently-renamed file in a browser save dialog.
 * Whitespace is collapsed so a newline in a title cannot split the filename.
 * Falls back to the generic share title when nothing usable survives.
 */
function exportFilename(): string {
  // \p{Cc} (Unicode "control") covers NUL through US plus DEL. Written as a
  // property escape rather than a \u0000-\u001f range because eslint's
  // no-control-regex rejects control characters written literally.
  // Truncate by code POINT, not UTF-16 unit: a plain slice can cut a surrogate
  // pair in half and leave a lone surrogate in the filename.
  const cleaned = (title.value || t('share.sharedConversation'))
    .replace(/[/\\:*?"<>|\p{Cc}]/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  const base = [...cleaned].slice(0, 80).join('').trim()
  return `${base || 'conversation'}.json`
}

/**
 * Export the snapshot verbatim as a JSON file.
 *
 * Exports exactly what this link serves (no re-serialization of parsed state,
 * no extra request): the payload is already sanitized server-side — absolute
 * paths were relativized by the share builder — so the download carries no more
 * than the page itself already shows.
 */
function onExportJson() {
  if (!snapshot.value) return
  try {
    downloadBlob(JSON.stringify(snapshot.value, null, 2), exportFilename(), 'application/json')
  } catch (err) {
    // Download can fail in restricted WebViews (no download manager). Surface
    // it in the byline rather than silently doing nothing on tap.
    appLog.w(TAG, 'snapshot export failed', err)
    error.value = t('share.exportFailed')
  }
}

function onShowMetadata(msg: Record<string, unknown>) {
  metadataModal.value.data = (msg.metadata as Record<string, unknown>) || {}
  metadataModal.value.backend = (msg.backend as string) || backendLabel.value
  metadataModal.value.createdAt = (msg.createdAt as string) || ''
  const files = msg.files as Array<{ path?: string }> | undefined
  metadataModal.value.relatedFile = files && files.length > 0 ? files[0].path || '' : ''
  metadataModal.value.messageId = (msg.id as string | number) || null
  metadataModal.value.sessionId = ''
  metadataModal.value.ftsIndexed = false
  metadataModal.value.vecIndexed = false
  metadataDrawer.open()
}

// ── Event forwarding ──

function onToggleTool(key: string) {
  toggleToolDetail(key)
}

function onShowToolDetail(payload: unknown) {
  handleShowToolDetail(payload as never)
}

/**
 * Collect the inlined tool calls and thinking text into the share-mode provider.
 *
 * The blocks already carry this data from the backend; the provider is the
 * safety net for components that lazily fetch regardless of what is present
 * (auto-expand tool cards, the thinking chip, the diff drawer).
 */
function indexInlinedData(payloadMessages: Record<string, unknown>[]) {
  const thinking = new Map<string, string>()
  const toolCalls = new Map<string, ShareToolCallData>()

  for (const msg of payloadMessages) {
    const msgId = msg.id as string | number
    const blocks = msg.blocks as Array<Record<string, unknown>> | undefined
    if (!Array.isArray(blocks)) continue
    for (const block of blocks) {
      if (block.type === 'thinking' && typeof block.think_id === 'string' && typeof block.text === 'string') {
        thinking.set(`${msgId}:${block.think_id}`, block.text)
      } else if (block.type === 'tool_use' && typeof block.id === 'string') {
        toolCalls.set(`${msgId}:${block.id}`, {
          input: block.input,
          output: typeof block.output === 'string' ? block.output : undefined,
          status: typeof block.status === 'string' ? block.status : undefined,
          done: typeof block.done === 'boolean' ? block.done : undefined,
          durationMs: typeof block.duration_ms === 'number' ? block.duration_ms : undefined,
          summary: typeof block.summary === 'string' ? block.summary : undefined,
          truncated: block.truncated === true,
        })
      }
    }
  }
  setShareSessionData(thinking, toolCalls)
}

async function loadSnapshot() {
  loading.value = true
  error.value = ''
  try {
    const resp = await fetch(shareApiUrl('session'))
    if (!resp.ok) {
      error.value = t('share.notFound')
      return
    }
    const payload = await resp.json()
    // Deep-copy BEFORE parseMessages mutates the payload's message objects in
    // place; see the `snapshot` declaration for why the live object is unusable.
    snapshot.value = JSON.parse(JSON.stringify(payload))

    title.value = payload?.session?.title || t('share.sharedConversation')
    backendLabel.value = payload?.session?.backend || ''
    const rawMessages = Array.isArray(payload?.messages) ? payload.messages : []
    messageCount.value = rawMessages.length

    // parseMessages mutates each message in place (adds blocks/metadata), which
    // is why the payload must not be frozen.
    const parsed = parseMessages(rawMessages, (content: string) => chatRender.parseAssistantContent(content))
    indexInlinedData(parsed)
    messages.value = parsed

    // Keep the store's roots empty so markdown path annotations cannot resolve
    // to in-app navigation (mirrors ShareView's file-share handling).
    store.state.projectRoot = store.state.projectRoot || ''
    store.state.homeDir = store.state.homeDir || ''
    // The message DOM exists only after this tick; the observer needs the rows.
    await nextTick()
    setupTocObserver()
    // Desktop opens with the TOC rail visible; narrow screens default closed
    // (opened on demand via the topbar button → slide-in drawer).
    tocOpen.value = !isNarrow.value
  } catch (err) {
    appLog.w(TAG, 'failed to load session snapshot', err)
    error.value = t('share.notFound')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  syncNarrow()
  if (typeof window.matchMedia === 'function') {
    tocMq = window.matchMedia('(max-width: 899px)')
    tocMq.addEventListener('change', syncNarrow)
  }
  void loadSnapshot()
})

onBeforeUnmount(() => {
  teardownTocObserver()
  tocMq?.removeEventListener('change', syncNarrow)
  tocMq = null
})
</script>

<style scoped>
/* This view reuses the shared share chrome (.share-view / .share-topbar /
   .share-body / .share-toc) from css/share-chrome.css, with the two-row
   .share-topbar--stacked variant. Only the conversation-specific pieces live
   here: the message column and the byline. */

/* Scroll container (the shared .share-content). The view keeps a ref to it so
   the TOC's jump math and the scroll-spy root are the same element. */
.session-share-content {
  padding-bottom: var(--space-6);
}

/* Message layout mirrors the chat area exactly (.chat-messages +
   .chat-messages-list), so a shared conversation reads identically to the
   in-app thread it was shared from:
   - no horizontal padding, so an assistant card is flush with the column's
     inner edge (the assistant bubble has no side margin and border-radius: 0;
     the user bubble owns its own 10px right inset);
   - flex column + `gap: var(--space-8)`, which is the chat list's message
     spacing. Block layout would leave adjacent messages touching (gap 0). */
.session-share-messages {
  padding: var(--space-6) 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
}

/* The conversation column: a centred 900px measure. No side rules — the
   message column reads as one continuous surface against the page background.
   `min-height: 100%` keeps the column filling the scroll container even for a
   short thread. */
.session-share-column {
  min-height: 100%;
  box-sizing: border-box;
  width: 100%;
  max-width: 900px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
}

/* Byline row inside the stacked topbar. */
.session-share-byline {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-height: 18px;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-muted, #656d76);
}

.session-share-agent {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  min-width: 0;
}

.session-share-agent-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-share-dot {
  opacity: .6;
}

.session-share-count,
.session-share-duration {
  white-space: nowrap;
}

.share-status {
  font-size: var(--font-size-sm);
  color: var(--text-muted, #656d76);
}
.share-error {
  color: #cf222e;
}

/* Loading / error states fill the content column. */
.share-center-hint,
.share-error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  min-height: 60vh;
  padding: 32px;
  color: var(--text-muted, #656d76);
  text-align: center;
}

.share-error-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
}

/* Jump target flash, mirroring ChatMessageList's chat-message-highlight. The
   share SPA does not load that component's styles, so the animation is
   re-declared here against the message card. */
:deep(.chat-message.chat-message-highlight .msg-card) {
  animation: session-share-highlight-flash var(--flash-duration, 0.7s) ease-out 1;
}
@keyframes session-share-highlight-flash {
  0%, 100% { outline-color: transparent; }
  14%      { outline-color: color-mix(in srgb, var(--accent-color) 70%, transparent); }
  45%      { outline-color: color-mix(in srgb, var(--accent-color) 35%, transparent); }
}
:deep(.chat-message.chat-message-highlight .msg-card) {
  outline: 2px solid transparent;
  outline-offset: 2px;
  border-radius: var(--radius-md);
}
@media (prefers-reduced-motion: reduce) {
  :deep(.chat-message.chat-message-highlight .msg-card) {
    animation: none !important;
    outline-color: color-mix(in srgb, var(--accent-color) 55%, transparent);
  }
}
</style>
