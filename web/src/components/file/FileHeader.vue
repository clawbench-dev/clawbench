<template>
  <div class="file-header-bar">
    <!-- Wide-screen only: navigation cluster at the top-left, where desktop users
         expect a back affordance. Touch layouts keep these on the bottom-center
         floating bar instead (thumb reach + no room in the header). -->
    <div v-if="isWideScreen && (canNavigateBack || canGoBackFile || canGoForwardFile)" class="file-header-nav">
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
      <span class="file-path-hint" :class="{ 'file-path-draggable': isWideScreen }" :draggable="isWideScreen" @click="$emit('showDetails')" @dragstart="handleFileNameDragStart" @dragend="handleFileNameDragEnd" :title="file.name">{{ file.name }}</span>
    </div>

    <!-- Region 2: Toolbar (ResizeObserver target) -->
    <div ref="headerActionsRef" class="header-actions">
      <!-- Refresh button — first in the toolbar, and the last to collapse. -->
      <RefreshButton v-if="toolbarInlineIds.includes('refresh')" icon="RotateCw" class="file-header-btn" :loading="refreshing" :disabled="refreshing" :title="t('nav.refresh')" @click.stop="handleRefresh" />

      <!-- TOC button (only for file types that support TOC) -->
      <button v-if="hasToc && toolbarInlineIds.includes('toc')" class="file-header-btn" :class="{ active: tocOpen }" @click.stop="handleToggleToc" :title="t('file.header.toc')">
        <List :size="14" />
      </button>

      <!-- Search button (only for file types that support search) -->
      <button v-if="hasSearch && toolbarInlineIds.includes('search')" class="file-header-btn" :class="{ active: searchOpen }" @click.stop="handleToggleSearch" :title="t('file.header.search')">
        <Search :size="14" />
      </button>

      <!-- Fit-width / reset zoom button (for PDF) -->
      <button v-if="toolbarInlineIds.includes('fitWidth')" class="file-header-btn" @click.stop="handleFitWidth" :title="t('file.header.fitWidth')">
        <MoveHorizontal :size="14" />
      </button>

      <!-- Quote in chat: opens the shared quote composer with this file attached,
           matching the issue/PR detail header. Not a toggle — the composer is the
           single entry point, and an attached file is removed from its chip in
           the chat input. -->
      <button v-if="toolbarInlineIds.includes('attach')" ref="attachBtnRef" class="file-header-btn" @click.stop="handleQuoteInChat" :title="t('file.header.quoteInChat')" :aria-label="t('file.header.quoteInChat')">
        <MessageSquare :size="14" />
      </button>

      <!-- Lightbox view button (image / svg files only): opens the image full-size
           in the shared Lightbox (zoom + sibling navigation). Attach lives on the
           same header — no need for a per-image header. -->
      <button v-if="isImageFile && toolbarInlineIds.includes('viewImage')" class="file-header-btn" @click.stop="handleViewImage" :title="t('imageBlock.view')">
        <Maximize2 :size="14" />
      </button>

      <!-- Toggle view button (source/rendered). Always an eye icon: highlighted
           when the rendered preview is shown, dimmed for the source view. It is
           disabled while editing so the edit button stays the sole relevant control. -->
      <button v-if="toolbarInlineIds.includes('toggleView')" class="file-header-btn" :class="{ active: effectiveViewMode === 'rendered' }" :disabled="editing" @click.stop="handleToggleView" :title="effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView')">
        <Eye :size="14" />
      </button>

      <!-- Edit toggle button — always directly beside the preview toggle -->
      <button v-if="toolbarInlineIds.includes('edit')" class="file-header-btn" :class="{ active: editing }" @click.stop="handleToggleEdit" :title="editing ? t('file.header.finishEditing') : t('file.header.edit')">
        <Pencil :size="14" />
      </button>

      <!-- Word wrap / line numbers / sticky scroll moved to the More menu
           (permanentMenuIds): they are editor preferences, set once and rarely
           touched, and all three are also available in Settings → File display. -->

      <!-- Download button -->
      <button v-if="toolbarInlineIds.includes('download')" class="file-header-btn" @click.stop="handleDownload" :title="t('common.download')">
        <Download :size="14" />
      </button>

      <!-- Open as text, share link, export HTML, set as wallpaper, open
           directory, file history, delete and details are all permanent More-menu
           actions (see permanentMenuIds) — no inline buttons for them. -->

      <!-- More actions dropdown. Always rendered: permanentMenuIds is never
           empty (delete is unconditional), and overflow-collapsed toolbar items
           are appended after the permanent group. -->
      <div class="dropdown-wrapper" ref="dropdownRef">
        <button class="file-header-btn" :class="{ active: menuOpen }" @click.stop="toggleMenu" :title="t('file.header.more')">
          <MoreVertical :size="14" />
        </button>
        <Teleport to="body">
          <div v-if="menuOpen" ref="menuRef" class="file-header-dropdown-menu" :style="menuStyle">
            <!-- Permanent group: low-frequency and destructive actions that never
                 occupy toolbar space. Order here is the display order. -->
            <button v-if="permanentMenuIds.includes('details')" class="dropdown-item" @click="$emit('showDetails'); menuOpen = false">
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
            <button v-if="permanentMenuIds.includes('shareLink')" class="dropdown-item" :class="{ active: isShared }" @click="$emit('shareLink'); menuOpen = false">
              <ScreenShare :size="14" />
              {{ isShared ? t('file.header.shareLinkActive') : t('file.header.shareLink') }}
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
            <button v-if="permanentMenuIds.includes('delete')" class="dropdown-item danger" @click="handleDelete(); menuOpen = false">
              <Trash2 :size="14" />
              {{ t('common.delete') }}
            </button>

            <!-- Overflow group: toolbar items demoted for lack of width. -->
            <div v-if="toolbarCollapsedIds.length > 0" class="dropdown-divider"></div>
            <button v-if="toolbarCollapsedIds.includes('toc')" class="dropdown-item" :class="{ active: tocOpen }" @click="handleToggleToc(); menuOpen = false">
              <List :size="14" />
              {{ t('file.header.toc') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('search')" class="dropdown-item" :class="{ active: searchOpen }" @click="handleToggleSearch(); menuOpen = false">
              <Search :size="14" />
              {{ t('file.header.search') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('fitWidth')" class="dropdown-item" @click="handleFitWidth(); menuOpen = false">
              <MoveHorizontal :size="14" />
              {{ t('file.header.fitWidth') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('attach')" class="dropdown-item" @click="handleQuoteInChat(); menuOpen = false">
              <MessageSquare :size="14" />
              {{ t('file.header.quoteInChat') }}
            </button>
            <button v-if="isImageFile && toolbarCollapsedIds.includes('viewImage')" class="dropdown-item" @click="handleViewImage(); menuOpen = false">
              <Maximize2 :size="14" />
              {{ t('imageBlock.view') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('refresh')" class="dropdown-item refresh-spin" :class="{ 'refresh-spin--active': refreshing }" :disabled="refreshing" @click="handleRefresh">
              <RotateCw :size="14" />
              {{ t('nav.refresh') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('toggleView')" class="dropdown-item" :class="{ active: effectiveViewMode === 'rendered' }" :disabled="editing" @click="handleToggleView">
              <Eye :size="14" />
              {{ effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('edit')" class="dropdown-item" :class="{ active: editing }" @click="handleToggleEdit">
              <Pencil :size="14" />
              {{ editing ? t('file.header.finishEditing') : t('file.header.edit') }}
            </button>
            <button v-if="isAppMode && toolbarCollapsedIds.includes('shareExternal')" class="dropdown-item" @click="handleShareExternal">
              <Share2 :size="14" />
              {{ t('file.header.shareExternal') }}
            </button>
            <a v-if="!isAppMode && toolbarCollapsedIds.includes('download')" class="dropdown-item" :href="buildLocalFileUrl(file.path, { download: true })" :download="file.name" @click="menuOpen = false">
              <Download :size="14" />
              {{ t('common.download') }}
            </a>
            <button v-else-if="toolbarCollapsedIds.includes('download')" class="dropdown-item" @click="handleDownload">
              <Download :size="14" />
              {{ t('common.download') }}
            </button>
          </div>
        </Teleport>
      </div>
    </div>

    <!-- Region 3: Overlay nav (close only, always present, fixed size) -->
    <div class="overlay-nav">
      <button class="file-header-btn overlay-close-btn" @click.stop="$emit('overlayClose')" :title="t('common.close')">
        <X :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick, inject } from 'vue'
import { isRefreshing } from '@/composables/useFileRefresh'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { useI18n } from 'vue-i18n'
import { List, Search, MoreVertical, Download, Trash2, GitBranch, TextWrap, Hash, RotateCw, Pin, X, MessageSquare, Share2, ScreenShare, FileOutput, Eye, MoveHorizontal, FolderOpen, Pencil, Code2, Info, Image, ArrowLeft, ArrowRight, Maximize2 } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import { fileSupportsToc } from '@/utils/tocSupport.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'
import { navToFileInManager } from '@/composables/useFilePathAnnotation.ts'
import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'
import { getNative } from '@/utils/clawbenchNative'
import { setAttachDragData, buildAttachDragImage, cleanupDragGhost } from '@/utils/attachDrag'
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
const emit = defineEmits(['delete', 'toggleView', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'openAsText', 'toggleWordWrap', 'toggleLineNumbers', 'toggleStickyScroll', 'refresh', 'overlayClose', 'shareExternal', 'shareLink', 'exportHtml', 'fitWidth', 'toggleEdit', 'setAsBackground', 'navigateBack', 'navigateForward', 'quoteInChat'])

const { isAppMode } = useAppMode()
const { t } = useI18n()
const { isWideScreen } = getWideScreenState()
const { refreshFileShare, isFileShared } = useFileShare()

// Whether the currently open file has an active public share link. Mirrors the
// ShareLinkDialog state via the module-level Set so the button highlights as
// soon as a link is created/revoked without extra prop plumbing.
const isShared = computed(() => !!props.file?.path && isFileShared(props.file.path))

// Query the server when the open file changes so the highlight reflects the
// authoritative persisted share state (e.g. after a page reload).
watch(() => props.file?.path, (path) => {
    if (path) void refreshFileShare(path)
}, { immediate: true })

const menuOpen = ref(false)
const dropdownRef = ref(null)
const menuRef = ref(null)
const menuStyle = ref({})
const attachBtnRef = ref(null)
const headerActionsRef = ref(null)

// Refresh-button spin feedback. The refresh is delegated to the parent
// (App.vue handleRefresh → refreshCurrentFile). Drive the spin from the shared
// isRefreshing ref so it tracks the real load duration.
const refreshing = computed(() => isRefreshing.value)
function triggerRefresh() {
  if (refreshing.value) return
  emit('refresh')
}

// Responsive toolbar overflow — only the "More" dropdown is always-inline (1).
// Permanent menu actions are excluded from the demotable list entirely, so they
// never render inline and never appear in collapsedIds.
// The array order must mirror the template order: index 0 renders leftmost and
// is the last to collapse.
const { inlineIds: toolbarInlineIds, collapsedIds: toolbarCollapsedIds, startObserving: startToolbarResize, stopObserving: stopToolbarResize } = useToolbarOverflow(
  () => headerActionsRef.value,
  () => {
    const ids = []
    if (hasTextContent.value) ids.push('refresh')
    if (hasToc.value) ids.push('toc')
    if (hasSearch.value) ids.push('search')
    if (hasFitWidth.value) ids.push('fitWidth')
    ids.push('attach')
    if (isImageFile.value) ids.push('viewImage')
    if (hasTextContent.value && !isMediaFile.value && (isMarkdown.value || isHtml.value || isOpenapi.value)) ids.push('toggleView')
    // Edit always sits right next to the preview toggle: the two form a single
    // view-mode control pair with no other buttons in between.
    if (isEditable.value) ids.push('edit')
    // Extra actions demote to the More dropdown when space runs out.
    if (isAppMode.value) ids.push('shareExternal')
    ids.push('download')
    return ids
  },
  { inlineCount: 1, gap: 8 },
)

function toggleMenu() {
    menuOpen.value = !menuOpen.value
    if (menuOpen.value) {
        nextTick(() => updateMenuPosition())
    }
}

function updateMenuPosition() {
    if (!dropdownRef.value) return
    const rect = dropdownRef.value.getBoundingClientRect()
    const vp = getZoomedViewport()
    menuStyle.value = {
        position: 'fixed',
        top: `${toFixedCSS(rect.bottom + 4)}px`,
        right: `${toFixedCSS(vp.width - rect.right)}px`,
        left: 'auto',
    }
}

const fileType = computed(() => props.file ? getFileType(props.file.name) : null)
const isMarkdown = computed(() => fileType.value?.isMarkdown || false)
const isHtml = computed(() => fileType.value?.isHtml || false)
const isImageFile = computed(() => fileType.value?.isImage || false)
const isOpenapi = computed(() => props.file?.subtype === 'openapi')
const isMarkdownRendered = computed(() => (isMarkdown.value || isHtml.value || isOpenapi.value) && props.viewMode === 'rendered' && !props.editing)
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
const isEditable = computed(() => {
    if (!hasTextContent.value || isMediaFile.value) return false
    if (isMarkdown.value) return true
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
  ids.push('details')
  ids.push('openDirectory')
  ids.push('gitHistory')
  if (!props.editing) ids.push('shareLink')
  if (props.file?.isBinary) ids.push('openAsText')
  if (isMarkdown.value && effectiveViewMode.value === 'rendered') ids.push('exportHtml')
  if (isWallpaperSource.value) ids.push('setAsBackground')
  if (hasTextContent.value && !isMediaFile.value && !isMarkdownRendered.value) {
    ids.push('wordWrap')
    ids.push('lineNumbers')
    ids.push('stickyScroll')
  }
  ids.push('delete')
  return ids
})

function handleToggleView() {
    menuOpen.value = false
    emit('toggleView')
}

function handleToggleEdit() {
    menuOpen.value = false
    emit('toggleEdit')
}

function handleToggleWordWrap() {
    menuOpen.value = false
    emit('toggleWordWrap')
}

function handleToggleLineNumbers() {
    menuOpen.value = false
    emit('toggleLineNumbers')
}

function handleToggleStickyScroll() {
    menuOpen.value = false
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
    menuOpen.value = false
    emit('openAsText')
}

function handleDownload() {
    menuOpen.value = false
    downloadFileByPath(props.file?.path || '', props.file?.name)
}

function handleExportHtml() {
    menuOpen.value = false
    emit('exportHtml')
}

function handleShareExternal() {
    menuOpen.value = false
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
    menuOpen.value = false
    emit('delete', props.file?.path)
}

function handleGitHistory() {
    menuOpen.value = false
    emit('openGitHistory')
}

async function handleOpenDirectory() {
    menuOpen.value = false
    const path = props.file?.path
    if (!path) return
    await navToFileInManager(path)
}

function handleSetAsBackground() {
    menuOpen.value = false
    const path = props.file?.path
    if (!path) return
    emit('setAsBackground', path)
}

function handleRefresh() {
    menuOpen.value = false
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
    e.dataTransfer.effectAllowed = 'copy'
    setAttachDragData(e.dataTransfer, path, false)
    const ghost = buildAttachDragImage(name, false)
    e.dataTransfer.setDragImage(ghost, 14, 16)
}

function handleFileNameDragEnd() {
    cleanupDragGhost()
}

// Close dropdown on outside click
function handleClickOutside(e) {
    if (menuOpen.value &&
        dropdownRef.value && !dropdownRef.value.contains(e.target) &&
        (!menuRef.value || !menuRef.value.contains(e.target))) {
        menuOpen.value = false
    }
}

onMounted(() => {
    document.addEventListener('click', handleClickOutside)
    startToolbarResize()
})

onBeforeUnmount(() => {
    document.removeEventListener('click', handleClickOutside)
    stopToolbarResize()
})
</script>

<style scoped>
.file-header-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--header-height);
    padding:0 var(--space-2) 0 var(--space-3);
    background: var(--bg-secondary);
    border: none;
    border-bottom: 1px solid var(--border-color);
    font-size: var(--font-size-sm);
    position: sticky;
    top: 0;
    left: 0;
    min-width: 0;
}

/* Wide-screen navigation cluster (back / forward) pinned to the top-left. */
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
    min-width: 80px;
    max-width: 40%;
    overflow: hidden;
}

.file-path-hint {
    flex: 0 0 auto;
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
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex: 1 1 0;
    min-width: 0;
    overflow: hidden;
    justify-content: flex-end;
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
    width: 14px;
    height: 14px;
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
    border-radius: 0;
    color: #fff;
}
@media (hover: hover) {
    .overlay-close-btn:hover {
        background: #991b1b;
        color: #fff;
    }
}

.wrap-check {
    margin-left: auto;
    color: var(--accent-color);
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-bold);
}
</style>

<!-- Unscoped styles for Teleported dropdown menu (rendered in body, outside scoped context) -->
<style>
.file-header-dropdown-menu {
    position: fixed;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-md);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    z-index: var(--z-popover);
    min-width: 140px;
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
.file-header-dropdown-menu .wrap-check {
    margin-left: auto;
    color: var(--accent-color);
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-bold);
}
</style>
