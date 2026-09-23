<template>
  <div v-if="files.length > 0" class="chat-files">
    <!-- Quoted-snippet cards (shared component — same card as the input chip).
         Clicking emits the same file-tag-click the parent already routes; the
         parent branches on kind==='quote' to open the detail drawer instead of
         a file. -->
    <QuoteCard
      v-for="(raw, idx) in quoteFiles"
      :key="'quote-' + idx"
      :quote="fromFileEntry(normalizeFileEntry(raw))"
      @click="$emit('file-tag-click', normalizeFileEntry(raw))"
    />

    <!-- External URL attachment (a forge issue/PR reference from "Analyze with
         AI"). Rendered as a real link so it stays clickable after a reload;
         the label comes from path (owner/repo#123), the target from url. -->
    <a v-for="(raw, idx) in urlFiles" :key="'url-' + idx"
      class="chat-file-attachment attachment-ref attachment-url"
      :href="safeHref(raw)"
      :class="{ 'attachment-url-inert': !isSafeExternalUrl(normalizeFileEntry(raw).url) }"
      target="_blank"
      rel="noopener noreferrer"
      :title="urlLabel(raw)">
      <LinkIcon :size="14" :stroke-width="1.5" class="attachment-quote-icon" />
      <span class="attachment-filename">{{ urlLabel(raw) }}</span>
    </a>

    <span v-for="(raw, idx) in fileFiles" :key="idx"
      class="chat-file-attachment"
      :class="[isUploadPath(normalizeFileEntry(raw).path) ? 'attachment-upload' : 'attachment-ref', { 'attachment-image-only': showsThumb(raw) }]"
      @click="$emit('file-tag-click', normalizeFileEntry(raw))"
      :title="t('chat.attach.openFile')">
      <template v-if="normalizeFileEntry(raw).startLine !== undefined">
        <MessageSquareQuote :size="14" :stroke-width="1.5" class="attachment-quote-icon" />
        <span class="attachment-filename">{{ getFileName(normalizeFileEntry(raw).path) }}<span class="attachment-range">{{ rangeLabel(normalizeFileEntry(raw)) }}</span></span>
      </template>
      <template v-else>
        <img v-if="showsThumb(raw)"
          class="attachment-thumb-img"
          :src="thumbUrl(normalizeFileEntry(raw).path)" loading="lazy"
          @error="onThumbError(normalizeFileEntry(raw).path)" />
        <!-- Icon + filename fallback: non-images AND images the backend cannot
             thumbnail (SVG/WebP/BMP/… or a failed thumb request) render as a
             file card, never a blank square. -->
        <FileIcon v-if="!showsThumb(raw)" :path="normalizeFileEntry(raw).path" :is-dir="normalizeFileEntry(raw).isDir" :size="22" class="attachment-file-icon" />
        <span v-if="!showsThumb(raw)" class="attachment-filename">{{ getFileName(normalizeFileEntry(raw).path) }}</span>
      </template>
    </span>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { baseName } from '@/utils/path.ts'
import { normalizeFileEntry, isUploadPath, isImageFile, isUrlEntry, isQuoteEntry, isSafeExternalUrl } from '@/utils/fileAttachmentUtils.ts'
import { fromFileEntry } from '@/utils/quoteItem.ts'
import { isThumbableExt } from '@/utils/fileManager.ts'
import { buildPathThumbUrl } from '@/utils/fileIcon.ts'
import FileIcon from '@/components/common/FileIcon.vue'
import QuoteCard from '@/components/chat/QuoteCard.vue'
import { MessageSquareQuote, Link as LinkIcon } from 'lucide-vue-next'

const { t } = useI18n()

const props = defineProps({
  files: { type: Array, required: true },
})
defineEmits(['file-tag-click'])

// URL entries must not go through the file-card branch: their path is a label,
// not a filesystem path, so a thumbnail/preview click would be meaningless.
// Quotes are likewise not files, and render through the shared QuoteCard.
const urlFiles = computed(() => (props.files ?? []).filter(f => isUrlEntry(normalizeFileEntry(f))))
const quoteFiles = computed(() => (props.files ?? []).filter(f => isQuoteEntry(normalizeFileEntry(f))))
const fileFiles = computed(() => (props.files ?? []).filter(f => {
  const e = normalizeFileEntry(f)
  return !isUrlEntry(e) && !isQuoteEntry(e)
}))

/** Chip text: the stored label, falling back to the address itself. */
function urlLabel(raw) {
  const e = normalizeFileEntry(raw)
  return e.path || e.url || ''
}

/** href for a URL entry, or undefined when the scheme is not http(s). */
function safeHref(raw) {
  const url = normalizeFileEntry(raw).url
  return isSafeExternalUrl(url) ? url : undefined
}

function getFileName(path) {
  return baseName(path)
}

function rangeLabel(f) {
  if (f.endLine === undefined || f.endLine === f.startLine) return `:${f.startLine}`
  return `:${f.startLine}-${f.endLine}`
}

/**
 * A card shows the square thumbnail only when the backend can serve one and it
 * has not failed to load. Everything else (non-images, image formats without a
 * backend thumbnail like SVG/WebP/BMP, and failed thumb loads) falls back to the
 * icon + filename file card.
 */
function showsThumb(raw) {
  const path = normalizeFileEntry(raw).path
  return isImageFile(path) && isThumbableExt(path) && !thumbErrors.value.has(path)
}

const thumbUrl = buildPathThumbUrl

// Track thumbnail load errors — must replace Set to trigger Vue reactivity
const thumbErrors = ref(new Set())
function onThumbError(path) {
  const next = new Set(thumbErrors.value)
  next.add(path)
  thumbErrors.value = next
}

// Clear thumb errors when files are removed
watch(() => props.files.length, (len) => {
  if (len === 0 && thumbErrors.value.size > 0) {
    thumbErrors.value = new Set()
  }
})
</script>

<style scoped>
.chat-files {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: var(--space-3);
  margin: var(--space-2) 0;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-files::-webkit-scrollbar {
  display: none;
}

/* Non-image file card: filename pill */
.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  border-radius: var(--radius-lg);
  height: 40px;
  padding:0 var(--space-5);
  max-width: 150px;
  font-size: var(--font-size-sm);
  text-decoration: none;
  cursor: pointer;
  transition: opacity var(--duration-base);
  flex-shrink: 0;
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

/* Image card: square thumbnail */
.chat-file-attachment.attachment-image-only {
  width: 40px;
  height: 40px;
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

.attachment-upload,
.attachment-ref {
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.35);
}

/* External URL chip: reads as a link (accent text + underline on hover). */
.attachment-url {
  color: var(--accent-color, #0066cc);
}
.attachment-url .attachment-filename {
  text-decoration: none;
}
@media (hover: hover) {
  .attachment-url:hover .attachment-filename {
    text-decoration: underline;
  }
}
/* A URL with a non-http(s) scheme renders inert rather than as a live link. */
.attachment-url-inert {
  color: var(--text-muted, #8b8b8b);
  cursor: default;
}
</style>
