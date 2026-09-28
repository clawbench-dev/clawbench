<template>
  <BottomSheet
    :open="drawer.effectiveOpen.value"
    auto
    :title="t('chat.btw.title')"
    @close="drawer.close()"
  >
    <template #header>
      <Sparkles :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.btw.title') }}</span>
    </template>

    <div class="btw-content">
      <div v-if="question" class="btw-question">{{ question }}</div>
      <!-- Rendered through the same pipeline as chat messages: ChatMessageItem
           reads the chatRender/chatSession/chatUI/autoSpeech injections, which
           ChatPanelContent already provides on this subtree. The synthetic
           message is a plain assistant message with streaming:false, so the
           full (non-streaming) render branch runs — markdown, KaTeX, code
           highlighting, path/commit annotation, tables.
           ChatMessageItem (not ChatMessageList) is the right unit here: the list
           component owns the chat scroll container, lazy-load and scroll FABs,
           none of which belong in a drawer. SessionShareView and TaskExecDetail
           are the existing precedents for hosting a single message this way. -->
      <ChatMessageItem
        v-if="message"
        :msg="message"
        :index="0"
        :expanded-tools="expandedTools"
        :block-tasks="blockTasks"
        :block-ask-questions="blockAskQuestions"
        :agents="[]"
        :static-block-cache="staticBlockCache"
        :active="false"
        :is-last-assistant="true"
        :is-last-message="true"
        :hide-session-actions="true"
        :read-only="true"
        @render-flush="() => {}"
      />
    </div>
  </BottomSheet>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'

const props = defineProps({
  /** The /btw question text (shown above the answer). */
  question: { type: String, default: '' },
  /** The summary model's answer; empty means "nothing to show". */
  answer: { type: String, default: '' },
  /** Monotonic id so a repeat /btw for the same text still remounts cleanly. */
  answerId: { type: [String, Number], default: 0 },
  /** Render maps forwarded from ChatPanelContent's useChatRender instance, so
   *  the answer goes through the exact same pipeline as chat messages. */
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

/** A synthetic assistant message, shaped like TaskExecDetail's. */
const message = computed(() => {
  if (!props.answer) return null
  return {
    id: `btw-${props.answerId}`,
    role: 'assistant',
    content: '',
    blocks: [{ type: 'text', text: props.answer }],
    metadata: null,
    createdAt: new Date().toISOString(),
    streaming: false,
    cancelled: false,
  }
})

defineExpose({
  open: drawer.open,
  close: drawer.close,
  isOpen: drawer.isOpen,
})
</script>

<style scoped>
.btw-content {
  padding: 0 12px 16px;
}

/* The question that produced this answer, kept small and secondary: the drawer
   is about the answer, the question is context. */
.btw-question {
  margin: 4px 0 12px;
  padding: 8px 10px;
  border-radius: var(--radius-md, 8px);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-sm, 13px);
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
