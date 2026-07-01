<template>
  <div class="pdf-preview-container">
    <!-- Toolbar -->
    <div class="pdf-toolbar">
      <div class="pdf-toolbar-left">
        <button class="pdf-btn" @click="goPrevPage" :disabled="currentPage <= 1" title="上一页">
          <ChevronLeft :size="14" />
        </button>
        <span class="pdf-page-info">
          <input
            class="pdf-page-input"
            type="number"
            :value="currentPage"
            :min="1"
            :max="pageCount"
            @change="goToPage($event)"
            @keydown.enter="goToPage($event)"
          />
          <span class="pdf-page-sep">/</span>
          <span class="pdf-page-total">{{ pageCount }}</span>
        </span>
        <button class="pdf-btn" @click="goNextPage" :disabled="currentPage >= pageCount" title="下一页">
          <ChevronRight :size="14" />
        </button>
      </div>
      <div class="pdf-toolbar-right">
        <button class="pdf-btn" @click="zoomOut" :disabled="scale <= MIN_SCALE" title="缩小">
          <ZoomOut :size="14" />
        </button>
        <span class="pdf-zoom-label">{{ Math.round(scale * 100) }}%</span>
        <button class="pdf-btn" @click="zoomIn" :disabled="scale >= MAX_SCALE" title="放大">
          <ZoomIn :size="14" />
        </button>
        <button class="pdf-btn" @click="fitWidth" title="适合宽度">
          <MoveHorizontal :size="14" />
        </button>
        <a v-if="!isAppMode" class="pdf-btn" :href="buildLocalFileUrl(file.path, { download: true })" download title="下载">
          <Download :size="14" />
        </a>
        <button v-else class="pdf-btn" @click="handleDownload" title="下载">
          <Download :size="14" />
        </button>
      </div>
    </div>

    <!-- Pages -->
    <div class="pdf-pages-scroll" ref="scrollRef" @scroll="onScroll" @touchstart.passive="onTouchStart" @touchmove="onTouchMove" @touchend="onTouchEnd" @touchcancel="onTouchEnd" @wheel.prevent="onWheel">
      <div class="pdf-pages-inner" :style="pagesInnerStyle">
        <div
          v-for="page in pageCount"
          :key="page"
          class="pdf-page-wrapper"
          :data-page="page"
        >
          <canvas :ref="el => setCanvasRef(page, el)" class="pdf-page-canvas" />
        </div>
      </div>
    </div>

    <!-- Global loading overlay -->
    <div v-if="loading" class="pdf-loading-overlay">
      <Loader :size="32" />
      <span class="pdf-loading-text">加载中...</span>
    </div>

    <!-- Error -->
    <div v-if="error" class="pdf-error">
      <FileX :size="48" />
      <div class="pdf-error-title">PDF 加载失败</div>
      <div class="pdf-error-desc">{{ error }}</div>
      <a v-if="!isAppMode" :href="buildLocalFileUrl(file.path, { download: true })" class="pdf-download-link" download>
        <Download :size="14" />
        下载文件
      </a>
      <button v-else class="pdf-download-link" @click="handleDownload">
        <Download :size="14" />
        下载文件
      </button>
    </div>
  </div>
</template>

<script setup>
import {
  ChevronLeft, ChevronRight, ZoomIn, ZoomOut,
  Download, Loader, FileX, MoveHorizontal,
} from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'

const MIN_SCALE = 0.25
const MAX_SCALE = 5.0
const SCALE_STEP = 0.25
const RENDER_PADDING = 1

const props = defineProps({
  file: Object,
})

const { isAppMode } = useAppMode()

// PDF outline (bookmarks) for TOC
const outline = ref([])

// Flatten PDF outline tree into TocItem-like list
function flattenOutline(items, level = 1) {
  const result = []
  for (const item of items) {
    const title = item.title || ''
    let page = 0
    // Try to extract page number from dest
    if (item.dest) {
      if (typeof item.dest === 'string') {
        // Named dest — resolve later; store as string for now
        page = -1 // marker: needs resolution
      } else if (Array.isArray(item.dest) && item.dest.length > 0) {
        // dest[0] is a page ref object
        page = -1 // will resolve below
      }
    }
    // pdfjs-dist: item.dest can be resolved via pdfDoc.getDestination()
    result.push({
      level,
      text: title,
      id: `pdf-toc-${result.length}`,
      line: page, // reuse 'line' as page number for TocDrawer compatibility
      _dest: item.dest,
    })
    if (item.items && item.items.length > 0) {
      result.push(...flattenOutline(item.items, level + 1))
    }
  }
  return result
}

// Resolve page numbers for outline items
async function resolveOutlinePages(items) {
  if (!pdfDoc) return
  for (const item of items) {
    if (item._dest) {
      try {
        let dest = item._dest
        if (typeof dest === 'string') {
          dest = await pdfDoc.getDestination(dest)
        }
        if (Array.isArray(dest) && dest.length > 0) {
          const pageRef = dest[0]
          const pageIndex = await pdfDoc.getPageIndex(pageRef)
          item.line = pageIndex + 1 // 1-based page number
        }
      } catch {
        item.line = 0
      }
    }
    delete item._dest
  }
}

// PDF.js state
let pdfDoc = null
const pageCount = ref(0)
const currentPage = ref(1)
const scale = ref(1.0)
const loading = ref(true)
const error = ref('')

// DOM refs
const scrollRef = ref(null)
const canvasRefs = {}

// Observer for lazy rendering
let observer = null
const renderedPages = new Set()

// Viewport info per page (from pdf.getPage at scale=1)
const pageViewports = ref([])

// Computed
const mediaUrl = computed(() =>
  `/api/local-file/${encodeURIComponent(props.file.path)}`
)

const pagesInnerStyle = computed(() => {
  if (pageViewports.value.length === 0) return {}
  let maxW = 0
  for (const vp of pageViewports.value) {
    if (vp) maxW = Math.max(maxW, Math.ceil(vp.width * scale.value))
  }
  return maxW ? { minWidth: maxW + 'px' } : {}
})

// Methods
function setCanvasRef(page, el) {
  if (el) canvasRefs[page] = el
  else delete canvasRefs[page]
}

async function loadPdf() {
  loading.value = true
  error.value = ''
  renderedPages.clear()

  try {
    const pdfjsLib = await import('pdfjs-dist')
    const workerUrl = await import('pdfjs-dist/build/pdf.worker.min.mjs?url')
    pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl.default || workerUrl

    const loadingTask = pdfjsLib.getDocument(mediaUrl.value)
    pdfDoc = await loadingTask.promise
    pageCount.value = pdfDoc.numPages
    currentPage.value = 1

    // Cache viewports at scale=1 for all pages
    const vps = []
    for (let i = 1; i <= pdfDoc.numPages; i++) {
      const page = await pdfDoc.getPage(i)
      vps.push(page.getViewport({ scale: 1 }))
    }
    pageViewports.value = vps

    loading.value = false
    await nextTick()

    // Extract PDF outline (bookmarks/TOC)
    try {
      const rawOutline = await pdfDoc.getOutline()
      if (rawOutline && rawOutline.length > 0) {
        const flat = flattenOutline(rawOutline)
        await resolveOutlinePages(flat)
        outline.value = flat
      } else {
        outline.value = []
      }
    } catch {
      outline.value = []
    }

    fitWidth()
    setupObserver()
  } catch (e) {
    loading.value = false
    error.value = e.message || '未知错误'
  }
}

async function renderPage(pageNum, force = false) {
  if (!pdfDoc || (renderedPages.has(pageNum) && !force)) return
  const canvas = canvasRefs[pageNum]
  if (!canvas) return

  renderedPages.add(pageNum)

  try {
    const page = await pdfDoc.getPage(pageNum)
    const viewport = page.getViewport({ scale: scale.value })
    const ctx = canvas.getContext('2d')

    const dpr = window.devicePixelRatio || 1
    canvas.width = Math.floor(viewport.width * dpr)
    canvas.height = Math.floor(viewport.height * dpr)
    canvas.style.width = Math.floor(viewport.width) + 'px'
    canvas.style.height = Math.floor(viewport.height) + 'px'
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

    await page.render({ canvasContext: ctx, viewport }).promise
  } catch {
    renderedPages.delete(pageNum)
  }
}

// Update CSS dimensions of all page canvases for instant visual scaling
function updateCanvasCssSizes() {
  for (const [pageNum, canvas] of Object.entries(canvasRefs)) {
    if (!canvas) continue
    const vp = pageViewports.value[pageNum - 1]
    if (vp) {
      canvas.style.width = Math.floor(vp.width * scale.value) + 'px'
      canvas.style.height = Math.floor(vp.height * scale.value) + 'px'
    }
  }
}

// Mark all pages as needing re-render, then re-render visible ones
function invalidateAndRerender() {
  renderedPages.clear()
  // Re-render visible pages with new scale
  renderVisiblePages()
}

function renderVisiblePages() {
  if (!scrollRef.value || pageCount.value === 0) return
  const containerTop = scrollRef.value.getBoundingClientRect().top
  const containerH = scrollRef.value.clientHeight
  for (let i = 1; i <= pageCount.value; i++) {
    const wrapper = scrollRef.value.querySelector(`[data-page="${i}"]`)
    if (!wrapper) continue
    const rect = wrapper.getBoundingClientRect()
    // Render pages within viewport + generous padding
    if (rect.bottom >= containerTop - 500 && rect.top <= containerTop + containerH + 500) {
      renderPage(i, true)
    }
  }
}

// Navigation
function goPrevPage() {
  if (currentPage.value > 1) {
    currentPage.value--
    scrollToPage(currentPage.value)
  }
}

function goNextPage() {
  if (currentPage.value < pageCount.value) {
    currentPage.value++
    scrollToPage(currentPage.value)
  }
}

function goToPage(e) {
  const val = parseInt(e.target.value, 10)
  if (val >= 1 && val <= pageCount.value) {
    currentPage.value = val
    scrollToPage(val)
  } else {
    e.target.value = currentPage.value
  }
}

function scrollToPage(pageNum) {
  const el = scrollRef.value
  if (!el) return
  const pageWrapper = el.querySelector(`[data-page="${pageNum}"]`)
  if (pageWrapper) {
    pageWrapper.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
}

// Zoom
function zoomIn() {
  scale.value = Math.min(scale.value + SCALE_STEP, MAX_SCALE)
}

function zoomOut() {
  scale.value = Math.max(scale.value - SCALE_STEP, MIN_SCALE)
}

function fitWidth() {
  if (!scrollRef.value || pageViewports.value.length === 0) return
  const containerWidth = scrollRef.value.clientWidth
  const vp = pageViewports.value[0]
  if (vp) {
    scale.value = Math.max(MIN_SCALE, Math.min(containerWidth / vp.width, MAX_SCALE))
  }
}

// Pinch-to-zoom (touch)
const pinchStartDist = ref(0)
const pinchStartScale = ref(1)

function onTouchStart(e) {
  if (e.touches.length === 2) {
    pinchStartDist.value = Math.hypot(
      e.touches[0].clientX - e.touches[1].clientX,
      e.touches[0].clientY - e.touches[1].clientY,
    )
    pinchStartScale.value = scale.value
  }
}

function onTouchMove(e) {
  if (e.touches.length === 2 && pinchStartDist.value > 0) {
    e.preventDefault()
    const dist = Math.hypot(
      e.touches[0].clientX - e.touches[1].clientX,
      e.touches[0].clientY - e.touches[1].clientY,
    )
    const ratio = dist / pinchStartDist.value
    const newScale = Math.max(MIN_SCALE, Math.min(pinchStartScale.value * ratio, MAX_SCALE))
    scale.value = newScale
  }
}

function onTouchEnd(e) {
  if (e.touches.length < 2) {
    pinchStartDist.value = 0
  }
}

// Ctrl+scroll-to-zoom (desktop)
function onWheel(e) {
  if (e.ctrlKey || e.metaKey) {
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    scale.value = Math.max(MIN_SCALE, Math.min(scale.value + delta, MAX_SCALE))
  }
}

// Scroll tracking
let scrollRafId = 0
function onScroll() {
  if (scrollRafId) return
  scrollRafId = requestAnimationFrame(() => {
    scrollRafId = 0
    updateCurrentPage()
  })
}

function updateCurrentPage() {
  if (!scrollRef.value || pageCount.value === 0) return
  const containerTop = scrollRef.value.getBoundingClientRect().top
  const containerH = scrollRef.value.clientHeight
  let bestPage = 1
  let bestOverlap = 0
  const wrappers = scrollRef.value.querySelectorAll('.pdf-page-wrapper')
  wrappers.forEach(wrapper => {
    const rect = wrapper.getBoundingClientRect()
    const overlapTop = Math.max(rect.top, containerTop)
    const overlapBottom = Math.min(rect.bottom, containerTop + containerH)
    const overlap = Math.max(0, overlapBottom - overlapTop)
    if (overlap > bestOverlap) {
      bestOverlap = overlap
      bestPage = parseInt(wrapper.dataset.page, 10)
    }
  })
  if (bestPage !== currentPage.value) {
    currentPage.value = bestPage
  }
}

// IntersectionObserver for lazy rendering
function setupObserver() {
  if (observer) observer.disconnect()
  observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        const pageNum = parseInt(entry.target.dataset.page, 10)
        if (pageNum >= 1 && pageNum <= pageCount.value) {
          renderPage(pageNum, true)
          for (let i = Math.max(1, pageNum - RENDER_PADDING); i <= Math.min(pageCount.value, pageNum + RENDER_PADDING); i++) {
            renderPage(i, true)
          }
        }
      }
    }
  }, {
    root: scrollRef.value,
    rootMargin: '200px 0px',
  })

  nextTick(() => {
    const wrappers = scrollRef.value?.querySelectorAll('.pdf-page-wrapper')
    wrappers?.forEach(wrapper => observer.observe(wrapper))
  })
}

// Download
function handleDownload() {
  downloadFileByPath(props.file.path)
}

// Lifecycle
onMounted(() => {
  loadPdf()
})

onUnmounted(() => {
  if (observer) { observer.disconnect(); observer = null }
  if (pdfDoc) { pdfDoc.destroy(); pdfDoc = null }
  if (scrollRafId) { cancelAnimationFrame(scrollRafId); scrollRafId = 0 }
})

// Re-load when file changes
watch(() => props.file?.path, (newPath, oldPath) => {
  if (newPath && newPath !== oldPath) {
    if (observer) { observer.disconnect(); observer = null }
    if (pdfDoc) { pdfDoc.destroy(); pdfDoc = null }
    loadPdf()
  }
})

// Re-render when scale changes: instant CSS resize, then async high-res render
watch(scale, () => {
  updateCanvasCssSizes()
  invalidateAndRerender()
})

// Expose outline and scrollToPage for TOC integration
defineExpose({
  outline,
  scrollToPage,
})
</script>

<style scoped>
.pdf-preview-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  position: relative;
  background: var(--bg-primary);
}

/* Toolbar */
.pdf-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 3px 8px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
  gap: 4px;
  flex-shrink: 0;
  overflow-x: auto;
}

.pdf-toolbar-left,
.pdf-toolbar-right {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.pdf-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.15s;
  flex-shrink: 0;
  text-decoration: none;
}

.pdf-btn:hover:not(:disabled) {
  background: var(--accent-color);
  color: #fff;
}

.pdf-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.pdf-page-info {
  display: flex;
  align-items: center;
  gap: 2px;
  font-size: 12px;
  color: var(--text-secondary);
}

.pdf-page-input {
  width: 32px;
  text-align: center;
  border: 1px solid var(--border-color);
  border-radius: 3px;
  padding: 1px 2px;
  font-size: 12px;
  color: var(--text-primary);
  background: var(--bg-primary);
  -moz-appearance: textfield;
}

.pdf-page-input::-webkit-inner-spin-button,
.pdf-page-input::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}

.pdf-page-sep {
  color: var(--text-muted);
  font-size: 11px;
}

.pdf-page-total {
  color: var(--text-muted);
  font-size: 12px;
}

.pdf-zoom-label {
  font-size: 11px;
  color: var(--text-muted);
  min-width: 30px;
  text-align: center;
}

/* Pages scroll area */
.pdf-pages-scroll {
  flex: 1;
  overflow: auto;
  padding: 8px 0;
  background: #525659;
  touch-action: pan-x pan-y;
  overscroll-behavior: contain;
}

.pdf-pages-inner {
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.pdf-page-wrapper {
  position: relative;
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.2);
  border-radius: 2px;
  flex-shrink: 0;
}

.pdf-page-canvas {
  display: block;
  border-radius: 2px;
}

/* Global loading overlay */
.pdf-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: #525659;
  color: rgba(255, 255, 255, 0.7);
  gap: 12px;
  z-index: 10;
}

.pdf-loading-overlay svg {
  animation: pdf-spin 1s linear infinite;
}

.pdf-loading-text {
  font-size: 14px;
}

@keyframes pdf-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Error */
.pdf-error {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 48px 24px;
  text-align: center;
  color: var(--text-muted);
  background: var(--bg-primary);
}

.pdf-error > svg {
  width: 48px;
  height: 48px;
  margin-bottom: 12px;
}

.pdf-error-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.pdf-error-desc {
  font-size: 14px;
  margin-bottom: 20px;
  max-width: 400px;
  word-break: break-word;
}

.pdf-download-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 6px 16px;
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: 14px;
  text-decoration: none;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  gap: 6px;
  transition: filter 0.15s;
}

.pdf-download-link:hover {
  filter: brightness(1.15);
}

/* Dark theme */
:global([data-theme="dark"]) .pdf-pages-scroll {
  background: #2a2d30;
}

:global([data-theme="dark"]) .pdf-page-wrapper {
  background: #1a1a1a;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.5);
}

:global([data-theme="dark"]) .pdf-loading-overlay {
  background: #2a2d30;
}

/* Mobile-friendly: slightly larger touch targets */
@media (hover: none) {
  .pdf-btn {
    width: 30px;
    height: 30px;
  }

  .pdf-page-input {
    width: 36px;
    height: 26px;
    font-size: 12px;
  }

  .pdf-toolbar {
    padding: 4px 6px;
  }
}
</style>
