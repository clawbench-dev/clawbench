<template>
  <Transition name="file-overlay">
    <div
      v-if="overlayOpen"
      class="file-overlay"
    >
      <!-- Main viewer area (no separate topbar — nav buttons are in FileManagerContent toolbar) -->
      <div class="file-overlay-body" ref="contentRef" @click="handleContentClick">
        <FileViewer
          ref="fileViewerRef"
          :file="currentFile"
          :toc-open="tocOpen"
          :search-open="searchOpen"
          :markdown-view-mode="markdownViewMode"
          :external-loading="fileLoading"
          @delete="emit('delete', $event)"
          @show-details="emit('showDetails')"
          @open-git-history="emit('openGitHistory')"
          @toggle-toc="emit('toggleToc')"
          @toggle-search="emit('toggleSearch')"
          @toggle-view="emit('toggleView')"
          @refresh="emit('refresh')"
          @open-file="emit('openFile', $event)"
          @overlay-close="emit('overlayClose')"
          @overlay-go-back="emit('overlayGoBack')"
        />
        <!-- File loading mask — same style as chat session-switch -->
        <Transition name="loading-fade">
          <div v-if="fileLoading" class="loading-mask">
            <div class="loading-mask-spinner"></div>
          </div>
        </Transition>
      </div>

      <!-- Drawers -->
      <TocDrawer
        :open="tocOpen"
        :file="tocFile"
        :pdf-outline="pdfOutline"
        @close="emit('toggleToc')"
        @jump="emit('jump', $event)"
        @jump-page="emit('jumpPage', $event)"
      />

      <SearchDrawer
        :open="searchOpen"
        :file="currentFile"
        :view-mode="markdownViewMode"
        @close="emit('toggleSearch')"
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
  </Transition>
</template>

<script setup>
import { ref, computed } from 'vue'
import '@/assets/loading-mask.css'
import FileViewer from '@/components/file/FileViewer.vue'
import TocDrawer from '@/components/TocDrawer.vue'
import SearchDrawer from '@/components/common/SearchDrawer.vue'
import GitHistoryDrawer from '@/components/git/GitHistoryDrawer.vue'

const props = defineProps({
  overlayOpen: Boolean,
  currentFile: Object,
  fileLoading: Boolean,
  tocOpen: Boolean,
  searchOpen: Boolean,
  markdownViewMode: String,
  fileHistoryOpen: Boolean,
  tocFile: Object,
  pdfOutline: Object,
})

const emit = defineEmits([
  'delete', 'showDetails', 'openGitHistory',
  'toggleToc', 'toggleSearch', 'toggleView', 'refresh',
  'jump', 'jumpPage', 'closeGitHistory', 'openFile',
  'overlayClose', 'overlayGoBack',
])

const contentRef = ref(null)
const fileViewerRef = ref(null)

// Forward pdfOutline from FileViewer's exposed API
const pdfOutline = computed(() => fileViewerRef.value?.pdfOutline || props.pdfOutline || [])

function pdfScrollToPage(pageNum) {
  fileViewerRef.value?.pdfScrollToPage(pageNum)
}

defineExpose({ pdfScrollToPage, pdfOutline })

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
    const filePath = btn.getAttribute('data-file-path')
    if (filePath) {
      emit('openFile', filePath)
    }
    return
  }

  // 2. Handle clicks on annotated file-path spans (markdown or code)
  const pathSpan = event.target.closest('.chat-file-path, .code-file-path')
  if (pathSpan) {
    event.preventDefault()
    event.stopPropagation()
    const filePath = pathSpan.getAttribute('data-file-path')
    if (filePath) {
      emit('openFile', filePath)
    }
    return
  }
}
</script>

<style scoped>
.file-overlay {
  position: absolute;
  inset: 0;
  z-index: 100;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
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
