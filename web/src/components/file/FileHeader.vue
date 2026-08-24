<template>
  <div class="file-header-bar">
    <!-- Region 1: File name -->
    <div class="file-name-wrap">
      <span class="file-path-hint" style="cursor:pointer" @click="$emit('showDetails')" :title="file.name">{{ file.name }}</span>
    </div>

    <!-- Region 2: Toolbar (ResizeObserver target) -->
    <div ref="headerActionsRef" class="header-actions">
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

      <!-- Attach to chat button -->
      <button v-if="toolbarInlineIds.includes('attach')" ref="attachBtnRef" class="file-header-btn" :class="{ active: isAttached }" @click.stop="handleAttachToChat" :title="isAttached ? t('chat.attach.removeFromChat') : t('chat.actions.attachToChat')">
        <Paperclip :size="14" />
      </button>

      <!-- Refresh button -->
      <RefreshButton v-if="toolbarInlineIds.includes('refresh')" icon="RotateCw" class="file-header-btn" :loading="refreshing" :disabled="refreshing" :title="t('nav.refresh')" @click.stop="handleRefresh" />

      <!-- Toggle view button (source/rendered) -->
      <button v-if="toolbarInlineIds.includes('toggleView')" class="file-header-btn" @click.stop="handleToggleView" :title="effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView')">
        <Code2 v-if="effectiveViewMode === 'rendered'" :size="14" />
        <Eye v-else :size="14" />
      </button>

      <!-- Word wrap toggle button -->
      <button v-if="toolbarInlineIds.includes('wordWrap')" class="file-header-btn" :class="{ active: wordWrap }" @click.stop="handleToggleWordWrap" :title="t('file.header.wordWrap')">
        <TextWrap :size="14" />
      </button>

      <!-- Line numbers toggle button -->
      <button v-if="toolbarInlineIds.includes('lineNumbers')" class="file-header-btn" :class="{ active: showLineNumbers }" @click.stop="handleToggleLineNumbers" :title="t('file.header.lineNumbers')">
        <Hash :size="14" />
      </button>

      <!-- Sticky scroll toggle button -->
      <button v-if="toolbarInlineIds.includes('stickyScroll')" class="file-header-btn" :class="{ active: stickyScroll }" @click.stop="handleToggleStickyScroll" :title="t('file.header.stickyScroll')">
        <Pin :size="14" />
      </button>

      <!-- Edit toggle button -->
      <button v-if="toolbarInlineIds.includes('edit')" class="file-header-btn" :class="{ active: editing }" @click.stop="handleToggleEdit" :title="editing ? t('file.header.finishEditing') : t('file.header.edit')">
        <Pencil :size="14" />
      </button>

      <!-- Open as text button (binary files only) -->
      <button v-if="file.isBinary && toolbarInlineIds.includes('openAsText')" class="file-header-btn" @click.stop="handleOpenAsText" :title="t('file.header.openAsText')">
        <Code2 :size="14" />
      </button>

      <!-- Share external button (app mode only) -->
      <button v-if="isAppMode && toolbarInlineIds.includes('shareExternal')" class="file-header-btn" @click.stop="handleShareExternal" :title="t('file.header.shareExternal')">
        <Share2 :size="14" />
      </button>

      <!-- Download button -->
      <button v-if="toolbarInlineIds.includes('download')" class="file-header-btn" @click.stop="handleDownload" :title="t('common.download')">
        <Download :size="14" />
      </button>

      <!-- Export HTML button (markdown rendered only) -->
      <button v-if="isMarkdown && effectiveViewMode === 'rendered' && toolbarInlineIds.includes('exportHtml')" class="file-header-btn" @click.stop="handleExportHtml" :title="t('file.header.exportHtml')">
        <FileOutput :size="14" />
      </button>

      <!-- Open directory button -->
      <button v-if="toolbarInlineIds.includes('openDirectory')" class="file-header-btn" @click.stop="handleOpenDirectory" :title="t('file.header.openDirectory')">
        <FolderOpen :size="14" />
      </button>

      <!-- Git history button -->
      <button v-if="toolbarInlineIds.includes('gitHistory')" class="file-header-btn" @click.stop="handleGitHistory" :title="t('file.header.fileHistory')">
        <GitBranch :size="14" />
      </button>

      <!-- Delete button (last action) -->
      <button v-if="toolbarInlineIds.includes('delete')" class="file-header-btn danger" @click.stop="handleDelete" :title="t('common.delete')">
        <Trash2 :size="14" />
      </button>

      <!-- More actions dropdown (only when collapsed items exist) -->
      <div v-if="toolbarCollapsedIds.length > 0" class="dropdown-wrapper" ref="dropdownRef">
        <button class="file-header-btn" @click.stop="toggleMenu" :title="t('file.header.more')">
          <MoreVertical :size="14" />
        </button>
        <Teleport to="body">
          <div v-if="menuOpen" ref="menuRef" class="file-header-dropdown-menu" :style="menuStyle">
            <!-- Collapsed toolbar items -->
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
            <button v-if="toolbarCollapsedIds.includes('attach')" class="dropdown-item" :class="{ active: isAttached }" @click="handleAttachToChat(); menuOpen = false">
              <Paperclip :size="14" />
              {{ isAttached ? t('chat.attach.removeFromChat') : t('chat.actions.attachToChat') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('refresh')" class="dropdown-item refresh-spin" :class="{ 'refresh-spin--active': refreshing }" :disabled="refreshing" @click="handleRefresh">
              <RotateCw :size="14" />
              {{ t('nav.refresh') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('toggleView')" class="dropdown-item" @click="handleToggleView">
              <Code2 v-if="effectiveViewMode === 'rendered'" :size="14" />
              <Eye v-else :size="14" />
              {{ effectiveViewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('wordWrap')" class="dropdown-item" @click="handleToggleWordWrap">
              <TextWrap :size="14" />
              {{ t('file.header.wordWrap') }}
              <span v-if="wordWrap" class="wrap-check">✓</span>
            </button>
            <button v-if="toolbarCollapsedIds.includes('lineNumbers')" class="dropdown-item" @click="handleToggleLineNumbers">
              <Hash :size="14" />
              {{ t('file.header.lineNumbers') }}
              <span v-if="showLineNumbers" class="wrap-check">✓</span>
            </button>
            <button v-if="toolbarCollapsedIds.includes('stickyScroll')" class="dropdown-item" @click="handleToggleStickyScroll">
              <Pin :size="14" />
              {{ t('file.header.stickyScroll') }}
              <span v-if="stickyScroll" class="wrap-check">✓</span>
            </button>
            <button v-if="toolbarCollapsedIds.includes('edit')" class="dropdown-item" :class="{ active: editing }" @click="handleToggleEdit">
              <Pencil :size="14" />
              {{ editing ? t('file.header.finishEditing') : t('file.header.edit') }}
            </button>
            <!-- Collapsible extra items (shown inline when space allows) -->
            <button v-if="file.isBinary && toolbarCollapsedIds.includes('openAsText')" class="dropdown-item" @click="handleOpenAsText(); menuOpen = false">
              <Code2 :size="14" />
              {{ t('file.header.openAsText') }}
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
            <button v-if="isMarkdown && effectiveViewMode === 'rendered' && toolbarCollapsedIds.includes('exportHtml')" class="dropdown-item" @click="handleExportHtml(); menuOpen = false">
              <FileOutput :size="14" />
              {{ t('file.header.exportHtml') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('openDirectory')" class="dropdown-item" @click="handleOpenDirectory">
              <FolderOpen :size="14" />
              {{ t('file.header.openDirectory') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('gitHistory')" class="dropdown-item" @click="handleGitHistory">
              <GitBranch :size="14" />
              {{ t('file.header.fileHistory') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('delete')" class="dropdown-item danger" @click="handleDelete(); menuOpen = false">
              <Trash2 :size="14" />
              {{ t('common.delete') }}
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
import { computed, ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { isRefreshing } from '@/composables/useFileRefresh'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { useI18n } from 'vue-i18n'
import { List, Search, MoreVertical, Code2, Download, Trash2, GitBranch, TextWrap, Hash, RotateCw, Pin, X, Paperclip, Share2, FileOutput, Eye, MoveHorizontal, FolderOpen, Pencil } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { useToast } from '@/composables/useToast.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'
import { navToFileInManager } from '@/composables/useFilePathAnnotation.ts'
import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'
import { getNative } from '@/utils/clawbenchNative'

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
})
const emit = defineEmits(['delete', 'toggleView', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'openAsText', 'toggleWordWrap', 'toggleLineNumbers', 'toggleStickyScroll', 'refresh', 'overlayClose', 'shareExternal', 'exportHtml', 'fitWidth', 'toggleEdit'])

const { isAppMode } = useAppMode()
const { t } = useI18n()
const { addAttachedFile, hasAttachedFile, removeAttachedFileByPath } = useChatContext()
const toast = useToast()

const isAttached = computed(() => !!props.file?.path && hasAttachedFile(props.file.path))

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

// Responsive toolbar overflow — only the "More" dropdown is always-inline (1)
const { inlineIds: toolbarInlineIds, collapsedIds: toolbarCollapsedIds, startObserving: startToolbarResize, stopObserving: stopToolbarResize } = useToolbarOverflow(
  () => headerActionsRef.value,
  () => {
    const ids = []
    if (hasToc.value) ids.push('toc')
    if (hasSearch.value) ids.push('search')
    if (hasFitWidth.value) ids.push('fitWidth')
    ids.push('attach')
    if (hasTextContent.value) ids.push('refresh')
    if (hasTextContent.value && !isMediaFile.value && (isMarkdown.value || isHtml.value || isOpenapi.value)) ids.push('toggleView')
    if (hasTextContent.value && !isMediaFile.value && !isMarkdownRendered.value) ids.push('wordWrap')
    if (hasTextContent.value && !isMediaFile.value && !isMarkdownRendered.value) ids.push('lineNumbers')
    if (hasTextContent.value && !isMediaFile.value && !isMarkdownRendered.value) ids.push('stickyScroll')
    if (isEditable.value) ids.push('edit')
    // Extra actions demote to the More dropdown when space runs out.
    // Order = left-to-right display priority; delete is kept last.
    if (props.file?.isBinary) ids.push('openAsText')
    if (isAppMode.value) ids.push('shareExternal')
    ids.push('download')
    if (isMarkdown.value && effectiveViewMode.value === 'rendered') ids.push('exportHtml')
    ids.push('openDirectory')
    ids.push('gitHistory')
    ids.push('delete')
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
// Editable: text/source files in raw view (excludes media).
// Markdown is always editable (even in rendered view) so users can edit the source.
const isEditable = computed(() => {
    if (!hasTextContent.value || isMediaFile.value) return false
    if (isMarkdown.value) return true
    // Other templated types (HTML/OpenAPI) are only editable in source view
    return !isMarkdownRendered.value
})
const hasToc = computed(() => {
    if (!props.file) return false
    const ft = fileType.value
    if (!ft) return false
    // PDF: always show TOC button (outline may be available)
    if (ft.isPdf) return true
    // Other file types: need content
    if (!props.file.content) return false
    if (ft.isImage || ft.isAudio || ft.isVideo || ft.isExcalidraw) return false
    // OpenAPI rendered mode: ReDoc has its own sidebar, TOC/Search would operate on raw text
    if (isOpenapi.value && effectiveViewMode.value === 'rendered') return false
    return true
})

// Search requires file.content — PDF/Office/Excalidraw don't have usable text, hide (not disable) search
const hasSearch = computed(() => {
    if (!props.file) return false
    if (props.file.isPdf || props.file.isOffice || props.file.isExcalidraw) return false
    return hasToc.value
})

// Show reset-zoom button for zoomable file types (PDF, PPT)
const hasFitWidth = computed(() => {
    if (!props.file) return false
    return fileType.value?.isPdf || (fileType.value?.isOffice && props.file.name?.toLowerCase().endsWith('.pptx')) || false
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

function handleRefresh() {
    menuOpen.value = false
    triggerRefresh()
}

function handleAttachToChat() {
    const path = props.file?.path
    if (!path) return
    if (hasAttachedFile(path)) {
        removeAttachedFileByPath(path)
        toast.show(t('chat.attach.removedFromChat'), { icon: '📎', type: 'info', duration: 1500 })
        return
    }
    addAttachedFile(path)
    toast.show(t('chat.attach.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })

    // Fly-to-chat animation — capture button position before any async work
    const btn = attachBtnRef.value
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = btn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null
    if (animFrom && animTo) {
        window.dispatchEvent(new CustomEvent('attach-to-chat', {
            detail: {
                from: { x: animFrom.left + animFrom.width / 2, y: animFrom.top + animFrom.height / 2 },
                to: { x: animTo.left + animTo.width / 2, y: animTo.top + animTo.height / 2 },
            }
        }))
    }
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
    gap: 4px;
    padding: 2px 4px 2px 6px;
    background: var(--bg-secondary);
    border: none;
    font-size: 12px;
    position: sticky;
    top: 0;
    left: 0;
    min-width: 0;
}

/* Region 1: File name — shrinks when toolbar needs space, but has a minimum width */
.file-name-wrap {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: 0 1 auto;
    min-width: 80px;
    max-width: 40%;
    overflow: hidden;
}

.file-path-hint {
    flex: 0 0 auto;
    max-width: 100%;
    color: var(--text-muted);
    font-family: monospace;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
    transition: color 0.15s;
}
@media (hover: hover) {
    .file-path-hint:hover {
        color: var(--accent-color);
    }
}
.file-path-hint.copied {
    color: #22c55e;
}

/* Region 2: Toolbar — takes remaining space, shrinks to trigger overflow */
.header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1 1 0;
    min-width: 0;
    overflow: hidden;
    justify-content: flex-end;
}

.file-header-btn {
    padding: 6px;
    border: none;
    border-radius: 4px;
    background: transparent;
    font-size: 11px;
    cursor: pointer;
    color: var(--text-secondary);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}
@media (hover: hover) {
    .file-header-btn:hover {
        background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
    }
}
.file-header-btn svg {
    width: 14px;
    height: 14px;
}
.file-header-btn:disabled {
    opacity: 0.4;
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
    background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
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
    font-size: 14px;
    font-weight: 700;
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
    z-index: 9999;
    min-width: 140px;
    padding: 4px 0;
    overflow: hidden;
}

.file-header-dropdown-menu .dropdown-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    width: 100%;
    border: none;
    background: none;
    color: var(--text-primary);
    font-size: 13px;
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
    background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
    color: var(--accent-color);
}
.file-header-dropdown-menu .dropdown-item svg {
    flex-shrink: 0;
}
.file-header-dropdown-menu .dropdown-divider {
    height: 1px; background: var(--border-color); margin: 4px 0;
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
    font-size: 14px;
    font-weight: 700;
}
</style>
