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
      @quote-in-chat="(path) => emit('quoteInChat', path)"
    />

    <!-- Content row: file content + (wide-screen) inline TOC dock -->
    <div class="file-viewer-body" :data-toc-side="tocDockSide">
      <div class="file-viewer-content" ref="contentRef">
        <!-- Loading (suppressed when external loading mask is active to avoid double flash).
             A preview has no rows to preserve, so the placeholder previews the
             text shape instead of centring a spinner in an empty pane. -->
        <div v-if="loading && !externalLoading" class="loading">
          <PanelSkeleton class="file-viewer-skeleton" variant="text" :rows="10" :label="t('common.loading')" />
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
        <UnsupportedFileBody
          :name="file.name"
          :path="file.path"
          :size="file.size"
          :description="t('file.viewer.fileTooLarge')"
        >
          <button class="download-btn" @click="handleDownload(file.path)">
            <Download :size="14" color="#fff" />
            {{ t('common.download') }}
          </button>
        </UnsupportedFileBody>
      </div>

      <!-- Binary file -->
      <div v-else-if="file.isBinary" class="raw-content-viewer">
        <UnsupportedFileBody
          :name="file.name"
          :path="file.path"
          :size="file.size"
          :description="t('file.viewer.binaryFile')"
        >
          <button class="download-btn" @click="handleDownload(file.path)">
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
        </UnsupportedFileBody>
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
          @exit-edit="handleExitEdit"
          @search-change="emit('searchChange', $event)"
        />
      </template>

      <!-- HTML file -->
      <template v-else-if="isHtml">
        <div v-if="markdownViewMode === 'rendered'" class="html-preview-pane">
          <iframe
            ref="htmlPreviewRef"
            class="html-preview-iframe"
            :src="htmlPreviewSrc || undefined"
            :srcdoc="htmlPreviewSrc ? undefined : file.content"
            sandbox="allow-scripts allow-same-origin"
            @load="onHtmlPreviewLoad"
          />
          <!-- Inline find bar for the previewed document. The iframe owns its
               text nodes, so the bar is told which document to search (see
               PreviewSearchBar's `iframe` prop) — the host TreeWalker cannot
               reach them and a host Range cannot wrap them. -->
          <PreviewSearchBar
            ref="htmlSearchBarRef"
            :open="!!searchOpen"
            :iframe="htmlPreviewRef"
            @close="emit('closeSearch')"
          />
        </div>
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
          :editable="editing || isUntitled"
          :saving="saving"
          :untitled="isUntitled"
          @save="handleSave"
          @save-and-exit="handleSaveAndExit"
          @cancel="editing = false"
          @exit-edit="handleExitEdit"
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
import UnsupportedFileBody from './UnsupportedFileBody.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import ImagePreview from '@/components/media/ImagePreview.vue'
import PdfPreview from '@/components/media/PdfPreview.vue'
import AudioPreview from '@/components/media/AudioPreview.vue'
import VideoPreview from '@/components/media/VideoPreview.vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent.ts'
const OfficePreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('@/components/media/OfficePreview.vue') }))
const ExcalidrawViewer = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./ExcalidrawViewer.vue') }))
import MarkdownPreview from './MarkdownPreview.vue'
import PreviewSearchBar from './PreviewSearchBar.vue'
const CodeMirrorViewer = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./CodeMirrorViewer.vue') }))
const OpenApiPreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('./OpenApiPreview.vue') }))
import DiffDrawer from './DiffDrawer.vue'
import { useDiffDrawer } from '@/composables/useDiffDrawer.ts'
import { diffDrawer } from '@/composables/useMarkdownDiff.ts'
import { useFileScrollRestore } from '@/composables/useFileScrollRestore'
import { refreshTick } from '@/composables/useFileRefresh.ts'
import FileHeader from './FileHeader.vue'
import TocDock from './TocDock.vue'
import { getFileType } from '@/utils/fileType.ts'
import { store } from '@/stores/app.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { useFileNavStack } from '@/composables/useFileNavStack.ts'
import { useFileEditor } from '@/composables/useFileEditor.ts'
import { useTocDockPreference } from '@/composables/useTocDockPreference.ts'
import { exportMarkdownToHtml, imageIssueReasonKey } from '@/utils/exportMarkdownHtml.ts'
import { downloadBlob, downloadFileByPath } from '@/utils/download.ts'
import { buildHtmlPreviewSrc, installPreviewLinkHandler, installPreviewScrollbar, installPreviewSearchStyles, watchPreviewTheme } from '@/utils/htmlPreview.ts'
import { attachIframeSelectionQuoteBridge, installIframeSearchShortcut, resolveIframeWindow } from '@/utils/iframeSelectionQuote.ts'
import { makeIframeQuoteShowHandler, useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useToast } from '@/composables/useToast.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'
import { getNative } from '@/utils/clawbenchNative'

const { t, locale } = useI18n()
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
    /** Inline TOC dock payload (wide-screen only). */
    tocFile: Object,
    pdfOutline: { type: Array, default: () => [] },
    /** Wide-screen layout — renders the inline TOC dock vs narrow drawer. */
    docked: { type: Boolean, default: false },
    canNavigateBack: Boolean,
    backLabel: String,
    /** Whether selecting text in the rendered HTML preview surfaces the quote
     *  bar. The main app enables it; the share SPA (no session/chat) keeps it
     *  off — the same gate OpenApiPreview uses. */
    chatQuote: { type: Boolean, default: false },
})
const emit = defineEmits(['delete', 'showDetails', 'openGitHistory', 'toggleToc', 'closeToc', 'toggleSearch', 'closeSearch', 'searchChange', 'toggleView', 'refresh', 'openFile', 'overlayClose', 'closeUntitled', 'navigateBack', 'navigateForward', 'shareExternal', 'shareLink', 'jump', 'jumpPage', 'setAsBackground', 'captureScroll', 'quoteInChat', 'openSearch'])

const fileNav = useFileNavStack()
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
/** Inline find bar for the HTML preview's own document. */
const htmlSearchBarRef = ref(null)
/** Cache-buster for the HTML preview iframe (see htmlPreviewSrc). */
const htmlPreviewTimestamp = ref(Date.now())
const quoteQuestion = useQuoteQuestion()
/** Selection→quote bridge for the previewed HTML document (null when detached). */
let disposeHtmlQuoteBridgeFn = null
/** Path of the document the bridge was attached to, for bar dismissal. */
let htmlQuoteBridgePath = ''
/** Ctrl+F forwarder installed inside the previewed document. */
let disposeHtmlSearchShortcut = null
/** Repaints the injected sheets when the app theme changes. */
let disposeHtmlThemeWatcher = null
/** Takes over outbound-link clicks the sandbox would otherwise swallow. */
let disposeHtmlLinkHandler = null

// HTML preview loads the document through /api/fs/raw/ rather than inlining it
// via srcdoc, so the browser resolves the document's own relative references
// (stylesheets, scripts, images, fonts) against its real URL instead of the
// app's root. The URL rules (including the srcdoc fallback for absolute paths)
// and the sandbox trade live in buildHtmlPreviewSrc — the single source of
// truth shared with the quick code-link preview pane. See fileViewerSandbox.test.ts.
const htmlPreviewSrc = computed(() =>
    buildHtmlPreviewSrc(props.file?.path || '', htmlPreviewTimestamp.value)
)

/** Give the previewed document the app's scrollbar bar and search-match
 *  highlight styles (see installPreviewScrollbar / installPreviewSearchStyles).
 *  Runs on every load — including the srcdoc fallback and reloads — and both
 *  helpers are idempotent. Also (re)attaches the selection→quote bridge: a
 *  reload is a NEW document, so the previous bridge's listeners are gone.
 *
 *  The bar's colours are resolved from the host theme, so the theme watcher is
 *  (re)registered here too: a theme switch while the preview is open must
 *  repaint it, and a reload must not stack listeners. */
function onHtmlPreviewLoad() {
    installPreviewScrollbar(htmlPreviewRef.value)
    installPreviewSearchStyles(htmlPreviewRef.value)
    disposeHtmlThemeWatcher?.()
    disposeHtmlThemeWatcher = watchPreviewTheme(() => htmlPreviewRef.value)
    // The sandbox has no `allow-popups`, so a previewed page's `target="_blank"`
    // links are dead taps; hand them to the system browser instead.
    disposeHtmlLinkHandler?.()
    disposeHtmlLinkHandler = installPreviewLinkHandler(htmlPreviewRef.value)
    void attachHtmlQuoteBridge()
    attachHtmlSearchShortcut()
}

/**
 * Make Ctrl+F work while focus is inside the previewed document.
 *
 * Keydowns in the frame never reach the host, so App.vue's global handler is
 * blind to them: the user clicks into the preview, presses Ctrl+F, and gets the
 * browser's own find bar instead of ours. The listener lives in the frame and
 * asks the app to open the inline find bar (which searches that same document).
 */
function attachHtmlSearchShortcut() {
    disposeHtmlSearchShortcut?.()
    disposeHtmlSearchShortcut = null
    const win = htmlPreviewRef.value?.contentWindow
    if (!win) return
    disposeHtmlSearchShortcut = installIframeSearchShortcut(win, () => emit('openSearch'))
}

/**
 * Bridge text selections inside the previewed HTML document to the shared quote
 * bar.
 *
 * The iframe is a separate document: its `selectionchange` never reaches the
 * host, and its pointer events do not bubble here, so the global
 * useQuoteQuestion handler is blind to it. The same factory the OpenAPI preview
 * uses re-creates the pipeline inside that document (see
 * attachIframeSelectionQuoteBridge).
 */
async function attachHtmlQuoteBridge() {
    if (!props.chatQuote) return
    const win = await resolveIframeWindow(() => htmlPreviewRef.value)
    if (!win) return
    // ALWAYS dispose and re-attach — never dedupe on the window.
    //
    // An earlier version returned early when `htmlQuoteBridgeWin === win`, on
    // the assumption that "a real reload is a different contentWindow". That
    // assumption is wrong: after an iframe navigates, Chromium hands back the
    // SAME WindowProxy and only `contentDocument` is replaced (measured: same
    // window, different document). So the guard did the opposite of what it
    // claimed — the old listeners were left attached to a destroyed document,
    // the fresh document was skipped because the window compared equal, and
    // selecting text in the preview stopped producing a quote bar at all, with
    // no error. A reload here is the MAIN path: an fsnotify refresh or an AI
    // edit bumps `htmlPreviewTimestamp` and re-navigates the iframe.
    //
    // Re-attaching is cheap and safe: `disposeHtmlQuoteBridge` only removes
    // listeners and deliberately leaves the quote bar alone, so a user halfway
    // through typing a comment on the same file keeps their text. The search
    // shortcut right above already re-attaches unconditionally for the same
    // reason.
    disposeHtmlQuoteBridge()
    htmlQuoteBridgePath = props.file?.path || ''
    disposeHtmlQuoteBridgeFn = attachIframeSelectionQuoteBridge({
        win,
        filePath: () => props.file?.path || '',
        // Shared with the other two iframe hosts (inline quick preview, OpenAPI
        // docs): it maps the frame's anchor into host coordinates, shows the bar
        // immediately (the bridge already debounced), and suppresses the in-page
        // bar on Android where the system selection menu is the affordance.
        // Hand-written copies drifted — one of them silently dropped `anchor`,
        // which is exactly the "menu pops up at the top" defect.
        show: makeIframeQuoteShowHandler(quoteQuestion.showBar),
        hide: () => quoteQuestion.hideBar(),
    })
}

/**
 * Tear down the bridge (listeners only).
 *
 * Deliberately does NOT touch the quote bar: a re-attach happens whenever the
 * previewed document reloads — including an fsnotify refresh of the SAME file —
 * and the user may be halfway through typing a comment. Only the paths that
 * leave the document for good (see detachHtmlQuoteBridge) dismiss the bar.
 */
function disposeHtmlQuoteBridge() {
    if (disposeHtmlQuoteBridgeFn) {
        disposeHtmlQuoteBridgeFn()
        disposeHtmlQuoteBridgeFn = null
    }
}

/**
 * Tear down the bridge AND dismiss a quote bar whose text came from it.
 *
 * Once the iframe (or the file) is gone the quoted text cannot be reopened, so
 * leaving the bar up would offer to send something the user can no longer see.
 * The check is by file path rather than unconditional — a bar opened from
 * somewhere else (a chat selection) must survive a file switch.
 */
function detachHtmlQuoteBridge() {
    disposeHtmlQuoteBridge()
    const staged = quoteQuestion.quoteData.value
    if (htmlQuoteBridgePath && staged?.filePath === htmlQuoteBridgePath) {
        quoteQuestion.hideBar()
    }
    htmlQuoteBridgePath = ''
}

// Edit mode (source text editing via CodeEditor).
// Shared at module level so the global back gesture (App.vue) can exit edit
// mode first instead of navigating back / closing the file while editing.
//
// An unsaved Untitled buffer IS an edit session: it opens in edit mode and stays
// there, which is what makes the back state machine run the save/discard
// confirmation before anything leaves it. It has no read-only form to fall back
// to, though, so every exit ends the visit — VSCode closes the untitled tab. The
// back-gesture path does that in handleExitEditRequest below; the header/nav
// paths navigate away through guardExitEdit.
const fileEditor = useFileEditor()
const editing = fileEditor.editing
const isUntitled = computed(() => !!props.file?.untitled)
const { saving, saveFile } = useCodeEditorSave()
const cmEditorRef = ref(null)
const excalidrawViewerRef = ref(null)
const mdPreviewRef = ref(null)
/**
 * Resolves when an in-flight first save of an Untitled buffer finishes (true on
 * success). The editor's exit flow emits 'saveAndExit' without awaiting the
 * save, so the close has to wait here — otherwise the viewer, and the filename
 * prompt it is showing, would be torn down mid-save.
 */
let untitledSavePromise = null

// The global back handler calls exitEdit() → run the active editor's exit flow,
// which confirms save/discard/cancel when there are unsaved changes. For
// Excalidraw files the iframe editor registers its own flow; otherwise it's
// CodeMirrorViewer.
//
// The state machine's 'edit' step navigates nowhere on its own, so an Untitled
// buffer — which has no read-only form to fall back to — has to close itself
// once the flow settles (VSCode closes the untitled tab). It cannot use
// 'overlayClose' for that: the close would route back into the state machine,
// which is still busy with this very step and would swallow it. Hence the
// dedicated 'closeUntitled' event. The other exit paths (header X / nav) already
// navigate through guardExitEdit's action, so they need nothing extra.
function handleExitEditRequest() {
    if (props.file?.isExcalidraw) {
        excalidrawViewerRef.value?.requestExit?.()
        return
    }
    if (!isUntitled.value) {
        cmEditorRef.value?.handleExit?.()
        return
    }
    Promise.resolve(cmEditorRef.value?.handleExit?.()).then(async (exited) => {
        if (exited !== true) return // user cancelled — stay in the buffer
        // A "save" choice returns before its write (and filename prompt) settle.
        // Awaiting that promise covers the failure cases too: a cancelled prompt
        // or a failed write resolves false and leaves the buffer open.
        if (untitledSavePromise) {
            const pending = untitledSavePromise
            untitledSavePromise = null
            if (!(await pending)) return
        }
        emit('closeUntitled')
    })
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null
let unregisterLeaveGuard = null
onMounted(() => {
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExitEditRequest)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => cmEditorRef.value?.isDirty?.() ?? false)
    // A project switch unmounts this viewer and would silently drop unsaved
    // edits. Reuse the canonical guard (save/discard/cancel) without the back
    // gesture's untitled close side effect, so the switch aborts only when the
    // user cancels or a save fails.
    unregisterLeaveGuard = fileEditor.registerLeaveGuard(() => guardExitEdit(() => {}))
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
    if (unregisterLeaveGuard) {
        unregisterLeaveGuard()
        unregisterLeaveGuard = null
    }
})

// Save and stay in edit mode. Clicking save / Ctrl+S only persists the file,
// it does not leave the edit view so the user can keep making edits.
//
// The first save of an Untitled buffer names the file and clears its `untitled`
// flag, which is what kept the editor editable. Edit mode must therefore be
// entered explicitly, or the buffer would silently turn read-only the moment it
// was saved — the opposite of what the user just asked for.
async function handleSave(content) {
    const wasUntitled = isUntitled.value
    const ok = await saveFile(props.file?.path || '', content)
    if (ok && wasUntitled) {
        editing.value = true
    }
}

// Save and then exit edit mode. Used by the exit flows (back / toggle view)
// which confirm save-or-discard; only these paths leave the edit view.
//
// An Untitled buffer's first save is asynchronous (it prompts for a filename),
// so the promise is exposed for handleExitEditRequest to await before closing.
// The close itself is not emitted here: every caller of this handler navigates
// away through guardExitEdit's action, and closing here as well would do it
// twice. Only the back gesture's 'edit' step — which navigates nowhere — closes
// on its own, in handleExitEditRequest.
async function handleSaveAndExit(content) {
    const save = saveFile(props.file?.path || '', content)
    if (isUntitled.value) untitledSavePromise = save
    const ok = await save
    if (ok) {
        editing.value = false
    }
}

// Discard-and-exit, or a plain exit with nothing to save. Pure state change —
// see handleSaveAndExit for why the close is not emitted here.
function handleExitEdit() {
    editing.value = false
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
    // inline search bar (opened via the searchOpen prop); the rendered HTML
    // preview focuses its own inline bar, which searches the preview iframe's
    // document.
    if (isCodeMirrorView.value) {
        cmEditorRef.value?.openSearch?.()
    } else if (!editing.value && props.markdownViewMode === 'rendered' && isMarkdown.value) {
        mdPreviewRef.value?.focusSearchInput?.()
    } else if (!editing.value && props.markdownViewMode === 'rendered' && isHtml.value) {
        htmlSearchBarRef.value?.focus?.()
    }
}

/**
 * Dismiss the active search UI. The mirror of focusSearchInput, and the only
 * way to close the CodeMirror panel from outside: focusing it is idempotent
 * (a repeat Ctrl+F must not close the bar), so the toolbar toggle cannot use
 * it as a close.
 */
function closeSearchInput() {
    if (isCodeMirrorView.value) {
        cmEditorRef.value?.closeSearch?.()
    }
}

/**
 * Ctrl/Cmd+G — go to line (issue #547).
 *
 * Only CodeMirror surfaces have line numbers, so rendered previews (markdown,
 * HTML, OpenAPI, media) return false and let the shortcut fall through to the
 * browser instead of opening a prompt that cannot be honoured.
 */
function openGoToLine() {
    if (!isCodeMirrorView.value) return false
    // Strictly `=== true`: a missing/older child implementation must not claim
    // the key, or App would preventDefault a shortcut nobody handled.
    return cmEditorRef.value?.openGoToLine?.() === true
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
    // Untitled buffers are always in an editor session, whether or not they are
    // dirty — an empty new file still needs its save/discard decision before the
    // visit is closed.
    if (editing.value || isUntitled.value) {
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
    // The bridge lives in the previewed document; leaving it attached would
    // keep listeners alive on a document nothing can see.
    detachHtmlQuoteBridge()
    disposeHtmlSearchShortcut?.()
    disposeHtmlSearchShortcut = null
    disposeHtmlThemeWatcher?.()
    disposeHtmlThemeWatcher = null
    disposeHtmlLinkHandler?.()
    disposeHtmlLinkHandler = null
})

// Save/restore scroll position when switching files. The watcher keeps the
// viewer-level responsibilities (edit-mode reset, loading flags); the scroll
// bookkeeping itself is delegated to the composable, preserving the original
// ordering: save the outgoing file → reset edit mode → schedule a restore.
watch(() => props.file, (f, oldF) => {
    scrollRestore.onFileWillChange()
    // The previewed document belongs to the outgoing file; its bridge must not
    // outlive it (switching to a non-HTML file unmounts the iframe without a
    // load event to re-attach through).
    detachHtmlQuoteBridge()
    disposeHtmlSearchShortcut?.()
    disposeHtmlSearchShortcut = null

    // An unsaved Untitled buffer opens directly in edit mode — there is no
    // read-only form of a file that does not exist yet. Set here rather than by
    // the opener because this watcher also runs on mount (immediate), after the
    // opener has returned, so it is the only place that reliably covers it.
    editing.value = !!f?.untitled
    // A pending first-save belongs to the buffer being left; carrying it into the
    // next file would let a stale success trigger that file's untitled close.
    untitledSavePromise = null

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
    // The HTML preview iframe loads the document by URL, so a content change
    // (fsnotify-driven refresh, or an AI edit) does NOT re-render it the way the
    // old srcdoc binding did. Bump the cache-buster to force a re-fetch.
    if (isHtml.value) htmlPreviewTimestamp.value = Date.now()
})

/**
 * Re-run the previewed document on every refresh, even a no-op one.
 *
 * The content watcher above only fires when the BYTES changed, but a previewed
 * page can change appearance without its own source changing — an external
 * stylesheet, script or image it references. Clicking refresh in that case
 * produced no visible effect at all (same content ⇒ same `?t=` ⇒ no navigation),
 * which reads as a dead button. `refreshTick` is bumped by every completed
 * refresh, so this is the signal that the user asked, not that the file moved.
 *
 * The timestamp is monotonic rather than `Date.now()` so two refreshes in the
 * same millisecond still change the URL.
 */
watch(refreshTick, () => {
    if (isHtml.value) htmlPreviewTimestamp.value += 1
})

// Sync scroll position when toggling rendered <-> raw view for a markdown file.
// Runs as a pre-flush watcher: the pane being left is still mounted, so we
// capture its anchor before the new pane swaps in.
watch(() => props.markdownViewMode, (newMode, oldMode) => {
    if (newMode === oldMode) return
    // Toggling to source unmounts the HTML preview iframe without a load event,
    // so the bridge into its document has to be dropped here.
    if (isHtml.value) {
        detachHtmlQuoteBridge()
        disposeHtmlSearchShortcut?.()
        disposeHtmlSearchShortcut = null
    }
    if (!isMarkdown.value || !props.file?.content) return
    const oldEl = scrollRestore.scrollElFor(oldMode, editing.value)
    const saved = scrollRestore.captureScroll(oldEl)
    if (saved) scrollRestore.restoreAfterContainerSwitch(saved)
})

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

    toast.show(t('file.header.exportingHtml'), { icon: 'file', type: 'info', duration: 0 })
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
        toast.show(msgs.join('. '), { icon: 'success', type: 'success', duration: 4000 })
    } catch {
        toast.show(t('file.header.exportHtmlFailed'), { icon: 'error', type: 'error', duration: 3000 })
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
    closeSearchInput,
    openGoToLine,
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

/* The placeholder layout (icon / title / desc / actions) lives in
   UnsupportedFileBody.vue, shared with the quick-preview pane. Only the action
   buttons below are viewer-specific. */
.open-as-text-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: calc(5 * var(--dp)) var(--space-6);
    background: transparent;
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-lg);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
    transition: all var(--duration-base);
    gap: var(--space-2);
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
    padding: calc(5 * var(--dp)) var(--space-6);
    background: var(--accent-color);
    color: #fff;
    border: none;
    border-radius: var(--radius-lg);
    text-decoration: none;
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-medium);
    transition: filter var(--duration-base);
    gap: var(--space-2);
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
    /* Vertical only: the horizontal inset belongs to the skeleton, so it can
       match the markdown column it stands in for. */
    padding: calc(40 * var(--dp)) 0;
}

/* The placeholder owns its own width: the wrapper centres, but a text skeleton
   squeezed to its content would not line up with the real paragraph. It also
   takes the markdown reading column's measure and inset (`.markdown-body`:
   900dp cap, 10dp padding) — at the wrapper's old 40dp the first line sat 30dp
   right of where the preview text lands. */
.file-viewer-skeleton {
    width: 100%;
    max-width: min(calc(900 * var(--dp)), 100%);
    margin: 0 auto;
    --panel-skeleton-inset: calc(10 * var(--dp));
}


.error-bubble {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    background: rgba(239, 68, 68, 0.1);
    color: var(--color-red);
    padding: var(--space-3) var(--space-6);
    border-radius: calc(20 * var(--dp));
    font-size: var(--font-size-md);
    margin: calc(24 * var(--dp)) auto;
    max-width: 90%;
    line-height: var(--line-height-snug);
    align-self: center;
}

.html-preview-pane {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
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
    gap: var(--space-3);
    padding: var(--space-3) var(--space-6);
    background: rgba(245, 158, 11, 0.1);
    color: var(--color-orange);
    font-size: var(--font-size-sm);
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
