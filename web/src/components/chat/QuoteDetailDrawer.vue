<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <MessageSquareQuote :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('quoteBar.drawerTitle') }}</span>
    </template>

    <div v-if="quote" class="qd-content">
      <!-- Source line: what this quote came from, plus the jump action.
           A chat-message or free-form selection quote has nothing to open, so
           the button is hidden rather than rendered as a no-op. -->
      <div class="qd-source-row">
        <span class="qd-source" :title="quote.filePath || quote.text">
          <MessageSquareQuote v-if="quote.sourceKind === 'file' || quote.sourceKind === 'url'" :size="13" class="qd-source-icon" />
          <MousePointer2 v-else-if="quote.sourceKind === 'selection'" :size="13" class="qd-source-icon" />
          <MessageSquareText v-else :size="13" class="qd-source-icon" />
          {{ sourceLabel }}
        </span>
        <button
          v-if="canJump"
          class="qd-jump"
          :title="t('quoteBar.jumpToSource')"
          :aria-label="t('quoteBar.jumpToSource')"
          @click="$emit('jump', quote)"
        >
          <ExternalLink :size="14" />
        </button>
      </div>

      <!-- Quoted content, verbatim and read-only.
           Hidden entirely when there is no text: a whole-object quote (a file or
           an issue/PR reference) carries only a label, and an empty <pre> read as
           a rendering bug rather than as "this quote references the object". -->
      <template v-if="quote.text">
        <div class="qd-section-title">{{ t('quoteBar.quotedContent') }}</div>
        <pre class="qd-quoted-text">{{ quote.text }}</pre>
      </template>

      <!-- Annotation, editable. Saved explicitly: the sent case writes to the
           DB, so an implicit save on every keystroke would be wasteful and an
           implicit save on close would be invisible. -->
      <div class="qd-section-title">{{ t('quoteBar.annotation') }}</div>
      <textarea
        ref="noteRef"
        v-model="note"
        class="qd-note-input"
        rows="3"
        :placeholder="t('quoteBar.notePlaceholder')"
        :disabled="saving"
        @keydown.ctrl.enter.prevent="handleSave"
        @keydown.meta.enter.prevent="handleSave"
      />
    </div>

    <template #footer>
      <button class="fbtn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" :disabled="!dirty || saving" @click="handleSave">
        <LoadingIndicator v-if="saving" size="sm" inline />
        <span v-else>{{ t('common.save') }}</span>
      </button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquareQuote, MessageSquareText, MousePointer2, ExternalLink } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { quoteLabel, quoteLineRange, canJumpToSource, type QuoteItem } from '@/utils/quoteItem'

const props = withDefaults(defineProps<{
  open: boolean
  quote: QuoteItem | null
  /** True while the save request is in flight. */
  saving?: boolean
}>(), {
  saving: false,
})

const emit = defineEmits<{
  close: []
  save: [note: string]
  jump: [quote: QuoteItem]
}>()

const { t } = useI18n()
const noteRef = ref<HTMLTextAreaElement | null>(null)

// Local draft, seeded from the quote. Kept separate from the prop so the input
// is not clobbered mid-typing by a parent re-render, and so `dirty` can be
// computed without mutating the source.
const note = ref('')
let focusTimer: ReturnType<typeof setTimeout> | null = null

watch(
  () => [props.open, props.quote?.id] as const,
  ([open]) => {
    if (focusTimer) { clearTimeout(focusTimer); focusTimer = null }
    if (!open) return
    note.value = props.quote?.note || ''
    // Focus only AFTER the slide-up animation finishes (BottomSheet: 250ms).
    // Focusing during it makes the browser scroll the still-animating,
    // overflow-hidden panel to reveal the focused textarea, which fights the
    // slide and reads as the drawer bouncing. Every other drawer that
    // auto-focuses an input waits for the same reason (SearchDrawer,
    // UserMsgIndexDrawer both use 300ms).
    focusTimer = setTimeout(() => {
      focusTimer = null
      noteRef.value?.focus({ preventScroll: true })
    }, 300)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (focusTimer) { clearTimeout(focusTimer); focusTimer = null }
})

const dirty = computed(() => note.value !== (props.quote?.note || ''))

const canJump = computed(() => (props.quote ? canJumpToSource(props.quote) : false))

/**
 * Label for the source line.
 *
 * A real label (a file path, "owner/repo#7", a session name) always wins — it
 * is what tells the user which source this came from. Only a quote with no
 * label at all falls back to a generic kind label, so the row is never blank.
 */
const sourceLabel = computed(() => {
  const q = props.quote
  if (!q) return ''
  const labelled = `${quoteLabel(q)}${quoteLineRange(q)}`
  if (labelled) return labelled
  if (q.sourceKind === 'message') return t('quoteBar.messageQuote')
  if (q.sourceKind === 'selection') return t('quoteBar.selectionQuote')
  return ''
})

function handleSave() {
  if (!dirty.value || props.saving) return
  emit('save', note.value)
}
</script>

<style scoped>
.qd-content {
  padding: var(--space-4) var(--space-7) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.qd-source-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.qd-source {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex: 1;
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--accent-color, #0066cc);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qd-source-icon {
  flex-shrink: 0;
}

.qd-jump {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .qd-jump:hover {
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  }
}

.qd-section-title {
  margin-top: var(--space-2);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* Quoted content: read-only, scrollable when long, monospace so code quotes
   keep their alignment. */
.qd-quoted-text {
  margin: 0;
  padding: var(--space-3) var(--space-4);
  max-height: 40vh;
  overflow: auto;
  background: var(--bg-tertiary);
  border-left: 2px solid var(--accent-color, #0066cc);
  border-radius: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}

.qd-note-input {
  width: 100%;
  box-sizing: border-box;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-xs);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-md);
  line-height: var(--input-line-height, 20px);
  resize: vertical;
  outline: none;
}

.qd-note-input:focus {
  border-color: var(--accent-color, #0066cc);
}
</style>
