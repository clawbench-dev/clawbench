<template>
  <BottomSheet :open="open" :close-guard="filePickerOpen" auto @close="onDrawerClose">
    <template #header>
      <div class="ad-header">
        <Paperclip :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('chat.attach.drawerTitle') }}</span>
        <button class="ad-upload-btn" @click="handleUploadClick" :title="t('chat.attach.uploadFile')">
          <Upload :size="16" />
          <span class="ad-upload-label">{{ t('chat.attach.uploadFile') }}</span>
        </button>
      </div>
    </template>

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
          @click="toggleAttached(effectiveCurrentDir, true)">
          <div class="ad-icon-wrap">
            <FileIcon path="" :is-dir="true" :size="28" class="ad-file-icon" />
            <Check v-show="isAttached(effectiveCurrentDir)" :size="12" class="ad-icon-check" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentDir') }}</span>
              {{ currentDirDisplayName }}
            </span>
            <span class="ad-file-meta">{{ effectiveCurrentDir }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', effectiveCurrentDir)" />
        </button>
        <!-- Current file -->
        <button v-if="currentFile"
          class="ad-file-row ad-current-item" :class="{ 'ad-file-attached': isAttached(currentFile) }"
          @click="toggleAttached(currentFile)">
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(currentFile) && isThumbableExt(currentFile) && !thumbErrors.has(currentFile)"
              class="ad-thumb" :src="thumbUrl(currentFile)" loading="lazy" @error="onThumbError(currentFile)" />
            <FileIcon v-else :path="currentFile" :size="28" class="ad-file-icon" />
            <Check v-show="isAttached(currentFile)" :size="12" class="ad-icon-check" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentFile') }}</span>
              {{ baseName(currentFile) }}
            </span>
            <span class="ad-file-meta">{{ dirName(currentFile) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', currentFile)" />
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
            <FileIcon v-else :path="item.path" :size="28" class="ad-file-icon" />
            <Check v-show="isAttached(item.path)" :size="12" class="ad-icon-check" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ baseName(item.path) }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · x{{ item.count }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
        </button>
      </template>

      <!-- Recently shared -->
      <template v-if="activeTab === 'shares'">
        <div v-if="!recentShares?.length" class="ad-empty">{{ t('chat.attach.emptyShares') }}</div>
        <button
          v-for="item in recentShares" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="toggleAttached(item.path)"
        >
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <FileIcon v-else :path="item.path" :size="28" class="ad-file-icon" />
            <Check v-show="isAttached(item.path)" :size="12" class="ad-icon-check" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatRelativeTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Trash2 :size="14" class="ad-file-delete" @click.stop="handleDeleteShare(item)" :title="t('chat.attach.deleteRecent')" />
        </button>
      </template>

      <!-- Recently uploaded + pending uploads -->
      <template v-if="activeTab === 'uploads'">
        <!-- Pending uploads: only show while uploading (failed items removed by useFileUpload) -->
        <button
          v-for="(f, idx) in pendingFiles" :key="'pending-' + idx"
          v-show="f.uploading"
          class="ad-file-row" :class="{ 'ad-file-attached': f.path && isAttached(f.path) }"
          @click="f.path && !f.uploading && toggleAttached(f.path)"
        >
          <div class="ad-icon-wrap ad-uploading-icon">
            <span class="ad-upload-pct">{{ f.progress }}%</span>
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ getFileName(f.path) || t('chat.attach.uploading') }}</span>
            <span class="ad-file-meta">{{ f.path ? dirName(f.path) + ' · ' : '' + formatFileSize(f.size) }}</span>
          </div>
        </button>
        <!-- Completed uploads from server -->
        <div v-if="!recentUploads?.length && pendingFiles.length === 0" class="ad-empty">{{ t('chat.attach.emptyUploads') }}</div>
        <button
          v-for="item in recentUploads" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="toggleAttached(item.path)"
        >
          <div class="ad-icon-wrap">
            <img v-if="isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <FileIcon v-else :path="item.path" :size="28" class="ad-file-icon" />
            <Check v-show="isAttached(item.path)" :size="12" class="ad-icon-check" />
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatRelativeTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Trash2 :size="14" class="ad-file-delete" @click.stop="handleDeleteUpload(item)" :title="t('chat.attach.deleteRecent')" />
        </button>
      </template>
    </div>

    <!-- Selected files footer (reuses AttachmentTags component) -->
    <template v-if="attachedFiles.length > 0" #footer>
      <AttachmentTags :files="attachedFiles" @file-click="emit('file-open', $event)" @remove="emit('remove-attached', $event)" />
    </template>

    <!-- Hidden file input (owned by drawer) -->
    <input type="file" ref="fileInputRef" @change="onFileSelect" @blur="onFileInputBlur" style="display:none" multiple />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { Paperclip, Upload, Check, ExternalLink, Trash2 } from 'lucide-vue-next'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import FileIcon from '@/components/common/FileIcon.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AttachmentTags from '@/components/chat/AttachmentTags.vue'
import { useI18n } from 'vue-i18n'
import { useDialog } from '@/composables/useDialog'
import { useShareIn } from '@/composables/useShareIn'
import { useUploadRecent } from '@/composables/useUploadRecent'
import { useFileUpload } from '@/composables/useFileUpload'
import { baseName, dirName } from '@/utils/path'
import { formatFileSize } from '@/utils/fileType'
import { formatRelativeTime } from '@/utils/format'
import { isThumbableExt } from '@/utils/fileManager'
import { isImageFile, type FileEntry } from '@/utils/fileAttachmentUtils'

interface ReferencedFile {
  path: string
  count: number
}

const props = withDefaults(defineProps<{
  open: boolean
  currentFile?: string | null
  currentDir?: string | null
  attachedFiles?: FileEntry[]
  recentReferencedFiles?: ReferencedFile[]
}>(), {
  currentFile: null,
  currentDir: null,
  attachedFiles: () => [],
  recentReferencedFiles: () => [],
})

const emit = defineEmits<{
  close: []
  'add-attached': [path: string, isDir?: boolean]
  'remove-attached': [path: string]
  'file-open': [path: string]
}>()

const { t } = useI18n()
const dialog = useDialog()
const { recentShares, fetchRecentShares, deleteRecentShare } = useShareIn()
const { recentUploads, fetchRecentUploads, deleteRecentUpload } = useUploadRecent()

// ── Upload logic (now lives inside the drawer) ──
const { pendingFiles, attachedFiles, handleFileSelect, handleFileDrop } = useFileUpload()

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
    // Defer click to nextTick so BottomSheet's closeGuard watch
    // runs first and blocks all close attempts. Without this,
    // Android's native file chooser launch can trigger a close event
    // that closes the drawer before closeGuard takes effect.
    nextTick(() => { fileInputRef.value?.click() })
  }
}

async function onFileSelect(e: Event) {
  filePickerOpen.value = false
  await handleFileSelect(e)
  // Switch to uploads tab to show the upload progress
  activeTab.value = 'uploads'
}

// When uploads complete, remove finished items from pendingFiles
// (they'll appear in recentUploads after refresh) and fetch the list.
let wasUploading = false
let uploadRefreshScheduled = false
watch(uploadingFiles, (now) => {
  if (wasUploading && now.length === 0) {
    // Remove finished (non-uploading) entries from pendingFiles,
    // but preserve auto-attached ones (path also in attachedFiles)
    // so they survive until sendMessage processes them.
    const attachedPaths = new Set(attachedFiles.value.map(a => a.path))
    pendingFiles.value = pendingFiles.value.filter(f => f.uploading || attachedPaths.has(f.path))
    // Refresh recent uploads so completed files appear
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
  return props.attachedFiles?.some(f => f.path === path) ?? false
}

function toggleAttached(path: string, isDir: boolean = false) {
  if (isAttached(path)) {
    emit('remove-attached', path)
  } else {
    emit('add-attached', path, isDir)
  }
}

// Delete a recent share/upload. If it's currently attached, detach it first so
// the footer doesn't hold a reference to a removed file.
async function handleDeleteShare(item: { path: string; name?: string }) {
  const confirmed = await dialog.confirm(t('chat.attach.deleteShareConfirm', { name: item.name ?? baseName(item.path) }), {
    dangerous: true,
    confirmText: t('common.delete'),
  })
  if (!confirmed) return
  if (isAttached(item.path)) emit('remove-attached', item.path)
  await deleteRecentShare(item.path)
}

async function handleDeleteUpload(item: { path: string; name?: string }) {
  const confirmed = await dialog.confirm(t('chat.attach.deleteUploadConfirm', { name: item.name ?? baseName(item.path) }), {
    dangerous: true,
    confirmText: t('common.delete'),
  })
  if (!confirmed) return
  if (isAttached(item.path)) emit('remove-attached', item.path)
  await deleteRecentUpload(item.path)
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
// we need to reset filePickerOpen so BottomSheet's closeGuard lifts
// and the drawer becomes closable again.
// (onFileSelect also resets it, but the cancel path has no JS
// callback — only focus/visibility events fire.)
// We use multiple redundant events because no single event is
// reliable across all platforms:
// - window focus: works on desktop browsers when the picker is a
//   separate OS dialog that blurs the window
// - visibilitychange: works on Android WebView where the file
//   chooser is a separate activity that hides the document
// - input blur: works in some browsers when the file input loses
//   focus as the native picker dismisses
function onWindowFocus() {
  if (filePickerOpen.value) filePickerOpen.value = false
}
function onVisibilityChange() {
  if (filePickerOpen.value && document.visibilityState === 'visible') {
    filePickerOpen.value = false
  }
}
function onFileInputBlur() {
  // Defer slightly — on some platforms blur fires before the change
  // event, and we don't want to lift the guard prematurely if the
  // user actually selected files (onFileSelect will reset it).
  setTimeout(() => { filePickerOpen.value = false }, 150)
}
onMounted(() => {
  window.addEventListener('focus', onWindowFocus)
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onUnmounted(() => {
  window.removeEventListener('focus', onWindowFocus)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

// ── Drawer close handler ──
// BottomSheet emits 'close' → propagate to parent so ChatInputBar
// sets showAttachDrawer = false. No guard needed here — BottomSheet's
// closeGuard prop handles all close blocking while the native file
// picker is open.
function onDrawerClose() {
  emit('close')
}

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
  gap: 4px;
  padding: 0 8px;
  height: 28px;
  border-radius: 8px;
  border: none;
  background: var(--bg-hover);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
}
.ad-upload-label {
  line-height: 1;
}
.ad-upload-btn:active {
  background: var(--accent-color);
  color: #fff;
}

/* Tab bar */
.ad-tab-bar {
  display: flex;
  gap: 0;
  padding: 0 12px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border-color);
  -webkit-overflow-scrolling: touch;
  flex-shrink: 0;
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

/* Attachment check badge: sits on the bottom-right corner of the icon */
.ad-icon-wrap .ad-icon-check {
  position: absolute;
  right: -3px;
  bottom: -3px;
  width: 14px;
  height: 14px;
  padding: 2px;
  box-sizing: border-box;
  border-radius: 50%;
  background: var(--accent-color);
  color: #fff;
  box-shadow: 0 0 0 2px var(--bg-panel, #fff);
  pointer-events: none;
}

/* Delete button for recent shares/uploads */
.ad-file-delete {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: 0.5;
  cursor: pointer;
  transition: opacity 0.15s, color 0.15s;
}
.ad-file-row:hover .ad-file-delete,
.ad-file-row:active .ad-file-delete {
  opacity: 1;
  color: var(--danger-color, #dc3545);
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

/* Upload progress percent in icon slot */
.ad-uploading-icon {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  border-radius: 6px;
}
.ad-upload-pct {
  font-size: 10px;
  font-weight: 700;
  color: var(--accent-color);
  letter-spacing: -0.3px;
}
/* Override BottomSheet footer's flex-end alignment to left-align tags */
.bs-panel > .bs-footer:has(.chat-attachment-tags) {
  justify-content: flex-start;
}
</style>
