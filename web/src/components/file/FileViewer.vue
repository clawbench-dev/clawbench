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
      :overlay-open="fileNav.overlayOpen.value"
      :recent-files-available="recentFilesAvailable"
      :editing="editing"
      @delete="emit('delete', file.path)"
      @toggle-view="emit('toggleView')"
      @toggle-edit="handleToggleEdit"
      @show-details="emit('showDetails')"
      @open-git-history="emit('openGitHistory')"
      @toggle-toc="emit('toggleToc')"
      @toggle-search="emit('toggleSearch')"
      @open-as-text="handleOpenAsText"
      @toggle-word-wrap="toggleWordWrap"
      @toggle-line-numbers="toggleLineNumbers"
      @refresh="emit('refresh')"
      @overlay-close="emit('overlayClose')"
      @open-recent-files="emit('openRecentFiles')"
      @share-external="emit('shareExternal')"
      @export-html="handleExportHtml"
      @fit-width="handleFitWidth"
    />

    <div class="file-viewer-content" ref="contentRef">
      <!-- Loading (suppressed when external loading mask is active to avoid double flash) -->
      <div v-if="loading && !externalLoading" class="loading">
        <div class="loading-spinner"></div>
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
          :file="file"
          :view-mode="markdownViewMode"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          @delete="emit('delete', file.path)"
          @show-details="emit('showDetails')"
          @open-git-history="emit('openGitHistory')"
        />
        <!-- Source/raw mode: a single CodeMirrorViewer for both browse and edit
             (editable toggles), so scroll survives the edit toggle. -->
        <CodeMirrorViewer
          v-else
          :file="file"
          :content="file.content"
          :language="rawFileLanguage"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :editable="editing"
          :saving="saving"
          @save="handleSave"
          @cancel="editing = false"
          @exit-edit="editing = false"
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
          :file="file"
          :content="file.content"
          language="xml"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :editable="false"
        />
      </template>

      <!-- OpenAPI / Swagger spec file -->
      <template v-else-if="isOpenapi">
        <OpenApiPreview
          v-if="markdownViewMode === 'rendered'"
          :file="file"
          :view-mode="markdownViewMode"
        />
        <div v-else class="raw-content-viewer">
          <CodeMirrorViewer
            :file="file"
            :content="file.content"
            :language="rawFileLanguage"
            :word-wrap="wordWrap"
            :show-line-numbers="showLineNumbers"
            :editable="false"
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
          :file="file"
          :content="file.content"
          :language="rawFileLanguage"
          :word-wrap="wordWrap"
          :show-line-numbers="showLineNumbers"
          :editable="editing"
          :saving="saving"
          @save="handleSave"
          @cancel="editing = false"
          @exit-edit="editing = false"
        />
      </div>
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
import { Download, Code2, AlertTriangle, Share2 } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import ImagePreview from '@/components/media/ImagePreview.vue'
import PdfPreview from '@/components/media/PdfPreview.vue'
import AudioPreview from '@/components/media/AudioPreview.vue'
import VideoPreview from '@/components/media/VideoPreview.vue'
const OfficePreview = defineAsyncComponent(() => import('@/components/media/OfficePreview.vue'))
import MarkdownPreview from './MarkdownPreview.vue'
const CodeMirrorViewer = defineAsyncComponent(() => import('./CodeMirrorViewer.vue'))
const OpenApiPreview = defineAsyncComponent(() => import('./OpenApiPreview.vue'))
import DiffDrawer from './DiffDrawer.vue'
import { useDiffDrawer } from '@/composables/useDiffDrawer.ts'
import { diffDrawer } from '@/composables/useMarkdownDiff.ts'
import FileHeader from './FileHeader.vue'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'
import { store } from '@/stores/app.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useFileNavStack } from '@/composables/useFileNavStack.ts'
import { useRecentFiles } from '@/composables/useRecentFiles'
import { exportRenderedHtml } from '@/utils/exportHtml.ts'
import { downloadBlob, buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { useToast } from '@/composables/useToast.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'

const { t } = useI18n()
const { isAppMode } = useAppMode()
const toast = useToast()
const { drawerMarkerType, drawerCharDiff, drawerDiffLines, closeDrawer } = useDiffDrawer()
// diffDrawer is imported from useMarkdownDiff (encapsulated TabDrawer)

const props = defineProps({
    file: Object,
    tocOpen: Boolean,
    searchOpen: Boolean,
    markdownViewMode: String,
    externalLoading: Boolean,
})
const emit = defineEmits(['delete', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'toggleView', 'refresh', 'openFile', 'overlayClose', 'openRecentFiles', 'shareExternal'])

const fileNav = useFileNavStack()
const { recentFilesExcluding } = useRecentFiles()
const filteredRecentFiles = recentFilesExcluding(computed(() => props.file?.path ?? null))
const recentFilesAvailable = computed(() => filteredRecentFiles.value.length)

const fileType = computed(() => props.file ? getFileType(props.file.name) : null)
const rawFileLanguage = computed(() => getFileType(props.file?.name)?.lang || 'plaintext')
const isMarkdown = computed(() => fileType.value?.isMarkdown || false)
const isHtml = computed(() => fileType.value?.isHtml || false)
const isOpenapi = computed(() => props.file?.subtype === 'openapi')
const loading = ref(false)
const contentRef = ref(null)
const pdfPreviewRef = ref(null)
const officePreviewRef = ref(null)
const htmlPreviewRef = ref(null)

// Edit mode (source text editing via CodeEditor)
const editing = ref(false)
const { saving, saveFile } = useCodeEditorSave()

async function handleSave(content) {
    const saved = captureScrollRatio()
    const ok = await saveFile(props.file?.path || '', content)
    if (ok) {
        editing.value = false
        // Save reloads the file content (which can reset scroll), so restore it.
        restoreScrollAfter(saved)
    }
}

function handleToggleEdit() {
    const saved = captureScrollRatio()
    editing.value = !editing.value
    restoreScrollAfter(saved)
}

// Restore the previously captured scroll ratio once the current scroll container
// is ready. The browse/edit view can swap scroll containers that mount async
// (CodeMirror is lazy-loaded), so retry until it is laid out and scrollable.
function restoreScrollAfter(saved) {
    if (!saved) return
    let attempts = 0
    const timer = setInterval(() => {
        const el = getScrollEl()
        if (el && el.scrollHeight > el.clientHeight) {
            clearInterval(timer)
            restoreScrollRatio(saved)
        } else if (++attempts > 60) {
            clearInterval(timer)
        }
    }, 50)
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

function toggleWordWrap() {
    setLocalConfig('wordWrap', !wordWrap.value)
}

function toggleLineNumbers() {
    setLocalConfig('lineNumbers', !showLineNumbers.value)
}

// Per-file scroll position cache
const scrollPositions = new Map()
let pendingRestore = null // { path, scrollTop }
let restoreTimer = null
let restoreAttempts = 0
const MAX_RESTORE_ATTEMPTS = 100 // 100 * 50ms = 5 seconds max
let currentFilePath = null
let scrollHandler = null
let scrollEl = null // reference to the element we attached scroll listener to

function clearRestoreTimer() {
    if (restoreTimer) {
        clearInterval(restoreTimer)
        restoreTimer = null
    }
}

// Find the actual scroll container based on file type
function getScrollEl() {
    const el = contentRef.value
    if (!el) return null
    // Edit mode always renders the CodeMirror editor
    if (editing.value) {
        return el.querySelector('.cm-scroller')
    }
    if (isMarkdown.value) {
        // Rendered markdown scrolls in .markdown-body; source view uses CM
        return props.markdownViewMode === 'rendered'
            ? el.querySelector('.markdown-body')
            : el.querySelector('.cm-scroller')
    }
    /* v8 ignore next - trivial prop access fix, tested via integration */
    if (isHtml.value && props.markdownViewMode === 'rendered') {
        return null // iframe handles its own scrolling
    }
    if (isOpenapi.value && props.markdownViewMode === 'rendered') {
        return null // ReDoc iframe handles its own scrolling
    }
    // CodeMirror-based viewers scroll inside .cm-scroller
    return el.querySelector('.cm-scroller')
}

// Capture the current scroll position as a ratio so it survives a component
// switch (rendered markdown ↔ CodeMirror source/edit) with different heights.
function captureScrollRatio() {
    const el = getScrollEl()
    if (!el) return null
    const max = el.scrollHeight - el.clientHeight
    if (max <= 0) return null
    return { top: el.scrollTop, ratio: el.scrollTop / max }
}

function restoreScrollRatio(saved) {
    if (!saved) return
    const el = getScrollEl()
    if (!el) return
    const max = el.scrollHeight - el.clientHeight
    if (max <= 0) return
    el.scrollTop = Math.round(saved.ratio * max)
}

// Listen for scroll events on the actual scroll container and save position
function attachScrollListener() {
    detachScrollListener()
    const el = getScrollEl()
    if (!el || !currentFilePath) return
    scrollEl = el
    scrollHandler = () => {
        scrollPositions.set(currentFilePath, el.scrollTop)
    }
    el.addEventListener('scroll', scrollHandler, { passive: true })
}

function detachScrollListener() {
    if (scrollHandler && scrollEl) {
        scrollEl.removeEventListener('scroll', scrollHandler)
    }
    scrollHandler = null
    scrollEl = null
}

function tryRestoreOrAttach() {
    restoreAttempts++
    if (restoreAttempts > MAX_RESTORE_ATTEMPTS) {
        clearRestoreTimer()
        // Even if not scrollable, attach listener for future scroll events
        attachScrollListener()
        return
    }
    if (loading.value) return
    const el = getScrollEl()
    if (!el) return
    // Content must be scrollable (scrollHeight > clientHeight)
    if (el.scrollHeight <= el.clientHeight) return

    // Restore scroll if needed
    if (pendingRestore) {
        el.scrollTop = pendingRestore.scrollTop
        pendingRestore = null
        clearRestoreTimer()
    }
    // Always attach listener once content is ready
    attachScrollListener()
}

function handleCancelScrollRestore() {
    pendingRestore = null
}

onBeforeUnmount(() => {
    detachScrollListener()
    clearRestoreTimer()
    window.removeEventListener('cancel-scroll-restore', handleCancelScrollRestore)
})

// When an explicit scroll-to-line is requested (e.g. clicking a file path
// annotation with line numbers), cancel any pending scroll-position restore
// so it doesn't override the line scroll.
onMounted(() => {
    window.addEventListener('cancel-scroll-restore', handleCancelScrollRestore)
})

// Save/restore scroll position when switching files
watch(() => props.file, (f, oldF) => {
    // Stop listening on old scroll container
    detachScrollListener()

    editing.value = false

    clearRestoreTimer()
    if (!f) { currentFilePath = null; loading.value = true; return }
    currentFilePath = f.path
    if (f.isImage || f.isPdf || f.isAudio || f.isVideo || f.isOffice || f.isBinary || f.tooLarge || f.error) {
        loading.value = false
    } else {
        loading.value = f.content == null
    }
    if (f?.path !== oldF?.path) {
        const savedScroll = scrollPositions.get(f.path)
        pendingRestore = { path: f.path, scrollTop: savedScroll ?? 0 }
        // Poll until content is rendered and scrollable
        restoreAttempts = 0
        restoreTimer = setInterval(tryRestoreOrAttach, 50)
        tryRestoreOrAttach()
    }
}, { immediate: true })

watch(() => props.file?.content, (content) => {
    if (!props.file) return
    if (props.file.isImage || props.file.isPdf || props.file.isAudio || props.file.isVideo || props.file.isOffice || props.file.isBinary || props.file.tooLarge || props.file.error) return
    loading.value = content == null
    // Content loaded, try restore or attach listener
    if (content != null) {
        tryRestoreOrAttach()
    }
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
    if (!props.file?.path || !contentRef.value) return
    const markdownBodyEl = contentRef.value.querySelector('.markdown-body')
    if (!markdownBodyEl) return

    toast.show(t('file.header.exportingHtml'), { icon: '📄', type: 'info', duration: 0 })
    try {
        const result = await exportRenderedHtml({
            markdownBodyEl,
            filePath: props.file.path,
            fileName: props.file.name,
        })
        const htmlName = props.file.name.replace(/\.md$/i, '.html')
        downloadBlob(result.html, htmlName, 'text/html')
        const msgs = [t('file.header.exportHtmlSuccess')]
        if (result.skippedImages > 0) msgs.push(t('file.header.exportHtmlSkippedImages', { n: result.skippedImages }))
        if (result.externalImages > 0) msgs.push(t('file.header.exportHtmlSkippedImages', { n: result.externalImages }))
        toast.show(msgs.join('. '), { icon: '✅', type: 'success', duration: 3000 })
    } catch {
        toast.show(t('file.header.exportHtmlFailed'), { icon: '❌', type: 'error', duration: 3000 })
    }
}

function handleShareExternal() {
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

// Expose for parent (App.vue) to access PDF TOC
defineExpose({
    pdfOutline,
    pdfScrollToPage,
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

.file-viewer-content {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
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

.open-as-text-btn:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
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

.download-btn:hover {
    filter: brightness(1.15);
}

.loading {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
}

.loading-spinner {
    width: 28px;
    height: 28px;
    border: 3px solid var(--border-color);
    border-top-color: var(--accent-color);
    border-radius: 50%;
    animation: loading-spin 0.7s linear infinite;
}

@keyframes loading-spin {
    to { transform: rotate(360deg); }
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
[data-theme="dark"] .error-bubble {
    background: rgba(239, 68, 68, 0.15);
    color: #fca5a5;
}

[data-theme="dark"] .truncated-notice {
    background: rgba(245, 158, 11, 0.15);
    color: #fbbf24;
    border-bottom-color: rgba(245, 158, 11, 0.3);
}
</style>
