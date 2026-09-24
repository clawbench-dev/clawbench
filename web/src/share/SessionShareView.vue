<template>
  <div class="session-share">
    <!-- Conversation column. Mirrors the chat area: a framed 900px measure
         bounded by full-height vertical rules, with the title inside the frame
         at the top so the rules start at the title and run to the bottom. -->
    <div class="session-share-column">
      <div class="session-share-header">
        <div class="session-share-header-inner">
          <h1 class="session-share-title" :title="title">{{ title }}</h1>
          <!-- Byline: which CLI produced this conversation, and how long it is.
               The model is deliberately NOT shown — it is per-message, so a
               single session-level label would misreport a thread that switched
               models midway. Each message keeps its own metadata modal. -->
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
            </template>
          </div>
        </div>
      </div>

      <div class="session-share-body">
        <div v-if="loading" class="share-center-hint">
          <LoadingIndicator size="md" />
        </div>

        <div v-else-if="error" class="share-error-state">
          <FileX2 :size="40" />
          <div class="share-error-title">{{ t('share.invalidTitle') }}</div>
          <div class="share-error-desc">{{ error }}</div>
        </div>

        <div v-else class="session-share-messages">
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
import { computed, onMounted, provide, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileX2 } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import ToolDetailDrawer from '@/components/chat/ToolDetailDrawer.vue'
import ChatMetadataModal from '@/components/chat/ChatMetadataModal.vue'
import { useChatRender } from '@/composables/useChatRender'
import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { isLastAssistantMessage, isShowingSummary, normalizeDisplayMode, parseMessages } from '@/utils/chatSessionUtils'
import { localConfig } from '@/composables/useSettingsConfig'
import { getBackendDisplayName } from '@/utils/backendNames'
import { setShareSessionData, shareApiUrl, type ShareToolCallData } from './shareMode'
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
  } catch (err) {
    appLog.w(TAG, 'failed to load session snapshot', err)
    error.value = t('share.notFound')
  } finally {
    loading.value = false
  }
}

onMounted(loadSnapshot)
</script>

<style scoped>
/* This view does NOT use .share-topbar from css/share-chrome.css: that bar is a
   left-title / right-actions toolbar, whereas a conversation wants its title
   above the thread, aligned to the message column (see .session-share-header).
   The file-share SPA and the markdown export still use .share-topbar. */

.session-share {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.session-share-body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}

.session-share-messages {
  /* The column owns the width; the horizontal padding must match
     .session-share-header-inner so the title and the messages share a left edge. */
  padding: 16px 20px 48px;
}

/* The conversation column: a centred 900px measure framed by full-height
   vertical rules, mirroring the chat area. Owning the width HERE (rather than
   on the header and the message list separately) is what lets one pair of
   borders run unbroken from the title to the bottom of the page. */
.session-share-column {
  flex: 1;
  min-height: 0;
  width: 100%;
  max-width: 900px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--border-color, rgba(128, 128, 128, .25));
  border-right: 1px solid var(--border-color, rgba(128, 128, 128, .25));
}

.session-share-header {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, rgba(128, 128, 128, .25));
  background: var(--bg-secondary, #f6f8fa);
}

.session-share-header-inner {
  padding: 14px 20px 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.session-share-title {
  margin: 0;
  font-size: var(--font-size-xl, 15px);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1f2328);
  line-height: var(--line-height-snug, 1.3);
  /* Session titles are often long: wrap to at most two lines, then ellipsise. */
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

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

.session-share-count {
  white-space: nowrap;
}

.share-status {
  font-size: var(--font-size-sm);
  color: var(--text-muted, #656d76);
}
.share-error {
  color: #cf222e;
}

.share-center-hint,
.share-error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  height: 100%;
  padding: 32px;
  color: var(--text-muted, #656d76);
  text-align: center;
}

.share-error-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
}
</style>
