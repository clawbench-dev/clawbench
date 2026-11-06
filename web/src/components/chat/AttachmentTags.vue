<template>
  <div
    class="chat-attachment-tags"
    :class="{ 'is-readonly': readonly, 'is-not-clickable': notClickable }"
    @wheel="onHorizontalWheel"
  >
    <!-- Whole-batch terminate, shown while the picker's sequential upload is
         still running.
         The drawer's own terminate button is unreachable in practice: picking
         files closes the drawer, so the only thing on screen during an upload
         is this strip. Per-card "×" is NOT a substitute — it aborts one file
         and the loop moves on to the next. -->
    <button v-if="hasUploadingPending"
      class="chat-attachment-terminate"
      type="button"
      :title="t('chat.attach.terminateUpload')"
      :aria-label="t('chat.attach.terminateUpload')"
      @click.stop="handleTerminateUpload">
      <Square :size="12" />
      <span class="attachment-terminate-label">{{ t('chat.attach.terminateUpload') }}</span>
    </button>

    <!-- Uploading pending file cards (instant local Blob thumbnail & upload progress) -->
    <template v-for="(pf, idx) in pendingFiles" :key="'pending-' + idx">
      <span v-if="pf.uploading"
        class="chat-file-attachment attachment-pending"
        :class="{ 'attachment-image-only': pf.isImage && pf.previewUrl }">
        <img v-if="pf.isImage && pf.previewUrl"
          class="attachment-thumb-img attachment-uploading-img"
          :src="pf.previewUrl" />
        <FileIcon v-else :path="pf.path || 'file'" :size="22" class="attachment-file-icon" />
        <div class="attachment-upload-overlay">
          <LoadingIndicator class="attachment-spinner" size="sm" inline />
          <span class="attachment-progress-text">{{ pf.progress }}%</span>
        </div>
        <button v-if="!readonly" class="attachment-close-btn" @click.stop="$emit('remove-pending', idx)" :title="t('common.remove')">×</button>
      </span>
    </template>

    <!-- External URL attachment (e.g. a GitHub issue / PR reference). Rendered
         as a link chip; clicking opens the address instead of a file preview. -->
    <a v-for="fileEntry in urlEntries"
      :key="'url-' + fileEntry.url"
      class="chat-file-attachment attachment-ref attachment-url"
      :class="{ 'attachment-url-inert': !isSafeExternalUrl(fileEntry.url) }"
      :href="isSafeExternalUrl(fileEntry.url) ? fileEntry.url : undefined"
      target="_blank"
      rel="noopener noreferrer"
      :title="fileEntry.url">
      <LinkIcon :size="16" class="attachment-file-icon" />
      <span class="attachment-filename">{{ fileEntry.path || fileEntry.url }}</span>
      <button v-if="!readonly" class="attachment-close-btn" @click.stop.prevent="$emit('remove', fileEntry)" :title="t('common.remove')">×</button>
    </a>

    <!-- Attached file reference cards -->
    <span v-for="fileEntry in fileEntries" :key="'att-' + entryKey(fileEntry)"
      class="chat-file-attachment attachment-ref"
      :class="{ 'attachment-image-only': showsThumb(fileEntry) }"
      @click="onFileCardClick(fileEntry)"
      :title="cardTitle(fileEntry)">
      <img v-if="showsThumb(fileEntry)"
        class="attachment-thumb-img"
        :src="thumbUrlFor(fileEntry)" loading="lazy" @error="onThumbError(fileEntry.path)" />
      <!-- Icon + filename fallback: non-images AND images the backend cannot
           thumbnail (SVG/WebP/BMP/… or a failed thumb request) render as a
           file card, never a blank pill. -->
      <FileIcon v-if="!showsThumb(fileEntry)" :path="fileEntry.path" :is-dir="fileEntry.isDir" :size="22" class="attachment-file-icon" />
      <span v-if="!showsThumb(fileEntry)" class="attachment-filename">
        {{ getFileName(fileEntry.path) }}<template v-if="fileEntry.startLine !== undefined"><span class="attachment-range">{{ lineRangeLabel(fileEntry) }}</span></template>
      </span>
      <button v-if="!readonly" class="attachment-close-btn" @click.stop="$emit('remove', fileEntry)" :title="t('common.remove')">×</button>
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { isThumbableExt } from '@/utils/fileManager'
import { isImageFile, isUrlEntry, isQuoteEntry, isSafeExternalUrl, type FileEntry } from '@/utils/fileAttachmentUtils'
import { Link as LinkIcon, Square } from 'lucide-vue-next'
import { baseName } from '@/utils/path'
import { onHorizontalWheel } from '@/utils/horizontalWheelScroll'
import { useFileUpload, type PendingFile } from '@/composables/useFileUpload'

const props = withDefaults(defineProps<{
  files?: FileEntry[]
  pendingFiles?: PendingFile[]
  /**
   * Render the cards without their remove buttons and without the space
   * reserved for one. Used by surfaces that only DISPLAY attachments (the
   * queued-message row); removal there is a separate action on the row.
   */
  readonly?: boolean
  /**
   * Whether a file card opens the file (`file-click`). Default true.
   *
   * Distinct from `readonly`, which removes the "×" as well: the queued-message
   * edit sheet keeps removal (its own action) but has nothing to open — a card
   * there emitted `file-click` into the void while still showing the hand
   * cursor and the "open file" tooltip, i.e. it read as a broken control.
   * Passing false here drops the handler, the tooltip's promise and the pointer
   * affordance, and leaves the card otherwise identical.
   */
  clickable?: boolean
}>(), {
  files: () => [],
  pendingFiles: () => [],
  readonly: false,
  clickable: true,
})

const emit = defineEmits<{
  'file-click': [entry: FileEntry]
  'remove': [entry: FileEntry]
  'remove-pending': [index: number]
}>()

const { t } = useI18n()
const thumbUrl = buildPathThumbUrl
const { terminateChatUploadBatch, pendingFiles: uploadPendingFiles } = useFileUpload()

/**
 * Local blob previews for the files this session just uploaded.
 *
 * A finished upload keeps its pending entry (the drawer's watcher retains it
 * while the path is attached, which is what makes the input strip able to drop
 * the row later) together with the local Blob it was previewed from. The
 * committed card would otherwise switch to a server thumbnail that still has to
 * be fetched — leaving an empty tile in exactly the spot where a picture was a
 * frame earlier, which reads as "the image vanished", i.e. the reported #629
 * symptom. Reusing the blob makes the hand-off seamless; it is revoked together
 * with the entry, so the two lifetimes stay aligned.
 */
const localPreviews = computed(() => {
  const map = new Map<string, string>()
  for (const f of uploadPendingFiles.value) {
    if (f.path && f.previewUrl && !f.uploading) map.set(f.path, f.previewUrl)
  }
  return map
})

/** Thumbnail source for a committed card: the local blob while we still hold one. */
function thumbUrlFor(entry: FileEntry): string {
  return localPreviews.value.get(entry.path) ?? thumbUrl(entry.path)
}

/** True while the picker's sequential upload still has a file in flight. */
const hasUploadingPending = computed(() => (props.pendingFiles ?? []).some(f => f.uploading))

/**
 * Abort the WHOLE upload batch.
 *
 * Handled here rather than in the parent: `terminateChatUploadBatch` is the one
 * implementation of "stop the batch, abort the transfer, detach the files that
 * already finished", and it also removes the cards this component renders. The
 * same call backs the drawer's banner button, so the two entry points cannot
 * drift on whether finished files are detached.
 *
 * The per-card "×" next to each file is deliberately a different action: it
 * aborts only that file and the sequential loop continues with the next one.
 */
function handleTerminateUpload() {
  terminateChatUploadBatch()
}

// URL attachments render as links; local entries render as file cards. Keeping
// them in separate lists avoids branching inside the file-card markup.
// Quotes are excluded entirely: they carry no path, so a file card would render
// as an empty pill. They have their own card (QuoteCard).
const urlEntries = computed(() => (props.files ?? []).filter(f => isUrlEntry(f)))
const fileEntries = computed(() => (props.files ?? []).filter(f => !isUrlEntry(f) && !isQuoteEntry(f)))

/** Composite key so distinct line-range references of one file stay separate. */
function entryKey(entry: FileEntry): string {
  return `${entry.path}|${entry.startLine ?? 0}|${entry.endLine ?? 0}`
}

/**
 * Whether a card can open anything.
 *
 * `readonly` implies it: a display-only strip (the queued ROW) has nothing to
 * open either, so it must not keep a click handler that emits into the void
 * just because it did not also pass `clickable: false`.
 */
const notClickable = computed(() => !props.clickable || props.readonly)

/**
 * Open the file behind a card — only when this surface can actually open one.
 *
 * A `clickable: false` surface (the queued-message edit sheet) has no viewer to
 * route to, so the click must be a no-op rather than an event nobody listens
 * for. The cursor/tooltip are suppressed in the template + CSS for the same
 * reason: the card must not advertise an action it cannot perform.
 */
function onFileCardClick(entry: FileEntry): void {
  if (notClickable.value) return
  emit('file-click', entry)
}

/**
 * Tooltip for a file card. While clickable it names the ACTION; otherwise it
 * falls back to the file name, which is the only information an image-only
 * card would otherwise hide (it renders no filename label).
 */
function cardTitle(entry: FileEntry): string | undefined {
  if (!notClickable.value) return t('chat.attach.openFile')
  return getFileName(entry.path) || undefined
}

/** "N" for a single line or "N-M" for a range. */
function lineRangeLabel(entry: FileEntry): string {
  if (entry.startLine === undefined) return ''
  if (entry.endLine === undefined || entry.endLine === entry.startLine) return `:${entry.startLine}`
  return `:${entry.startLine}-${entry.endLine}`
}

const thumbErrors = ref(new Set<string>())
function onThumbError(path: string) {
  const next = new Set(thumbErrors.value)
  next.add(path)
  thumbErrors.value = next
}

function getFileName(path: string) {
  return path ? baseName(path) : ''
}

/**
 * A card shows the square thumbnail only when the backend can serve one and it
 * has not failed to load. Everything else (non-images, image formats without a
 * backend thumbnail like SVG/WebP/BMP, and failed thumb loads) falls back to the
 * icon + filename file card.
 */
function showsThumb(entry: FileEntry): boolean {
  return isImageFile(entry.path) && isThumbableExt(entry.path) && !thumbErrors.value.has(entry.path)
}

// Clear thumb errors when files list empties
watch(() => props.files, (files) => {
  if (files.length === 0 && thumbErrors.value.size > 0) {
    thumbErrors.value = new Set()
  }
})
</script>

<style>
.chat-attachment-tags {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-attachment-tags::-webkit-scrollbar {
  display: none;
}

.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  border-radius: var(--radius-lg);
  height: calc(40 * var(--dp));
  padding: 0 var(--space-4);
  padding-right: calc(24 * var(--dp));
  flex-shrink: 0;
  max-width: calc(150 * var(--dp));
  position: relative;
  font-size: var(--font-size-sm);
  text-decoration: none;
  cursor: pointer;
  transition: opacity var(--duration-base);
  box-sizing: border-box;
}

.attachment-file-icon {
  flex-shrink: 0;
}

.attachment-filename {
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.chat-file-attachment.attachment-image-only {
  width: calc(40 * var(--dp));
  height: calc(40 * var(--dp));
  padding: 0;
  overflow: hidden;
  border-radius: var(--radius-md);
}

.attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.attachment-close-btn {
  position: absolute;
  top: calc(4 * var(--dp));
  right: calc(4 * var(--dp));
  width: calc(16 * var(--dp));
  height: calc(16 * var(--dp));
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--duration-base);
  z-index: 1;
}

@media (hover: hover) {
  .attachment-close-btn:hover {
    background: var(--color-red);
  }
}

.chat-attachment-tags .attachment-ref {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 20%, transparent);
  color: var(--accent-color, #0066cc);
}

/* Read-only surfaces (queued rows): no × means no need for the space reserved
   for it, which otherwise reads as an unexplained gap at the card's right.
   Image-only cards are EXCLUDED on purpose: they are a fixed 40dp square whose
   thumbnail must fill it edge to edge. This selector is three classes deep, so
   without the :not() it outranks `.chat-file-attachment.attachment-image-only`
   (two classes) and re-introduces a right padding — the thumbnail then renders
   narrower than its tile and looks off-centre. */
.chat-attachment-tags.is-readonly .chat-file-attachment:not(.attachment-image-only) {
  padding-right: var(--space-4);
}

/* Surfaces with no click handler behind the cards must not advertise
   interactivity: a hand cursor over a card that does nothing reads as a broken
   control. Two ways to get there — `is-readonly` (display only, no "×") and
   `is-not-clickable` (removable but nothing to open, the queued-message edit
   sheet). URL chips are EXCLUDED — those are real links and keep both the
   pointer and the hover. */
.chat-attachment-tags.is-readonly .chat-file-attachment:not(.attachment-url),
.chat-attachment-tags.is-not-clickable .chat-file-attachment:not(.attachment-url) {
  cursor: default;
}

.chat-attachment-tags .attachment-ref .attachment-filename {
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  /* Hover tint is a click affordance too, so it is gated on the card actually
     being clickable rather than overridden back to its resting colour. */
  .chat-attachment-tags:not(.is-readonly):not(.is-not-clickable) .attachment-ref:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
  }
}

.chat-attachment-tags .attachment-pending {
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px dashed var(--border-color, #d1d5db);
  color: var(--text-secondary, #4b5563);
}

.attachment-upload-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  border-radius: inherit;
  z-index: 1;
}

/* Upload spinner on the dark overlay → keep the arc white */
.attachment-upload-overlay .attachment-spinner {
  --li-color: #fff;
}

.attachment-progress-text {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: #ffffff;
  line-height: 1;
}

/* Whole-batch terminate pill. Sits first in the strip (the upload it aborts is
   what the following cards are), and uses the danger tint so it does not read
   as another attachment card. */
.chat-attachment-terminate {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  height: calc(40 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: var(--radius-lg);
  background: var(--bg-tertiary, #f3f4f6);
  color: var(--color-red, #dc2626);
  font-size: var(--font-size-sm);
  cursor: pointer;
  box-sizing: border-box;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .chat-attachment-terminate:hover {
    background: color-mix(in srgb, var(--color-red, #dc2626) 10%, var(--bg-tertiary, #f3f4f6));
  }
}

/* Square thumbnails make the strip narrow; keep the label so the control is
   self-explanatory, but let it shrink first. */
.attachment-terminate-label {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* A URL whose scheme is not http(s) renders inert: the backend rejects these
   now, but a pre-existing row could still carry one. */
.attachment-url-inert {
  color: var(--text-muted, #8b8b8b);
  cursor: default;
}
</style>
