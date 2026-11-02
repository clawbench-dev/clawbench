<template>
  <BottomSheet :open="open" :title="t('chat.pending.editTitle')" auto @close="requestClose">
    <div class="qme">
      <p class="qme-hint">{{ t('chat.pending.editPrompt') }}</p>

      <textarea
        ref="textareaRef"
        v-model="draftText"
        class="qme-textarea"
        rows="4"
        :placeholder="t('chat.pending.editPlaceholder')"
        :aria-label="t('chat.pending.editTitle')"
        @keydown.ctrl.enter.prevent="save"
        @keydown.meta.enter.prevent="save"
      />

      <div class="qme-chips">
        <!-- First in the list, always: adding is the entry point of this row,
             and it is sized like the cards so the row reads as one control
             group rather than a button plus a separate strip. -->
        <button
          type="button"
          class="qme-add"
          data-action="queue-edit-add-attachment"
          :title="t('chat.pending.addAttachment')"
          @click="attachOpen = true"
        >
          <Paperclip :size="14" />
          {{ t('chat.pending.addAttachment') }}
        </button>

        <!-- Quotes are rendered here rather than through AttachmentTags: that
             component deliberately drops quote entries (they carry no path),
             and a quote is still an attachment the user may want to remove. -->
        <span
          v-for="q in quoteEntries"
          :key="fileEntryKey(q)"
          class="qme-chip qme-chip-quote"
          :title="q.text || q.path"
        >
          <span class="qme-chip-label">{{ q.path || t('chat.pending.fileReference') }}</span>
          <button
            type="button"
            class="qme-chip-remove"
            data-action="queue-edit-remove-quote"
            :title="t('common.remove')"
            @click="removeEntry(q)"
          >×</button>
        </span>

        <!-- `clickable: false` — this sheet has no viewer to open a file into,
             so the cards keep their "×" (removal is the whole point here) but
             must not show a hand cursor / "open file" tooltip for a click that
             goes nowhere. Not `readonly`: that would drop the "×" as well. -->
        <AttachmentTags
          :files="fileEntries"
          :clickable="false"
          @remove="removeEntry"
        />
      </div>

      <p v-if="error" class="qme-error">{{ error }}</p>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="requestClose">{{ t('common.cancel') }}</button>
      <button
        class="fbtn fbtn-primary"
        type="button"
        data-action="queue-edit-save"
        :disabled="saving"
        @click="save"
      >
        <LoadingIndicator v-if="saving" size="sm" inline />
        <span v-else>{{ t('common.save') }}</span>
      </button>
    </template>
  </BottomSheet>

  <!-- The app's only file picker: it owns the upload (progress, camera/photo
       entries on mobile) and reports the COMMITTED selection through
       add/remove-attached when the user confirms. A second, sheet-local picker
       would have to re-implement all of that — the exact "inline file selector"
       the feature was scoped to avoid. -->
  <AttachDrawer
    :open="attachOpen"
    :attached-files="draftFiles"
    @close="attachOpen = false"
    @add-attached="addPath"
    @remove-attached="removeEntry"
  />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Paperclip } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AttachmentTags from '@/components/chat/AttachmentTags.vue'
import AttachDrawer from '@/components/chat/AttachDrawer.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useFileUpload } from '@/composables/useFileUpload'
import { fileEntryKey, isQuoteEntry, type FileEntry } from '@/utils/fileAttachmentUtils'
import type { QueuedMessage } from '@/composables/useMessageQueue'

const props = defineProps<{
  open: boolean
  /** The entry being edited. Its text/files seed the draft when the sheet opens. */
  entry: QueuedMessage | null
  /** Whether a save request is in flight (owned by the caller). */
  saving: boolean
}>()

const emit = defineEmits<{
  close: []
  save: [payload: { text: string; files: FileEntry[] }]
}>()

const { t } = useI18n()
// Only used to drop the upload mirror the shared picker leaves behind — see
// addPath. The sheet never renders or cancels the input bar's uploads.
const { removePendingByPath } = useFileUpload()

const textareaRef = ref<HTMLTextAreaElement | null>(null)
const draftText = ref('')
const draftFiles = ref<FileEntry[]>([])
const attachOpen = ref(false)
const error = ref('')

const quoteEntries = computed(() => draftFiles.value.filter((f) => isQuoteEntry(f)))
const fileEntries = computed(() => draftFiles.value.filter((f) => !isQuoteEntry(f)))

let focusTimer: ReturnType<typeof setTimeout> | null = null
function clearFocusTimer() {
  if (focusTimer !== null) {
    clearTimeout(focusTimer)
    focusTimer = null
  }
}

/**
 * Seed the draft from the entry each time the sheet OPENS.
 *
 * Deliberately not a `watch` on `entry` contents: a queue_added broadcast can
 * land while the user is typing (another device edited the same entry), and
 * re-seeding then would overwrite their in-progress text under their cursor.
 */
watch(() => props.open, (open) => {
  if (!open) {
    attachOpen.value = false
    clearFocusTimer()
    return
  }
  draftText.value = props.entry?.text ?? ''
  draftFiles.value = [...(props.entry?.files ?? [])]
  error.value = ''
  // Focus only after the slide-in animation: focusing during it makes the
  // browser scroll the still-moving panel, which reads as the whole sheet
  // bouncing (same contract as SearchDrawer / UserMsgIndexDrawer).
  clearFocusTimer()
  focusTimer = setTimeout(() => {
    focusTimer = null
    textareaRef.value?.focus({ preventScroll: true })
  }, 300)
}, { immediate: true })

onBeforeUnmount(clearFocusTimer)

/**
 * Identity of an attachment, shared with the send path (dedupe) so "did the
 * user change the list?" uses the same notion of sameness. Quotes key on their
 * stable id: two quotes of the same range are genuinely different attachments.
 */
function addPath(path: string, isDir?: boolean) {
  if (!path) return
  const key = fileEntryKey({ path, isDir })
  if (draftFiles.value.some((f) => fileEntryKey(f) === key)) return
  // Publish a new array so the chips re-render (same trap the queue store
  // documents: mutating the array in place keeps the same reference).
  draftFiles.value = [...draftFiles.value, { path, isDir: !!isDir }]
  // The upload itself ran through the shared chat pipeline, which parks the
  // finished file in `pendingFiles` — that list is the chat input's send
  // payload, so leaving the mirror here would attach this file to the NEXT
  // ordinary message as well. It belongs to the queued entry only.
  removePendingByPath(path)
}

function removeEntry(entry: FileEntry | string) {
  const target = typeof entry === 'string' ? { path: entry } : entry
  const key = fileEntryKey(target)
  const kept = draftFiles.value.filter((f) => fileEntryKey(f) !== key)
  if (kept.length !== draftFiles.value.length) {
    draftFiles.value = kept
    return
  }
  // The picker toggles attachments by PATH only (no line range), so a ranged
  // attachment never matches the composite key. Fall back to path for plain
  // files — never for quotes, whose (often empty) label is not an identity.
  if (isQuoteEntry(target) || !target.path) return
  draftFiles.value = draftFiles.value.filter((f) => isQuoteEntry(f) || f.path !== target.path)
}

function requestClose() {
  if (props.saving) return
  emit('close')
}

function save() {
  if (props.saving) return
  const text = draftText.value.trim()
  // Nothing to send: the backend rejects this too (MessageOrFilesRequired), but
  // failing here keeps the sheet open with the user's draft intact.
  if (!text && draftFiles.value.length === 0) {
    error.value = t('chat.pending.editNothingToSend')
    return
  }
  error.value = ''
  emit('save', { text, files: draftFiles.value })
}
</script>

<style scoped>
.qme {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-6) var(--space-4);
  overflow-y: auto;
  min-height: 0;
}

.qme-hint {
  margin: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: var(--line-height-relaxed);
}

.qme-textarea {
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  min-height: calc(88 * var(--dp));
  max-height: 40vh;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-relaxed);
}

.qme-textarea:focus {
  outline: none;
  border-color: var(--accent-color);
}

/* One row: the add control, quote chips, then the shared attachment cards.
   The cards keep their own horizontal scroll, so a long list never pushes the
   add control out of reach — it stays pinned at the left. */
.qme-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

/* The shared strip brings the input box's inset; the row above already provides
   one, so drop it and let the cards line up with the add control. */
.qme-chips :deep(.chat-attachment-tags) {
  flex: 1 1 auto;
  min-width: 0;
  padding: 0;
  gap: var(--space-2);
}

.qme-chip {
  display: inline-flex;
  align-items: center;
  gap: calc(4 * var(--dp));
  max-width: 60%;
  padding: 1px var(--space-2) 1px var(--space-3);
  border-radius: var(--radius-full);
  background: rgba(127, 127, 127, 0.14);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.qme-chip-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qme-chip-remove {
  background: none;
  border: none;
  padding: 0 var(--space-1);
  cursor: pointer;
  color: inherit;
  font-size: var(--font-size-md);
  line-height: 1;
}

.qme-chip-remove:hover {
  color: var(--text-primary);
}

/* Same height and corner radius as the attachment cards it sits next to, so the
   row reads as one set of tiles instead of a small pill plus cards. Only the
   dashed accent outline says "this one adds". */
.qme-add {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  gap: var(--space-3);
  height: calc(40 * var(--dp));
  /* The 1px dashed outline must not make the tile 2px taller than the cards
     next to it (there is no global border-box reset in this app). */
  box-sizing: border-box;
  padding: 0 var(--space-4);
  border: 1px dashed color-mix(in srgb, var(--accent-color, #0066cc) 45%, transparent);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-sm);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .qme-add:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  }
}

.qme-error {
  margin: 0;
  font-size: var(--font-size-xs);
  color: #e08a8a;
}
</style>
