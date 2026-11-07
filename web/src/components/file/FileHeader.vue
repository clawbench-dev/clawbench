<template>
  <div class="file-header-bar">
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
    <ActionBar ref="actionsRef" class="header-actions" mode="menu" align="end" always-more>
      <!-- Refresh button — first in the toolbar, and the last to collapse. -->
      <RefreshButton v-if="hasTextContent && !isUntitled" data-toolbar-id="refresh" icon="RotateCw" class="file-header-btn" :loading="refreshing" :disabled="refreshing" :title="t('nav.refresh')" @click.stop="handleRefresh">
        <span class="action-label">{{ t('nav.refresh') }}</span>
      </RefreshButton>

      <!-- Collapse / restore the chat pane (wide screen only).
           The pre-existing affordance is the collapse handle that appears on
           the split divider under the cursor — a ~14px target on a 6px-wide
           line, which is both hard to hit and easy to trigger by accident while
           aiming at the scrollbar. This button is a full-size target in the
           place the user is already looking, and does the same thing, so the
           divider handle stays as the discoverable one.

           ICON: PanelRightClose / PanelRightOpen, NOT Maximize2/Minimize2.
           This toolbar already has a Maximize2 for images ("view full size"),
           and the two sat three buttons apart wearing the same glyph for
           completely different actions. Maximize2 also means "open external
           link", "enlarge a shared screen" and "maximize the OS window"
           elsewhere in the app — a fifth meaning is unreadable. These two
           glyphs say what actually happens: the RIGHT pane (chat) closes or
           opens. -->
      <button
        v-if="isWideScreen"
        class="file-header-btn"
        data-toolbar-id="maximize"
        data-action="toggle-maximize-pane"
        :class="{ active: chatCollapsed }"
        @click.stop="emit('toggleMaximizePane')"
        :title="chatCollapsed ? t('nav.showChat') : t('nav.hideChat')"
        :aria-label="chatCollapsed ? t('nav.showChat') : t('nav.hideChat')"
      >
        <PanelRightOpen v-if="chatCollapsed" :size="14" />
        <PanelRightClose v-else :size="14" />
        <span class="action-label">{{ chatCollapsed ? t('actionLabels.showChat') : t('actionLabels.hideChat') }}</span>
      </button>

      <!-- TOC button (only for file types that support TOC) -->
      <button v-if="hasToc" data-toolbar-id="toc" class="file-header-btn" :class="{ active: tocOpen }" @click.stop="handleToggleToc" :title="t('file.header.toc')">
        <List :size="14" />
        <span class="action-label">{{ t('file.header.toc') }}</span>
      </button>

      <!-- Search button (only for file types that support search) -->
      <button v-if="hasSearch" data-toolbar-id="search" class="file-header-btn" :class="{ active: searchOpen }" @click.stop="handleToggleSearch" :title="t('file.header.search')">
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
        <button v-if="permanentMenuIds.includes('details')" class="dropdown-item" @click="$emit('showDetails')">
          <Info :size="14" />
          {{ t('file.header.details') }}
        </button>
        <button v-if="permanentMenuIds.includes('openDirectory')" class="dropdown-item" @click="handleOpenDirectory">
          <FolderOpen :size="14" />
          {{ t('file.header.openDirectory') }}
        </button>
        <button v-if="permanentMenuIds.includes('gitHistory')" class="dropdown-item" @click="handleGitHistory">
          <GitBranch :size="14" />
          {{ t('file.header.fileHistory') }}
        </button>
        <button v-if="permanentMenuIds.includes('shareLink')" class="dropdown-item" :class="{ active: isShared }" @click="$emit('shareLink')">
          <ScreenShare :size="14" />
          {{ t('file.header.shareLink') }}
          <span v-if="isShared" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('openAsText')" class="dropdown-item" @click="handleOpenAsText">
          <Code2 :size="14" />
          {{ t('file.header.openAsText') }}
        </button>
        <button v-if="permanentMenuIds.includes('exportHtml')" class="dropdown-item" @click="handleExportHtml">
          <FileOutput :size="14" />
          {{ t('file.header.exportHtml') }}
        </button>
        <button v-if="permanentMenuIds.includes('setAsBackground')" class="dropdown-item" @click="handleSetAsBackground">
          <Image :size="14" />
          {{ t('file.header.setAsBackground') }}
        </button>
        <button v-if="permanentMenuIds.includes('wordWrap')" class="dropdown-item" @click="handleToggleWordWrap">
          <TextWrap :size="14" />
          {{ t('file.header.wordWrap') }}
          <span v-if="wordWrap" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('lineNumbers')" class="dropdown-item" @click="handleToggleLineNumbers">
          <Hash :size="14" />
          {{ t('file.header.lineNumbers') }}
          <span v-if="showLineNumbers" class="wrap-check">✓</span>
        </button>
        <button v-if="permanentMenuIds.includes('stickyScroll')" class="dropdown-item" @click="handleToggleStickyScroll">
          <Pin :size="14" />
          {{ t('file.header.stickyScroll') }}
          <span v-if="stickyScroll" class="wrap-check">✓</span>
        </button>
        <button v-if="isAppMode && !isUntitled" class="dropdown-item" @click="handleShareExternal">
          <Share2 :size="14" />
          {{ t('file.header.shareExternal') }}
        </button>
        <button v-if="permanentMenuIds.includes('delete')" class="dropdown-item danger" @click="handleDelete()">
          <Trash2 :size="14" />
          {{ t('common.delete') }}
        </button>
      </template>
    </ActionBar>

    <!-- Region 3: Overlay nav (close only, always present, fixed size) -->
    <div class="overlay-nav">
      <button class="file-header-btn overlay-close-btn" @click.stop="$emit('overlayClose')" :title="t('common.close')">
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
import { List, Search, Download, Trash2, GitBranch, TextWrap, Hash, Pin, X, MessageSquare, Share2, ScreenShare, FileOutput, Eye, MoveHorizontal, FolderOpen, Pencil, Code2, Info, Image, ArrowLeft, ArrowRight, Maximize2, PanelRightClose, PanelRightOpen } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import { fileSupportsToc } from '@/utils/tocSupport.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { navToFileInManager } from '@/composables/useFilePathAnnotation.ts'
import { getNative } from '@/utils/clawbenchNative'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { getWideScreenState } from '@/composables/useWideScreenLayout'
import { useFileShare } from '@/composables/useFileShare'

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
    /** Wide-screen header back button: in-file jump history has a previous entry. */
    canGoBackFile: Boolean,
    canGoForwardFile: Boolean,
    backLabel: String,
})
const emit = defineEmits(['delete', 'toggleView', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'openAsText', 'toggleWordWrap', 'toggleLineNumbers', 'toggleStickyScroll', 'refresh', 'overlayClose', 'shareExternal', 'shareLink', 'exportHtml', 'fitWidth', 'toggleEdit', 'setAsBackground', 'navigateBack', 'navigateForward', 'quoteInChat', 'toggleMaximizePane'])

const { isAppMode } = useAppMode()
const { t } = useI18n()
// `chatCollapsed` decides whether the maximize button reads as maximize or
// restore, so it has to be reactive here — the pane can also be collapsed from
// the divider handle or the settings drawer while this header is mounted.
const { isWideScreen, chatCollapsed } = getWideScreenState()
const { refreshFileShare, isFileShared } = useFileShare()

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
    return ft?.isImage || ft?.isAudio || ft?.isVideo || ft?.isPdf || ft?.isExcalidraw || false
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
// Editable: text/source files in raw view (excludes media).
// Markdown is always editable (even in rendered view) so users can edit the source.
//
// An Untitled buffer is excluded: it is always in edit mode, so an "edit" toggle
// would only offer to drop it back to a read-only view of a file that does not
// exist. Its exit affordance is the close button / Back.
const isEditable = computed(() => {
    if (isUntitled.value) return false
    if (!hasTextContent.value || isMediaFile.value) return false
    if (isMarkdown.value) return true
    // The table view is read-only by nature, but the file is still text: the
    // edit button opens it in the source editor (same as markdown's preview).
    if (isTableFile.value) return true
    // Other templated types (HTML/OpenAPI) are only editable in source view
    return !isMarkdownRendered.value
})
const hasToc = computed(() => fileSupportsToc(props.file, effectiveViewMode.value))

// Search button for any file with searchable text. For the rendered markdown
// preview it opens the SearchDrawer bottom sheet; for CodeMirror-rendered
// views (code, markdown raw/editing, HTML/OpenAPI raw) it opens CodeMirror's
// own search panel. Media/binary files without content have no search.
const hasSearch = computed(() => {
    if (!props.file) return false
    if (props.file.isPdf || props.file.isOffice || props.file.isExcalidraw) return false
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

function handleGitHistory() {
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
    background: var(--bg-secondary);
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
    gap: var(--space-4);
    flex: 1 1 0;
}
/* Labelled buttons get room for the text; the icon+text gap is what keeps the
   two from touching. ActionBar decides when the labels are shown. */
.header-actions.ab--labels .file-header-btn {
    gap: calc(4 * var(--dp));
    padding: 0 calc(8 * var(--dp));
}

.file-header-btn {
    padding: var(--space-3);
    border: none;
    border-radius: var(--radius-xs);
    background: transparent;
    font-size: var(--font-size-xs);
    cursor: pointer;
    color: var(--text-secondary);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}
@media (hover: hover) {
    .file-header-btn:hover {
        background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    }
}
.file-header-btn svg {
    width: calc(14 * var(--dp));
    height: calc(14 * var(--dp));
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
.file-header-btn.active {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    color: var(--accent-color);
}
.file-header-btn.danger {
    color: #ef4444;
}
@media (hover: hover) {
    .file-header-btn.danger:hover {
        background: #fef2f2;
        color: #dc2626;
    }
    [data-theme-base="dark"] .file-header-btn.danger:hover {
        background: #2d1b1b;
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
}
.overlay-close-btn {
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
</style>

<!-- Unscoped styles for Teleported dropdown menu (rendered in body, outside scoped context) -->
<style>
.file-header-dropdown-menu {
    position: fixed;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-md);
    box-shadow: 0 calc(4 * var(--dp)) calc(12 * var(--dp)) rgba(0, 0, 0, 0.15);
    z-index: var(--z-popover);
    min-width: calc(140 * var(--dp));
    padding: var(--space-2) 0;
    overflow: hidden;
}

.file-header-dropdown-menu .dropdown-item {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-6);
    width: 100%;
    border: none;
    background: none;
    color: var(--text-primary);
    font-size: var(--font-size-md);
    cursor: pointer;
    text-decoration: none;
    white-space: nowrap;
}
@media (hover: hover) {
    .file-header-dropdown-menu .dropdown-item:hover {
        background: var(--accent-color);
        color: #fff;
    }
}
.file-header-dropdown-menu .dropdown-item.active {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    color: var(--accent-color);
}
.file-header-dropdown-menu .dropdown-item svg {
    flex-shrink: 0;
}
.file-header-dropdown-menu .dropdown-item:disabled {
    opacity: var(--opacity-disabled);
    cursor: not-allowed;
    pointer-events: none;
}
.file-header-dropdown-menu .dropdown-divider {
    height: 1px; background: var(--border-color); margin: var(--space-2) 0;
}
.file-header-dropdown-menu .dropdown-item.danger {
    color: #ef4444;
}
@media (hover: hover) {
    .file-header-dropdown-menu .dropdown-item.danger:hover {
        background: #fef2f2;
        color: #dc2626;
    }
    [data-theme-base="dark"] .file-header-dropdown-menu .dropdown-item.danger:hover {
        background: #2d1b1b;
    }
}
</style>
