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
        <Camera :size="16" />
        <span class="ad-source-label">{{ t('chat.attach.takePhoto') }}</span>
      </button>
      <button type="button" class="ad-source-btn" @click.stop="handlePhotoClick">
        <ImageIcon :size="16" />
        <span class="ad-source-label">{{ t('chat.attach.pickImage') }}</span>
      </button>
      <button type="button" class="ad-source-btn" @click.stop="handleUploadClick">
        <Upload :size="16" />
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
            <Trash2 :size="14" />
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
            <Trash2 :size="14" />
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

    <!-- Footer: the DRAFT selection (not the composer's list — a pick is not
         applied until OK, except a finished upload, which is committed as it
         lands) above the shared .fbtn action pair. Always rendered:
         Cancel/OK are the sheet's only explicit exit, since the narrow header
         tap is not discoverable and the wide card has its own X. -->
    <template #footer>
      <div class="ad-footer">
        <!-- Selected-so-far. A bare strip of chips was not readable as a list:
             no count, no way to clear it, and no hint that it scrolls sideways.
             The head line answers "how many" and "get me out", the fades say
             "there is more this way" (and disappear once scrolled there). -->
        <div v-if="draft.length > 0" class="ad-selected">
          <div class="ad-selected-head">
            <span class="ad-selected-count" data-role="attach-selected-count">
              {{ t('chat.attach.selectedCount', { n: draft.length }) }}
            </span>
            <button type="button" class="ad-selected-clear" data-action="attach-clear-selection" @click="clearDraft">
              {{ t('chat.attach.clearAll') }}
            </button>
          </div>
          <div
            ref="selectedStripRef"
            class="ad-selected-strip"
            :class="{ 'more-right': selectedMoreRight }"
          >
            <AttachmentTags
              :files="draft"
              @file-click="emit('file-open', $event)"
              @remove="removeFromDraft"
            />
          </div>
        </div>
        <div class="ad-footer-actions">
          <button type="button" class="fbtn" data-action="attach-cancel" @click="handleCancel">{{ t('common.cancel') }}</button>
          <button type="button" class="fbtn fbtn-primary" data-action="attach-ok" @click="handleConfirm">{{ t('common.confirm') }}</button>
        </div>
      </div>
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
// The footer's Cancel/OK pair uses the shared .fbtn language; import it here
// rather than relying on BottomSheet having loaded it (design-guide rule).
import '@/assets/modal-footer-btn.css'

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
// `attachedFiles` from the composable is the GLOBAL chat draft. It must not
// shadow the `attachedFiles` PROP: the draft seeded on open and the "already
// selected" checks describe the list the CALLER is editing, which is not
// always the chat draft (the queued-message editor passes its own list).
// Shadowing them made a file picked from that editor vanish from the footer —
// the selection only ever reached the caller, never the shared draft.
const { pendingFiles, attachedFiles: chatAttachedFiles, handleFileSelect, handleFileDrop, readPickedCount, terminateChatUploadBatch, cancelChatUpload, chatUploadCancelled } = useFileUpload()

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
  // Whether anything was picked comes from the composable's readPickedCount,
  // NOT from an inline read of `e.target.files`: handleFileSelect clears
  // `input.value`, which empties the FileList, so the count must be taken
  // before the call. That ordering contract now lives with the code that does
  // the clearing (and is asserted by its own tests) instead of being a comment
  // on this side that a future refactor of the composable could silently break.
  //
  // The sheet now STAYS OPEN until OK (selection is local — see `draft`), so
  // the transfer needs somewhere to report progress: jump to the uploads tab
  // before awaiting, which is where the in-flight rows and the terminate
  // button live. A cancelled picker reports 0 and leaves the tab alone.
  if (readPickedCount(e) > 0) {
    activeTab.value = 'uploads'
    // A fresh picker batch: the previous batch's committed paths are no longer
    // this batch's to clear. Only on a real pick — re-opening the picker and
    // cancelling it starts no batch, and clearing the list there would leave
    // the still-running batch's finished files with nobody to clear them.
    batchCommitted = []
  }
  // A finished upload lands in THIS sheet's draft — never in the composer.
  //
  // The composer is only touched by OK (see `handleConfirm`). That is the
  // picker's promise: nothing you did not confirm ends up in the message. It is
  // also why the input bar hides in-flight cards while this sheet is open
  // (ChatInputBar's `visiblePendingFiles`) — otherwise the pending card unmounted
  // the moment `uploading` flipped false, the strip went empty, and the user read
  // it as "the thumbnail flashed and the file was lost" (#629).
  //
  // The commit is PER FILE (`onFileUploaded`), not once per batch: the footer
  // chips then grow as each transfer lands instead of everything appearing at
  // the end of a long batch.
  const commit = (path: string) => {
    // `isAttached` looks at the draft only; a path can also already sit in the
    // baseline (seeded from the composer on open) and must not be added twice.
    if (isAttached(path) || attachSnapshot.value.some(f => f.path === path)) return
    draft.value = [...draft.value, { path, isDir: false }]
    batchCommitted.push(path)
  }
  // Marks the window in which a batch THIS sheet started is still running —
  // the only window in which Cancel has a transfer of its own to abort (see
  // `handleCancel`).
  uploadsInFlight++
  try {
    const uploadedPaths = await handleFileSelect(e, commit)
    // Terminated mid-batch: the user asked to clear everything, so do not
    // select the files that had already finished. The terminate handler already
    // dropped them, and `chatUploadCancelled` is only reset when a new batch
    // starts.
    if (chatUploadCancelled.value) return
    if (uploadedPaths.length === 0) return
    activeTab.value = 'uploads'
  } finally {
    uploadsInFlight--
  }
}

/**
 * Terminate the whole in-flight upload batch.
 *
 * The transfer work lives in useFileUpload.terminateChatUploadBatch, shared
 * with the input bar's attachment strip — the same control, reachable from
 * whichever surface the user is looking at. Two copies of "cancel, then abort"
 * would drift on exactly that step.
 */
function handleTerminateUpload() {
  terminateChatUploadBatch()
  // "Terminate = clear everything" for THIS sheet's view. Nothing was handed to
  // the host (the draft only lands on OK), so there is nothing to tell it about
  // — the paths are ours alone to forget.
  if (batchCommitted.length === 0) return
  const gone = new Set(batchCommitted)
  batchCommitted = []
  draft.value = draft.value.filter(f => !gone.has(f.path))
}

/**
 * How many batches THIS sheet started are still running.
 *
 * Leaving the sheet must stop them — but `cancelChatUpload` works on the
 * module-level chat batch, which a drag-drop or paste can own just as well.
 * Aborting that one from here silently killed a transfer the user never asked
 * to stop, so the sheet only reaches for it while one of its own is in flight.
 *
 * A COUNT, not a flag: two picks can overlap (the sheet stays open and the
 * upload entry stays tappable), and the superseded one settles first — a flag
 * would read "nothing of mine is running" while the second batch still is.
 */
let uploadsInFlight = 0

/**
 * Leave the sheet without pressing OK: stop the transfer and drop the draft.
 *
 * This is the ONE meaning of leaving without OK, whatever the gesture got you
 * here: the Cancel button, a swipe, the back gesture, an overlay tap or the
 * header ×. Nothing reached the composer (the draft only lands on OK), so
 * "undo" is just: abort the transfers and let the draft be reseeded from the
 * composer next time the sheet opens.
 */
function handleCancel() {
  // Stop the batch first: `commit` fires per file as it lands, so aborting must
  // happen before the remaining transfers can reach it.
  if (uploadsInFlight > 0) cancelChatUpload()
  batchCommitted = []
  emit('close')
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
    const attachedPaths = new Set(chatAttachedFiles.value.map(a => a.path))
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

/**
 * The selection being edited. This sheet is a PICKER: tapping a row only
 * changes this draft, and the composer is touched on OK — uploads included.
 * That is the whole contract: nothing the user did not confirm reaches the
 * message, so leaving the sheet any other way simply drops the draft.
 *
 * Seeded from the caller's list when the sheet opens (see the open watcher);
 * the composer's own `attachedFiles` is read only to compute the OK delta.
 */
const draft = ref<FileEntry[]>([])

/**
 * Paths this batch uploaded into the draft, in upload order.
 *
 * "Terminate" has to drop them from the footer too, and the sheet cannot ask
 * the shared helper which ones: `terminateChatUploadBatch` reports only what it
 * detached from the GLOBAL composer list, which this sheet never touches. Left
 * unhandled, terminating a batch left the already-uploaded images sitting in
 * the draft as if they had survived.
 */
let batchCommitted: string[] = []

/**
 * The composer's list when the sheet opened — the baseline for OK's delta.
 * Uploaded files are deliberately NOT added to it: they are new, so OK must
 * emit `add-attached` for them. Nothing else advances it.
 */
const attachSnapshot = ref<FileEntry[]>([])

function isAttached(path: string) {
  return draft.value.some(f => f.path === path)
}

function toggleAttached(path: string, isDir: boolean = false) {
  if (isAttached(path)) {
    draft.value = draft.value.filter(f => f.path !== path)
  } else {
    draft.value = [...draft.value, { path, isDir }]
  }
}

/** Footer chip "×": drops one entry from the draft (never the composer). */
function removeFromDraft(entry: FileEntry) {
  draft.value = draft.value.filter(f => f.path !== entry.path)
}

/** Empty the selection in one tap (the footer's 清空). */
function clearDraft() {
  draft.value = []
  // Nothing of this batch is left in the draft, so "terminate" has nothing to
  // clear either.
  batchCommitted = []
}

// ── Footer selection strip: right-edge fade ──
// Same rule as the composer's action bar: the fade exists only while there is
// hidden content on that side. Left opaque on purpose — the strip always starts
// at scrollLeft 0, so a left fade would dim the first chip for no reason.
//
// The scrolling element is AttachmentTags' own root (it owns `overflow-x`), so
// the listener is attached there rather than on the wrapper — `scroll` does not
// bubble, and the wrapper never scrolls.
const selectedStripRef = ref<HTMLElement | null>(null)
const selectedMoreRight = ref(false)

function selectedScroller(): HTMLElement | null {
  return (selectedStripRef.value?.firstElementChild as HTMLElement | null) ?? null
}

function updateSelectedFade() {
  const el = selectedScroller()
  if (!el) {
    selectedMoreRight.value = false
    return
  }
  // 1px tolerance for sub-pixel rounding, mirroring the overflow test used by
  // the composer's strip.
  selectedMoreRight.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 1
}

let stopSelectedScroll: (() => void) | null = null

/** Re-bind whenever the strip mounts (it only exists while something is selected). */
function bindSelectedScroll() {
  stopSelectedScroll?.()
  stopSelectedScroll = null
  const el = selectedScroller()
  if (!el) return
  el.addEventListener('scroll', updateSelectedFade, { passive: true })
  stopSelectedScroll = () => el.removeEventListener('scroll', updateSelectedFade)
  updateSelectedFade()
}

watch(selectedStripRef, bindSelectedScroll)
// Content changes (a file lands, a chip is removed) move the overflow boundary
// without any scroll event.
watch(draft, () => nextTick(updateSelectedFade), { deep: true })

/**
 * Forget paths that were destroyed on disk.
 *
 * Unlike a toggle this is NOT undone by Cancel — the file is gone. Both the
 * draft and the baseline drop it, and the composer is told right away: leaving
 * a deleted file attached would make the next message reference a path that no
 * longer exists.
 */
function forgetPaths(paths: string[]) {
  const gone = new Set(paths)
  draft.value = draft.value.filter(e => !gone.has(e.path))
  attachSnapshot.value = attachSnapshot.value.filter(e => !gone.has(e.path))
}

function composerHas(path: string) {
  return (props.attachedFiles ?? []).some(f => f.path === path)
}

/**
 * OK: apply the draft to the composer as a delta, then close.
 *
 * A delta (not a full replacement) because the parent's handlers are
 * add/remove primitives — replaying the whole list would detach and re-attach
 * every untouched entry, which for line-range references and staged quotes is
 * not the same thing.
 */
function handleConfirm() {
  const before = new Set(attachSnapshot.value.map(e => e.path))
  const now = new Set(draft.value.map(e => e.path))
  for (const entry of draft.value) {
    if (!before.has(entry.path)) emit('add-attached', entry.path, entry.isDir === true)
  }
  for (const entry of attachSnapshot.value) {
    if (!now.has(entry.path)) emit('remove-attached', { path: entry.path, isDir: entry.isDir })
  }
  // The user has accepted this batch's uploads, so a later "terminate" (which
  // can fire for a batch started elsewhere while the sheet is reopened) must
  // not detach them again.
  batchCommitted = []
  emit('close')
}

// Delete a recent share/upload. If it's currently attached, detach it first so
// the footer doesn't hold a reference to a removed file.
async function handleDeleteShare(item: { path: string; name?: string }) {
  const confirmed = await dialog.confirm(t('chat.attach.deleteShareConfirm', { name: item.name ?? baseName(item.path) }), {
    dangerous: true,
    confirmText: t('common.delete'),
  })
  if (!confirmed) return
  forgetPaths([item.path])
  if (composerHas(item.path)) emit('remove-attached', { path: item.path })
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
  forgetPaths([item.path])
  if (composerHas(item.path)) emit('remove-attached', { path: item.path })
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

    // Detach every attached file that lives in this directory — in the draft
    // AND in the composer — not just the ones in the visible list: the sweep
    // removes the whole directory.
    const swept = [...new Set([
      ...draft.value.map(f => f.path),
      ...(props.attachedFiles ?? []).map(f => f.path),
    ])].filter(p => isInRecentDir(p, 'share-in'))
    for (const path of swept) if (composerHas(path)) emit('remove-attached', { path })
    forgetPaths(swept)
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

    const swept = [...new Set([
      ...draft.value.map(f => f.path),
      ...(props.attachedFiles ?? []).map(f => f.path),
    ])].filter(p => isInRecentDir(p, 'uploads'))
    for (const path of swept) if (composerHas(path)) emit('remove-attached', { path })
    forgetPaths(swept)
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
    // Seed the draft (what the footer and the row check marks show) and the
    // baseline OK diffs against. Copy the entries: toggling must never mutate
    // the caller's objects.
    attachSnapshot.value = (props.attachedFiles ?? []).map(e => ({ ...e }))
    draft.value = (props.attachedFiles ?? []).map(e => ({ ...e }))
    // A fresh session: the previous batch's paths are not this one's to clear.
    batchCommitted = []
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
}, { immediate: true })

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
  stopSelectedScroll?.()
})

// ── Drawer close handler ──
// BottomSheet emits 'close' → propagate to parent so ChatInputBar
// sets showAttachDrawer = false. No guard needed here — BottomSheet's
// closeGuard prop handles all close blocking while the native file
// picker is open.
function onDrawerClose() {
  // Swipe / back / overlay tap / header × — every way of leaving without OK is
  // a dismissal, and a dismissal is a cancel: same handler, so the two can
  // never drift on whether the uploads stay (issue #629 follow-up).
  handleCancel()
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
   ways to add a file that is not already in the project. Above the tab bar so
   it reads as the primary action rather than as header chrome.

   Compact single-line pills: icon and label sit side by side on a 34dp row
   with a 6dp radius, so the strip stays out of the file list's way on a phone
   while every entry keeps its icon (the icon is what makes them scannable at a
   glance). Equal thirds rather than content-sized: the labels are short and of
   similar length in both languages, so thirds give every entry the same target
   and keep the strip from reflowing when the locale changes. */
.ad-source-bar {
  display: flex;
  align-items: stretch;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.ad-source-btn {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--accent-color);
  cursor: pointer;
  font-size: var(--font-size-sm);
  transition: background var(--duration-fast), border-color var(--duration-fast);
}
.ad-source-btn:active {
  background: var(--bg-tertiary);
  border-color: var(--accent-color);
}
/* The icon must never be the thing that gives way when the label is long. */
.ad-source-btn svg {
  flex-shrink: 0;
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
  /* Row layout: the label is a flex item now, so it needs an explicit
     min-width to shrink (the default `auto` would push the icon out instead). */
  min-width: 0;
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
  /* 14dp is `.ad-file-row`'s inset. */
  --panel-skeleton-inset: calc(14 * var(--dp));
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
  position: relative;
  transition: background var(--duration-fast);
}
.ad-file-row:active {
  background: var(--bg-hover);
}
/* Selected row: the design guide's three-signal pattern — 2px accent bar +
   12% accent tint + a heavier name. Deliberately NOT the old "dim the whole
   row" treatment: lowering opacity reads as disabled / unavailable, which is
   the opposite of "you picked this". The bar is load-bearing (the tint alone
   measures ~1.09 contrast on light themes). */
.ad-file-attached {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}
.ad-file-attached::before {
  content: '';
  position: absolute;
  left: 0;
  top: calc(6 * var(--dp));
  bottom: calc(6 * var(--dp));
  width: calc(2 * var(--dp));
  background: var(--accent-color);
}
.ad-file-attached .ad-file-name {
  font-weight: var(--font-weight-semibold);
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

/* Attachment check badge: sits on the bottom-right corner of the icon.
   16dp rather than 14: at 14 the tick was unreadable on a phone, and it is the
   second signal that a row is selected (the accent bar is the first). */
.ad-icon-wrap .ad-icon-check {
  position: absolute;
  right: calc(-3 * var(--dp));
  bottom: calc(-3 * var(--dp));
  width: calc(16 * var(--dp));
  height: calc(16 * var(--dp));
  padding: calc(2 * var(--dp));
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
  letter-spacing: calc(0.3 * var(--dp));
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
  letter-spacing: calc(-0.3 * var(--dp));
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
/* Clear-all. Deliberately a borderless icon+label text button, matching the
   clear-all in the task detail header (`.clear-all-btn`) — a bordered pill
   reads as a primary action, and this one is a secondary destructive shortcut
   that shares its row with a muted hint. Red only on hover/active: red at rest
   would out-shout every file row in the list below it. */
.ad-clear-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: color var(--duration-base), background var(--duration-base);
}
.ad-clear-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: default;
}
.ad-clear-btn:active:not(:disabled) {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
}
@media (hover: hover) {
  .ad-clear-btn:hover:not(:disabled) {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
  }
}

/* Footer: selected tags above the Cancel/OK pair. The wrapper spans the whole
   footer width so the tags keep their left edge (BottomSheet's `.bs-footer` is
   flex-end) while the buttons stay right-aligned under them. */
.ad-footer {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  width: 100%;
  min-width: 0;
}
.ad-footer-actions {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  justify-content: flex-end;
  justify-content: safe flex-end;
  gap: var(--space-4);
  overflow-x: auto;
  overflow-y: hidden;
}

/* ── Selected-so-far list ──
   A bare strip of chips did not read as a list: no count, no way to clear it,
   and no hint that it scrolls sideways. */
.ad-selected {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}
.ad-selected-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 0 calc(14 * var(--dp));
}
.ad-selected-count {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
}
.ad-selected-clear {
  margin-left: auto;
  padding: 0;
  border: none;
  background: none;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  cursor: pointer;
  transition: color var(--duration-fast);
}
.ad-selected-clear:active {
  color: var(--accent-color);
}
@media (hover: hover) {
  .ad-selected-clear:hover {
    color: var(--accent-color);
  }
}
.ad-selected-strip {
  min-width: 0;
}
/* Edge fade while the strip still has hidden content on the right. A MASK, not
   an overlay of a background-coloured gradient: the sheet can sit over a
   wallpaper, so an opaque gradient would paint a visible band. The fade is
   dropped once the user reaches the end (see `selectedMoreRight`) — otherwise
   the last chip looks clipped rather than reachable. */
.ad-selected-strip.more-right {
  -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 20 * var(--dp)), transparent 100%);
  mask-image: linear-gradient(to right, #000 calc(100% - 20 * var(--dp)), transparent 100%);
}
</style>
