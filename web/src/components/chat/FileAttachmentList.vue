<template>
  <div v-if="files.length > 0" class="chat-files">
    <span v-for="(f, idx) in files" :key="idx"
      class="chat-file-attachment"
      :class="[isUploadPath(normalizeFileEntry(f).path) ? 'attachment-upload' : 'attachment-ref', { 'attachment-image-only': isImageFile(normalizeFileEntry(f).path) }]"
      @click="$emit('file-tag-click', normalizeFileEntry(f).path)"
      :title="t('chat.attach.openFile')">
      <img v-if="isImageFile(normalizeFileEntry(f).path) && isThumbableExt(normalizeFileEntry(f).path) && !thumbErrors.has(normalizeFileEntry(f).path)"
        class="attachment-thumb-img"
        :src="thumbUrl(normalizeFileEntry(f).path)" loading="lazy"
        @error="onThumbError(normalizeFileEntry(f).path)" />
      <!-- Non-image: icon + filename -->
      <component v-if="!isImageFile(normalizeFileEntry(f).path)" :is="getFileIcon(normalizeFileEntry(f).path)" :size="14" :stroke-width="1.5" class="attachment-file-icon" />
      <span v-if="!isImageFile(normalizeFileEntry(f).path)" class="attachment-filename">{{ getFileName(normalizeFileEntry(f).path) }}</span>
    </span>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText, FileImage, FileVideo, FileMusic } from 'lucide-vue-next'
import { baseName } from '@/utils/path.ts'
import { normalizeFileEntry, isUploadPath, isImageFile } from '@/utils/fileAttachmentUtils.ts'
import { isThumbableExt } from '@/utils/fileManager.ts'
import { getFileType } from '@/utils/fileType.ts'

const { t } = useI18n()

const props = defineProps({
  files: { type: Array, required: true },
})
defineEmits(['file-tag-click'])

function getFileName(path) {
  return baseName(path)
}

function getFileIcon(path) {
  const ft = getFileType(path)
  if (ft.isImage) return FileImage
  if (ft.isAudio) return FileMusic
  if (ft.isVideo) return FileVideo
  return FileText
}

function thumbUrl(path) {
  return `/api/file/thumb?path=${encodeURIComponent(path)}&w=80`
}

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
  gap: 6px;
  margin: 4px 0;
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
  gap: 4px;
  border-radius: 12px;
  height: 40px;
  padding: 0 10px;
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: opacity 0.15s;
  flex-shrink: 0;
  box-sizing: border-box;
}

.attachment-file-icon {
  flex-shrink: 0;
}

.attachment-filename {
  font-family: monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Image card: square thumbnail */
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

.attachment-upload,
.attachment-ref {
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.35);
}
</style>
