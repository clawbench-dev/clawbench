<template>
  <div class="pdf-preview-container">
    <!-- Pages -->
    <div class="pdf-pages-scroll" ref="scrollRef" @scroll="onScroll" @touchstart.passive="onTouchStart" @touchmove="onTouchMove" @touchend="onTouchEnd" @touchcancel="onTouchEnd" @wheel="onWheel">
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
  Download, Loader, FileX,
} from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'

const MIN_SCALE = 0.25
const MAX_SCALE = 5.0
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

// Per-page render bookkeeping to avoid concurrent renders of the same page.
// Both the scale watcher (renderVisiblePages) and the IntersectionObserver can
// request the same page at once; overlapping page.render() calls corrupt the
// canvas (e.g. appears upside down on first paint) until a later re-render.
const renderTasks = {} // pageNum -> RenderTask
const renderGen = {} // pageNum -> generation counter

// Observer for lazy rendering
let observer = null
const renderedPages = new Set()

// Viewport info per page (from pdf.getPage at scale=1)
const pageViewports = ref([])

// Computed
const mediaUrl = computed(() =>
  buildLocalFileUrl(props.file.path)
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
    // Use pdfjs-dist's *legacy* builds which bundle core-js polyfills for
    // older engines (mobile WebViews, Chrome < 133). The modern build hard
    // requires Uint8Array.prototype.toHex (Chromium 133+, used in worker for
    // PDF fingerprints), URL.parse (126+) and Promise.try (134+). The worker
    // must come from the same legacy tree or the handshake dies mid-parse.
    // Cost: worker grows ~50KB (core-js). On new engines, polyfills are no-ops.
    // 改用 legacy 构建（内嵌 core-js），兼容旧引擎。现代构建硬依赖
    // toHex/URL.parse/Promise.try，旧引擎上报错或永久转圈。主模块与 worker
    // 必须来自同一 legacy 树，否则握手中途失败。代价：worker +50KB。
    const pdfjsLib = await import('pdfjs-dist/legacy/build/pdf.mjs')
    const workerUrl = await import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url')
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

  // Bump generation and cancel any in-flight render for this page so we never
  // have two page.render() calls drawing to the same canvas concurrently.
  const gen = (renderGen[pageNum] || 0) + 1
  renderGen[pageNum] = gen
  if (renderTasks[pageNum]) {
    renderTasks[pageNum].cancel()
    renderTasks[pageNum] = null
  }

  renderedPages.add(pageNum)

  try {
    const page = await pdfDoc.getPage(pageNum)
    if (renderGen[pageNum] !== gen) return
    const viewport = page.getViewport({ scale: scale.value })
    const ctx = canvas.getContext('2d')

    const dpr = window.devicePixelRatio || 1
    canvas.width = Math.floor(viewport.width * dpr)
    canvas.height = Math.floor(viewport.height * dpr)
    canvas.style.width = Math.floor(viewport.width) + 'px'
    canvas.style.height = Math.floor(viewport.height) + 'px'
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

    const task = page.render({ canvasContext: ctx, viewport })
    renderTasks[pageNum] = task
    await task.promise
  } catch {
    if (renderGen[pageNum] === gen) renderedPages.delete(pageNum)
  } finally {
    if (renderGen[pageNum] === gen && renderTasks[pageNum]) {
      renderTasks[pageNum] = null
    }
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
function scrollToPage(pageNum) {
  const el = scrollRef.value
  if (!el) return
  const pageWrapper = el.querySelector(`[data-page="${pageNum}"]`)
  if (pageWrapper) {
    pageWrapper.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
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
    // Prevent browser page zoom while we zoom the PDF instead.
    e.preventDefault()
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

// Cancel all in-flight page renders (used on teardown / file switch).
function cancelAllRenders() {
  for (const key of Object.keys(renderTasks)) {
    if (renderTasks[key]) {
      renderTasks[key].cancel()
      renderTasks[key] = null
    }
  }
}

// Lifecycle
onMounted(() => {
  loadPdf()
})

onUnmounted(() => {
  if (observer) { observer.disconnect(); observer = null }
  cancelAllRenders()
  if (pdfDoc) { pdfDoc.destroy(); pdfDoc = null }
  if (scrollRafId) { cancelAnimationFrame(scrollRafId); scrollRafId = 0 }
})

// Re-load when file changes
watch(() => props.file?.path, (newPath, oldPath) => {
  if (newPath && newPath !== oldPath) {
    if (observer) { observer.disconnect(); observer = null }
    cancelAllRenders()
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
  fitWidth,
  renderPage,
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
:global([data-theme-base="dark"]) .pdf-pages-scroll {
  background: #2a2d30;
}

:global([data-theme-base="dark"]) .pdf-page-wrapper {
  background: #1a1a1a;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.5);
}

:global([data-theme-base="dark"]) .pdf-loading-overlay {
  background: #2a2d30;
}
</style>
