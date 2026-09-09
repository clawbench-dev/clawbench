<template>
  <div v-if="files.length > 0" class="chat-files">
    <span v-for="(raw, idx) in files" :key="idx"
      class="chat-file-attachment"
      :class="[isUploadPath(normalizeFileEntry(raw).path) ? 'attachment-upload' : 'attachment-ref', { 'attachment-image-only': isImageFile(normalizeFileEntry(raw).path) }]"
      @click="$emit('file-tag-click', normalizeFileEntry(raw))"
      :title="t('chat.attach.openFile')">
      <template v-if="normalizeFileEntry(raw).startLine !== undefined">
        <Code2 :size="14" :stroke-width="1.5" class="attachment-quote-icon" />
        <span class="attachment-filename">{{ getFileName(normalizeFileEntry(raw).path) }}<span class="attachment-range">{{ rangeLabel(normalizeFileEntry(raw)) }}</span></span>
      </template>
      <template v-else>
        <img v-if="isImageFile(normalizeFileEntry(raw).path) && isThumbableExt(normalizeFileEntry(raw).path) && !thumbErrors.has(normalizeFileEntry(raw).path)"
          class="attachment-thumb-img"
          :src="thumbUrl(normalizeFileEntry(raw).path)" loading="lazy"
          @error="onThumbError(normalizeFileEntry(raw).path)" />
        <!-- Non-image: icon + filename -->
        <FileIcon v-if="!isImageFile(normalizeFileEntry(raw).path)" :path="normalizeFileEntry(raw).path" :is-dir="normalizeFileEntry(raw).isDir" :size="22" class="attachment-file-icon" />
        <span v-if="!isImageFile(normalizeFileEntry(raw).path)" class="attachment-filename">{{ getFileName(normalizeFileEntry(raw).path) }}</span>
      </template>
    </span>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { baseName } from '@/utils/path.ts'
import { normalizeFileEntry, isUploadPath, isImageFile } from '@/utils/fileAttachmentUtils.ts'
import { isThumbableExt } from '@/utils/fileManager.ts'
import { buildPathThumbUrl } from '@/utils/fileIcon.ts'
import FileIcon from '@/components/common/FileIcon.vue'
import { Code2 } from 'lucide-vue-next'

const { t } = useI18n()

const props = defineProps({
  files: { type: Array, required: true },
})
defineEmits(['file-tag-click'])

function getFileName(path) {
  return baseName(path)
}

function rangeLabel(f) {
  if (f.endLine === undefined || f.endLine === f.startLine) return `:${f.startLine}`
  return `:${f.startLine}-${f.endLine}`
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
  gap: 6px;
  border-radius: 12px;
  height: 40px;
  padding: 0 10px;
  max-width: 150px;
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
  font-family: var(--font-mono, monospace);
  font-size: 12px;
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
