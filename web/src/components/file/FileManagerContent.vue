<template>
  <div class="file-manager-content">
    <!-- Dir nav -->
    <div id="dirNav" class="dir-nav">
      <div class="dir-toolbar">
        <div ref="dirToolbarRef" class="dir-toolbar-btns">
          <div class="toolbar-dropdown-wrap">
            <button class="toolbar-btn" :class="{ 'sort-active': sortField }" @click="sortMenuOpen = !sortMenuOpen" :title="t('file.sortDefault')">
              <ArrowDownAz v-if="!sortField || sortDir === 'asc'" :size="16" />
              <ArrowUpZa v-else :size="16" />
            </button>
            <div v-if="sortMenuOpen" class="toolbar-dropdown" @click.stop>
              <button class="toolbar-dropdown-item" :class="{ active: sortField === 'name' }" @click="onSortSelect('name')">
                <ArrowDownAz :size="14" />
                <span>{{ t('file.sortByName') }}</span>
                <ChevronUp v-if="sortField === 'name' && sortDir === 'asc'" :size="12" class="sort-dir-icon" />
                <ChevronDown v-else-if="sortField === 'name' && sortDir === 'desc'" :size="12" class="sort-dir-icon" />
              </button>
              <button class="toolbar-dropdown-item" :class="{ active: sortField === 'time' }" @click="onSortSelect('time')">
                <Clock :size="14" />
                <span>{{ t('file.sortByTime') }}</span>
                <ChevronUp v-if="sortField === 'time' && sortDir === 'asc'" :size="12" class="sort-dir-icon" />
                <ChevronDown v-else-if="sortField === 'time' && sortDir === 'desc'" :size="12" class="sort-dir-icon" />
              </button>
              <button class="toolbar-dropdown-item" :class="{ active: sortField === 'type' }" @click="onSortSelect('type')">
                <FileText :size="14" />
                <span>{{ t('file.sortByType') }}</span>
                <ChevronUp v-if="sortField === 'type' && sortDir === 'asc'" :size="12" class="sort-dir-icon" />
                <ChevronDown v-else-if="sortField === 'type' && sortDir === 'desc'" :size="12" class="sort-dir-icon" />
              </button>
              <button class="toolbar-dropdown-item" :class="{ active: sortField === 'size' }" @click="onSortSelect('size')">
                <HardDrive :size="14" />
                <span>{{ t('file.sortBySize') }}</span>
                <ChevronUp v-if="sortField === 'size' && sortDir === 'asc'" :size="12" class="sort-dir-icon" />
                <ChevronDown v-else-if="sortField === 'size' && sortDir === 'desc'" :size="12" class="sort-dir-icon" />
              </button>
            </div>
          </div>
          <button v-if="toolbarInlineIds.includes('hidden')" class="toolbar-btn" @click="$emit('toggleHidden')" :title="showHidden ? t('file.hideHiddenFiles') : t('file.showHiddenFiles')">
            <EyeOff v-if="!showHidden" :size="16" />
            <Eye v-else :size="16" />
          </button>
          <button v-if="toolbarInlineIds.includes('refresh')" class="toolbar-btn" @click="$emit('refresh')" :title="t('nav.refresh')">
            <RotateCw :size="16" />
          </button>
          <button v-if="toolbarInlineIds.includes('multiselect')" class="toolbar-btn" :class="{ active: multiSelect.active }" @click="multiSelect.active ? exitMultiSelect() : enterMultiSelect()" :title="multiSelect.active ? t('file.multiSelect.exit') : t('file.multiSelect.enter')">
            <CheckSquare :size="16" />
          </button>
          <button v-if="toolbarInlineIds.includes('newFile')" class="toolbar-btn" @click="doNewFile()" :title="t('file.context.newFile')">
            <FilePlus :size="16" />
          </button>
          <button v-if="toolbarInlineIds.includes('upload')" class="toolbar-btn" :disabled="dirUploading" @click="triggerUpload()" :title="t('file.uploadHere')">
            <Upload :size="16" />
          </button>
          <button v-if="toolbarInlineIds.includes('viewToggle')" class="toolbar-btn" @click="viewMode = viewMode === 'grid' ? 'list' : 'grid'" :title="viewMode === 'grid' ? t('file.viewList') : t('file.viewGrid')">
            <LayoutGrid v-if="viewMode === 'list'" :size="16" />
            <LayoutList v-else :size="16" />
          </button>
          <div class="toolbar-dropdown-wrap">
            <button class="toolbar-btn" @click="moreMenuOpen = !moreMenuOpen" :title="t('nav.more')">
              <MoreHorizontal :size="16" />
            </button>
            <div v-if="moreMenuOpen" class="toolbar-dropdown toolbar-dropdown-right" @click.stop>
              <template v-if="toolbarCollapsedIds.includes('hidden')">
                <button class="toolbar-dropdown-item" @click="$emit('toggleHidden'); moreMenuOpen = false">
                  <EyeOff v-if="!showHidden" :size="14" />
                  <Eye v-else :size="14" />
                  <span>{{ showHidden ? t('file.hideHiddenFiles') : t('file.showHiddenFiles') }}</span>
                </button>
              </template>
              <template v-if="toolbarCollapsedIds.includes('refresh')">
                <button class="toolbar-dropdown-item" @click="$emit('refresh'); moreMenuOpen = false">
                  <RotateCw :size="14" />
                  <span>{{ t('nav.refresh') }}</span>
                </button>
              </template>
              <template v-if="toolbarCollapsedIds.includes('multiselect')">
                <button class="toolbar-dropdown-item" @click="multiSelect.active ? exitMultiSelect() : enterMultiSelect(); moreMenuOpen = false">
                  <CheckSquare :size="14" />
                  <span>{{ multiSelect.active ? t('file.multiSelect.exit') : t('file.multiSelect.enter') }}</span>
                </button>
              </template>
              <div v-if="toolbarCollapsedIds.length > 0" class="toolbar-dropdown-divider" />
              <template v-if="toolbarCollapsedIds.includes('newFile')">
                <button class="toolbar-dropdown-item" @click="doNewFile(); moreMenuOpen = false">
                  <FilePlus :size="14" />
                  <span>{{ t('file.context.newFile') }}</span>
                </button>
              </template>
              <button class="toolbar-dropdown-item" @click="doNewFolder(); moreMenuOpen = false">
                <FolderPlus :size="14" />
                <span>{{ t('file.context.newFolder') }}</span>
              </button>
              <div class="toolbar-dropdown-divider" />
              <template v-if="toolbarCollapsedIds.includes('upload')">
                <button class="toolbar-dropdown-item" :disabled="dirUploading" @click="triggerUpload(); moreMenuOpen = false">
                  <Upload :size="14" />
                  <span>{{ t('file.uploadHere') }}</span>
                </button>
              </template>
              <template v-if="toolbarCollapsedIds.includes('viewToggle')">
                <button class="toolbar-dropdown-item" @click="viewMode = viewMode === 'grid' ? 'list' : 'grid'; moreMenuOpen = false">
                  <LayoutGrid v-if="viewMode === 'list'" :size="14" />
                  <LayoutList v-else :size="14" />
                  <span>{{ viewMode === 'grid' ? t('file.viewList') : t('file.viewGrid') }}</span>
                </button>
              </template>
            </div>
          </div>
        </div>
        <SearchInput v-model="searchQuery" :placeholder="t('search.defaultPlaceholder')" @dblclick="searchQuery = ''" />
      </div>
      <!-- Multi-select info bar -->
      <div v-if="multiSelect.active" class="ms-info-bar">
        <button class="ms-info-btn" @click="exitMultiSelect">
          <X :size="14" />
        </button>
        <span class="ms-info-text">{{ multiSelect.selected.size > 0 ? t('file.multiSelect.selectedCount', { n: multiSelect.selected.size }) : t('file.multiSelect.tapToSelect') }}</span>
        <button class="ms-info-btn ms-select-all-btn" @click="toggleSelectAll">
          {{ isAllSelected ? t('file.multiSelect.deselectAll') : t('file.multiSelect.selectAll') }}
        </button>
      </div>
      <DirBreadcrumb v-else :path="currentDir" @navigate="$emit('navigateDir', $event)" />
    </div>

    <!-- Hidden file input for upload -->
    <input type="file" ref="uploadInputRef" @change="onUploadFileSelect" style="display:none" multiple />

    <!-- Upload progress bar -->
    <div v-if="dirUploading" class="dir-upload-progress">
      <div class="dir-upload-progress-bar" :style="{ width: dirUploadProgress + '%' }"></div>
      <span class="dir-upload-progress-text">{{ dirUploadDone }}/{{ dirUploadTotal }}</span>
    </div>

    <!-- File list -->
    <div v-if="viewMode === 'list'" class="file-list" id="fileList"
      @click="handleItemClick"
      @contextmenu.prevent="handleCtxMenu"
      v-long-press="onContainerLongPress"
    >
      <Transition name="loading-fade">
        <div v-if="dirLoading" class="loading-mask">
          <div class="loading-mask-spinner"></div>
        </div>
      </Transition>
      <div v-if="filteredEntries.length === 0 && !dirLoading" class="empty-state">
        <Folder :size="48" />
        <p>{{ currentDir ? t('file.emptyDir') : t('file.noFiles') }}</p>
      </div>

      <template v-for="entry in visibleEntries" :key="entry.name">
        <!-- Directory -->
        <div v-if="entry.type === 'dir'"
          v-long-press="(e) => onLongPress(entry, e)"
          class="file-item dir-item"
          :class="{
            'ms-selected': multiSelect.active && multiSelect.selected.has(itemPath(entry.name)),
            'ctx-highlight': ctxMenu.visible && ctxMenu.entry?.path === itemPath(entry.name),
            'cut-item': isCutItem(itemPath(entry.name))
          }"
          :data-action="'dir'"
          :data-path="itemPath(entry.name)"
        >
          <div v-if="multiSelect.active" class="ms-check" :class="{ checked: multiSelect.selected.has(itemPath(entry.name)) }">
            <Check v-if="multiSelect.selected.has(itemPath(entry.name))" :size="12" />
          </div>
          <div class="file-icon-wrap" :class="{ 'has-attach': hasAttachedFile(itemPath(entry.name)) }">
            <Folder class="file-icon" :size="28" />
            <Paperclip v-if="hasAttachedFile(itemPath(entry.name))" class="attach-badge" :size="15" @click.stop="toggleAttach(itemPath(entry.name))" />
          </div>
          <span class="file-name">{{ entry.name }}</span>
          <span class="file-meta">{{ formatDate(entry.modified) }}</span>
        </div>

        <!-- File -->
        <div v-else
          v-long-press="(e) => onLongPress(entry, e)"
          class="file-item"
          :class="{
            active: !multiSelect.active && currentFile?.path === itemPath(entry.name),
            'ms-selected': multiSelect.active && multiSelect.selected.has(itemPath(entry.name)),
            'ctx-highlight': ctxMenu.visible && ctxMenu.entry?.path === itemPath(entry.name),
            'cut-item': isCutItem(itemPath(entry.name))
          }"
          :data-action="'file'"
          :data-path="itemPath(entry.name)"
        >
          <div v-if="multiSelect.active" class="ms-check" :class="{ checked: multiSelect.selected.has(itemPath(entry.name)) }">
            <Check v-if="multiSelect.selected.has(itemPath(entry.name))" :size="12" />
          </div>
          <div class="file-icon-wrap" :class="{ 'has-attach': hasAttachedFile(itemPath(entry.name)) }">
            <img v-if="isThumbLoaded(entry)" class="file-thumb" :src="thumbUrl(entry)" :alt="entry.name" loading="lazy" @error="onThumbError(entry)" />
            <FileImage v-else-if="isImage(entry)" class="file-icon" :size="28" color="#a855f7" />
            <FileMusic v-else-if="isAudio(entry)" class="file-icon" :size="28" color="#22c55e" />
            <FileText v-else class="file-icon" :size="28" :color="getFileType(entry.name).color" />
            <Paperclip v-if="hasAttachedFile(itemPath(entry.name))" class="attach-badge" :size="15" @click.stop="toggleAttach(itemPath(entry.name))" />
          </div>
          <span class="file-name">{{ entry.name }}</span>
          <span class="file-meta">{{ formatFileSize(entry.size) }} · {{ formatDate(entry.modified) }}</span>
        </div>
      </template>
      <div v-if="hasMoreEntries" class="truncate-hint">
        {{ t('file.truncateHint', { max: MAX_VISIBLE_ENTRIES, total: filteredEntries.length }) }}
      </div>
    </div>

    <!-- File grid -->
    <div v-else class="file-grid" id="fileList"
      @click="handleItemClick"
      @contextmenu.prevent="handleCtxMenu"
      v-long-press="onContainerLongPress"
    >
      <Transition name="loading-fade">
        <div v-if="dirLoading" class="loading-mask">
          <div class="loading-mask-spinner"></div>
        </div>
      </Transition>
      <div v-if="filteredEntries.length === 0 && !dirLoading" class="empty-state">
        <Folder :size="48" />
        <p>{{ currentDir ? t('file.emptyDir') : t('file.noFiles') }}</p>
      </div>

      <div v-for="entry in visibleEntries" :key="entry.name"
        v-long-press="(e) => onLongPress(entry, e)"
        class="grid-item"
        :class="{
          'grid-dir': entry.type === 'dir',
          'grid-active': !multiSelect.active && entry.type !== 'dir' && currentFile?.path === itemPath(entry.name),
          'ms-selected': multiSelect.active && multiSelect.selected.has(itemPath(entry.name)),
          'ctx-highlight': ctxMenu.visible && ctxMenu.entry?.path === itemPath(entry.name),
          'cut-item': isCutItem(itemPath(entry.name))
        }"
        :data-action="entry.type === 'dir' ? 'dir' : 'file'"
        :data-path="itemPath(entry.name)"
      >
        <div v-if="multiSelect.active" class="grid-ms-check" :class="{ checked: multiSelect.selected.has(itemPath(entry.name)) }">
          <Check v-if="multiSelect.selected.has(itemPath(entry.name))" :size="12" />
        </div>
        <div class="grid-thumb" :class="{ 'has-attach': hasAttachedFile(itemPath(entry.name)) }">
          <img v-if="isThumbLoaded(entry)" :src="thumbUrl(entry)" :alt="entry.name" loading="lazy" @error="onThumbError(entry)" />
          <component v-else :is="entryIcon(entry)" class="grid-icon" :size="32" :color="entryIconColor(entry)" />
          <Paperclip v-if="hasAttachedFile(itemPath(entry.name))" class="attach-badge" :size="15" @click.stop="toggleAttach(itemPath(entry.name))" />
        </div>
        <div class="grid-name">{{ entry.name }}</div>
      </div>
      <div v-if="hasMoreEntries" class="truncate-hint">
        {{ t('file.truncateHint', { max: MAX_VISIBLE_ENTRIES, total: filteredEntries.length }) }}
      </div>
    </div>

    <!-- Multi-select bottom action bar -->
    <div v-if="multiSelect.active && multiSelect.selected.size > 0" class="ms-action-bar">
      <button class="ms-action-btn" @click="doBatchCopy">
        <Copy :size="14" />
        {{ t('file.context.copy') }}
      </button>
      <button class="ms-action-btn" @click="doBatchCut">
        <Scissors :size="14" />
        {{ t('file.context.cut') }}
      </button>
      <button class="ms-action-btn" @click="doBatchArchive">
        <Package :size="14" />
        {{ t('file.multiSelect.archive') }}
      </button>
      <button class="ms-action-btn ms-danger" @click="doBatchDelete">
        <Trash2 :size="14" />
        {{ t('common.delete') }}
      </button>
    </div>

    <!-- Context menu -->
    <Teleport to="body">
      <div v-if="ctxMenu.visible" class="context-menu visible" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }" @click.stop>
        <!-- Group 1: Clipboard operations -->
        <template v-if="ctxMenu.entry">
          <div class="context-menu-item" @click.stop="doCopy">
            <Copy :size="14" />
            {{ t('file.context.copy') }}
          </div>
          <div class="context-menu-item" @click.stop="doCut">
            <Scissors :size="14" />
            {{ t('file.context.cut') }}
          </div>
        </template>
        <div class="context-menu-item" :class="{ disabled: !clipboard.entries.length }" @click.stop="clipboard.entries.length && doPaste()">
          <ClipboardPaste :size="14" />
          {{ t('file.context.paste') }}
        </div>
        <!-- New file/folder when no entry selected (empty area) -->
        <template v-if="!ctxMenu.entry">
          <div class="context-menu-divider" />
          <div class="context-menu-item" @click.stop="doNewFile">
            <FilePlus :size="14" />
            {{ t('file.context.newFile') }}
          </div>
          <div class="context-menu-item" @click.stop="doNewFolder">
            <FolderPlus :size="14" />
            {{ t('file.context.newFolder') }}
          </div>
        </template>
        <!-- Group 2: Entry actions -->
        <template v-if="ctxMenu.entry">
          <div class="context-menu-divider" />
          <div class="context-menu-item" @click.stop="doRename">
            <Pencil :size="14" />
            {{ t('common.rename') }}
          </div>
          <div class="context-menu-item" v-if="ctxMenu.entry.type !== 'dir'" @click.stop="doDownload">
            <Download :size="14" />
            {{ t('common.download') }}
          </div>
          <div class="context-menu-item" v-if="ctxMenu.entry.type === 'dir'" @click.stop="doArchiveDir">
            <Package :size="14" />
            {{ t('file.context.archiveDir') }}
          </div>
          <div class="context-menu-item" @click.stop="doAttachToChat">
            <Paperclip :size="14" />
            {{ ctxMenu.entry && hasAttachedFile(ctxMenu.entry.path) ? t('chat.attach.removeFromChat') : t('chat.actions.attachToChat') }}
          </div>
          <div class="context-menu-item danger" @click.stop="doDelete">
            <Trash2 :size="14" />
            {{ t('common.delete') }}
          </div>
          <div class="context-menu-item" v-if="ctxMenu.entry.type === 'dir'" @click.stop="doOpenAsProject">
            <FolderOpen :size="14" />
            {{ t('file.context.openAsProject') }}
          </div>
        </template>
        <!-- Group 4: Terminal -->
        <template v-if="!isTerminalDisabled">
          <div class="context-menu-divider" />
          <div class="context-menu-item" @click.stop="doOpenTerminal">
            <TerminalIcon :size="14" />
            {{ t('file.context.openTerminal') }}
          </div>
        </template>
      </div>
      <div v-if="ctxMenu.visible" class="ctx-overlay" @click="closeCtxMenu" />
    </Teleport>
  </div>
</template>

<script setup>
import '@/assets/loading-mask.css'
import { ref, computed, reactive, inject, nextTick, onMounted, onUnmounted, Teleport, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'
import { joinPath } from '@/utils/path'
import { Folder, ArrowDownAz, ArrowUpZa, ChevronDown, ChevronUp, Clock, FileText, HardDrive, Eye, EyeOff, FileImage, FileMusic, Copy, Scissors, ClipboardPaste, FilePlus, FolderPlus, Pencil, Download, Trash2, FolderOpen, RotateCw, Terminal as TerminalIcon, CheckSquare, Check, X, LayoutList, LayoutGrid, FileVideo, Package, Upload, MoreHorizontal, Paperclip } from 'lucide-vue-next'
import { getFileType } from '@/utils/fileType.ts'
import {
  buildThumbUrl,
  isImage as isImageEntry, isAudio as isAudioEntry, isVideo as isVideoEntry,
  isThumbable as isThumbableEntry, formatSize as formatFileSize, THUMBABLE_EXTS,
  createMultiSelect as _createMultiSelect, createClipboard as _createClipboard,
  resolveClickAction,
} from '@/utils/fileManager.ts'
import { store } from '@/stores/app.ts'
import { localConfig, setLocalConfig, useSettingsConfig, getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { useTerminalStatus } from '@/composables/useTerminalStatus.ts'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { useFileUpload } from '@/composables/useFileUpload.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { downloadFileByPath } from '@/utils/download.ts'
import { useToast } from '@/composables/useToast.ts'
import { useFileNavStack } from '@/composables/useFileNavStack'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'
import SearchInput from '@/components/common/SearchInput.vue'
import DirBreadcrumb from './DirBreadcrumb.vue'

const toast = inject('toast', null)
const { isAppMode } = useAppMode()
const { t, locale } = useI18n()
const TAG = 'FileManager'

// File upload to current directory
const { dirUploading, dirUploadProgress, dirUploadTotal, dirUploadDone, handleFileSelectToDir } = useFileUpload()
const uploadInputRef = ref(null)

function triggerUpload() {
  uploadInputRef.value?.click()
}

async function onUploadFileSelect(e) {
  await handleFileSelectToDir(e, props.currentDir || '.')
  // Refresh directory listing after uploads complete
  emit('refresh')
}
const dialog = useDialog()
const { addAttachedFile, hasAttachedFile, removeAttachedFileByPath } = useChatContext()
const { terminalRuntimeEnabled } = useTerminalStatus()
const isTerminalDisabled = computed(() => terminalRuntimeEnabled.value !== true)

const activeTab = inject('activeTab', ref(''))

const fileNav = useFileNavStack()

// Register back handler for file browser directory navigation
// PRIORITY_PAGE < PRIORITY_OVERLAY, so file-overlay always wins when open.
// The !overlayOpen guard is redundant with priority but makes intent explicit.
// canGoBack: true when not at project root (currentDir !== '')
useFeatureBackHandler(
  'browse',
  () => activeTab.value === 'browse' && !fileNav.overlayOpen.value && props.currentDir !== '',
  () => emit('navigateBack'),
  PRIORITY_PAGE,
)

const props = defineProps({
    entries: Array,
    currentDir: String,
    currentFile: Object,
    showHidden: Boolean,
    sortField: String,
    sortDir: String,
    dirLoading: Boolean,
})

const emit = defineEmits(['navigateDir', 'navigateBack', 'selectFile', 'toggleSort', 'toggleHidden', 'rename', 'delete', 'refresh', 'openTerminal', 'batchDelete'])


const searchQuery = ref('')
const sortMenuOpen = ref(false)
const moreMenuOpen = ref(false)

// Responsive toolbar overflow
const dirToolbarRef = ref(null)
const { inlineIds: toolbarInlineIds, collapsedIds: toolbarCollapsedIds, startObserving: startToolbarResize, stopObserving: stopToolbarResize } = useToolbarOverflow(
  () => dirToolbarRef.value,
  () => ['hidden', 'refresh', 'multiselect', 'newFile', 'upload', 'viewToggle'],
  { inlineCount: 2, gap: 6 },
)

// ── View mode (list / grid) from settings config ──
const viewMode = ref(localConfig.fileView || 'list')
watch(viewMode, v => setLocalConfig('fileView', v))

// ── Thumbnail loading errors ──
const thumbErrors = reactive(new Set())
function thumbUrl(entry) {
    return buildThumbUrl(props.currentDir || '', entry.name)
}
function onThumbError(entry) {
    thumbErrors.add(entry.name)
}
// Extensions that the backend thumbnail API can decode (Go stdlib: png, jpg, gif).
// SVG, WebP, AVIF, PDF, BMP, TIFF are excluded — they'll cause a 404 round-trip if attempted.

function isThumbable(entry) {
    return isThumbableEntry(entry)
}

function isThumbLoaded(entry) {
    return isThumbable(entry) && !thumbErrors.has(entry.name)
}
function entryIcon(entry) {
    if (entry.type === 'dir') return Folder
    if (isImageEntry(entry)) return FileImage
    if (isAudioEntry(entry)) return FileMusic
    if (isVideoEntry(entry)) return FileVideo
    return FileText
}
function entryIconColor(entry) {
    if (entry.type === 'dir') return undefined
    if (isImageEntry(entry)) return '#a855f7'
    if (isAudioEntry(entry)) return '#22c55e'
    if (isVideoEntry(entry)) return '#ef4444'
    return getFileType(entry.name).color
}
function isVideo(entry) {
    return isVideoEntry(entry)
}

function onSortSelect(field) {
  emit('toggleSort', field)
  sortMenuOpen.value = false
}

function closeDropdowns(e) {
  if (!e.target.closest('.toolbar-dropdown-wrap')) {
    sortMenuOpen.value = false
    moreMenuOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', closeDropdowns)
  document.addEventListener('keydown', handleKeydown)
  startToolbarResize()
})
onUnmounted(() => {
  document.removeEventListener('click', closeDropdowns)
  document.removeEventListener('keydown', handleKeydown)
  stopToolbarResize()
})

// Helper: build item path from entry name
function itemPath(name) {
    return joinPath(props.currentDir, name)
}

// ── Multi-select ──
const { state: multiSelect, enterMultiSelect, exitMultiSelect, toggleSelect } = _createMultiSelect()

defineExpose({
    multiSelectState: multiSelect,
    searchQuery,
    viewMode,
    // Test helper: set searchQuery and trigger reactivity
    _setSearchQuery(val) { searchQuery.value = val },
    _setViewMode(val) { viewMode.value = val },
    _getFilteredEntries() { return filteredEntries.value },
})

const isAllSelected = computed(() => {
    if (!multiSelect.active || visibleEntries.value.length === 0) return false
    return visibleEntries.value.every(e => multiSelect.selected.has(itemPath(e.name)))
})

function toggleSelectAll() {
    if (isAllSelected.value) {
        // Deselect all visible
        visibleEntries.value.forEach(e => multiSelect.selected.delete(itemPath(e.name)))
    } else {
        // Select all visible
        visibleEntries.value.forEach(e => multiSelect.selected.add(itemPath(e.name)))
    }
}

// Auto-exit multi-select on directory change
watch(() => props.currentDir, () => {
    searchQuery.value = ''
    if (multiSelect.active) exitMultiSelect()
    thumbErrors.clear()
})

const ctxMenu = reactive({ visible: false, x: 0, y: 0, entry: null })

function closeCtxMenu() {
    ctxMenu.visible = false
    ctxMenu.entry = null
}

// ── Unified context menu trigger (right-click + long-press) ──

function onLongPress(entry, e) {
    const touch = e.touches[0]
    ctxMenu.x = toFixedCSS(touch.clientX)
    ctxMenu.y = toFixedCSS(touch.clientY + 10)
    // DirEntry from v-for has no .path — compute it like handleCtxMenu does
    ctxMenu.entry = { type: entry.type, name: entry.name, path: itemPath(entry.name) }
    ctxMenu.visible = true
    nextTick(() => clampCtxMenu())
}

function onContainerLongPress(e) {
    // Ignore if touch originated on a file/dir item — child v-long-press handles it
    if (e.target?.closest('.file-item, .grid-item')) return
    // Long-press on empty area — show menu without entry (paste, new file/folder, terminal)
    const touch = e.touches[0]
    ctxMenu.x = toFixedCSS(touch.clientX)
    ctxMenu.y = toFixedCSS(touch.clientY + 10)
    ctxMenu.entry = null
    ctxMenu.visible = true
    nextTick(() => clampCtxMenu())
}

function handleCtxMenu(e) {
    const item = e.target?.closest('.file-item, .grid-item')
    ctxMenu.x = toFixedCSS(e.clientX)
    ctxMenu.y = toFixedCSS(e.clientY)
    if (!item) {
        ctxMenu.entry = null
        ctxMenu.visible = true
        nextTick(() => clampCtxMenu())
        return
    }
    const action = item.dataset.action
    const path = item.dataset.path
    const name = item.querySelector('.file-name, .grid-name')?.textContent || ''
    ctxMenu.entry = { type: action === 'dir' ? 'dir' : 'file', name, path }
    ctxMenu.visible = true
    nextTick(() => clampCtxMenu())
}

// Clipboard now supports multiple entries
const { clipboard, clear: clearClipboard } = _createClipboard()

// Check if a given path is in clipboard as cut (for visual half-transparent effect)
const cutPaths = computed(() => {
  if (!clipboard.isCut || !clipboard.entries.length) return null
  return new Set(clipboard.entries.map(e => e.path))
})
function isCutItem(path) {
  return cutPaths.value?.has(path) ?? false
}

function getDestDir(entry) {
    if (!entry) return props.currentDir.replace(/^\/+/, '')
    if (entry.type === 'dir') return entry.path
    const idx = entry.path.lastIndexOf('/')
    return idx > 0 ? entry.path.slice(0, idx) : ''
}

async function doCopy() {
    clipboard.entries = [ctxMenu.entry]
    clipboard.isCut = false
    appLog.d(TAG, '[doCopy] entry:', ctxMenu.entry?.path)
    closeCtxMenu()
    if (toast) toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
}

async function doCut() {
    clipboard.entries = [ctxMenu.entry]
    clipboard.isCut = true
    appLog.d(TAG, '[doCut] entry:', ctxMenu.entry?.path)
    closeCtxMenu()
    if (toast) toast.show(t('file.toast.cutDone'), { icon: '✂️', type: 'success', duration: 1500 })
}

async function doPaste() {
    if (!clipboard.entries.length) return
    const entry = ctxMenu.entry
    closeCtxMenu()
    const destDir = getDestDir(entry)
    const api = clipboard.isCut ? '/api/file/move' : '/api/file/copy'
    appLog.d(TAG, '[doPaste] api:', api, 'destDir:', destDir, 'entries:', clipboard.entries.map(e => e.path))
    let allOk = true
    for (const srcEntry of clipboard.entries) {
        try {
            let destPath = (destDir ? destDir + '/' : '') + srcEntry.name
            // Cut to same location is a no-op — skip the API call
            if (clipboard.isCut && srcEntry.path === destPath) {
                appLog.d(TAG, '[doPaste] same-path no-op, skipping:', srcEntry.path)
                continue
            }
            appLog.d(TAG, '[doPaste] moving:', srcEntry.path, '→', destPath)
            let resp = await fetch(api, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: srcEntry.path, dest: destPath }),
            })
            if (resp.status === 409) {
                const newName = await dialog.prompt(t('file.prompt.pasteNewName', { name: srcEntry.name }), { value: srcEntry.name })
                if (!newName || !newName.trim()) continue
                destPath = (destDir ? destDir + '/' : '') + newName.trim()
                resp = await fetch(api, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ path: srcEntry.path, dest: destPath }),
                })
            }
            if (!resp.ok) {
                const errBody = await resp.text().catch(() => '')
                appLog.e(TAG, '[doPaste] API error:', resp.status, errBody, 'src:', srcEntry.path, 'dest:', destPath)
                allOk = false
            }
        } catch (err) {
            appLog.e(TAG, '[doPaste] exception:', err, 'src:', srcEntry.path)
            allOk = false
        }
    }
    // Only clear clipboard on successful cut-paste; on failure keep entries so user can retry
    if (clipboard.isCut && allOk) {
        // If the currently viewed file was moved, clear it to avoid
        // refreshCurrentFile hitting 404 and showing "file not found"
        const currentFilePath = store.state.currentFile?.path
        if (currentFilePath && clipboard.entries.some(e => e.path === currentFilePath)) {
            store.state.currentFile = null
        }
        clipboard.entries = []
    }
    emit('refresh')
    if (allOk) {
        if (toast) toast.show(clipboard.isCut ? t('file.toast.moved') : t('common.copied'), { icon: '✅', type: 'success', duration: 1500 })
    } else {
        if (toast) toast.show(t('common.operationFailed'), { icon: '❌', type: 'error', duration: 2000 })
    }
}

async function doNewFile() {
    const entry = ctxMenu.entry
    closeCtxMenu()
    moreMenuOpen.value = false
    const name = await dialog.prompt(t('file.prompt.fileName'))
    if (!name || !name.trim()) return
    const dir = getDestDir(entry)
    try {
        const resp = await fetch('/api/file/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: dir, name: name.trim() }),
        })
        if (resp.ok) {
            emit('refresh')
            if (toast) toast.show(t('file.toast.fileCreated'), { icon: '📄', type: 'success', duration: 1500 })
        } else {
            const err = await resp.json()
            if (toast) toast.show(t('file.toast.createFailedDetail', { error: err.error || '' }), { icon: '❌', type: 'error', duration: 2000 })
        }
    } catch (err) {
        if (toast) toast.show(t('file.toast.createFailed'), { icon: '❌', type: 'error', duration: 2000 })
    }
}

async function doNewFolder() {
    const entry = ctxMenu.entry
    closeCtxMenu()
    moreMenuOpen.value = false
    const name = await dialog.prompt(t('file.prompt.folderName'))
    if (!name || !name.trim()) return
    const dir = getDestDir(entry)
    try {
        const resp = await fetch('/api/dir/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: dir, name: name.trim() }),
        })
        if (resp.ok) {
            emit('refresh')
            if (toast) toast.show(t('file.toast.folderCreated'), { icon: '📁', type: 'success', duration: 1500 })
        } else {
            const err = await resp.json()
            if (toast) toast.show(t('file.toast.createFailedDetail', { error: err.error || '' }), { icon: '❌', type: 'error', duration: 2000 })
        }
    } catch (err) {
        if (toast) toast.show(t('file.toast.createFailed'), { icon: '❌', type: 'error', duration: 2000 })
    }
}

// ── Batch operations (multi-select) ──

function doBatchCopy() {
    const entries = [...multiSelect.selected].map(path => {
        const name = path.split('/').pop()
        const entry = props.entries.find(e => e.name === name)
        return entry ? { ...entry, path } : null
    }).filter(Boolean)
    clipboard.entries = entries
    clipboard.isCut = false
    if (toast) toast.show(t('file.multiSelect.allCopied', { n: entries.length }), { icon: '📋', type: 'success', duration: 1500 })
}

function doBatchCut() {
    const entries = [...multiSelect.selected].map(path => {
        const name = path.split('/').pop()
        const entry = props.entries.find(e => e.name === name)
        return entry ? { ...entry, path } : null
    }).filter(Boolean)
    clipboard.entries = entries
    clipboard.isCut = true
    if (toast) toast.show(t('file.multiSelect.allCut', { n: entries.length }), { icon: '✂️', type: 'success', duration: 1500 })
}

async function doBatchDelete() {
    const paths = [...multiSelect.selected]
    if (!paths.length) return
    const confirmed = await dialog.confirm(t('file.multiSelect.confirmDelete', { n: paths.length }), { dangerous: true })
    if (!confirmed) return
    emit('batchDelete', paths)
    exitMultiSelect()
}

const MAX_VISIBLE_ENTRIES = 1000

const filteredEntries = computed(() => {
    let entries = [...props.entries]
    if (!props.showHidden) entries = entries.filter(e => !e.name.startsWith('.'))
    const q = searchQuery.value.toLowerCase()
    if (q) entries = entries.filter(e => e.name.toLowerCase().includes(q))
    if (props.sortField) {
        entries = entries.sort((a, b) => {
            // When sorting by type, directories participate normally
            // When sorting by size, directories go to the end
            // When sorting by name/time, directories float to top
            if (props.sortField === 'size') {
                if (a.type === 'dir' && b.type !== 'dir') return 1
                if (a.type !== 'dir' && b.type === 'dir') return -1
            } else if (props.sortField !== 'type') {
                if (a.type === 'dir' && b.type !== 'dir') return -1
                if (a.type !== 'dir' && b.type === 'dir') return 1
            }
            let cmp = 0
            if (props.sortField === 'name') cmp = a.name.localeCompare(b.name)
            else if (props.sortField === 'time') cmp = new Date(a.modified) - new Date(b.modified)
            else if (props.sortField === 'size') {
                const sizeA = a.size ?? 0
                const sizeB = b.size ?? 0
                cmp = sizeA - sizeB
                if (cmp === 0) cmp = a.name.localeCompare(b.name)
            }
            else if (props.sortField === 'type') {
                const extA = a.name.includes('.') ? a.name.split('.').pop().toLowerCase() : ''
                const extB = b.name.includes('.') ? b.name.split('.').pop().toLowerCase() : ''
                cmp = extA < extB ? -1 : extA > extB ? 1 : 0
                if (cmp === 0) cmp = a.name < b.name ? -1 : a.name > b.name ? 1 : 0
            }
            return props.sortDir === 'asc' ? cmp : -cmp
        })
    }
    return entries
})

const hasMoreEntries = computed(() => filteredEntries.value.length > MAX_VISIBLE_ENTRIES)
const visibleEntries = computed(() => filteredEntries.value.slice(0, MAX_VISIBLE_ENTRIES))

function handleItemClick(e) {
    if (props.dirLoading) return
    const item = e.target.closest('.file-item, .grid-item')
    if (!item) return
    const action = item.dataset.action
    const path = item.dataset.path

    // Multi-select mode: toggle selection on click
    if (multiSelect.active) {
        toggleSelect(path)
        return
    }

    if (action === 'dir') {
        emit('navigateDir', path)
    } else {
        emit('selectFile', path)
    }
}

function isImage(entry) {
    return isImageEntry(entry)
}

function isAudio(entry) {
    return isAudioEntry(entry)
}

function formatDate(modified) {
    if (!modified) return ''
    const d = new Date(modified)
    const isToday = d.toDateString() === new Date().toDateString()
    const loc = locale.value === 'zh' ? 'zh-CN' : 'en-US'
    return isToday
        ? d.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit' })
        : d.toLocaleDateString(loc, { month: '2-digit', day: '2-digit' })
}

// Clamp menu position to stay within viewport on all sides
function clampCtxMenu() {
    const menu = document.querySelector('.context-menu.visible')
    if (!menu) return
    const w = menu.offsetWidth
    const h = menu.offsetHeight
    const vp = getZoomedViewport()
    // Add small padding from edges
    const pad = 8
    const vpW = toFixedCSS(vp.width)
    const vpH = toFixedCSS(vp.height)
    ctxMenu.x = Math.max(pad, Math.min(ctxMenu.x, vpW - w - pad))
    ctxMenu.y = Math.max(pad, Math.min(ctxMenu.y, vpH - h - pad))
}

function doOpenAsProject() {
    if (!ctxMenu.entry || ctxMenu.entry.type !== 'dir') return
    const entryPath = ctxMenu.entry.path
    closeCtxMenu()
    const absPath = store.state.projectRoot + '/' + entryPath
    fetch('/api/project', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: absPath }),
    }).then(resp => {
        if (resp.ok) {
            window.location.reload()
        } else {
            resp.text().then(text => {
                let msg = text
                try { msg = JSON.parse(text).error || msg } catch (_) {}
                if (toast) toast.show(t('file.toast.switchProjectFailed', { error: msg }), { icon: '❌', type: 'error', duration: 2000 })
            })
        }
    }).catch(() => {
        if (toast) toast.show(t('file.toast.switchProjectFailedShort'), { icon: '❌', type: 'error', duration: 2000 })
    })
}

function doOpenTerminal() {
    const targetCwd = ctxMenu.entry && ctxMenu.entry.type === 'dir'
        ? ctxMenu.entry.path
        : props.currentDir
    closeCtxMenu()
    emit('openTerminal', targetCwd || '')
}

async function doRename() {
    const entry = ctxMenu.entry
    const newName = await dialog.prompt(t('file.prompt.newName'), { value: entry.name })
    if (!newName || newName === entry.name) { closeCtxMenu(); return }
    emit('rename', { path: entry.path, name: newName })
    closeCtxMenu()
}

function doDownload() {
    const path = ctxMenu.entry.path
    const name = ctxMenu.entry.name
    closeCtxMenu()
    downloadFileByPath(path, name)
}

// ── Archive download (zip) ──
async function doArchive(paths, zipName) {
    if (!paths.length) return
    if (toast) toast.show(t('file.toast.archiving', { n: paths.length }), { icon: '📦', type: 'info', duration: 0 })
    try {
        const resp = await fetch('/api/file/archive', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths }),
        })
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({ error: 'Unknown error' }))
            if (toast) toast.show(t('file.toast.archiveFailedDetail', { error: err.error || '' }), { icon: '❌', type: 'error', duration: 3000 })
            return
        }
        const blob = await resp.blob()
        const native = window.AndroidNative
        if (isAppMode.value && native && native.downloadBlob) {
            // Android native: convert blob to base64 and pass to native bridge
            const reader = new FileReader()
            reader.onload = () => {
                // reader.result is "data:application/zip;base64,XXXX..."
                const base64 = reader.result.split(',')[1]
                native.downloadBlob(base64, zipName || 'archive.zip')
            }
            reader.onerror = () => {
                if (toast) toast.show(t('file.toast.archiveFailed'), { icon: '❌', type: 'error', duration: 2000 })
            }
            reader.readAsDataURL(blob)
        } else {
            // Web: standard blob download via <a> tag
            const url = URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = zipName || 'archive.zip'
            document.body.appendChild(a)
            a.click()
            document.body.removeChild(a)
            URL.revokeObjectURL(url)
        }
        if (toast) toast.show(t('file.toast.archiveDone'), { icon: '✅', type: 'success', duration: 1500 })
    } catch (err) {
        if (toast) toast.show(t('file.toast.archiveFailed'), { icon: '❌', type: 'error', duration: 2000 })
    }
}

function doArchiveDir() {
    if (!ctxMenu.entry || ctxMenu.entry.type !== 'dir') return
    const entry = ctxMenu.entry
    closeCtxMenu()
    const zipName = entry.name + '.zip'
    doArchive([entry.path], zipName)
}

function doBatchArchive() {
    const paths = [...multiSelect.selected]
    if (!paths.length) return
    const zipName = paths.length === 1
        ? paths[0].split('/').pop() + '.zip'
        : 'archive.zip'
    doArchive(paths, zipName)
    exitMultiSelect()
}

function doAttachToChat() {
    const path = ctxMenu.entry.path
    closeCtxMenu()
    if (hasAttachedFile(path)) {
        removeAttachedFileByPath(path)
        toast.show(t('chat.attach.removedFromChat'), { icon: '📎', type: 'info', duration: 1500 })
        return
    }
    addAttachedFile(path)
    toast.show(t('chat.attach.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })

    // Fly-to-chat particle animation
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null
    if (animTo && ctxMenu.x && ctxMenu.y) {
        window.dispatchEvent(new CustomEvent('attach-to-chat', {
            detail: {
                from: { x: ctxMenu.x, y: ctxMenu.y },
                to: { x: animTo.left + animTo.width / 2, y: animTo.top + animTo.height / 2 },
            }
        }))
    }
}

function toggleAttach(path) {
    if (hasAttachedFile(path)) {
        removeAttachedFileByPath(path)
        toast.show(t('chat.attach.removedFromChat'), { icon: '📎', type: 'info', duration: 1500 })
    } else {
        addAttachedFile(path)
        toast.show(t('chat.attach.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })
    }
}

function doDelete() {
    const path = ctxMenu.entry.path
    appLog.d(TAG, '[doDelete] emitting delete for:', path)
    closeCtxMenu()
    emit('delete', path)
}

// ── PC keyboard shortcuts (Ctrl+C/X/V, Delete) ──
function handleKeydown(e) {
    // Only active when browse tab is focused
    if (activeTab.value !== 'browse') return
    // Skip in Android app mode
    if (isAppMode.value) return
    // Skip if a dialog/prompt is open (don't interfere with input fields)
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return

    const isCtrl = e.ctrlKey || e.metaKey

    // Ctrl+C — copy
    if (isCtrl && e.key === 'c') {
        if (multiSelect.active && multiSelect.selected.size > 0) {
            e.preventDefault()
            doBatchCopy()
        } else if (currentFileForClipboard()) {
            e.preventDefault()
            clipboard.entries = [currentFileForClipboard()]
            clipboard.isCut = false
            if (toast) toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
        }
        return
    }

    // Ctrl+X — cut
    if (isCtrl && e.key === 'x') {
        if (multiSelect.active && multiSelect.selected.size > 0) {
            e.preventDefault()
            doBatchCut()
        } else if (currentFileForClipboard()) {
            e.preventDefault()
            clipboard.entries = [currentFileForClipboard()]
            clipboard.isCut = true
            if (toast) toast.show(t('file.toast.cutDone'), { icon: '✂️', type: 'success', duration: 1500 })
        }
        return
    }

    // Ctrl+V — paste
    if (isCtrl && e.key === 'v') {
        if (clipboard.entries.length) {
            e.preventDefault()
            // Paste into current directory
            const fakeEntry = { type: 'dir', name: '', path: props.currentDir }
            const savedEntry = ctxMenu.entry
            ctxMenu.entry = fakeEntry
            doPaste().then(() => { ctxMenu.entry = savedEntry })
        }
        return
    }

    // Delete — delete
    if (e.key === 'Delete') {
        if (multiSelect.active && multiSelect.selected.size > 0) {
            e.preventDefault()
            doBatchDelete()
        } else if (props.currentFile) {
            e.preventDefault()
            appLog.d(TAG, '[kbd:Delete] emitting delete for:', props.currentFile.path)
            emit('delete', props.currentFile.path)
        }
        return
    }

    // Ctrl+A — select all
    if (isCtrl && e.key === 'a') {
        if (!multiSelect.active) {
            e.preventDefault()
            enterMultiSelect()
        }
        toggleSelectAll()
        return
    }
}

// Build a clipboard entry from the currently viewed/selected file
function currentFileForClipboard() {
    if (!props.currentFile) return null
    const path = props.currentFile.path
    const name = path.split('/').pop() || ''
    const entry = props.entries.find(e => e.name === name)
    return { type: entry?.type || 'file', name, path }
}

</script>

<style scoped>
/* ── File manager content ── */
.file-manager-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* ── File manager specific ── */

.fm-header-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
}

.fm-project-path {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-muted, #999);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
}

.fm-copy-icon {
    flex-shrink: 0;
    cursor: pointer;
    color: var(--text-muted, #999);
    transition: color 0.15s;
}
.fm-copy-icon:hover {
    color: var(--accent-color, #4a90d9);
}

.dir-nav {
    padding: 3px 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-height: 28px;
    border-bottom: 1px solid var(--border-color, #e5e5e5);
    background: var(--bg-tertiary, #f5f5f5);
    flex-shrink: 0;
}

.dir-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
}

.dir-toolbar-btns {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
}

.dir-toolbar :deep(.search-pill) {
    margin-left: auto;
    min-width: 80px;
    max-width: 200px;
    transition: opacity 0.15s;
}

.dir-nav :deep(.dir-breadcrumb) {
    padding: 0 6px;
    min-height: 0;
}

/* ── Multi-select info bar ── */
.ms-info-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    font-size: 12px;
    color: var(--text-secondary, #666);
}

.ms-info-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-secondary, #666);
    cursor: pointer;
    flex-shrink: 0;
    padding: 0;
}

.ms-info-btn:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--accent-color, #4a90d9);
}

.ms-select-all-btn {
    width: auto;
    height: auto;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 11px;
    background: var(--bg-secondary, #e0e0e0);
    white-space: nowrap;
}

.ms-info-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* ── Multi-select checkbox ── */
.ms-check {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 2px solid var(--border-color, #d0d0d0);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.15s;
}

.ms-check.checked {
    background: var(--accent-color, #4a90d9);
    border-color: var(--accent-color, #4a90d9);
    color: #fff;
}

.file-item.ms-selected {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}

.file-item.ctx-highlight {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
}

/* ── Cut item half-transparent effect ── */
.file-item.cut-item,
.grid-item.cut-item {
    opacity: 0.5;
}

/* ── Multi-select bottom action bar ── */
.ms-action-bar {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 8px 12px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
    border-top: 1px solid var(--border-color, #e5e5e5);
    background: var(--bg-secondary, #fff);
    flex-shrink: 0;
}

.ms-action-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 12px;
    border: 1px solid var(--border-color, #e5e5e5);
    border-radius: 16px;
    background: var(--bg-tertiary, #f5f5f5);
    color: var(--text-primary, #1a1a1a);
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s;
    white-space: nowrap;
}

.ms-action-btn:hover {
    background: var(--bg-secondary, #e0e0e0);
}

.ms-action-btn.ms-danger {
    color: #ef4444;
    border-color: #fecaca;
}

.ms-action-btn.ms-danger:hover {
    background: #fef2f2;
}

[data-theme="dark"] .ms-action-btn.ms-danger {
    border-color: #7f1d1d;
}

[data-theme="dark"] .ms-action-btn.ms-danger:hover {
    background: #2d1b1b;
}

/* ── File list area ── */
.file-list {
    position: relative;
    flex: 1;
    overflow-y: auto;
    padding: 4px 6px;
}

/* Unified toolbar button */
.toolbar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 3px;
    width: 26px;
    height: 26px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: var(--bg-tertiary, #f0f0f0);
    color: var(--text-secondary, #666);
    cursor: pointer;
    transition: all 0.15s;
    flex-shrink: 0;
}

.toolbar-btn:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--accent-color, #4a90d9);
}

.toolbar-btn.active {
    background: var(--accent-color, #4a90d9);
    color: #fff;
}

.toolbar-btn.sort-active {
    background: var(--accent-color, #4a90d9);
    color: #fff;
}

.toolbar-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
}

.toolbar-btn:disabled:hover {
    background: transparent;
    color: var(--text-secondary, #666);
}

.toolbar-btn svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
}

/* Sort dropdown */
.toolbar-dropdown-wrap {
    position: relative;
    flex-shrink: 0;
}

.toolbar-dropdown {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 100;
    min-width: 140px;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
    padding: 4px;
}

.toolbar-dropdown-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 10px;
    border: none;
    border-radius: 6px;
    background: none;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    white-space: nowrap;
}

.toolbar-dropdown-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
}

.toolbar-dropdown-item.active {
    color: var(--accent-color, #4a90d9);
    font-weight: 500;
}

.toolbar-dropdown-item svg {
    flex-shrink: 0;
}

.toolbar-dropdown-divider {
    height: 1px;
    background: var(--border-color, #e5e5e5);
    margin: 4px 6px;
}

.toolbar-dropdown-item .sort-dir-icon {
    margin-left: auto;
}

.toolbar-dropdown-item:disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

.toolbar-dropdown-right {
    left: auto;
    right: 0;
}

/* ── File Items ── */

.file-item + .file-item {
    border-top: 1px solid var(--border-color, #e5e5e5);
}

.file-item {
    display: flex;
    align-items: center;
    padding: 6px 8px;
    border-radius: 0;
    min-height: 44px;
    cursor: pointer;
    transition: background 0.15s;
    gap: 8px;
    color: var(--text-secondary, #666);
    font-size: 13px;
    user-select: none;
    -webkit-user-select: none;
}

.file-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
}

.file-item.active {
    background: var(--accent-color, #4a90d9);
    color: white;
}

.file-item.dir-item {
    color: var(--text-primary, #1a1a1a);
    font-weight: 500;
}

.file-item.dir-item .file-icon {
    color: var(--accent-color, #4a90d9);
}

.file-item.dir-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
}

.file-item.dir-item .file-meta {
    margin-left: auto;
}

.file-icon {
    flex-shrink: 0;
    width: 28px;
    height: 28px;
}

.file-icon-wrap {
    position: relative;
    flex-shrink: 0;
    width: 28px;
    height: 28px;
}

.file-icon-wrap .file-icon {
    width: 28px;
    height: 28px;
}

.file-icon-wrap .file-thumb {
    width: 28px;
    height: 28px;
}

.attach-badge {
    position: absolute;
    bottom: -5px;
    right: -5px;
    background: var(--accent-color, #4a90d9);
    color: #fff;
    border-radius: 50%;
    padding: 3px;
    cursor: pointer;
    z-index: 2;
    transition: transform 0.15s, background 0.15s;
}

.attach-badge:hover {
    transform: scale(1.2);
    background: #ef4444;
}

.file-thumb {
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    border-radius: 4px;
    object-fit: contain;
}

.file-name {
    flex: 1;
    overflow-x: auto;
    white-space: nowrap;
    scrollbar-width: none;
}
.file-name::-webkit-scrollbar {
    display: none;
}

.file-meta {
    font-size: 11px;
    color: var(--text-muted, #999);
    flex-shrink: 0;
}

.file-item.active .file-meta {
    color: rgba(255,255,255,0.7);
}

/* Empty State */
.empty-state {
    text-align: center;
    padding: 40px 20px;
    color: var(--text-muted, #999);
}

.empty-state svg {
    width: 48px;
    height: 48px;
    margin-bottom: 12px;
    opacity: 0.5;
}

/* Truncate hint */
.truncate-hint {
    text-align: center;
    padding: 10px 16px;
    font-size: 12px;
    color: var(--text-muted, #999);
    background: var(--bg-tertiary, #f5f5f5);
    border-top: 1px solid var(--border-color, #e5e5e5);
    flex-shrink: 0;
}

/* ── File Grid ── */
.file-grid {
    position: relative;
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(90px, 1fr));
    gap: 8px;
    align-content: start;
}

.grid-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    cursor: pointer;
    border-radius: 8px;
    padding: 6px;
    transition: background 0.15s, opacity 0.15s;
    position: relative;
    user-select: none;
    -webkit-user-select: none;
}

.grid-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
}

.grid-item.grid-active {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
}

.grid-item.ms-selected {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}

.grid-item.ctx-highlight {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
}

.grid-thumb {
    width: 100%;
    aspect-ratio: 1;
    border-radius: 8px;
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-tertiary, #f5f5f5);
    position: relative;
}

.grid-thumb .attach-badge {
    position: absolute;
    bottom: 4px;
    right: 4px;
    background: var(--accent-color, #4a90d9);
    color: #fff;
    border-radius: 50%;
    padding: 3px;
    cursor: pointer;
    z-index: 2;
    transition: transform 0.15s, background 0.15s;
}

.grid-thumb .attach-badge:hover {
    transform: scale(1.2);
    background: #ef4444;
}

.grid-thumb img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
}

.grid-item.grid-dir .grid-thumb {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, var(--bg-tertiary, #f5f5f5));
}

.grid-icon {
    width: 32px;
    height: 32px;
    flex-shrink: 0;
}

.grid-name {
    margin-top: 4px;
    font-size: 12px;
    text-align: center;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    width: 100%;
    color: var(--text-secondary, #666);
}

.grid-item.grid-dir .grid-name {
    color: var(--text-primary, #1a1a1a);
    font-weight: 500;
}

/* Grid multi-select check */
.grid-ms-check {
    position: absolute;
    top: 4px;
    left: 4px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 2px solid var(--border-color, #d0d0d0);
    background: var(--bg-primary, #fff);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2;
    transition: all 0.15s;
}

.grid-ms-check.checked {
    background: var(--accent-color, #4a90d9);
    border-color: var(--accent-color, #4a90d9);
    color: #fff;
}

[data-theme="dark"] .grid-thumb {
    background: var(--bg-secondary, #2a2a2a);
}

[data-theme="dark"] .grid-item.grid-dir .grid-thumb {
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, var(--bg-secondary, #2a2a2a));
}

/* Upload progress bar */
.dir-upload-progress {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 12px;
    height: 20px;
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
    flex-shrink: 0;
}

.dir-upload-progress-bar {
    flex: 1;
    height: 3px;
    background: var(--accent-color, #4a90d9);
    border-radius: 2px;
    transition: width 0.15s ease;
}

.dir-upload-progress-text {
    font-size: 11px;
    color: var(--text-secondary, #666);
    white-space: nowrap;
}

</style>
