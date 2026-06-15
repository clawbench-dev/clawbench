<template>
  <Transition name="file-overlay">
    <div
      v-if="overlayOpen"
      class="file-overlay"
      @click="handleOverlayClick"
    >
      <!-- Top bar with close / go-back buttons -->
      <div class="file-overlay-topbar">
        <button
          v-if="canGoBack"
          class="overlay-btn overlay-btn-back"
          @click.stop="emit('goBack')"
        >
          <ChevronLeft :size="20" />
        </button>
        <button
          class="overlay-btn overlay-btn-close"
          @click.stop="emit('close')"
        >
          <X :size="20" />
        </button>
      </div>

      <!-- Main viewer area -->
      <div class="file-overlay-body" ref="contentRef" @click="handleContentClick">
        <FileViewer
          ref="fileViewerRef"
          :file="currentFile"
          :toc-open="tocOpen"
          :search-open="searchOpen"
          :markdown-view-mode="markdownViewMode"
          @delete="emit('delete')"
          @show-details="emit('showDetails')"
          @open-git-history="emit('openGitHistory')"
          @toggle-toc="emit('toggleToc')"
          @toggle-search="emit('toggleSearch')"
          @toggle-view="emit('toggleView')"
          @refresh="emit('refresh')"
          @open-file="emit('openFile', $event)"
        />
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
import { ChevronLeft, X } from 'lucide-vue-next'
import FileViewer from '@/components/file/FileViewer.vue'
import TocDrawer from '@/components/TocDrawer.vue'
import SearchDrawer from '@/components/common/SearchDrawer.vue'
import GitHistoryDrawer from '@/components/git/GitHistoryDrawer.vue'

const props = defineProps({
  overlayOpen: Boolean,
  currentFile: Object,
  canGoBack: Boolean,
  tocOpen: Boolean,
  searchOpen: Boolean,
  markdownViewMode: String,
  fileHistoryOpen: Boolean,
  tocFile: Object,
  pdfOutline: Object,
})

const emit = defineEmits([
  'close', 'goBack', 'delete', 'showDetails', 'openGitHistory',
  'toggleToc', 'toggleSearch', 'toggleView', 'refresh',
  'jump', 'jumpPage', 'closeGitHistory', 'openFile',
])

const contentRef = ref(null)
const fileViewerRef = ref(null)

// Forward pdfOutline from FileViewer's exposed API
const pdfOutline = computed(() => fileViewerRef.value?.pdfOutline || props.pdfOutline || [])

// Click on the overlay background (outside the body) closes it
function handleOverlayClick(event) {
  if (event.target === event.currentTarget) {
    emit('close')
  }
}

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

.file-overlay-topbar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  flex-shrink: 0;
  z-index: 1;
}

.overlay-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.overlay-btn:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.overlay-btn:active {
  background: var(--bg-secondary);
}

.overlay-btn-close {
  margin-left: auto;
}

.file-overlay-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
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
