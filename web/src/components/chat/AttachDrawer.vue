<template>
  <BottomSheet :open="open" :no-swipe-close="filePickerOpen" auto @close="$emit('close')">
    <template #header>
      <div class="ad-header">
        <Paperclip :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('chat.attach.drawerTitle') }}</span>
        <button class="ad-upload-btn" @click="handleUploadClick" :title="t('chat.attach.uploadFile')">
          <Upload :size="16" />
        </button>
      </div>
    </template>

    <!-- Upload progress (inside drawer) -->
    <div v-if="uploadingFiles.length > 0" class="ad-upload-progress">
      <div v-for="(f, idx) in uploadingFiles" :key="'prog-' + idx" class="ad-progress-item">
        <div class="ad-progress-bar" :style="{ width: f.progress + '%' }"></div>
      </div>
    </div>

    <!-- Tab bar (horizontal scroll) -->
    <div class="ad-tab-bar">
      <button
        v-for="tab in tabs" :key="tab.key"
        class="ad-tab" :class="{ 'ad-tab-active': activeTab === tab.key }"
        @click="activeTab = tab.key"
      >{{ tab.label }}</button>
    </div>

    <!-- Tab content -->
    <div class="ad-content">
      <!-- Current (file + directory) -->
      <template v-if="activeTab === 'current'">
        <!-- Current directory -->
        <button v-if="effectiveCurrentDir"
          class="ad-file-row ad-current-item" :class="{ 'ad-file-attached': isAttached(effectiveCurrentDir) }"
          @click="toggleAttached(effectiveCurrentDir)">
          <div class="ad-icon-wrap">
            <Folder :size="28" class="ad-file-icon" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentDir') }}</span>
              {{ currentDirDisplayName }}
            </span>
            <span class="ad-file-meta">{{ effectiveCurrentDir }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', effectiveCurrentDir)" />
          <Check :size="14" class="ad-file-check" />
        </button>
        <!-- Current file -->
        <button v-if="currentFile"
          class="ad-file-row ad-current-item" :class="{ 'ad-file-attached': isAttached(currentFile) }"
          @click="toggleAttached(currentFile)">
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(currentFile) && isThumbableExt(currentFile) && !thumbErrors.has(currentFile)"
              class="ad-thumb" :src="thumbUrl(currentFile)" loading="lazy" @error="onThumbError(currentFile)" />
            <component v-else :is="getFileIcon(currentFile)" :size="28" class="ad-file-icon" :color="getFileIconColor(currentFile)" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentFile') }}</span>
              {{ baseName(currentFile) }}
            </span>
            <span class="ad-file-meta">{{ dirName(currentFile) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', currentFile)" />
          <Check :size="14" class="ad-file-check" />
        </button>
        <div v-if="!currentFile && !effectiveCurrentDir" class="ad-empty">{{ t('chat.attach.emptyCurrent') }}</div>
      </template>

      <!-- Recently referenced -->
      <template v-if="activeTab === 'references'">
        <div v-if="!recentReferencedFiles?.length" class="ad-empty">{{ t('chat.attach.emptyReferences') }}</div>
        <button
          v-for="item in recentReferencedFiles" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="toggleAttached(item.path)"
        >
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <component v-else :is="getFileIcon(item.path)" :size="28" class="ad-file-icon" :color="getFileIconColor(item.path)" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ baseName(item.path) }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · x{{ item.count }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>

      <!-- Recently shared -->
      <template v-if="activeTab === 'shares'">
        <div v-if="recentShares.length === 0" class="ad-empty">{{ t('chat.attach.emptyShares') }}</div>
        <button
          v-for="item in recentShares" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="toggleAttached(item.path)"
        >
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <component v-else :is="getFileIcon(item.path)" :size="28" class="ad-file-icon" :color="getFileIconColor(item.path)" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatRelativeTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>

      <!-- Recently uploaded + pending uploads -->
      <template v-if="activeTab === 'uploads'">
        <!-- Pending (in-flight) uploads -->
        <div v-for="(f, idx) in pendingFiles" :key="'pending-' + idx"
          class="ad-file-row ad-pending-item" :class="{ 'ad-file-attached': f.path && isAttached(f.path) }"
          @click="f.path && !f.uploading && toggleAttached(f.path)">
          <div class="ad-icon-wrap">
            <img v-if="f.isImage && f.previewUrl" class="ad-thumb" :src="f.previewUrl" loading="lazy" />
            <img v-else-if="f.isImage && f.path && !thumbErrors.has(f.path)"
              class="ad-thumb" :src="thumbUrl(f.path)" loading="lazy" @error="onThumbError(f.path)" />
            <Loader2 v-else-if="f.uploading" :size="20" class="ad-file-icon spin-icon" />
            <component v-else :is="getFileIcon(f.path || '')" :size="28" class="ad-file-icon" :color="f.path ? getFileIconColor(f.path) : undefined" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ getFileName(f.path) || t('chat.attach.uploading') }}</span>
            <span class="ad-file-meta ad-progress-text">{{ f.uploading ? f.progress + '%' : formatFileSize(f.size) }}</span>
          </div>
          <button v-if="!f.uploading" class="ad-pending-close" @click.stop="removeFile(idx)" :title="t('common.remove')">
            <X :size="14" />
          </button>
          <Check v-if="f.path && isAttached(f.path)" :size="14" class="ad-file-check" />
        </div>
        <!-- Completed uploads from server -->
        <div v-if="recentUploads.length === 0 && pendingFiles.length === 0" class="ad-empty">{{ t('chat.attach.emptyUploads') }}</div>
        <button
          v-for="item in recentUploads" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="toggleAttached(item.path)"
        >
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <component v-else :is="getFileIcon(item.path)" :size="28" class="ad-file-icon" :color="getFileIconColor(item.path)" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatRelativeTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>
    </div>

    <!-- Hidden file input (owned by drawer) -->
    <input type="file" ref="fileInputRef" @change="onFileSelect" style="display:none" multiple />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted } from 'vue'
import { Paperclip, Upload, Check, ExternalLink, Loader2, X } from 'lucide-vue-next'
import { getFileIcon, getFileIconColor, buildPathThumbUrl, Folder } from '@/utils/fileIcon'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useI18n } from 'vue-i18n'
import { useShareIn } from '@/composables/useShareIn'
import { useUploadRecent } from '@/composables/useUploadRecent'
import { useFileUpload } from '@/composables/useFileUpload'
import { baseName, dirName } from '@/utils/path'
import { formatFileSize } from '@/utils/fileType'
import { formatRelativeTime } from '@/utils/format'
import { isThumbableExt } from '@/utils/fileManager'
import { isImageFile } from '@/utils/fileAttachmentUtils'

interface ReferencedFile {
  path: string
  count: number
}

const props = withDefaults(defineProps<{
  open: boolean
  currentFile?: string | null
  currentDir?: string | null
  attachedFiles?: string[]
  recentReferencedFiles?: ReferencedFile[]
}>(), {
  currentFile: null,
  currentDir: null,
  attachedFiles: () => [],
  recentReferencedFiles: () => [],
})

const emit = defineEmits<{
  close: []
  'add-attached': [path: string]
  'remove-attached': [path: string]
  'file-open': [path: string]
}>()

const { t } = useI18n()
const { recentShares, fetchRecentShares } = useShareIn()
const { recentUploads, fetchRecentUploads } = useUploadRecent()

// ── Upload logic (now lives inside the drawer) ──
const { pendingFiles, handleFileSelect, handleFileDrop, removeFile } = useFileUpload()

const activeTab = ref('current')
const fileInputRef = ref<HTMLInputElement | null>(null)
const filePickerOpen = ref(false)

const tabs = [
  { key: 'current', label: '' },
  { key: 'references', label: '' },
  { key: 'shares', label: '' },
  { key: 'uploads', label: '' },
]

// Lazy-init tab labels
tabs[0].label = t('chat.attach.currentTab')
tabs[1].label = t('chat.attach.recentReferences')
tabs[2].label = t('chat.attach.recentShares')
tabs[3].label = t('chat.attach.recentUploads')

// ── File icon and thumbnail helpers (imported from utils/fileIcon) ──
const thumbUrl = buildPathThumbUrl

const thumbErrors = ref(new Set<string>())
function onThumbError(path: string) {
  const next = new Set(thumbErrors.value)
  next.add(path)
  thumbErrors.value = next
}

// ── Upload UI logic ──

const uploadingFiles = computed(() => pendingFiles.value.filter(f => f.uploading))

function getFileName(path: string) {
  return path ? baseName(path) : ''
}

function handleUploadClick() {
  if (fileInputRef.value) {
    fileInputRef.value.value = ''
    filePickerOpen.value = true
    fileInputRef.value.click()
  }
}

async function onFileSelect(e: Event) {
  filePickerOpen.value = false
  await handleFileSelect(e)
  // Switch to uploads tab to show the upload progress
  activeTab.value = 'uploads'
}

// When uploads complete, refresh recent uploads list and stay on uploads tab
let wasUploading = false
let uploadRefreshScheduled = false
watch(uploadingFiles, (now) => {
  if (wasUploading && now.length === 0) {
    // Upload just finished — refresh recent uploads
    if (!uploadRefreshScheduled) {
      uploadRefreshScheduled = true
      Promise.resolve().then(() => {
        uploadRefreshScheduled = false
        fetchRecentUploads()
      })
    }
  }
  wasUploading = now.length > 0
})

// ── Attachment logic ──

function isAttached(path: string) {
  return props.attachedFiles?.includes(path) ?? false
}

function toggleAttached(path: string) {
  if (isAttached(path)) {
    emit('remove-attached', path)
  } else {
    emit('add-attached', path)
  }
}

// Effective current dir: always show something (even at project root)
const effectiveCurrentDir = computed(() => props.currentDir || '.')
const currentDirDisplayName = computed(() => {
  const dir = effectiveCurrentDir.value
  return dir === '.' ? '/' : baseName(dir)
})

// Fetch data when drawer opens
watch(() => props.open, (v) => {
  if (v) {
    fetchRecentShares()
    fetchRecentUploads()
  } else {
    filePickerOpen.value = false
    // Clear thumb errors when drawer closes
    if (thumbErrors.value.size > 0) {
      thumbErrors.value = new Set()
    }
  }
})

// When the native file picker closes (user picks files or cancels),
// the window regains focus. Reset filePickerOpen so the drawer's
// back handler is re-enabled. (onFileSelect also resets it, but
// the cancel path has no JS callback — only the focus event fires.)
function onWindowFocus() {
  if (filePickerOpen.value) filePickerOpen.value = false
}
onMounted(() => window.addEventListener('focus', onWindowFocus))
onUnmounted(() => window.removeEventListener('focus', onWindowFocus))

// Expose for parent: activeTab + handleFileDrop (for drag-and-drop from ChatInputBar)
defineExpose({ activeTab, handleFileDrop })
</script>

<style>
.ad-header {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.ad-upload-btn {
  margin-left: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: none;
  background: var(--bg-hover);
  color: var(--text-secondary);
  cursor: pointer;
}
.ad-upload-btn:active {
  background: var(--accent-color);
  color: #fff;
}

/* Upload progress bars inside drawer */
.ad-upload-progress {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 14px 0;
}
.ad-progress-item {
  height: 3px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  border-radius: 2px;
  overflow: hidden;
}
.ad-progress-bar {
  height: 100%;
  background: var(--accent-color, #0066cc);
  border-radius: 2px;
  transition: width 0.15s ease;
}

/* Tab bar */
.ad-tab-bar {
  display: flex;
  gap: 0;
  padding: 0 12px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border-color);
  -webkit-overflow-scrolling: touch;
}
.ad-tab {
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  white-space: nowrap;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
}
.ad-tab-active {
  color: var(--accent-color);
  border-bottom-color: var(--accent-color);
}

/* Content area */
.ad-content {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
.ad-empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

/* File row */
.ad-file-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.ad-file-row:active {
  background: var(--bg-hover);
}
.ad-file-attached {
  opacity: 0.45;
}

/* Pending upload row */
.ad-pending-item {
  background: var(--bg-secondary);
}
.ad-pending-close {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 6px;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
}
.ad-pending-close:active {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ad-progress-text {
  color: var(--accent-color);
}

/* Icon container: holds icon or thumbnail.
 * 28x28 matches FileManagerContent list-view icon size.
 * Thumbnails fill the container; icons stay at their :size prop. */
.ad-icon-wrap {
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border-radius: 6px;
  position: relative;
}
.ad-icon-wrap .ad-thumb {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 6px;
}

.ad-file-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}
.ad-file-info {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.ad-file-name {
  display: block;
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ad-file-meta {
  display: block;
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}
.ad-file-check {
  flex-shrink: 0;
  color: var(--accent-color);
  visibility: hidden;
}
.ad-file-attached .ad-file-check {
  visibility: visible;
}
.ad-file-open {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: 0.5;
  transition: opacity 0.15s, color 0.15s;
}
.ad-file-row:hover .ad-file-open,
.ad-file-row:active .ad-file-open {
  opacity: 1;
  color: var(--accent-color);
}

/* Current item label */
.ad-label {
  display: inline-block;
  font-size: 10px;
  font-weight: 600;
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  padding: 1px 5px;
  border-radius: 3px;
  margin-right: 6px;
  vertical-align: middle;
  letter-spacing: 0.3px;
}
.ad-current-item {
  background: var(--bg-secondary);
}

/* Spinner */
.spin-icon {
  animation: spin 1s linear infinite;
  color: var(--accent-color);
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
