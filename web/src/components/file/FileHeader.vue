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
      <button v-if="hasToc && toolbarInlineIds.includes('search')" class="file-header-btn" :class="{ active: searchOpen }" :disabled="!file.content" @click.stop="handleToggleSearch" :title="t('file.header.search')">
        <Search :size="14" />
      </button>

      <!-- Attach to chat button -->
      <button v-if="toolbarInlineIds.includes('attach')" ref="attachBtnRef" class="file-header-btn" :class="{ active: isAttached }" @click.stop="handleAttachToChat" :title="isAttached ? t('chat.attach.removeFromChat') : t('chat.actions.attachToChat')">
        <Paperclip :size="14" />
      </button>

      <!-- Refresh button -->
      <button v-if="toolbarInlineIds.includes('refresh')" class="file-header-btn" @click.stop="handleRefresh" :title="t('nav.refresh')">
        <RotateCw :size="14" />
      </button>

      <!-- Toggle view button (source/rendered) -->
      <button v-if="toolbarInlineIds.includes('toggleView')" class="file-header-btn" @click.stop="handleToggleView" :title="viewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView')">
        <Code2 v-if="viewMode === 'rendered'" :size="14" />
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

      <!-- More actions dropdown -->
      <div class="dropdown-wrapper" ref="dropdownRef">
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
            <button v-if="toolbarCollapsedIds.includes('attach')" class="dropdown-item" :class="{ active: isAttached }" @click="handleAttachToChat(); menuOpen = false">
              <Paperclip :size="14" />
              {{ isAttached ? t('chat.attach.removeFromChat') : t('chat.actions.attachToChat') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('refresh')" class="dropdown-item" @click="handleRefresh">
              <RotateCw :size="14" />
              {{ t('nav.refresh') }}
            </button>
            <button v-if="toolbarCollapsedIds.includes('toggleView')" class="dropdown-item" @click="handleToggleView">
              <Code2 v-if="viewMode === 'rendered'" :size="14" />
              <Eye v-else :size="14" />
              {{ viewMode === 'rendered' ? t('file.header.sourceView') : t('file.header.renderedView') }}
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
            <div v-if="toolbarCollapsedIds.length > 0" class="dropdown-divider" />
            <!-- Always-in-dropdown items -->
            <button v-if="file.isBinary" class="dropdown-item" @click="handleOpenAsText">
              <Code2 :size="14" />
              {{ t('file.header.openAsText') }}
            </button>
            <button v-if="isAppMode" class="dropdown-item" @click="handleShareExternal">
              <Share :size="14" />
              {{ t('file.header.shareExternal') }}
            </button>
            <a v-if="!isAppMode" class="dropdown-item" :href="buildLocalFileUrl(file.path, { download: true })" :download="file.name" @click="menuOpen = false">
              <Download :size="14" />
              {{ t('common.download') }}
            </a>
            <button v-else class="dropdown-item" @click="handleDownload">
              <Download :size="14" />
              {{ t('common.download') }}
            </button>
            <button v-if="isMarkdown && viewMode === 'rendered'" class="dropdown-item" @click="handleExportHtml">
              <FileOutput :size="14" />
              {{ t('file.header.exportHtml') }}
            </button>
            <button class="dropdown-item danger" @click="handleDelete">
              <Trash2 :size="14" />
              {{ t('common.delete') }}
            </button>
            <button class="dropdown-item" @click="handleGitHistory">
              <GitBranch :size="14" />
              {{ t('file.header.fileHistory') }}
            </button>
          </div>
        </Teleport>
      </div>
    </div>

    <!-- Region 3: Overlay nav (back + close, always present, fixed size) -->
    <div class="overlay-nav">
      <div class="overlay-nav-separator"></div>
      <button class="file-header-btn overlay-nav-btn" :disabled="!overlayCanGoBack" @click.stop="$emit('overlayGoBack')" :title="t('file.overlay.back')">
        <ChevronLeft :size="14" />
      </button>
      <button class="file-header-btn overlay-nav-btn overlay-close-btn" @click.stop="$emit('overlayClose')" :title="t('common.close')">
        <X :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { List, Search, MoreVertical, Code2, Download, Trash2, GitBranch, TextWrap, Hash, RotateCw, Pin, ChevronLeft, X, Paperclip, Share, FileOutput, Eye } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { useToast } from '@/composables/useToast.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'
import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'

const props = defineProps({
    file: Object,
    viewMode: String,
    tocOpen: Boolean,
    searchOpen: Boolean,
    wordWrap: Boolean,
    showLineNumbers: Boolean,
    stickyScroll: Boolean,
    overlayOpen: Boolean,
    overlayCanGoBack: Boolean,
})
const emit = defineEmits(['delete', 'toggleView', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'openAsText', 'toggleWordWrap', 'toggleLineNumbers', 'toggleStickyScroll', 'refresh', 'overlayClose', 'overlayGoBack', 'shareExternal', 'exportHtml'])

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

// Responsive toolbar overflow — only the "More" dropdown is always-inline (1)
const { inlineIds: toolbarInlineIds, collapsedIds: toolbarCollapsedIds, startObserving: startToolbarResize, stopObserving: stopToolbarResize } = useToolbarOverflow(
  () => headerActionsRef.value,
  () => {
    const ids = []
    if (hasToc.value) ids.push('toc')
    if (hasToc.value) ids.push('search')
    ids.push('attach')
    ids.push('refresh')
    if (isMarkdown.value || isHtml.value) ids.push('toggleView')
    if (!isMarkdownRendered.value) ids.push('wordWrap')
    if (!isMarkdownRendered.value) ids.push('lineNumbers')
    if (!isMarkdownRendered.value) ids.push('stickyScroll')
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
const isMarkdownRendered = computed(() => (isMarkdown.value || isHtml.value) && props.viewMode === 'rendered')
const hasToc = computed(() => {
    if (!props.file) return false
    const ft = fileType.value
    if (!ft) return false
    // PDF: always show TOC button (outline may be available)
    if (ft.isPdf) return true
    // Other file types: need content
    if (!props.file.content) return false
    if (ft.isImage || ft.isAudio || ft.isVideo) return false
    return true
})

function handleToggleView() {
    menuOpen.value = false
    emit('toggleView')
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
    const native = window.AndroidNative
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
    native.shareFile(path, mimeType)
}

function handleDelete() {
    menuOpen.value = false
    emit('delete', props.file?.path)
}

function handleGitHistory() {
    menuOpen.value = false
    emit('openGitHistory')
}

function handleRefresh() {
    menuOpen.value = false
    emit('refresh')
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
.file-path-hint:hover {
    color: var(--accent-color);
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
.file-header-btn:hover {
    background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
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
.file-header-btn:disabled:hover {
    background: transparent;
    color: var(--text-secondary);
}
.file-header-btn.active {
    background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
    color: var(--accent-color);
}

/* Dropdown */
.dropdown-wrapper {
    position: relative;
}

/* Region 3: Overlay nav — fixed size, never shrinks, always visible */
.overlay-nav {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
}
.overlay-nav-separator {
    width: 1px;
    height: 16px;
    background: var(--border-color);
    margin: 0 2px;
}
.overlay-nav-btn {
    border: none;
    border-radius: 4px;
}
.overlay-nav-btn:hover {
    background: var(--accent-color-dim, rgba(74, 144, 217, 0.12));
    color: var(--accent-color);
}
.overlay-close-btn {
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
.file-header-dropdown-menu .dropdown-item:hover {
    background: var(--accent-color);
    color: #fff;
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
.file-header-dropdown-menu .dropdown-item.danger:hover {
    background: #fef2f2;
    color: #dc2626;
}
[data-theme="dark"] .file-header-dropdown-menu .dropdown-item.danger:hover {
    background: #2d1b1b;
}
.file-header-dropdown-menu .wrap-check {
    margin-left: auto;
    color: var(--accent-color);
    font-size: 14px;
    font-weight: 700;
}
</style>
