<template>
  <div class="file-header-bar">
    <FileText :size="14" class="panel-header-icon" />
    <!-- Navigation cluster at the top-left: view back/forward plus in-file jump
         history. Same control set on every layout, touch included — there is no
         separate floating bar for narrow screens. -->
    <div v-if="canNavigateBack || canGoBackFile || canGoForwardFile" class="file-header-nav">
      <button
        v-if="canNavigateBack || canGoBackFile"
        class="file-header-btn file-header-back-btn"
        type="button"
        :title="backLabel || t('file.overlay.back')"
        :aria-label="backLabel || t('file.overlay.back')"
        @click.stop="$emit('navigateBack')"
      >
        <ArrowLeft :size="14" />
      </button>
      <button
        v-if="canGoForwardFile"
        class="file-header-btn"
        type="button"
        :title="t('file.overlay.forward')"
        :aria-label="t('file.overlay.forward')"
        @click.stop="$emit('navigateForward')"
      >
        <ArrowRight :size="14" />
      </button>
    </div>

    <!-- Region 1: File name -->
    <div class="file-name-wrap">
      <span class="file-path-hint" :class="{ 'file-path-draggable': isWideScreen && !isUntitled }" :draggable="isWideScreen && !isUntitled" @click="$emit('showDetails')" @dragstart="handleFileNameDragStart" @dragend="handleFileNameDragEnd" :title="displayName">{{ displayName }}</span>
      <!-- The viewer can open any path a chat annotation names, including files
           outside the project; without this the header looks identical to a
           project file and the user has no cue where the file came from. -->
      <ExternalBadge v-if="isExternalFile" kind="file" class="file-header-external" />
    </div>

    <!-- Region 2: Toolbar (ResizeObserver target) -->
    <ActionBar ref="actionsRef" class="header-actions" mode="menu" align="end" size="header" always-more>
      <!-- Refresh button — first in the toolbar, and the last to collapse. -->
      <RefreshButton v-if="hasTextContent && !isUntitled" data-toolbar-id="refresh" icon="RotateCw" class="file-header-btn" :loading="refreshing" :disabled="refreshing" :title="t('nav.refresh')" @click.stop="handleRefresh">
        <span class="action-label">{{ t('nav.refresh') }}</span>
      </RefreshButton>

      <!-- Collapse / restore the chat pane used to live here. It is now the
           panel title bar's maximize button, which does the same thing for
           every tab instead of only the file viewer. -->

      <!-- TOC button (only for file types that support TOC) -->
      <button v-if="hasToc" data-toolbar-id="toc" class="file-header-btn" :class="{ active: tocOpen }" @click.stop="handleToggleToc" :title="t('file.header.toc')">
        <List :size="14" />
        <span class="action-label">{{ t('file.header.toc') }}</span>
      </button>

      <!-- Search button (only for file types that support search) -->
      <button v-if="hasSearch && !visualEditing" data-toolbar-id="search" class="file-header-btn" :class="{ active: searchOpen }" @click.stop="handleToggleSearch" :title="t('file.header.search')">
        <Search :size="14" />
        <span class="action-label">{{ t('file.header.search') }}</span>
      </button>

      <!-- Fit-width / reset zoom button (for PDF) -->
      <button v-if="hasFitWidth" data-toolbar-id="fitWidth" class="file-header-btn" @click.stop="handleFitWidth" :title="t('file.header.fitWidth')">
        <MoveHorizontal :size="14" />
        <span class="action-label">{{ t('file.header.fitWidth') }}</span>
      </button>

      <!-- Quote in chat: opens the shared quote composer with this file attached,
           matching the issue/PR detail header. Not a toggle — the composer is the
           single entry point, and an attached file is removed from its chip in
           the chat input. -->
      <button v-if="!isUntitled" ref="attachBtnRef" data-toolbar-id="attach" class="file-header-btn" @click.stop="handleQuoteInChat" :title="t('file.header.quoteInChat')" :aria-label="t('file.header.quoteInChat')">
        <MessageSquare :size="14" />
        <span class="action-label">{{ t('file.header.quoteInChat') }}</span>
      </button>

      <!-- Send by email: stages this file (or packs it, for a directory) and
           opens the mail panel's composer with it attached. Inline rather than
           in the More menu — it is an action on the open file, the same class
           of thing as 引用对话 next to it; ActionBar folds it away on its own
           when the header runs out of room. -->
      <button v-if="!isUntitled && !editing && mailEnabled" data-toolbar-id="sendByMail" class="file-header-btn" @click.stop="handleSendByMail" :title="t('file.header.sendByMail')" :aria-label="t('file.header.sendByMail')">
        <Mail :size="14" />
        <span class="action-label">{{ t('file.header.sendByMail') }}</span>
      </button>

      <!-- Lightbox view button (image / svg files only): opens the image full-size
           in the shared Lightbox (zoom + sibling navigation). Attach lives on the
           same header — no need for a per-image header. -->
      <button v-if="isImageFile" data-toolbar-id="viewImage" class="file-header-btn" @click.stop="handleViewImage" :title="t('imageBlock.view')">
        <Maximize2 :size="14" />
        <span class="action-label">{{ t('imageBlock.view') }}</span>
      </button>

      <!-- Toggle view button (source/rendered). Always an eye icon: highlighted
           when the rendered preview is shown, dimmed for the source view. It is
           disabled while editing so the edit button stays the sole relevant control. -->
      <button v-if="canToggleView" data-toolbar-id="toggleView" class="file-header-btn" :class="{ active: effectiveViewMode === 'rendered' }" :disabled="editing" @click.stop="handleToggleView" :title="effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView')">
        <Eye :size="14" />
        <span class="action-label">{{ effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView') }}</span>
      </button>

      <!-- Edit toggle button — always directly beside the preview toggle -->
      <button v-if="isEditable" data-toolbar-id="edit" class="file-header-btn" :class="{ active: editing }" @click.stop="handleToggleEdit" :title="editing ? t('file.header.finishEditing') : t('file.header.edit')">
        <Pencil :size="14" />
        <span class="action-label">{{ editing ? t('file.header.finishEditing') : t('file.header.edit') }}</span>
      </button>

      <!-- Visual (WYSIWYG) markdown editing — the second editing mode. Only
           offered while not editing: once inside, the existing edit toggle owns
           the exit flow, and switching editors mid-session would have to carry
           unsaved text between two different document models. -->
      <button v-if="canWysiwygEdit" data-toolbar-id="wysiwygEdit" class="file-header-btn" @click.stop="handleWysiwygEdit" :title="t('file.header.wysiwygEdit')">
        <PenLine :size="14" />
        <span class="action-label">{{ t('file.header.wysiwygEdit') }}</span>
      </button>

      <!-- Word wrap / line numbers / sticky scroll moved to the More menu
           (permanentMenuIds): they are editor preferences, set once and rarely
           touched, and all three are also available in Settings → File display. -->

      <!-- Download button -->
      <button v-if="!isUntitled" data-toolbar-id="download" class="file-header-btn" @click.stop="handleDownload" :title="t('common.download')">
        <Download :size="14" />
        <span class="action-label">{{ t('common.download') }}</span>
      </button>

      <!-- Low-frequency actions (details, delete, editor preferences) live in
           the same "more" menu ActionBar builds for the buttons that do not
           fit, so there is exactly one overflow affordance. -->
      <template #menu-extra>
        <button v-if="permanentMenuIds.includes('details')" class="ab-menu-item" @click="$emit('showDetails')">
          <Info :size="14" />
          {{ t('file.header.details') }}
        </button>
        <button v-if="permanentMenuIds.includes('openDirectory')" class="ab-menu-item" @click="handleOpenDirectory">
          <FolderOpen :size="14" />
          {{ t('file.header.openDirectory') }}
        </button>
        <button v-if="permanentMenuIds.includes('gitHistory')" class="ab-menu-item" @click="handleGitHistory">
          <GitBranch :size="14" />
          {{ t('file.header.fileHistory') }}
        </button>
        <button v-if="permanentMenuIds.includes('shareLink') && shareAllowed" class="ab-menu-item" :class="{ active: isShared }" @click="$emit('shareLink')">
          <ScreenShare :size="14" />
          {{ t('file.header.shareLink') }}
          <span v-if="isShared" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('openAsText')" class="ab-menu-item" @click="handleOpenAsText">
          <Code2 :size="14" />
          {{ t('file.header.openAsText') }}
        </button>
        <button v-if="permanentMenuIds.includes('exportHtml')" class="ab-menu-item" @click="handleExportHtml">
          <FileOutput :size="14" />
          {{ t('file.header.exportHtml') }}
        </button>
        <button v-if="permanentMenuIds.includes('exportDrawio')" class="ab-menu-item" @click="$emit('exportDrawio')">
          <FileOutput :size="14" />
          {{ t('file.drawioExport.menuItem') }}
        </button>
        <button v-if="permanentMenuIds.includes('setAsBackground')" class="ab-menu-item" @click="handleSetAsBackground">
          <Image :size="14" />
          {{ t('file.header.setAsBackground') }}
        </button>
        <button v-if="permanentMenuIds.includes('wordWrap')" class="ab-menu-item" @click="handleToggleWordWrap">
          <TextWrap :size="14" />
          {{ t('file.header.wordWrap') }}
          <span v-if="wordWrap" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('lineNumbers')" class="ab-menu-item" @click="handleToggleLineNumbers">
          <Hash :size="14" />
          {{ t('file.header.lineNumbers') }}
          <span v-if="showLineNumbers" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('stickyScroll')" class="ab-menu-item" @click="handleToggleStickyScroll">
          <Pin :size="14" />
          {{ t('file.header.stickyScroll') }}
          <span v-if="stickyScroll" class="wrap-check">✓</span>
        </button>
        <button v-if="isAppMode && !isUntitled" class="ab-menu-item" @click="handleShareExternal">
          <Share2 :size="14" />
          {{ t('file.header.shareExternal') }}
        </button>
        <button v-if="permanentMenuIds.includes('delete')" class="ab-menu-item is-danger" @click="handleDelete()">
          <Trash2 :size="14" />
          {{ t('common.delete') }}
        </button>
      </template>
    </ActionBar>

    <!-- Region 3: Overlay nav (close only, always present, fixed size).
         Narrow: the overlay is the whole page and this filled block is its only
         way out — the red is the signal that it matters.
         Wide: the overlay is just another panel, so the same slot holds a plain
         header button — neutral at rest, red only on hover (`is-danger`, the
         shared danger recipe). It stays outside the ActionBar on purpose: that
         row folds its tail into "more", and a close that can disappear into a
         menu is not a way out. Being the header's own last child also puts it
         outermost, in front of the shared panel window controls, instead of in
         the caption reservation they own. -->
    <div class="overlay-nav">
      <button
        v-if="isWideScreen"
        data-toolbar-id="closeOverlay"
        class="file-header-btn is-danger overlay-close-btn--inline"
        :title="t('common.close')"
        :aria-label="t('common.close')"
        @click.stop="$emit('overlayClose')"
      >
        <X :size="14" />
      </button>
      <button v-else class="file-header-btn overlay-close-btn" @click.stop="$emit('overlayClose')" :title="t('common.close')">
        <X :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, inject } from 'vue'
import { isRefreshing } from '@/composables/useFileRefresh'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ActionBar from '@/components/common/ActionBar.vue'
import ExternalBadge from '@/components/file/ExternalBadge.vue'
import { isAbsolutePath } from '@/utils/path.ts'
import { useI18n } from 'vue-i18n'
import { List, Search, Download, Trash2, GitBranch, TextWrap, Hash, Pin, X, MessageSquare, Share2, ScreenShare, FileOutput, Eye, MoveHorizontal, FolderOpen, Pencil, PenLine, Code2, Info, Image, ArrowLeft, ArrowRight, Maximize2, Mail, FileText } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import { fileSupportsToc } from '@/utils/tocSupport.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { navToFileInManager } from '@/composables/useFilePathAnnotation.ts'
import { getNative } from '@/utils/clawbenchNative'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { getWideScreenState } from '@/composables/useWideScreenLayout'
import { useFileShare } from '@/composables/useFileShare'
import { useToast } from '@/composables/useToast'
import { sendPathsByMail } from '@/utils/mailAttach'
import { useFeatureFlags, useShareKindAllowed, notifyFeatureDisabled } from '@/composables/useFeatureFlags'
import { isDockTabHidden } from '@/composables/useDockPlugins'

const props = defineProps({
    file: Object,
    viewMode: String,
    tocOpen: Boolean,
    searchOpen: Boolean,
    wordWrap: Boolean,
    showLineNumbers: Boolean,
    stickyScroll: Boolean,
    overlayOpen: Boolean,
    editing: Boolean,
    /** Wide-screen header back button: view-back target exists. */
    canNavigateBack: Boolean,
    /** Visual (WYSIWYG) markdown editing: the in-editor search UI does not
     *  exist there, so the button would be dead. Ctrl+F still works through
     *  the browser's own find (App lets contenteditable keep the key). */
    visualEditing: Boolean,
    /** Wide-screen header back button: in-file jump history has a previous entry. */
    canGoBackFile: Boolean,
    canGoForwardFile: Boolean,
    backLabel: String,
})
const emit = defineEmits(['delete', 'toggleView', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'openAsText', 'toggleWordWrap', 'toggleLineNumbers', 'toggleStickyScroll', 'refresh', 'overlayClose', 'shareExternal', 'shareLink', 'exportHtml', 'exportDrawio', 'fitWidth', 'toggleEdit', 'editWysiwyg', 'setAsBackground', 'navigateBack', 'navigateForward', 'quoteInChat'])

const { isAppMode } = useAppMode()
const { t } = useI18n()
const { isWideScreen } = getWideScreenState()
const { refreshFileShare, isFileShared } = useFileShare()
// Master switches from 功能编排 — see useFeatureFlags. The share and mail
// entries disappear entirely when off: both open a flow that can only fail
// server-side. The share entry is gated per KIND (file links). The file-history
// entry follows the history TAB instead: it is a display switch, so the entry
// stays visible but explains itself when pressed.
const { mailEnabled } = useFeatureFlags()
const shareAllowed = useShareKindAllowed('files')
const historyTabHidden = computed(() => isDockTabHidden('history'))

/** The open file lives outside the project root (chat annotations may name any
 *  path the server exposes; the viewer opens them like any other file). */
const isExternalFile = computed(() => isAbsolutePath(props.file?.path || ''))

/**
 * A new, not-yet-saved buffer. It has no path on disk, so every path-backed
 * action (refresh, download, share, delete, details, history, open-directory,
 * quote-in-chat, set-as-wallpaper) is meaningless and is hidden rather than
 * offered as a button that would fail.
 */
const isUntitled = computed(() => !!props.file?.untitled)

/** Header label: the real filename, or the localized placeholder until saved. */
const displayName = computed(() => (isUntitled.value ? t('file.untitled') : props.file?.name || ''))

// Whether the currently open file has an active public share link. Mirrors the
// ShareLinkDialog state via the module-level Set so the button highlights as
// soon as a link is created/revoked without extra prop plumbing.
const isShared = computed(() => !!props.file?.path && isFileShared(props.file.path))

// Query the server when the open file changes so the highlight reflects the
// authoritative persisted share state (e.g. after a page reload).
watch(() => props.file?.path, (path) => {
    if (path) void refreshFileShare(path)
}, { immediate: true })

const attachBtnRef = ref(null)
const actionsRef = ref(null)

/** The More menu belongs to ActionBar; outside-click, Escape and positioning are
 *  the popup's own job. Kept as a named function (not an inline expose) so the
 *  header's tests can drive it through `setupState`. */
function toggleMenu() {
  actionsRef.value?.toggleMenu()
}
defineExpose({ toggleMenu })

// Refresh-button spin feedback. The refresh is delegated to the parent
// (App.vue handleRefresh → refreshCurrentFile). Drive the spin from the shared
// isRefreshing ref so it tracks the real load duration.
const refreshing = computed(() => isRefreshing.value)
function triggerRefresh() {
  if (refreshing.value) return
  emit('refresh')
}

// Whether the view-mode toggle applies (source ↔ rendered). How many buttons fit
// and whether they carry labels is ActionBar's decision.
const canToggleView = computed(
  () =>
    hasTextContent.value &&
    !isMediaFile.value &&
    (isMarkdown.value || isHtml.value || isOpenapi.value || isTableFile.value),
)

const fileType = computed(() => props.file ? getFileType(props.file.name) : null)
const isMarkdown = computed(() => fileType.value?.isMarkdown || false)
const isHtml = computed(() => fileType.value?.isHtml || false)
const isImageFile = computed(() => fileType.value?.isImage || false)
const isOpenapi = computed(() => props.file?.subtype === 'openapi')
const isTableFile = computed(() => fileType.value?.isTable || false)
const isMarkdownRendered = computed(() => (isMarkdown.value || isHtml.value || isOpenapi.value || isTableFile.value) && props.viewMode === 'rendered' && !props.editing)
// Effective view: when editing from rendered preview, the user sees source code
const effectiveViewMode = computed(() => (isMarkdownRendered.value) ? 'rendered' : 'raw')
const isMediaFile = computed(() => {
    const ft = fileType.value
    return ft?.isImage || ft?.isAudio || ft?.isVideo || ft?.isPdf || ft?.isExcalidraw || ft?.isDrawio || false
})
// File has usable text content for code-specific features.
// An empty (but loaded) file has content === '' and must still be editable;
// only null/undefined (media, binary, too-large, not-yet-loaded) exclude it.
const hasTextContent = computed(() => typeof props.file?.content === 'string' && !props.file?.tooLarge && !props.file?.isBinary)

// Whether the open file can become the theme wallpaper: raster/vector image
// formats supported by the wallpaper pipeline (png/jpg/jpeg/gif/webp/svg).
// Narrower than isImage (which also allows bmp/ico/tiff/avif).
const wallpaperExts = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg']
const isWallpaperSource = computed(() => {
  if (!props.file?.name) return false
  const ext = props.file.name.split('.').pop()?.toLowerCase() ?? ''
  return wallpaperExts.includes(ext)
})
// Editable: any file with text content (media is excluded above). Every
// rendered body — markdown, the HTML preview, the OpenAPI docs, the CSV table —
// has a source editor behind it, so the edit toggle is offered from either
// view; entering it swaps the preview for that editor and leaving returns to
// the preview. Offering it only in the source view used to strand the
// templated types: the toggle existed but the editor behind it was read-only.
//
// An Untitled buffer is excluded: it is always in edit mode, so an "edit" toggle
// would only offer to drop it back to a read-only view of a file that does not
// exist. Its exit affordance is the close button / Back.
const isEditable = computed(() => {
    if (isUntitled.value) return false
    if (!hasTextContent.value || isMediaFile.value) return false
    return true
})
const hasToc = computed(() => fileSupportsToc(props.file, effectiveViewMode.value))

// Visual markdown editing: only for a real markdown file with loaded text, and
// only while not already editing (the edit toggle owns the exit flow then).
const canWysiwygEdit = computed(
  () =>
    isMarkdown.value &&
    !isUntitled.value &&
    !props.editing &&
    hasTextContent.value &&
    !isMediaFile.value &&
    // A truncated buffer holds only the first slice of the file; saving from
    // the visual editor would write that slice back as the whole file.
    !props.file?.truncated,
)

// Search button for any file with searchable text. For the rendered markdown
// preview it opens the SearchDrawer bottom sheet; for CodeMirror-rendered
// views (code, markdown raw/editing, HTML/OpenAPI raw) it opens CodeMirror's
// own search panel. Media/binary files without content have no search.
const hasSearch = computed(() => {
    if (!props.file) return false
    if (props.file.isPdf || props.file.isOffice || props.file.isExcalidraw || props.file.isDrawio) return false
    return hasTextContent.value
})

// Show reset-zoom button for zoomable file types (PDF, PPT)
const hasFitWidth = computed(() => {
    if (!props.file) return false
    return fileType.value?.isPdf || (fileType.value?.isOffice && props.file.name?.toLowerCase().endsWith('.pptx')) || false
})

// Actions that live in the "More" menu permanently instead of competing for
// toolbar width. They are either low-frequency (file details, open directory,
// file history, share link, open as text, export HTML, set as wallpaper),
// editor preferences already exposed in Settings (word wrap, line numbers,
// sticky scroll), or destructive (delete — hiding it avoids stray clicks).
// Order here is the display order inside the menu; the toolbar never renders
// these inline.
const permanentMenuIds = computed(() => {
  const ids = []
  // Every entry below acts on the file's path on disk. An unsaved Untitled
  // buffer has none yet, so the whole permanent group (details/openDirectory/
  // gitHistory/shareLink/openAsText/exportHtml/setAsBackground/delete) is
  // omitted — only editor preferences remain.
  if (!isUntitled.value) {
    ids.push('details')
    ids.push('openDirectory')
    ids.push('gitHistory')
    if (!props.editing) ids.push('shareLink')
    if (props.file?.isBinary) ids.push('openAsText')
    if (isMarkdown.value && effectiveViewMode.value === 'rendered') ids.push('exportHtml')
    // drawio keeps the export entry visible: the editor's own menu is disabled
    // in embed mode, so this is the only way to get a PNG/SVG/XML out.
    if (props.file?.isDrawio) ids.push('exportDrawio')
    if (isWallpaperSource.value) ids.push('setAsBackground')
  }
  if (hasTextContent.value && !isMediaFile.value && !isMarkdownRendered.value) {
    ids.push('wordWrap')
    ids.push('lineNumbers')
    ids.push('stickyScroll')
  }
  if (!isUntitled.value) ids.push('delete')
  return ids
})

function handleToggleView() {
    emit('toggleView')
}

function handleToggleEdit() {
    emit('toggleEdit')
}

function handleWysiwygEdit() {
    emit('editWysiwyg')
}

function handleToggleWordWrap() {
    emit('toggleWordWrap')
}

function handleToggleLineNumbers() {
    emit('toggleLineNumbers')
}

function handleToggleStickyScroll() {
    emit('toggleStickyScroll')
}

function handleFitWidth() {
    emit('fitWidth')
}

function handleToggleToc() {
    emit('toggleToc')
}

function handleToggleSearch() {
    emit('toggleSearch')
}

function handleOpenAsText() {
    emit('openAsText')
}

function handleDownload() {
    downloadFileByPath(props.file?.path || '', props.file?.name)
}

function handleExportHtml() {
    emit('exportHtml')
}

/**
 * Send this file by mail.
 *
 * The composer belongs to the mail panel, so the file is staged first (a plain
 * file is used in place; a folder would be packed into a zip) and the panel
 * opens with it already attached. Nothing is sent yet — the user still picks
 * the recipient.
 */
async function handleSendByMail() {
    const path = props.file?.path
    if (!path) return
    const name = props.file?.name || path.split('/').pop() || ''
    const res = await sendPathsByMail([path], name)
    if (res.outcome === 'no-mailbox') {
        useToast().show(t('file.toast.mailNotConfigured'), { icon: 'warning', type: 'error', duration: 3000 })
    } else if (res.outcome === 'failed') {
        useToast().show(res.error || t('file.toast.mailSendFailed'), { icon: 'error', type: 'error', duration: 3000 })
    }
}

function handleShareExternal() {
    const native = getNative()
    if (!native || !native.shareFile) return
    const path = props.file?.path
    if (!path) return
    const ft = fileType.value
    let mimeType = '*/*'
    if (ft?.isImage) mimeType = 'image/*'
    else if (ft?.isVideo) mimeType = 'video/*'
    else if (ft?.isAudio) mimeType = 'audio/*'
    else if (ft?.isPdf) mimeType = 'application/pdf'
    else {
        const ext = path.split('.').pop()?.toLowerCase()
        if (ext === 'zip' || ext === 'tar' || ext === 'gz') mimeType = 'application/zip'
    }
    native.shareFile(path, mimeType)?.catch(() => {})
}

function handleDelete() {
    emit('delete', props.file?.path)
}

/**
 * Open the file's commit history.
 *
 * The history tab is only a display switch, so hiding it must not silently
 * remove this entry (the drawer still exists) — but pressing it would open a
 * surface the user just chose to remove from the dock. Explain instead, and
 * point at the place that turns it back on.
 */
function handleGitHistory() {
    if (historyTabHidden.value) { notifyFeatureDisabled(); return }
    emit('openGitHistory')
}

async function handleOpenDirectory() {
    const path = props.file?.path
    if (!path) return
    await navToFileInManager(path)
}

function handleSetAsBackground() {
    const path = props.file?.path
    if (!path) return
    emit('setAsBackground', path)
}

function handleRefresh() {
    triggerRefresh()
}

/**
 * Open the shared quote composer with this file attached, mirroring the
 * issue/PR detail header. This replaces the old attach/detach toggle: the
 * composer is the single entry point, and an already-attached file is removed
 * from its chip in the chat input rather than by clicking this button again.
 *
 * The parent (App.vue) owns the composer + tab switch, so we only emit.
 */
function handleQuoteInChat() {
    const path = props.file?.path
    if (!path) return
    emit('quoteInChat', path)
}

const openLightbox = inject('openLightbox', null)

function handleViewImage() {
    const path = props.file?.path
    if (!path || typeof openLightbox !== 'function') return
    openLightbox(buildLocalFileUrl(path))
}

// Drag the file name onto the chat column (wide-screen split view) to attach
// the current file as a chat attachment — same mechanism the file manager's
// list/grid items use. Clicking the name (no drag) still opens file details.
function handleFileNameDragStart(e) {
    const path = props.file?.path
    const name = props.file?.name
    if (!path || !name) return
    startAttachDrag(e, path, { name })
}

function handleFileNameDragEnd() {
    cleanupDragGhost()
}

</script>

<style scoped>
.file-header-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--header-height);
    padding: 0 var(--space-2) 0 var(--space-3);
    /* Follows the surface it sits on (same rule as every other panel header).
       A fixed `--bg-secondary` made this one header a different colour from the
       task / notification / meeting headers directly above it in the layout. */
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--border-color);
    font-size: var(--font-size-sm);
    position: sticky;
    top: 0;
    left: 0;
    min-width: 0;
}

/* Navigation cluster (back / forward) pinned to the top-left. */
.file-header-nav {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    margin-right: var(--space-1);
}
.file-header-back-btn {
    color: var(--text-primary);
}

/* Region 1: File name — shrinks when toolbar needs space, but has a minimum width */
.file-name-wrap {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 0 1 auto;
    min-width: calc(80 * var(--dp));
    max-width: 40%;
    overflow: hidden;
}

.file-path-hint {
    /* The name is what yields when the header runs out of room: the external
       badge beside it is icon-only and fixed, so letting the name ellipsize
       keeps the "outside the project" cue visible instead of clipping it. */
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
    transition: color var(--duration-base);
}
@media (hover: hover) {
    .file-path-hint:hover {
        color: var(--accent-color);
    }
}
.file-path-hint.file-path-draggable {
    cursor: grab;
}
.file-path-hint.file-path-draggable:active {
    cursor: grabbing;
}
.file-path-hint.copied {
    color: #22c55e;
}

/* Region 2: Toolbar — takes remaining space, shrinks to trigger overflow */
.header-actions {
    flex: 1 1 0;
}


/* Size, radius, colour and hover come from the shared ActionBar row rules
   (size="header"). This block keeps only the parts specific to this header. */
.file-header-btn {
    border: none;
    background: transparent;
    font-size: var(--font-size-xs);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
}
.file-header-btn:disabled {
    opacity: var(--opacity-disabled);
    cursor: not-allowed;
    pointer-events: none;
}
@media (hover: hover) {
    .file-header-btn:disabled:hover {
        background: transparent;
        color: var(--text-secondary);
    }
}
/* Dropdown */
.dropdown-wrapper {
    position: relative;
}

/* Region 3: Overlay nav — fixed size, never shrinks, always visible */
.overlay-nav {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    /* A little air between the toolbar's tail and the exit — the row's own 4dp
       gap is what made the close look crammed against the "more" button. */
    margin-left: var(--space-2);
}
/* Deliberately NOT an ActionBar button: it closes the overlay view rather than
   acting on the file, so it keeps its own filled-red shape and full 26px box
   (the shared row rules would make it a flat 14px glyph — the row's own
   `> button` sizing does not reach here because it is outside the bar, but the
   `.file-header-btn` base is shared, so the size is stated explicitly). */
.overlay-close-btn {
    width: calc(26 * var(--dp));
    height: calc(26 * var(--dp));
    padding: 0;
    background: #b91c1c;
    /* Just a hint of rounding — a larger radius on a filled block reads as a
       floating chip rather than a corner affordance. */
    border-radius: var(--radius-xs);
    color: #fff;
    /* The bar's 1px bottom border sits inside its border-box, so flex centring
       happens in a 35px content box and every child lands 0.5px above the bar's
       real middle. Bare 14px glyphs hide that; a 26px filled block does not — it
       reads as "sitting high" next to the icons. Nudge the block, not the shared
       bar geometry (the border is a literal 1px at every UI scale, so this
       offset does not need --dp). */
    position: relative;
    top: 0.5px;
}
@media (hover: hover) {
    .overlay-close-btn:hover {
        background: #991b1b;
        color: #fff;
    }
}
/* Wide-screen counterpart: a header-size square like the row's buttons, neutral
   at rest (colour + hover tint come from the shared `.is-danger` recipe), so it
   reads as "close this document", not as window chrome. */
.overlay-close-btn--inline {
    /* A step larger than the row's buttons (`--ab-btn-size`, 28dp): the exit is
       the one control in this row that must not look pinched between the
       toolbar's tail and the window controls. */
    width: calc(32 * var(--dp));
    height: calc(32 * var(--dp));
    padding: 0;
    border-radius: var(--radius-sm);
}
/* The glyph travels with the UI scale, like the row's icons do. A literal 14px
   stays 14px while everything around it grows, which is what made the close
   read as smaller than the icons beside it on a scaled UI. */
.overlay-nav svg {
    width: calc(15 * var(--dp));
    height: calc(15 * var(--dp));
}
.overlay-close-btn--inline svg {
    width: calc(16 * var(--dp));
    height: calc(16 * var(--dp));
}
</style>


