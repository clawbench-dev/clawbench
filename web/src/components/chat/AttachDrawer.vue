<template>
  <BottomSheet :open="open" :close-guard="filePickerOpen" auto @close="onDrawerClose">
    <template #header>
      <div class="ad-header">
        <Paperclip :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('chat.attach.drawerTitle') }}</span>
        <!-- Desktop only: the original header button. Kept for browsers and
             Electron, where the photo/camera entries have nothing to offer. -->
        <button
          v-if="!isMobileLayout"
          type="button"
          class="ad-header-upload-btn"
          :title="t('chat.attach.uploadFile')"
          @click.stop="handleUploadClick"
        >
          <Upload :size="16" />
          <span class="ad-upload-label">{{ t('chat.attach.uploadFile') }}</span>
        </button>
      </div>
    </template>

    <!-- Source actions — MOBILE ONLY. The header is a single 36px line shared
         with the title, so on a phone the three entries would wrap or shrink
         below a usable touch target; a full-width strip also fixes the
         discoverability problem that made mobile users report "there is no way
         to upload here". On desktop (browser or Electron) the original header
         button is used instead: there is no camera or photo library to reach
         from a mouse, and the strip only took vertical space away from the
         file list. Each button gets its own hidden input so the system picker
         opens in the right mode — file picker, photo library, or camera. -->
    <div v-if="isMobileLayout" class="ad-source-bar">
      <button type="button" class="ad-source-btn" @click.stop="handleCameraClick">
        <Camera :size="18" />
        <span class="ad-source-label">{{ t('chat.attach.takePhoto') }}</span>
      </button>
      <button type="button" class="ad-source-btn" @click.stop="handlePhotoClick">
        <ImageIcon :size="18" />
        <span class="ad-source-label">{{ t('chat.attach.pickImage') }}</span>
      </button>
      <button type="button" class="ad-source-btn" @click.stop="handleUploadClick">
        <Upload :size="18" />
        <span class="ad-source-label ad-upload-label">{{ t('chat.attach.uploadFile') }}</span>
      </button>
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
          @click="toggleAttached(item.path, item.isDir === true)"
        >
          <div class="ad-icon-wrap">
            <img v-if="!item.isDir && isImageFile(item.path) && isThumbableExt(item.path) && !thumbErrors.has(item.path)"
              class="ad-thumb" :src="thumbUrl(item.path)" loading="lazy" @error="onThumbError(item.path)" />
            <FileIcon v-else :path="item.path" :is-dir="item.isDir === true" :size="28" class="ad-file-icon" />
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
        <!-- Clear-all bar. Above the rows and only when there is something to
             clear: an always-visible destructive action on an empty list is a
             trap. The list is capped at 20, so this clears the whole directory
             rather than the visible rows. -->
        <div v-if="recentShares?.length" class="ad-clear-bar">
          <span class="ad-clear-hint">{{ t('chat.attach.clearSharesHint', { n: recentShares.length }) }}</span>
          <button class="ad-clear-btn" :disabled="clearing === 'shares'" @click.stop="handleClearShares">
            <Trash2 :size="12" />
            <span>{{ t('chat.attach.clearAll') }}</span>
          </button>
        </div>
        <PanelSkeleton v-if="loading" variant="list" :rows="6" :meta="false" :label="t('common.loading')" />
        <div v-else-if="!recentShares?.length" class="ad-empty">{{ t('chat.attach.emptyShares') }}</div>
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
        <!-- Batch terminate bar: one button for the whole in-flight batch. The
             upload is sequential, so this sits above the rows rather than on
             each one. Terminating clears the batch entirely (including files
             that already finished), per the product decision. -->
        <div v-if="uploadingFiles.length > 0" class="ad-upload-banner">
          <LoadingIndicator size="sm" inline />
          <span class="ad-upload-banner-text">
            {{ t('chat.attach.uploadingBatch', { n: uploadingFiles.length }) }}
          </span>
          <button class="ad-terminate-btn" @click="handleTerminateUpload">
            <Square :size="12" />
            <span>{{ t('chat.attach.terminateUpload') }}</span>
          </button>
        </div>
        <!-- Pending uploads: only the in-flight ones (failed items are removed by
             useFileUpload). Loop over `uploadingFiles` rather than `pendingFiles`
             with v-show, so finished-but-retained entries leave no hidden rows. The
             loading/empty fallbacks below key off the same visible count, otherwise
             a retained non-uploading entry hides every row while also suppressing the
             empty state → the content area collapses. -->
        <button
          v-for="(f, idx) in uploadingFiles" :key="'pending-' + idx"
          class="ad-file-row"
        >
          <div class="ad-icon-wrap ad-uploading-icon">
            <span class="ad-upload-pct">{{ f.progress }}%</span>
          </div>
          <div class="ad-file-info">
            <span class="ad-file-name">{{ getFileName(f.path) || t('chat.attach.uploading') }}</span>
            <span class="ad-file-meta">{{ f.path ? dirName(f.path) + ' · ' : '' + formatFileSize(f.size) }}</span>
          </div>
        </button>
        <!-- Clear-all bar. Sits above the completed rows (the in-flight batch has
             its own terminate action) and clears the whole directory. -->
        <div v-if="recentUploads?.length" class="ad-clear-bar">
          <span class="ad-clear-hint">{{ t('chat.attach.clearUploadsHint', { n: recentUploads.length }) }}</span>
          <button class="ad-clear-btn" :disabled="clearing === 'uploads'" @click.stop="handleClearUploads">
            <Trash2 :size="12" />
            <span>{{ t('chat.attach.clearAll') }}</span>
          </button>
        </div>
        <!-- Completed uploads from server -->
        <PanelSkeleton v-if="loading && !recentUploads?.length && uploadingFiles.length === 0" variant="list" :rows="6" :meta="false" :label="t('common.loading')" />
        <div v-else-if="!recentUploads?.length && uploadingFiles.length === 0" class="ad-empty">{{ t('chat.attach.emptyUploads') }}</div>
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
            <span class="ad-file-meta">{{ uploadDirLabel(item.path) }} · {{ formatRelativeTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <ExternalLink :size="14" class="ad-file-open" @click.stop="emit('file-open', item.path)" />
          <Trash2 :size="14" class="ad-file-delete" :class="{ 'ad-file-delete-forget': item.origin === 'project' }"
            @click.stop="handleDeleteUpload(item)"
            :title="t(item.origin === 'project' ? 'chat.attach.forgetUpload' : 'chat.attach.deleteRecent')" />
        </button>
      </template>
    </div>

    <!-- Selected files footer (reuses AttachmentTags component) -->
    <template v-if="attachedFiles.length > 0" #footer>
      <AttachmentTags :files="attachedFiles" @file-click="emit('file-open', $event)" @remove="emit('remove-attached', $event)" />
    </template>

    <!-- Hidden file inputs (owned by drawer).
         Three separate inputs, not one with a mutated `accept`, because
         `accept`/`capture` are read by the OS when the picker opens and some
         WebViews (notably Android's) snapshot the attributes of the element
         they were bound to — mutating them right before click() is unreliable.

         `capture="environment"` makes the camera open directly instead of a
         picker with a "Camera" entry. It is an advisory hint: on a desktop
         browser (no rear camera) it degrades to a normal image picker, so the
         photo entry stays usable there. -->
    <input type="file" ref="fileInputRef" @change="onFileSelect" @blur="onFileInputBlur" style="display:none" multiple />
    <input type="file" ref="photoInputRef" accept="image/*" @change="onFileSelect" @blur="onFileInputBlur" style="display:none" multiple />
    <input type="file" ref="cameraInputRef" accept="image/*" capture="environment" @change="onFileSelect" @blur="onFileInputBlur" style="display:none" />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { Paperclip, Upload, Check, ExternalLink, Trash2, Square, Camera, Image as ImageIcon } from 'lucide-vue-next'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import FileIcon from '@/components/common/FileIcon.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AttachmentTags from '@/components/chat/AttachmentTags.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { useI18n } from 'vue-i18n'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { usePlatformDetect, isMobileOSUA } from '@/composables/usePlatformDetect'
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
  /** Whether the referenced entry is a directory (drives the folder icon and
   *  the isDir flag sent with the attachment). */
  isDir?: boolean
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
  'remove-attached': [entry: FileEntry]
  'file-open': [entry: FileEntry | string]
}>()

const { t } = useI18n()
const dialog = useDialog()
const toast = useToast()
// Which attachment entries to show. "Mobile" here means the phone/tablet
// platforms — Android, iOS, and iPadOS in desktop mode — not the INPUT axis:
// a touchscreen laptop reports a coarse pointer in tablet mode but is still a
// PC, and its file picker has no camera or photo library behind it. The host
// axis is not the test either: Electron on a touch laptop stays desktop, and
// a browser on a phone is still mobile.
const { isAndroidApp } = usePlatformDetect()
// computed, not `isMobileOSUA || isAndroidApp`: the latter returns the ref
// object when the UA test is false, and a ref is always truthy — every desktop
// browser would have been shown the mobile strip.
const isMobileLayout = computed(() => isMobileOSUA || isAndroidApp.value)
const { recentShares, fetchRecentShares, deleteRecentShare, clearRecentShares } = useShareIn()
const { recentUploads, fetchRecentUploads, deleteRecentUpload, clearRecentUploads } = useUploadRecent()

// ── Upload logic (now lives inside the drawer) ──
const { pendingFiles, attachedFiles, handleFileSelect, handleFileDrop, cancelChatUpload, chatUploadCancelled } = useFileUpload()

const activeTab = ref('current')
const fileInputRef = ref<HTMLInputElement | null>(null)
// Image-only and camera inputs. Kept separate from fileInputRef so each entry
// opens the system picker in the mode its button promises (see the template).
const photoInputRef = ref<HTMLInputElement | null>(null)
const cameraInputRef = ref<HTMLInputElement | null>(null)
const filePickerOpen = ref(false)
const loading = ref(false)

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

/**
 * Directory label for a "最近上传" row.
 *
 * Uploads-directory rows always have a parent, but a project file uploaded to
 * the project ROOT has none — `dirName` returns "" there and the meta line
 * would start with a stray separator. Show the project root as "/" instead,
 * matching how the Current tab labels it.
 */
function uploadDirLabel(path: string) {
  return dirName(path) || '/'
}

/**
 * Open the system picker for one of the three hidden inputs.
 *
 * Shared by all entries so they cannot drift on the two things that actually
 * matter: clearing `value` (without it, re-picking the same file fires no
 * `change` event) and setting `filePickerOpen` BEFORE the click, which is what
 * installs the drawer's closeGuard while the OS picker owns the screen.
 *
 * The click is deferred to nextTick so BottomSheet's closeGuard watch runs
 * first and blocks all close attempts. Without this, Android's native file
 * chooser launch can trigger a close event that closes the drawer before
 * closeGuard takes effect.
 */
function openPicker(input: HTMLInputElement | null) {
  if (!input) return
  input.value = ''
  filePickerOpen.value = true
  nextTick(() => { input.click() })
}

function handleUploadClick() {
  openPicker(fileInputRef.value)
}

/** Photo library entry (accept="image/*"). */
function handlePhotoClick() {
  openPicker(photoInputRef.value)
}

/** Camera entry (capture="environment"). */
function handleCameraClick() {
  openPicker(cameraInputRef.value)
}

async function onFileSelect(e: Event) {
  filePickerOpen.value = false
  const uploadedPaths = await handleFileSelect(e)
  // Terminated mid-batch: the user asked to clear everything, so do not attach
  // the files that had already finished. The terminate handler already dropped
  // them, and `chatUploadCancelled` is only reset when a new batch starts.
  if (chatUploadCancelled.value) return
  // Switch to the uploads tab and select the freshly uploaded files so the
  // user immediately sees (and has attached) what they just uploaded. Only do
  // this on a successful upload — switching for a cancelled/empty picker would
  // yank the user away from their current tab for nothing.
  if (uploadedPaths.length === 0) return
  activeTab.value = 'uploads'
  for (const path of uploadedPaths) {
    if (!isAttached(path)) emit('add-attached', path, false)
  }
}

/**
 * Terminate the whole in-flight upload batch.
 *
 * Clears everything, per the product decision: the queued files stop, the
 * transfer in flight is aborted, and any file that had already finished is
 * detached again (so the input returns to its pre-upload state).
 */
function handleTerminateUpload() {
  const finished = cancelChatUpload()
  for (const path of finished) {
    if (isAttached(path)) emit('remove-attached', { path })
  }
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
    emit('remove-attached', { path, isDir })
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
  if (isAttached(item.path)) emit('remove-attached', { path: item.path })
  await deleteRecentShare(item.path)
}

/**
 * Remove a row from "最近上传".
 *
 * What this does depends on where the row came from, and the wording has to say
 * so: a chat attachment is a file the app owns (removing it deletes the file),
 * while a project file is only recorded here so it can be re-attached quickly
 * (removing the row leaves the file in the tree). Telling the user "delete the
 * file" for the latter would make them expect their source tree to change.
 */
async function handleDeleteUpload(item: { path: string; name?: string; origin?: 'uploads' | 'project' }) {
  const name = item.name ?? baseName(item.path)
  const isProjectFile = item.origin === 'project'
  const confirmed = await dialog.confirm(
    t(isProjectFile ? 'chat.attach.forgetUploadConfirm' : 'chat.attach.deleteUploadConfirm', { name }),
    { dangerous: !isProjectFile, confirmText: t(isProjectFile ? 'chat.attach.forgetUpload' : 'common.delete') },
  )
  if (!confirmed) return
  if (isAttached(item.path)) emit('remove-attached', { path: item.path })
  await deleteRecentUpload(item.path)
}

// Which tab's clear-all is in flight. A single ref rather than two booleans:
// only one tab is visible at a time, so this doubles as the button's disabled
// state and prevents a double-tap from firing two sweeps.
const clearing = ref<'shares' | 'uploads' | null>(null)

/**
 * Clear the whole recent-shares directory.
 *
 * Confirms first — this deletes every file in .clawbench/share-in/, including
 * any the user did not scroll to. Any share currently attached to the composer
 * is detached, otherwise the footer would keep pointing at a deleted file.
 */
async function handleClearShares() {
  if (clearing.value) return
  // Claimed BEFORE the confirm await: a double-tap would otherwise pass the
  // guard twice, because `clearing` is only observed after the dialog resolves.
  clearing.value = 'shares'
  try {
    const confirmed = await dialog.confirm(t('chat.attach.clearSharesConfirm'), {
      dangerous: true,
      confirmText: t('common.delete'),
    })
    if (!confirmed) return

    // Detach every attached file that lives in this directory, not just the
    // ones in the visible list: the sweep removes the whole directory.
    for (const f of [...(props.attachedFiles ?? [])]) {
      if (isInRecentDir(f.path, 'share-in')) emit('remove-attached', { path: f.path })
    }
    const result = await clearRecentShares()
    reportClearResult(result, 'chat.attach.clearSharesDone', 'chat.attach.clearSharesFailed')
  } finally {
    clearing.value = null
  }
}

/** Clear the whole recent-uploads directory. See handleClearShares. */
async function handleClearUploads() {
  if (clearing.value) return
  clearing.value = 'uploads' // see handleClearShares — claimed before the await
  try {
    const confirmed = await dialog.confirm(t('chat.attach.clearUploadsConfirm'), {
      dangerous: true,
      confirmText: t('common.delete'),
    })
    if (!confirmed) return

    for (const f of [...(props.attachedFiles ?? [])]) {
      if (isInRecentDir(f.path, 'uploads')) emit('remove-attached', { path: f.path })
    }
    const result = await clearRecentUploads()
    reportClearResult(result, 'chat.attach.clearUploadsDone', 'chat.attach.clearUploadsFailed')
  } finally {
    clearing.value = null
  }
}

/** Whether a project-relative path lives directly in .clawbench/<subDir>/. */
function isInRecentDir(path: string, subDir: string): boolean {
  const normalized = path.replace(/\\/g, '/')
  const prefix = `.clawbench/${subDir}/`
  if (!normalized.startsWith(prefix)) return false
  // Only flat entries: the sweep skips subdirectories, so detaching a nested
  // file would remove an attachment the sweep left intact.
  return !normalized.slice(prefix.length).includes('/')
}

/**
 * Report the outcome of a clear. A partial sweep must not read as success —
 * the user would assume the tab is empty when files are still on disk.
 */
function reportClearResult(
  result: { deleted: number; failed: number } | null,
  doneKey: string,
  failedKey: string,
) {
  if (!result) {
    toast.show(t(failedKey), { icon: 'warning', type: 'error' })
    return
  }
  if (result.failed > 0) {
    toast.show(t('chat.attach.clearPartial', { deleted: result.deleted, failed: result.failed }),
      { icon: 'warning', type: 'error', duration: 3000 })
    return
  }
  toast.show(t(doneKey, { n: result.deleted }), { icon: 'delete', type: 'success', duration: 2000 })
}

// Effective current dir: always show something (even at project root)
const effectiveCurrentDir = computed(() => props.currentDir || '.')
const currentDirDisplayName = computed(() => {
  const dir = effectiveCurrentDir.value
  return dir === '.' ? '/' : baseName(dir)
})

// Fetch data when drawer opens
watch(() => props.open, async (v) => {
  if (v) {
    loading.value = true
    try {
      await Promise.all([fetchRecentShares(), fetchRecentUploads()])
    } finally {
      loading.value = false
    }
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
  gap: var(--space-4);
  width: 100%;
}

/* Desktop header upload button — the pre-mobile-entry design, restored for
   browsers and Electron. Text + icon, accent coloured, pushed to the right of
   the title. */
.ad-header-upload-btn {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-2);
  height: calc(28 * var(--dp));
  border: none;
  background: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: var(--font-size-sm);
  white-space: nowrap;
  transition: opacity var(--duration-fast);
}
.ad-header-upload-btn:active {
  opacity: var(--opacity-muted);
}
.ad-upload-label {
  line-height: 1;
}

/* Source actions strip — MOBILE ONLY (see the template comment): the three
   ways to add a file that is not already in the project. Full width and above
   the tab bar so it reads as the primary action rather than as header chrome.

   Equal thirds rather than content-sized: the labels are short and of similar
   length in both languages, so thirds give every entry the same touch target
   and keep the strip from reflowing when the locale changes. 44px is the
   minimum comfortable touch height; the row is the drawer's first interactive
   element, so it must not be the smallest. */
.ad-source-bar {
  display: flex;
  align-items: stretch;
  gap: var(--space-2);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.ad-source-btn {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-4) var(--space-2);
  min-height: calc(44 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  color: var(--accent-color);
  cursor: pointer;
  font-size: var(--font-size-xs);
  transition: background var(--duration-fast), border-color var(--duration-fast);
}
.ad-source-btn:active {
  background: var(--bg-tertiary);
  border-color: var(--accent-color);
}
@media (hover: hover) {
  .ad-source-btn:hover {
    background: var(--bg-tertiary);
    border-color: var(--accent-color);
  }
}
.ad-source-label {
  line-height: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

/* Tab bar */
.ad-tab-bar {
  display: flex;
  gap: 0;
  padding: 0 var(--space-6);
  overflow-x: auto;
  border-bottom: 1px solid var(--border-color);
  -webkit-overflow-scrolling: touch;
  flex-shrink: 0;
}
.ad-tab {
  padding: var(--space-4) calc(14 * var(--dp));
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  border: none;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  white-space: nowrap;
  border-bottom: 2px solid transparent;
  transition: color var(--duration-base), border-color var(--duration-base);
}
.ad-tab-active {
  color: var(--accent-color);
  border-bottom-color: var(--accent-color);
}

/* Content area */
.ad-content {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-2) 0;
}
.ad-empty {
  padding: calc(24 * var(--dp)) var(--space-7);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-md);
}

/* File row */
.ad-file-row {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-5) calc(14 * var(--dp));
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  transition: background var(--duration-fast);
}
.ad-file-row:active {
  background: var(--bg-hover);
}
.ad-file-attached {
  opacity: var(--opacity-disabled);
}

/* Icon container: holds icon or thumbnail.
 * 28x28 matches FileManagerContent list-view icon size.
 * Thumbnails fill the container; icons stay at their :size prop.
 *
 * No `overflow: hidden` here: the selected-state check badge is anchored to
 * the bottom-right corner OUTSIDE the 28px box (right/bottom: -3px), so
 * clipping the container truncated the badge and its white ring along the
 * image edge. The thumbnail rounds itself via its own border-radius, so the
 * wrapper does not need to clip. */
.ad-icon-wrap {
  flex-shrink: 0;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  position: relative;
}
.ad-icon-wrap .ad-thumb {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: var(--radius-sm);
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
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ad-file-meta {
  display: block;
  font-size: var(--font-size-xs);
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
  opacity: var(--opacity-muted);
  transition: opacity var(--duration-base), color var(--duration-base);
}
.ad-file-row:active .ad-file-open {
  opacity: 1;
  color: var(--accent-color);
}
@media (hover: hover) {
  .ad-file-row:hover .ad-file-open {
    opacity: 1;
    color: var(--accent-color);
  }
}

/* Attachment check badge: sits on the bottom-right corner of the icon */
.ad-icon-wrap .ad-icon-check {
  position: absolute;
  right: calc(-3 * var(--dp));
  bottom: calc(-3 * var(--dp));
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  padding: var(--space-1);
  box-sizing: border-box;
  border-radius: 50%;
  background: var(--accent-color);
  color: #fff;
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--bg-primary);
  pointer-events: none;
}

/* Delete button for recent shares/uploads */
.ad-file-delete {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: var(--opacity-muted);
  cursor: pointer;
  transition: opacity var(--duration-base), color var(--duration-base);
}
.ad-file-row:active .ad-file-delete {
  opacity: 1;
  color: var(--color-red);
}
@media (hover: hover) {
  .ad-file-row:hover .ad-file-delete {
    opacity: 1;
    color: var(--color-red);
  }
}

/* A project file's row only un-lists the file, so its action must not look
   destructive. Red is the strongest signal in this row and would contradict the
   "the file itself stays" confirm text — the colour is part of the promise, not
   decoration. Placed after the red rules so the equal-specificity cascade
   resolves in its favour. */
.ad-file-row:active .ad-file-delete-forget,
.ad-file-row:hover .ad-file-delete-forget {
  color: var(--text-secondary);
}

/* Current item label */
.ad-label {
  display: inline-block;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
  margin-right: var(--space-3);
  vertical-align: middle;
  letter-spacing: 0.3px;
}
.ad-current-item {
  background: var(--bg-secondary);
}

/* Upload progress percent in icon slot */
.ad-uploading-icon {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  border-radius: var(--radius-sm);
}
.ad-upload-pct {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
  color: var(--accent-color);
  letter-spacing: -0.3px;
}

/* Batch terminate bar above the in-flight upload rows */
.ad-upload-banner {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) calc(14 * var(--dp));
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}
.ad-upload-banner-text {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ad-terminate-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(24 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: none;
  color: var(--color-red);
  font-size: var(--font-size-xs);
  cursor: pointer;
  transition: background var(--duration-fast), border-color var(--duration-fast);
}
.ad-terminate-btn:active {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
}
@media (hover: hover) {
  .ad-terminate-btn:hover {
    background: color-mix(in srgb, var(--color-red) 12%, transparent);
    border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
  }
}

/* ── Clear-all bar ──
   Same shape as .ad-upload-banner (a full-width strip above the rows) so the
   two destructive affordances in this drawer look like the same kind of thing.
   The hint is muted and ellipsised: on a narrow phone the count is the part
   that matters, the label is not. */
.ad-clear-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) calc(14 * var(--dp));
  border-bottom: 1px solid var(--border-color);
}
.ad-clear-hint {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ad-clear-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(24 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: none;
  color: var(--color-red);
  font-size: var(--font-size-xs);
  cursor: pointer;
  transition: background var(--duration-fast), border-color var(--duration-fast);
}
.ad-clear-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.ad-clear-btn:active:not(:disabled) {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
}
@media (hover: hover) {
  .ad-clear-btn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--color-red) 12%, transparent);
    border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
  }
}

/* Override BottomSheet footer's flex-end alignment to left-align tags */
.bs-panel > .bs-footer:has(.chat-attachment-tags) {
  justify-content: flex-start;
}
</style>
