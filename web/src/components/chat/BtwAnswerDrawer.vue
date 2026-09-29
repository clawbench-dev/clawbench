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
/* The drawer is a full-width reading surface: the chat area's 900px measure and
   the user bubble's right inset are chat-list layout, not message rendering, so
   they are removed here. Everything else (bubble background, radius, padding,
   typography) stays exactly as in the chat area. */
.btw-content {
  /* Breathing room above and below the exchange(s). Top matters most: without
     it the first bubble sits flush against the header's shadow line. Bottom is
     larger because the panel can be dragged to full height and the last line
     otherwise ends at the screen edge. */
  padding: var(--space-5) 0 var(--space-7);
}

.btw-exchange + .btw-exchange {
  margin-top: var(--space-7);
}

/* Assistant: full width, no side margins. */
.btw-content :deep(.chat-message.assistant),
.btw-content :deep(.chat-message.assistant .msg-card) {
  max-width: 100%;
}

/* User: keep the bubble, drop the chat list's right inset so it can use the
   full width too. */
.btw-content :deep(.chat-message.user .msg-card) {
  margin-right: 0;
  max-width: 100%;
}

/* The message rows carry the chat list's own spacing; the exchange wrapper
   already separates them. */
.btw-content :deep(.chat-message) {
  max-width: 100%;
}
</style>
