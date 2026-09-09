<template>
  <div class="file-viewer">
    <!-- Common header -->
    <FileHeader
      v-if="file && !loading && !file.error"
      :file="file"
      :view-mode="markdownViewMode"
      :toc-open="tocOpen"
      :search-open="searchOpen"
      :word-wrap="wordWrap"
      :show-line-numbers="showLineNumbers"
      :sticky-scroll="stickyScroll"
      :overlay-open="fileNav.overlayOpen.value"
      :editing="editing"
      :can-navigate-back="canNavigateBack"
      :can-go-back-file="fileNav.canGoBack.value"
      :can-go-forward-file="fileNav.canGoForward.value"
      :back-label="backLabel"
      @delete="handleDeleteRequest(file.path)"
      @toggle-view="handleToggleViewRequest"
      @toggle-edit="handleToggleEdit"
      @show-details="emit('showDetails')"
      @open-git-history="emit('openGitHistory')"
      @toggle-toc="emit('toggleToc')"
      @toggle-search="handleToggleSearch"
      @open-as-text="handleOpenAsText"
      @toggle-word-wrap="toggleWordWrap"
      @toggle-line-numbers="toggleLineNumbers"
      @toggle-sticky-scroll="toggleStickyScroll"
      @refresh="emit('refresh')"
      @overlay-close="handleOverlayCloseRequest"
      @navigate-back="handleNavBack"
      @navigate-forward="handleNavForward"
      @share-external="emit('shareExternal')"
      @share-link="emit('shareLink')"
      @export-html="handleExportHtml"
      @fit-width="handleFitWidth"
      @set-as-background="(path) => emit('setAsBackground', path)"
    />

    <!-- Content row: file content + (wide-screen) inline TOC dock -->
    <div class="file-viewer-body" :data-toc-side="tocDockSide">
      <div class="file-viewer-content" ref="contentRef">
        <!-- Loading (suppressed when external loading mask is active to avoid double flash) -->
        <div v-if="loading && !externalLoading" class="loading">
          <LoadingIndicator size="md" />
        </div>

      <!-- Error -->
      <div v-else-if="file.error" class="error-bubble">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
        <span>{{ file.error }}</span>
      </div>

      <!-- PDF -->
      <PdfPreview
        v-else-if="file.isPdf"
        ref="pdfPreviewRef"
        :file="file"
      />

      <!-- Image -->
      <ImagePreview
        v-else-if="file.isImage"
        :file="file"
      />

      <!-- Audio -->
      <AudioPreview
        v-else-if="file.isAudio"
        :file="file"
      />

      <!-- Video -->
      <VideoPreview
        v-else-if="file.isVideo"
        :file="file"
      />

      <!-- Office (Word/Excel/PPT) -->
      <OfficePreview
        v-else-if="file.isOffice"
        ref="officePreviewRef"
        :file="file"
      />

      <!-- Excalidraw diagram (opens directly in the editor) -->
      <ExcalidrawViewer
        v-else-if="file.isExcalidraw"
        ref="excalidrawViewerRef"
        :file="file"
      />

      <!-- Too large -->
      <div v-else-if="file.tooLarge" class="raw-content-viewer">
        <div class="unsupported-file">
          <FileIcon :path="file.name" :size="48" />
          <div class="unsupported-title">{{ file.name }}</div>
          <div class="unsupported-desc">{{ t('file.viewer.fileTooLarge') }} {{ file.size ? '(' + formatSize(file.size) + ')' : '' }}</div>
          <a v-if="!isAppMode" :href="buildLocalFileUrl(file.path, { download: true })" class="download-btn" :download="file.name">
            <Download :size="14" color="#fff" />
            {{ t('common.download') }}
          </a>
          <button v-else class="download-btn" @click="handleDownload(file.path)">
            <Download :size="14" color="#fff" />
            {{ t('common.download') }}
          </button>
        </div>
      </div>

      <!-- Binary file -->
      <div v-else-if="file.isBinary" class="raw-content-viewer">
        <div class="unsupported-file">
          <FileIcon :path="file.name" :size="48" />
          <div class="unsupported-title">{{ file.name }}</div>
          <div class="unsupported-desc">{{ t('file.viewer.binaryFile') }} {{ file.size ? '(' + formatSize(file.size) + ')' : '' }}</div>
          <div class="unsupported-actions">
            <a v-if="!isAppMode" :href="buildLocalFileUrl(file.path, { download: true })" class="download-btn" :download="file.name">
              <Download :size="14" color="#fff" />
              {{ t('common.download') }}
            </a>
            <button v-else class="download-btn" @click="handleDownload(file.path)">
              <Download :size="14" color="#fff" />
              {{ t('common.download') }}
            </button>
            <button class="open-as-text-btn" @click="handleOpenAsText">
              <Code2 :size="14" />
              {{ t('file.header.openAsText') }}
            </button>
            <button v-if="isAppMode" class="open-as-text-btn" @click="handleShareExternal">
              <Share2 :size="14" />
              {{ t('file.header.shareExternal') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Markdown file -->
      <template v-else-if="isMarkdown">
        <!-- Rendered browse (not editing) -->
        <MarkdownPreview
          v-if="!editing && markdownViewMode === 'rendered'"
          ref="mdPreviewRef"
          :file="file"
          :view-mode="markdownViewMode"
          :search-open="searchOpen"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          @delete="handleDeleteRequest(file.path)"
          @show-details="emit('showDetails')"
          @open-git-history="emit('openGitHistory')"
          @close-search="emit('closeSearch')"
          @capture-scroll="(entry) => emit('captureScroll', entry)"
        />
        <!-- Source/raw mode: a single CodeMirrorViewer for both browse and edit
             (editable toggles), so scroll survives the edit toggle. -->
        <CodeMirrorViewer
          v-else
          ref="cmEditorRef"
          :file="file"
          :content="file.content"
          :language="rawFileLanguage"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :sticky-scroll="stickyScroll"
          :editable="editing"
          :saving="saving"
          @save="handleSave"
          @save-and-exit="handleSaveAndExit"
          @cancel="editing = false"
          @exit-edit="editing = false"
          @search-change="emit('searchChange', $event)"
        />
      </template>

      <!-- HTML file -->
      <template v-else-if="isHtml">
        <iframe
          v-if="markdownViewMode === 'rendered'"
          ref="htmlPreviewRef"
          class="html-preview-iframe"
          :srcdoc="file.content"
          sandbox="allow-scripts"
        />
        <CodeMirrorViewer
          v-else
          ref="cmEditorRef"
          :file="file"
          :content="file.content"
          language="xml"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :sticky-scroll="stickyScroll"
          :editable="false"
          @search-change="emit('searchChange', $event)"
        />
      </template>

      <!-- OpenAPI / Swagger spec file -->
      <template v-else-if="isOpenapi">
        <OpenApiPreview
          v-if="markdownViewMode === 'rendered'"
          :file="file"
          :view-mode="markdownViewMode"
          :chat-quote="true"
        />
        <div v-else class="raw-content-viewer">
          <CodeMirrorViewer
            ref="cmEditorRef"
            :file="file"
            :content="file.content"
            :language="rawFileLanguage"
            :word-wrap="wordWrap"
            :show-line-numbers="showLineNumbers"
            :sticky-scroll="stickyScroll"
            :editable="false"
            @search-change="emit('searchChange', $event)"
          />
        </div>
      </template>

      <!-- Code / plain text -->
      <div v-else class="raw-content-viewer">
        <div v-if="file.truncated" class="truncated-notice">
          <AlertTriangle :size="14" />
          {{ t('file.viewer.truncated') }}
        </div>
        <CodeMirrorViewer
          ref="cmEditorRef"
          :file="file"
          :content="file.content"
          :language="rawFileLanguage"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :sticky-scroll="stickyScroll"
          :editable="editing"
          :saving="saving"
          @save="handleSave"
          @save-and-exit="handleSaveAndExit"
          @cancel="editing = false"
          @exit-edit="editing = false"
          @search-change="emit('searchChange', $event)"
        />
      </div>
      </div>

      <!-- Wide-screen inline TOC dock, inside the content area -->
      <TocDock
        v-if="docked && tocOpen"
        :file="tocFile"
        :pdf-outline="pdfOutline"
        :code-view="isCodeMirrorView"
        :side="tocDockSide"
        @close="emit('closeToc')"
        @jump="(line, anchorId) => emit('jump', line, anchorId)"
        @jump-page="emit('jumpPage', $event)"
      />
    </div>

    <!-- Touch-layout nav: floating bar at the bottom-center of the content area.
         Semi-transparent at rest; fully opaque on hover/focus. Wide screens get
         the same actions in the header instead, so this stays mobile-only. -->
    <div
      v-if="floatingNavVisible"
      class="file-nav-float"
    >
      <button
        v-if="canNavigateBack || fileNav.canGoBack.value"
        class="file-nav-btn"
        type="button"
        :title="backLabel || t('file.overlay.back')"
        :aria-label="backLabel || t('file.overlay.back')"
        @click.stop="handleNavBack"
      >
        <ArrowLeft :size="18" />
      </button>
      <button
        v-if="fileNav.canGoForward.value"
        class="file-nav-btn"
        type="button"
        :title="t('file.overlay.forward')"
        :aria-label="t('file.overlay.forward')"
        @click.stop="handleNavForward"
      >
        <ArrowRight :size="18" />
      </button>
    </div>

    <!-- Shared diff drawer for all file types -->
    <DiffDrawer
      :visible="diffDrawer.effectiveOpen.value"
      :marker-type="drawerMarkerType"
      :char-diff="drawerCharDiff"
      :diff-lines="drawerDiffLines"
      @close="closeDrawer"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch, onBeforeUnmount, onMounted, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { Download, Code2, AlertTriangle, Share2, ArrowLeft, ArrowRight } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ImagePreview from '@/components/media/ImagePreview.vue'
import PdfPreview from '@/components/media/PdfPreview.vue'
import AudioPreview from '@/components/media/AudioPreview.vue'
import VideoPreview from '@/components/media/VideoPreview.vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent.ts'
const OfficePreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('@/components/media/OfficePreview.vue') }))
const ExcalidrawViewer = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./ExcalidrawViewer.vue') }))
import MarkdownPreview from './MarkdownPreview.vue'
const CodeMirrorViewer = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./CodeMirrorViewer.vue') }))
const OpenApiPreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./OpenApiPreview.vue') }))
import DiffDrawer from './DiffDrawer.vue'
import { useDiffDrawer } from '@/composables/useDiffDrawer.ts'
import { diffDrawer } from '@/composables/useMarkdownDiff.ts'
import { useFileScrollRestore } from '@/composables/useFileScrollRestore'
import FileHeader from './FileHeader.vue'
import TocDock from './TocDock.vue'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'
import { store } from '@/stores/app.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useFileNavStack } from '@/composables/useFileNavStack.ts'
import { useTextSelectionActive } from '@/composables/useTextSelection.ts'
import { useFileEditor } from '@/composables/useFileEditor.ts'
import { useTocDockPreference } from '@/composables/useTocDockPreference.ts'
import { getWideScreenState } from '@/composables/useWideScreenLayout'
import { exportMarkdownToHtml, imageIssueReasonKey } from '@/utils/exportMarkdownHtml.ts'
import { downloadBlob, buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { useToast } from '@/composables/useToast.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'
import { getNative } from '@/utils/clawbenchNative'

const { t, locale } = useI18n()
const { isAppMode } = useAppMode()
const { isWideScreen } = getWideScreenState()
const toast = useToast()
const { drawerMarkerType, drawerCharDiff, drawerDiffLines, closeDrawer } = useDiffDrawer()
// diffDrawer is imported from useMarkdownDiff (encapsulated TabDrawer)

const props = defineProps({
    file: Object,
    tocOpen: Boolean,
    searchOpen: Boolean,
    markdownViewMode: String,
    externalLoading: Boolean,
    /** Inline TOC dock payload (wide-screen only). */
    tocFile: Object,
    pdfOutline: { type: Array, default: () => [] },
    /** Wide-screen layout — renders the inline TOC dock vs narrow drawer. */
    docked: { type: Boolean, default: false },
    canNavigateBack: Boolean,
    backLabel: String,
})
const emit = defineEmits(['delete', 'showDetails', 'openGitHistory', 'toggleToc', 'closeToc', 'toggleSearch', 'closeSearch', 'searchChange', 'toggleView', 'refresh', 'openFile', 'overlayClose', 'navigateBack', 'navigateForward', 'shareExternal', 'shareLink', 'jump', 'jumpPage', 'setAsBackground', 'captureScroll'])

const fileNav = useFileNavStack()
const { active: textSelecting } = useTextSelectionActive()
// Navigation lives in the header on wide screens; the floating bar is for touch.
const floatingNavVisible = computed(() =>
  !isWideScreen.value
  && fileNav.overlayOpen.value
  && !textSelecting.value
  && (props.canNavigateBack || fileNav.canGoBack.value || fileNav.canGoForward.value)
)
const fileType = computed(() => props.file ? getFileType(props.file.name) : null)
const rawFileLanguage = computed(() => getFileType(props.file?.name)?.lang || 'plaintext')
const isMarkdown = computed(() => fileType.value?.isMarkdown || false)
const isHtml = computed(() => fileType.value?.isHtml || false)
const isOpenapi = computed(() => props.file?.subtype === 'openapi')
// Whether the current view is rendered by CodeMirrorViewer. Code/plain text is
// always CodeMirror. Markdown uses the SearchDrawer only for the rendered
// preview; raw source view and editing are CodeMirror. HTML/OpenAPI rendered
// views (iframe/ReDoc) fall back to the SearchDrawer as well. Media/binary/
// oversized/errored files never render CodeMirror.
const isCodeMirrorView = computed(() => {
    if (!props.file || props.file.isExcalidraw) return false
    if (props.file.isImage || props.file.isAudio || props.file.isVideo || props.file.isPdf || props.file.isOffice) return false
    if (props.file.isBinary || props.file.tooLarge || props.file.error) return false
    if (isMarkdown.value) return editing.value || props.markdownViewMode !== 'rendered'
    if (isHtml.value || isOpenapi.value) return props.markdownViewMode !== 'rendered'
    return true
})
const loading = ref(false)
const contentRef = ref(null)
const pdfPreviewRef = ref(null)
const officePreviewRef = ref(null)
const htmlPreviewRef = ref(null)

// Edit mode (source text editing via CodeEditor).
// Shared at module level so the global back gesture (App.vue) can exit edit
// mode first instead of navigating back / closing the file while editing.
const fileEditor = useFileEditor()
const editing = fileEditor.editing
const { saving, saveFile } = useCodeEditorSave()
const cmEditorRef = ref(null)
const excalidrawViewerRef = ref(null)
const mdPreviewRef = ref(null)

// The global back handler calls exitEdit() → run the active editor's exit flow,
// which confirms save/discard/cancel when there are unsaved changes. For
// Excalidraw files the iframe editor registers its own flow; otherwise it's
// CodeMirrorViewer.
function handleExitEditRequest() {
    if (props.file?.isExcalidraw) {
        excalidrawViewerRef.value?.requestExit?.()
    } else {
        cmEditorRef.value?.handleExit?.()
    }
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null
onMounted(() => {
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExitEditRequest)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => cmEditorRef.value?.isDirty?.() ?? false)
})
onBeforeUnmount(() => {
    if (unregisterExitEdit) {
        unregisterExitEdit()
        unregisterExitEdit = null
    }
    if (unregisterDirtyGetter) {
        unregisterDirtyGetter()
        unregisterDirtyGetter = null
    }
})

// Save and stay in edit mode. Clicking save / Ctrl+S only persists the file,
// it does not leave the edit view so the user can keep making edits.
async function handleSave(content) {
    await saveFile(props.file?.path || '', content)
}

// Save and then exit edit mode. Used by the exit flows (back / toggle view)
// which confirm save-or-discard; only these paths leave the edit view.
async function handleSaveAndExit(content) {
    const ok = await saveFile(props.file?.path || '', content)
    if (ok) {
        editing.value = false
    }
}

function handleToggleSearch() {
    // Route the toggle upward: App's openFileSearch() sets the header-button
    // highlight (viewSearchActive) and then focuses the active search UI
    // (CodeMirror panel or the markdown preview inline bar) via focusSearchInput.
    emit('toggleSearch')
}

function focusSearchInput() {
    // Focus (not toggle) the active search UI. CodeMirror views open the
    // editor's own search panel; the rendered markdown preview focuses its
    // inline search bar (opened via the searchOpen prop).
    if (isCodeMirrorView.value) {
        cmEditorRef.value?.openSearch?.()
    } else if (!editing.value && props.markdownViewMode === 'rendered' && isMarkdown.value) {
        mdPreviewRef.value?.focusSearchInput?.()
    }
}

function handleToggleEdit() {
    if (editing.value) {
        // Exiting edit mode via the header Edit button: confirm unsaved changes
        // (save/discard/cancel) exactly like the back gesture and navigation.
        return guardExitEdit(() => {})
    }
    const saved = scrollRestore.captureScroll(scrollRestore.currentScrollEl())
    editing.value = true
    scrollRestore.restoreAfterContainerSwitch(saved)
}

// Confirm-and-exit edit mode before an action that would leave the current
// file's edit view. If there are unsaved changes, the active editor's exit
// flow (CodeMirrorViewer.handleExit or ExcalidrawViewer.requestExit) prompts
// save/discard/cancel; the action only runs once edit mode has really ended
// (save completed or changes discarded). Returns true if the action may
// proceed, false if the user cancelled or a save failed.
async function guardExitEdit(action) {
    // Excalidraw opens directly in the editor and never leaves "edit mode",
    // so the CodeMirror editing/exit bookkeeping doesn't apply — just confirm
    // the scene is saved (requestExit resolves once the write completes).
    if (props.file?.isExcalidraw) {
        const exited = await excalidrawViewerRef.value?.requestExit?.()
        if (exited !== true) return false
        action()
        return true
    }
    if (editing.value) {
        const exited = await cmEditorRef.value?.handleExit?.()
        if (exited !== true) return false
        // The "save and exit" path clears editing asynchronously; wait for it so
        // dependent UI (e.g. MarkdownPreview gated on !editing) is consistent.
        await waitEditingCleared()
        if (editing.value) return false // save still in flight / failed — abort
    }
    action()
    return true
}

// Preview / toggle-view request. When a markdown file is being edited, the
// rendered preview is gated on `!editing`, so opening it must first exit edit
// mode (with dirty confirmation). If the user cancels, stay put.
function handleToggleViewRequest() {
    return guardExitEdit(() => emit('toggleView'))
}

// File navigation / closing all leave the current edit view, so they go through
// the same dirty-save confirmation as the back gesture and toggle-view.
// Snapshot where the user is in the current file.
//
// Rendered markdown owns the authoritative capture: it holds the live
// .markdown-body element and refreshes the module-level cache itself. Delegate
// to it so the two paths cannot drift — it also emits 'captureScroll' up the
// chain, so the caller must not emit again.
function captureScroll() {
    if (!editing.value && isMarkdown.value && props.markdownViewMode === 'rendered') {
        const entry = mdPreviewRef.value?.captureCurrentScrollState?.()
        if (entry) return entry
    }
    const el = scrollRestore.currentScrollEl()
    // captureScroll only comes back empty when there is no live pane at all,
    // so there is no pixel-only fallback to emit here.
    const saved = scrollRestore.captureScroll(el)
    if (saved) {
        emit('captureScroll', saved)
        return saved
    }
    return null
}

function handleNavBack() {
    captureScroll()
    return guardExitEdit(() => emit('navigateBack'))
}
function handleNavForward() {
    return guardExitEdit(() => emit('navigateForward'))
}
function handleOverlayCloseRequest() {
    return guardExitEdit(() => emit('overlayClose'))
}
function handleDeleteRequest(path) {
    return guardExitEdit(() => emit('delete', path))
}

// Resolve once edit mode has been left, or after a timeout so callers never
// hang. Callers still re-check `editing` afterwards.
function waitEditingCleared(timeoutMs = 5000) {
    return new Promise((resolve) => {
        if (!editing.value) return resolve()
        const start = Date.now()
        const timer = setInterval(() => {
            if (!editing.value || Date.now() - start > timeoutMs) {
                clearInterval(timer)
                resolve()
            }
        }, 50)
    })
}

// Expose PDF outline and scrollToPage for TOC integration
const pdfOutline = computed(() => pdfPreviewRef.value?.outline || [])
const pdfScrollToPage = (pageNum) => pdfPreviewRef.value?.scrollToPage(pageNum)

// Fit-width: reset zoom to fit container width
function handleFitWidth() {
    pdfPreviewRef.value?.fitWidth()
    officePreviewRef.value?.fitWidth()
}

// Word wrap & line numbers preferences from settings config
const { localConfig, setLocalConfig } = useSettingsConfig()
const wordWrap = computed(() => !!localConfig.wordWrap)
const showLineNumbers = computed(() => localConfig.lineNumbers !== false)
const stickyScroll = computed(() => localConfig.stickyScroll !== false)
// Wide-screen inline TOC dock side (left/right). Toggled by a button in the
// dock header; kept in sync with the dock via the shared preference module.
const { tocDockSide } = useTocDockPreference()

function toggleWordWrap() {
    setLocalConfig('wordWrap', !wordWrap.value)
}

function toggleLineNumbers() {
    setLocalConfig('lineNumbers', !showLineNumbers.value)
}

function toggleStickyScroll() {
    setLocalConfig('stickyScroll', !stickyScroll.value)
}

// Scroll-position save/restore (cross-file reopen + rendered↔raw pane swaps)
// is delegated to the useFileScrollRestore composable; the viewer only declares
// what content it renders. Positions persist in the module-level fileScrollCache
// so they survive unmount/remount (overlay close → file manager → recent reopen).
const scrollRestore = useFileScrollRestore({
    contentRoot: () => contentRef.value,
    file: () => props.file,
    markdownViewMode: () => props.markdownViewMode,
    editing: () => editing.value,
    loading: () => loading.value,
    isMarkdown: () => isMarkdown.value,
    isHtml: () => isHtml.value,
    isOpenapi: () => isOpenapi.value,
})

onMounted(() => {
    scrollRestore.start()
})

onBeforeUnmount(() => {
    scrollRestore.dispose()
})

// Save/restore scroll position when switching files. The watcher keeps the
// viewer-level responsibilities (edit-mode reset, loading flags); the scroll
// bookkeeping itself is delegated to the composable, preserving the original
// ordering: save the outgoing file → reset edit mode → schedule a restore.
watch(() => props.file, (f, oldF) => {
    scrollRestore.onFileWillChange()

    editing.value = false

    if (!f) {
        loading.value = true
        scrollRestore.onFileChanged(null, false)
        return
    }
    if (f.isImage || f.isPdf || f.isAudio || f.isVideo || f.isOffice || f.isExcalidraw || f.isBinary || f.tooLarge || f.error) {
        loading.value = false
    } else {
        loading.value = f.content == null
    }
    scrollRestore.onFileChanged(f, f?.path !== oldF?.path)
}, { immediate: true })

watch(() => props.file?.content, (content) => {
    if (!props.file) return
    if (props.file.isImage || props.file.isPdf || props.file.isAudio || props.file.isVideo || props.file.isOffice || props.file.isExcalidraw || props.file.isBinary || props.file.tooLarge || props.file.error) return
    loading.value = content == null
    // Content loaded, try restore or attach listener
    if (content != null) {
        scrollRestore.onContentReady()
    }
})

// Sync scroll position when toggling rendered <-> raw view for a markdown file.
// Runs as a pre-flush watcher: the pane being left is still mounted, so we
// capture its anchor before the new pane swaps in.
watch(() => props.markdownViewMode, (newMode, oldMode) => {
    if (newMode === oldMode || !isMarkdown.value || !props.file?.content) return
    const oldEl = scrollRestore.scrollElFor(oldMode, editing.value)
    const saved = scrollRestore.captureScroll(oldEl)
    if (saved) scrollRestore.restoreAfterContainerSwitch(saved)
})

function formatSize(bytes) {
    return formatFileSize(bytes)
}

function handleOpenAsText() {
    if (!props.file?.path) return
    store.selectFile(props.file.path, false, false, false, true)
}

function handleDownload(path) {
    downloadFileByPath(path, props.file?.name)
}

async function handleExportHtml() {
    if (!props.file?.path) return
    const file = props.file
    // Export must re-render from source markdown (shared preview pipeline),
    // so it no longer depends on the live .markdown-body DOM being rendered.
    if (file.content == null) return

    toast.show(t('file.header.exportingHtml'), { icon: '📄', type: 'info', duration: 0 })
    try {
        const result = await exportMarkdownToHtml({
            content: file.content,
            path: file.path,
            projectRoot: store.state.projectRoot,
            homeDir: store.state.homeDir,
            fileName: file.name,
            locale: locale.value,
        })
        const htmlName = file.name.replace(/\.md$/i, '.html')
        downloadBlob(result.html, htmlName, 'text/html')
        const msgs = [t('file.header.exportHtmlSuccess')]
        if (result.issues.length > 0) {
            const MAX_DETAILS = 3
            const detailText = result.issues.slice(0, MAX_DETAILS).map(i => `${i.path}: ${t(imageIssueReasonKey(i.reason))}`).join('; ')
            const suffix = result.issues.length > MAX_DETAILS ? ` ...${t('file.header.exportHtmlMore', { n: result.issues.length - MAX_DETAILS })}` : ''
            msgs.push(`${t('file.header.exportHtmlSkippedImages', { n: result.issues.length })} ${detailText}${suffix}`)
        }
        toast.show(msgs.join('. '), { icon: '✅', type: 'success', duration: 4000 })
    } catch {
        toast.show(t('file.header.exportHtmlFailed'), { icon: '❌', type: 'error', duration: 3000 })
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

// Expose for parent (App.vue) to access PDF TOC
defineExpose({
    pdfOutline,
    pdfScrollToPage,
    focusSearchInput,
    captureScroll,
})
</script>

<style scoped>
.file-viewer {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
    position: relative;
}

/* Content row: file content + optional inline TOC dock (wide-screen) */
.file-viewer-body {
    display: flex;
    flex: 1;
    flex-direction: row;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
}

.file-viewer-content {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
}

/* TOC dock side: reorder the flex children so the dock renders on the left
   when the user chose that side (default right keeps the current order). */
.file-viewer-body[data-toc-side="left"] .file-viewer-content {
    order: 1;
}
.file-viewer-body[data-toc-side="left"] .toc-dock {
    order: 0;
}

/* Floating history nav (back/forward) overlaid on the content area.
   Semi-transparent at rest; fully opaque on hover/focus so it never obscures
   the code while remaining easy to reach. */
.file-nav-float {
    position: absolute;
    bottom: 16px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    gap: 10px;
    z-index: 5;
    opacity: 0.55;
    transition: opacity 0.15s;
    pointer-events: none;
}

@media (hover: hover) {
  .file-nav-float:hover,
  .file-nav-float:focus-within {
      opacity: 1;
  }
}

.file-nav-float .file-nav-btn {
    pointer-events: auto;
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: 1px solid var(--border-color, rgba(128, 128, 128, 0.35));
    background: var(--bg-primary, #fff);
    color: var(--text-secondary);
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.18);
    transition: background 0.15s, color 0.15s, transform 0.1s;
}

.file-nav-float .file-nav-btn:not(:disabled):active {
    background: var(--bg-tertiary);
    transform: scale(0.94);
}

.file-nav-float .file-nav-btn:disabled {
    opacity: 0.35;
    cursor: default;
}

.unsupported-file {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px 24px;
    text-align: center;
    height: 100%;
}

.unsupported-file > svg {
    width: 48px;
    height: 48px;
    color: var(--text-muted);
    margin-bottom: 12px;
}

.unsupported-title {
    font-size: 16px;
    font-weight: 500;
    color: var(--text-primary);
    margin-bottom: 8px;
    word-break: break-all;
}

.unsupported-desc {
    font-size: 14px;
    color: var(--text-muted);
    margin-bottom: 20px;
}

.unsupported-actions {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
}

.open-as-text-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 5px 12px;
    background: transparent;
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
    border-radius: 14px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s;
    gap: 4px;
    line-height: 1;
}

.open-as-text-btn svg {
    flex-shrink: 0;
}

@media (hover: hover) {
  .open-as-text-btn:hover {
      border-color: var(--accent-color);
      color: var(--accent-color);
  }
}

.download-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 5px 12px;
    background: var(--accent-color);
    color: #fff;
    border: none;
    border-radius: 14px;
    text-decoration: none;
    font-size: 12px;
    font-weight: 500;
    transition: filter 0.15s;
    gap: 4px;
    line-height: 1;
}

.download-btn svg {
    flex-shrink: 0;
}

@media (hover: hover) {
  .download-btn:hover {
      filter: brightness(1.15);
  }
}

.loading {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
}


.error-bubble {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: rgba(239, 68, 68, 0.1);
    color: var(--error-color, #dc2626);
    padding: 6px 12px;
    border-radius: 20px;
    font-size: 13px;
    margin: 24px auto;
    max-width: 90%;
    line-height: 1.4;
    align-self: center;
}

.html-preview-iframe {
    flex: 1;
    width: 100%;
    height: 100%;
    border: none;
    background: #fff;
}

.truncated-notice {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    background: rgba(245, 158, 11, 0.1);
    color: var(--warning-color, #d97706);
    font-size: 12px;
    border-bottom: 1px solid rgba(245, 158, 11, 0.2);
}
</style>

<style>
[data-theme-base="dark"] .error-bubble {
    background: rgba(239, 68, 68, 0.15);
    color: #fca5a5;
}

[data-theme-base="dark"] .truncated-notice {
    background: rgba(245, 158, 11, 0.15);
    color: #fbbf24;
    border-bottom-color: rgba(245, 158, 11, 0.3);
}
</style>
