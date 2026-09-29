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

    <div class="btw-content">
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
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageCircleQuestion } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'

const props = defineProps({
  /** The /btw records to show, oldest first. Each entry is a stored record
   *  ({ id, question, answer, error, createdAt, ... }). */
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

/**
 * One synthetic user/assistant message pair per record, shaped like the ones
 * TaskExecDetail builds. `streaming: false` selects the full (non-streaming)
 * render branch — the same one a settled chat message takes.
 *
 * A failed question has no answer; its assistant bubble carries the error text
 * so the drawer still explains what happened instead of showing an empty reply.
 */
const items = computed(() => props.records.map((rec, i) => {
  const failed = !rec.answer && rec.error
  const answerText = rec.answer || (failed ? t('chat.btw.failedWithReason', { reason: rec.error }) : '')
  return {
    key: `btw-${rec.id ?? i}`,
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
      blocks: answerText ? [{ type: 'text', text: answerText }] : [],
      metadata: null,
      createdAt: rec.createdAt || '',
      streaming: false,
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
