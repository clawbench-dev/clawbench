<template>
  <span
    class="chat-file-attachment attachment-quote"
    :class="{ 'attachment-quote-note': !!quote.note }"
    :title="cardTitle"
    @click="$emit('click', quote)"
  >
    <!--
      The root classes are load-bearing: `chat-file-attachment attachment-quote`
      is what makes the existing styles apply on BOTH sides — the global rules in
      ChatMessageItem.vue (`.chat-message .chat-file-attachment`) win in a sent
      bubble, and ChatInputBar.vue's scoped rules win in the input. Renaming
      these classes silently un-styles one of the two surfaces.

      This comment lives INSIDE the root element on purpose: a leading comment
      would make the template a fragment, and the root classes/title would no
      longer be reachable via the component wrapper.
    -->
    <MessageSquareQuote :size="14" :stroke-width="1.5" class="attachment-quote-icon" />
    <span class="attachment-filename">{{ label }}{{ lineRange }}</span>
    <!-- Annotation indicator: tells the user at a glance that this card carries
         a note, without printing the whole note on the chip. -->
    <MessageSquareText v-if="quote.note" :size="11" class="attachment-quote-note-icon" />
    <button
      v-if="removable"
      class="attachment-close-btn"
      :title="t('common.remove')"
      @click.stop="$emit('remove', quote)"
    >×</button>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquareQuote, MessageSquareText } from 'lucide-vue-next'
import { quoteLabel, quoteLineRange, type QuoteItem } from '@/utils/quoteItem'

const props = withDefaults(defineProps<{
  quote: QuoteItem
  /** Show the remove button (input side only — a sent card is not removable). */
  removable?: boolean
}>(), {
  removable: false,
})

defineEmits<{
  click: [quote: QuoteItem]
  remove: [quote: QuoteItem]
}>()

const { t } = useI18n()

const label = computed(() => quoteLabel(props.quote))
const lineRange = computed(() => quoteLineRange(props.quote))

/**
 * Tooltip: the annotation when there is one (it is the more specific context),
 * otherwise the source label, otherwise the quoted text so an unlabelled chat
 * quote is still identifiable on hover.
 */
const cardTitle = computed(() => {
  const q = props.quote
  if (q.note) return q.note
  if (q.filePath) return q.filePath
  return q.text
})
</script>

<style scoped>
/* Only the pieces that are genuinely new. Card geometry, colours and the close
   button come from the shared `.chat-file-attachment` / `.attachment-quote`
   rules in ChatInputBar.vue (input) and ChatMessageItem.vue (sent bubble).

   The icon rule is repeated here on purpose: the parent's scoped descendant
   selectors (`.chat-attachment-tags .attachment-quote .attachment-quote-icon`)
   stop at this component's boundary, so an icon rendered inside QuoteCard would
   lose its flex-shrink and squash when the filename is long. */
.attachment-quote-icon,
.attachment-quote-note-icon {
  flex-shrink: 0;
}

.attachment-quote-note-icon {
  opacity: 0.75;
}
</style>
