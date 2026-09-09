<template>
  <div class="chat-attachment-tags">
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
        <button class="attachment-close-btn" @click.stop="$emit('remove-pending', idx)" :title="t('common.remove')">×</button>
      </span>
    </template>

    <!-- Attached file reference cards -->
    <span v-for="fileEntry in files" :key="'att-' + entryKey(fileEntry)"
      class="chat-file-attachment attachment-ref"
      :class="{ 'attachment-image-only': isImageFile(fileEntry.path) && (isThumbableExt(fileEntry.path) || thumbErrors.has(fileEntry.path)) }"
      @click="$emit('file-click', fileEntry)"
      :title="t('chat.attach.openFile')">
      <img v-if="isImageFile(fileEntry.path) && isThumbableExt(fileEntry.path) && !thumbErrors.has(fileEntry.path)"
        class="attachment-thumb-img"
        :src="thumbUrl(fileEntry.path)" loading="lazy" @error="onThumbError(fileEntry.path)" />
      <FileIcon v-if="!isImageFile(fileEntry.path)" :path="fileEntry.path" :is-dir="fileEntry.isDir" :size="22" class="attachment-file-icon" />
      <span v-if="!isImageFile(fileEntry.path)" class="attachment-filename">
        {{ getFileName(fileEntry.path) }}<template v-if="fileEntry.startLine !== undefined"><span class="attachment-range">{{ lineRangeLabel(fileEntry) }}</span></template>
      </span>
      <button class="attachment-close-btn" @click.stop="$emit('remove', fileEntry)" :title="t('common.remove')">×</button>
    </span>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { isThumbableExt } from '@/utils/fileManager'
import { isImageFile, type FileEntry } from '@/utils/fileAttachmentUtils'
import { baseName } from '@/utils/path'
import type { PendingFile } from '@/composables/useFileUpload'

const props = withDefaults(defineProps<{
  files?: FileEntry[]
  pendingFiles?: PendingFile[]
}>(), {
  files: () => [],
  pendingFiles: () => [],
})

defineEmits<{
  'file-click': [entry: FileEntry]
  'remove': [entry: FileEntry]
  'remove-pending': [index: number]
}>()

const { t } = useI18n()
const thumbUrl = buildPathThumbUrl

/** Composite key so distinct line-range references of one file stay separate. */
function entryKey(entry: FileEntry): string {
  return `${entry.path}|${entry.startLine ?? 0}|${entry.endLine ?? 0}`
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
  gap: 6px;
  padding: 4px 6px;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-attachment-tags::-webkit-scrollbar {
  display: none;
}

.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 12px;
  height: 40px;
  padding: 0 8px;
  padding-right: 24px;
  flex-shrink: 0;
  max-width: 150px;
  position: relative;
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: opacity 0.15s;
  box-sizing: border-box;
}

.attachment-file-icon {
  flex-shrink: 0;
}

.attachment-filename {
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.chat-file-attachment.attachment-image-only {
  width: 40px;
  height: 40px;
  padding: 0;
  overflow: hidden;
  border-radius: 10px;
}

.attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.attachment-close-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 10px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s;
  z-index: 1;
}

@media (hover: hover) {
  .attachment-close-btn:hover {
    background: var(--danger-color, #dc3545);
  }
}

.chat-attachment-tags .attachment-ref {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 20%, transparent);
  color: var(--accent-color, #0066cc);
}

.chat-attachment-tags .attachment-ref .attachment-filename {
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .chat-attachment-tags .attachment-ref:hover {
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
  gap: 2px;
  border-radius: inherit;
  z-index: 1;
}

/* Upload spinner on the dark overlay → keep the arc white */
.attachment-upload-overlay .attachment-spinner {
  --li-color: #fff;
}

.attachment-progress-text {
  font-size: 9px;
  font-weight: 600;
  color: #ffffff;
  line-height: 1;
}
</style>
