<template>
  <Transition name="file-overlay">
    <div
      v-if="overlayOpen"
      class="file-overlay"
    >
      <div class="file-overlay-col">
        <!-- Main viewer area (no separate topbar — nav buttons are in FileManagerContent toolbar) -->
        <div class="file-overlay-body" ref="contentRef" @click="handleContentClick">
          <FileViewer
            ref="fileViewerRef"
            :file="currentFile"
            :toc-open="tocOpen"
            :search-open="searchOpen"
            :markdown-view-mode="markdownViewMode"
            :external-loading="fileLoading"
            :toc-file="tocFile"
            :pdf-outline="pdfOutline"
            :docked="docked"
            :can-navigate-back="canNavigateBack"
            :back-label="backLabel"
            :chat-quote="chatQuote"
            @delete="emit('delete', $event)"
            @show-details="emit('showDetails')"
            @open-git-history="emit('openGitHistory')"
            @toggle-toc="emit('toggleToc')"
            @close-toc="emit('closeToc')"
            @toggle-search="emit('toggleSearch')"
            @close-search="emit('closeSearch')"
            @search-change="emit('searchChange', $event)"
            @toggle-view="emit('toggleView')"
            @refresh="emit('refresh')"
            @open-file="emit('openFile', $event)"
            @overlay-close="emit('overlayClose')"
            @close-untitled="emit('closeUntitled')"
            @navigate-back="emit('navigateBack')"
            @navigate-forward="emit('navigateForward')"
            @capture-scroll="emit('captureScroll', $event)"
            @share-external="emit('shareExternal')"
            @share-link="emit('shareLink')"
            @set-as-background="(path) => emit('setAsBackground', path)"
            @quote-in-chat="(path) => emit('quoteInChat', path)"
            @open-search="emit('openSearch')"
            @jump="(line, anchorId) => emit('jump', line, anchorId)"
            @jump-page="emit('jumpPage', $event)"
          />
          <!-- File loading mask — masks the previous file's content while the
               new one arrives, so the placeholder also has to be opaque. -->
          <Transition name="loading-fade">
            <div v-if="fileLoading" class="file-loading-mask">
              <PanelSkeleton variant="text" :rows="10" :label="t('common.loading')" />
            </div>
          </Transition>
        </div>

        <!-- Drawers -->
        <!-- Narrow-screen TOC: bottom drawer. Wide-screen uses the inline
             TocDock rendered to the right of the content column instead. -->
        <TocDrawer
          v-if="!docked"
          :open="tocOpen"
          :file="tocFile"
          :pdf-outline="pdfOutline"
          @close="emit('toggleToc')"
          @jump="(line, anchorId) => emit('jump', line, anchorId)"
          @jump-page="emit('jumpPage', $event)"
        />

        <SearchDrawer
          ref="searchDrawerRef"
          :open="searchDrawerOpen"
          :file="currentFile"
          :view-mode="markdownViewMode"
          @close="emit('closeSearch')"
          @jump="emit('jump', $event)"
        />

        <GitHistoryDrawer
          :open="fileHistoryOpen"
          mode="file"
          :file="currentFile"
          @close="emit('closeGitHistory')"
          @open-file="emit('openFile', $event)"
        />
      </div>
    </div>
  </Transition>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import FileViewer from '@/components/file/FileViewer.vue'
import TocDrawer from '@/components/TocDrawer.vue'
import SearchDrawer from '@/components/common/SearchDrawer.vue'
import GitHistoryDrawer from '@/components/git/GitHistoryDrawer.vue'
import { getWideScreenState } from '@/composables/useWideScreenLayout'
import { readLineTargetFromEl } from '@/composables/useFilePathAnnotation.ts'

const { isWideScreen } = getWideScreenState()
const { t } = useI18n()

const props = defineProps({
  overlayOpen: Boolean,
  currentFile: Object,
  fileLoading: Boolean,
  tocOpen: Boolean,
  searchOpen: Boolean,
  /** Whether the SearchDrawer bottom sheet itself should be shown (fallback
      views only — markdown/code use inline search UIs driven by searchOpen). */
  searchDrawerOpen: Boolean,
  markdownViewMode: String,
  fileHistoryOpen: Boolean,
  tocFile: Object,
  pdfOutline: Object,
  canNavigateBack: Boolean,
  backLabel: String,
  /** Enable the selection→quote bridge in the rendered HTML preview. */
  chatQuote: { type: Boolean, default: false },
})

/** Wide-screen inline TOC dock vs narrow-screen bottom drawer. */
const docked = computed(() => isWideScreen.value)

const emit = defineEmits([
  'delete', 'showDetails', 'openGitHistory',
  'toggleToc', 'closeToc', 'toggleSearch', 'closeSearch', 'searchChange', 'toggleView', 'refresh',
  'jump', 'jumpPage', 'closeGitHistory', 'openFile',
  'overlayClose', 'closeUntitled', 'navigateBack', 'navigateForward', 'shareExternal', 'shareLink',
  'setAsBackground',
  'captureScroll',
  'quoteInChat',
  'openSearch',
])

const contentRef = ref(null)
const fileViewerRef = ref(null)
const searchDrawerRef = ref(null)

// Forward pdfOutline from FileViewer's exposed API
const pdfOutline = computed(() => fileViewerRef.value?.pdfOutline || props.pdfOutline || [])

function pdfScrollToPage(pageNum) {
  fileViewerRef.value?.pdfScrollToPage(pageNum)
}

function focusSearchInput() {
  // FileViewer routes internally: CodeMirror views open the editor's own
  // search panel, rendered markdown opens its inline search bar. The
  // SearchDrawer is only touched when it is already open (legacy fallback).
  fileViewerRef.value?.focusSearchInput?.()
  searchDrawerRef.value?.focusSearchInput?.()
}

/** Dismiss the active search UI (toolbar toggle's close half). */
function closeSearchInput() {
  fileViewerRef.value?.closeSearchInput?.()
}

function captureScroll() {
  return fileViewerRef.value?.captureScroll?.()
}

/** Ctrl/Cmd+G — go to line. Returns false for surfaces with no line numbers
    (rendered previews, media) so the caller can leave the key to the browser. */
function openGoToLine() {
  return fileViewerRef.value?.openGoToLine?.() ?? false
}

defineExpose({ pdfScrollToPage, pdfOutline, focusSearchInput, closeSearchInput, openGoToLine, captureScroll })

// Intercept file-path link clicks inside the overlay content.
// When a user clicks a .chat-file-open-btn, .chat-file-path, or .code-file-path,
// instead of navigating via store.selectFile, emit 'openFile' so the
// parent (App.vue) can push onto the nav stack and stay in overlay mode.
function handleContentClick(event) {
  // 1. Handle file-open button clicks
  const btn = event.target.closest('.chat-file-open-btn')
  if (btn) {
    event.preventDefault()
    event.stopPropagation()
    fileViewerRef.value?.captureScroll?.()
    const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(btn)
    if (filePath) {
      emit('openFile', { path: filePath, lineStart, lineEnd, lineRanges })
    }
    return
  }

  // 2. Handle clicks on annotated file-path spans (markdown or code).
  // Skip inert paths (verified missing / glob patterns): they carry
  // data-path-type="none" and no open affordance, so opening them would just
  // toast "File not found" — the defect reported in issue #501. They keep
  // their text (and their data-file-path, for a later re-verification pass),
  // which is why this guard is needed rather than relying on the selector.
  const pathSpan = event.target.closest('.chat-file-path:not([data-path-type="none"]), .code-file-path:not([data-path-type="none"])')
  if (pathSpan) {
    event.preventDefault()
    event.stopPropagation()
    fileViewerRef.value?.captureScroll?.()
    const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(pathSpan)
    if (filePath) {
      emit('openFile', { path: filePath, lineStart, lineEnd, lineRanges })
    }
    return
  }
}

</script>

<style scoped>
.file-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: row;
  background: var(--bg-primary);
  overflow: hidden;
  /* No z-index here. FileOverlay is the only child of its tab panel, so it
     needs no stacking level inside the panel; an explicit z-index would
     escape the panel unless the panel is isolated (TabPanel uses
     isolation:isolate), risking covers over the split divider. */
}

.file-overlay-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.file-overlay-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  position: relative;
}

/* Opaque mask: the previous file's content is still mounted underneath, so a
   transparent placeholder would show two documents at once. */
.file-loading-mask {
  position: absolute;
  inset: 0;
  z-index: 5;
  background: var(--bg-primary, #fff);
  padding: var(--space-6);
  overflow: hidden;
}
</style>

<style>
/* Slide-in animation — must be non-scoped for Transition classes */
.file-overlay-enter-active,
.file-overlay-leave-active {
  transition: transform 0.25s ease;
}
.file-overlay-enter-from,
.file-overlay-leave-to {
  transform: translateX(100%);
}
</style>
