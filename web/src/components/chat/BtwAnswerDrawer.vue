<template>
  <BottomSheet
    :open="drawer.effectiveOpen.value"
    auto
    :title="t('chat.btw.title')"
    @close="drawer.close()"
  >
    <template #header>
      <MessageCircleQuestion :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.btw.title') }}</span>
    </template>

    <!-- Container-level click delegation. ChatMessageList normally owns this,
         but the drawer is teleported to <body>, so a click inside it never
         reaches that handler. Without a local layer the annotations and code
         link previews render but do nothing (the elements and their
         data-path-type are produced by ContentBlocks either way). Mirrors
         ToolDetailDrawer's handleBodyClick. -->
    <div
      class="btw-content"
      ref="contentRef"
      @click="handleContentClick"
      @mousedown="onTableMouseDown"
      @touchstart.passive="onTableTouchStart"
    >
      <!-- Each /btw is a mini exchange: the question as a real user bubble, the
           answer as an assistant bubble. Both go through ChatMessageItem, so
           the rendering (markdown, KaTeX, code, annotations) is identical to the
           chat area — only the widths are overridden below. -->
      <div v-for="item in items" :key="item.key" class="btw-exchange">
        <ChatMessageItem
          :msg="item.questionMsg"
          :index="0"
          :expanded-tools="expandedTools"
          :block-tasks="blockTasks"
          :block-ask-questions="blockAskQuestions"
          :agents="[]"
          :static-block-cache="staticBlockCache"
          :active="false"
          :read-only="true"
          :hide-session-actions="true"
          @render-flush="() => {}"
        />
        <ChatMessageItem
          :msg="item.answerMsg"
          :index="1"
          :expanded-tools="expandedTools"
          :block-tasks="blockTasks"
          :block-ask-questions="blockAskQuestions"
          :agents="[]"
          :static-block-cache="staticBlockCache"
          :active="false"
          :is-last-assistant="true"
          :is-last-message="true"
          :read-only="true"
          :hide-session-actions="true"
          @render-flush="() => {}"
        />
      </div>
    </div>
  </BottomSheet>

  <!-- Floating file/code preview, same as the chat area's. It teleports to
       <body> and sits at --z-sheet (1200), above the drawer's overlay tier
       (1000..1049), so it is never clipped by the drawer it was opened from. -->
  <CodeLinkPreview
    v-if="codeLinkPreview.enabled.value"
    :preview="codeLinkPreview"
  />

  <!-- Table row expand modal, opened by clicking a table row inside a bubble. -->
  <TableRowModal
    :data="tableRowModal"
    @close="closeTableRowModal"
    @prev="tableRowPrev"
    @next="tableRowNext"
  />
</template>

<script setup>
import { computed, ref, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageCircleQuestion } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import TableRowModal from '@/components/common/TableRowModal.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useCodeLinkPreview, handleVerifiedFilePathClick } from '@/composables/useCodeLinkPreview.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'

const props = defineProps({
  /** The /btw records to show, oldest first. Each entry is a stored record
   *  ({ id, question, answer, error, createdAt, ... }) or, while a question is
   *  still being answered, an optimistic pending entry ({ pending: true }). */
  records: { type: Array, default: () => [] },
  /** Render maps forwarded from ChatPanelContent's useChatRender instance, so
   *  the bubbles go through the exact same pipeline as chat messages. */
  expandedTools: { type: Object, default: () => ({}) },
  blockTasks: { type: Object, default: () => ({}) },
  blockAskQuestions: { type: Object, default: () => ({}) },
  staticBlockCache: { type: Object, default: () => ({}) },
})

const { t } = useI18n()

// BottomSheet teleports to <body>, so it survives tab-panel hiding and would
// stay visible on other tabs. useTabDrawer scopes it to the chat tab and
// autoRestore:false keeps it from reopening when returning to chat.
const drawer = useTabDrawer('chat', { autoRestore: false })

// ── Click delegation ──
// The drawer renders the same bubbles as the chat area, so it needs the same
// click handling the chat list provides. It cannot reuse ChatMessageList's
// handler (the drawer is teleported to <body>, out of that component's tree),
// so it owns a local layer. Mirrors ToolDetailDrawer.handleBodyClick.
const contentRef = ref(null)
const codeLinkPreview = useCodeLinkPreview({ containerRef: contentRef, source: 'chat' })
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()
const { openFilePath, readLineTargetFromEl } = useFilePathAnnotation()
const { handleDblClick } = useDoubleClickCopy()
const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()
const chatUI = inject('chatUI', {})

async function handleContentClick(event) {
  // Code / table block header buttons (copy, wrap) — highest priority, same
  // order as ChatMessageList.handleChatClick.
  if (handleCodeBlockClick(event)) return
  if (handleTableBlockClick(event)) return

  // Localhost URL buttons (App mode only).
  if (handleLocalhostUrlClick(event)) return

  // Table row click — opens the row-form modal.
  if (handleTableRowClick(event)) return

  // Verified file paths: desktop plain-click opens the floating preview;
  // Ctrl/Cmd-click pins it; touch opens the bottom-sheet preview.
  if (handleVerifiedFilePathClick(event, codeLinkPreview)) return

  // Commit hash — navigate the git history tab to it.
  const commitEl = event.target.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    return
  }

  // File open button or directory path text.
  const btn = event.target.closest('.chat-file-open-btn')
  const dirEl = event.target.closest('.chat-file-path[data-path-type="dir"]')
  const linkOrBtn = btn || dirEl
  if (linkOrBtn) {
    event.preventDefault()
    event.stopPropagation()
    codeLinkPreview.close()
    const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(linkOrBtn)
    if (filePath) {
      const ok = lineRanges
        ? await openFilePath(filePath, lineStart, lineEnd, 'chat', lineRanges)
        : await openFilePath(filePath, lineStart, lineEnd, 'chat')
      if (ok) chatUI.navigateToFileViewer?.()
    }
    return
  }

  // Double-click fallback: open the anchor's file target.
  handleDblClick(event, async (href, lineStart, lineEnd, lineRanges) => {
    event.stopPropagation()
    codeLinkPreview.close()
    const ok = lineRanges
      ? await openFilePath(href, lineStart, lineEnd, 'chat', lineRanges)
      : await openFilePath(href, lineStart, lineEnd, 'chat')
    if (ok) chatUI.navigateToFileViewer?.()
  })
}

/**
 * One synthetic user/assistant message pair per record, shaped like the ones
 * TaskExecDetail builds. `streaming: false` selects the full (non-streaming)
 * render branch — the same one a settled chat message takes.
 *
 * A failed question has no answer. It is rendered with the SAME error block the
 * main chat area uses for a failed turn (`ContentBlocks`' `.chat-error-card`:
 * red left rail + alert icon + localized reason) rather than as plain assistant
 * text, so a /btw failure is visually indistinguishable from any other failure
 * the user sees. `error_source: 'clawbench'` is accurate — the answer came from
 * ClawBench's own summary model, not from the session's agent — and it drives
 * the source chip so the failure is not misread as the agent's.
 *
 * A pending record (the question was just asked and is still being answered)
 * renders an empty assistant bubble carrying `pending: true`; ChatMessageItem
 * turns that into the in-progress indicator, so the wait is shown here rather
 * than in the composer.
 */
const items = computed(() => props.records.map((rec, i) => {
  const failed = !rec.answer && rec.error
  const answerBlocks = rec.answer
    ? [{ type: 'text', text: rec.answer }]
    : failed
      ? [{ type: 'error', text: t('chat.btw.failedWithReason', { reason: rec.error }), error_source: 'clawbench' }]
      : []
  return {
    key: `btw-${rec.id ?? i}`,
    pending: rec.pending === true,
    questionMsg: {
      id: `btw-q-${rec.id ?? i}`,
      role: 'user',
      content: rec.question || '',
      blocks: rec.question ? [{ type: 'text', text: rec.question }] : [],
      metadata: null,
      createdAt: rec.createdAt || '',
      streaming: false,
      cancelled: false,
    },
    answerMsg: {
      id: `btw-a-${rec.id ?? i}`,
      role: 'assistant',
      content: '',
      blocks: answerBlocks,
      metadata: null,
      createdAt: rec.createdAt || '',
      // A pending answer takes the streaming branch so the bubble shows the
      // in-progress state; a settled one renders normally.
      streaming: rec.pending === true,
      cancelled: false,
    },
  }
}))

defineExpose({
  open: drawer.open,
  close: drawer.close,
  isOpen: drawer.isOpen,
})
</script>

<style scoped>
/* The drawer renders the same ChatMessageItem bubbles as the chat area; only
   the layout around them differs (the chat list's scrolling container and its
   own gap are not present here). Spacing below mirrors the chat area's values
   so an exchange reads the same as it does in the main conversation. */
.btw-content {
  /* Breathing room above and below the exchange(s). Top matters most: without
     it the first bubble sits flush against the header's shadow line. Bottom is
     larger because the panel can be dragged to full height and the last line
     otherwise ends at the screen edge. */
  padding: var(--space-5) 0 var(--space-7);
}

/* Each exchange is a Q&A pair. The pair's own gap matches the chat area's
   message-to-message gap (.chat-messages-list uses var(--space-8)). */
.btw-exchange {
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
}

/* A step larger between pairs than within one, so successive /btw questions
   read as separate exchanges rather than one long conversation. */
.btw-exchange + .btw-exchange {
  margin-top: var(--space-9);
}

/* No width overrides: ChatMessageItem's bubble rules are non-scoped, so the
   drawer already inherits the chat area's exact layout — the assistant bubble
   spans the full width and the user bubble keeps its own right inset
   (margin-right var(--space-5) + max-width calc(100% - 20px)). Overriding them
   here is what previously flattened the user inset. */
</style>
