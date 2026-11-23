<template>
  <div class="office-preview-container" @wheel="onWheel">
    <!-- 一条工具栏：左边工作表页签（放不下就横向滚动），右边「表格 / 原样」。
         .xls 是二进制老格式，解析库读不了，只留原样。 -->
    <div v-if="canShowExcelTable" class="excel-head">
      <div v-if="sheetNames.length > 1" class="sheet-tabs" role="tablist">
        <button
          v-for="(name, index) in sheetNames"
          :key="`${index}-${name}`"
          type="button"
          role="tab"
          class="sheet-tab"
          :class="{ 'is-on': index === activeSheet }"
          :aria-selected="index === activeSheet"
          @click="activeSheet = index"
        >
          {{ name || t('file.excel.sheetFallback', { n: index + 1 }) }}
        </button>
      </div>
      <div class="excel-mode">
        <ModeSwitch
          :model-value="excelView === 'original'"
          :left-label="t('file.excel.tableMode')"
          :right-label="t('file.excel.originalMode')"
          :aria-label="t('file.excel.viewMode')"
          @update:model-value="setExcelView($event ? 'original' : 'table')"
        />
      </div>
    </div>

    <!-- Preview body — component always mounted to avoid dead-lock -->
    <div
      ref="bodyRef"
      class="office-preview-body"
      :class="{ 'is-table': isExcelTable }"
      :data-file-path="file?.path || ''"
      @touchstart.passive="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @touchcancel="onTouchEnd"
    >
      <ExcelTableBody
        v-if="isExcelTable"
        :src="fileUrl"
        :active-sheet="activeSheet"
        @sheets="onSheets"
        @update:active-sheet="activeSheet = $event"
      />
      <VueOfficeDocx v-else-if="isWord" :src="fileUrl" @rendered="onRendered" @error="onError" />
      <VueOfficeExcel v-else-if="isExcel" :src="fileUrl" @rendered="onRendered" @error="onError" />
      <VueOfficePptx v-else-if="isPpt" :src="fileUrl" @rendered="onRendered" @error="onError" />
    </div>

    <!-- Loading overlay (absolute, does not block component mount). A document
         preview has no rows to preserve, so it previews the page shape.
         Excel 表格视图自己管加载/失败态，这里不能再盖一层。 -->
    <div v-if="loading && !isExcelTable" class="office-loading-overlay">
      <PanelSkeleton class="office-loading-skeleton" variant="text" :rows="12" :label="t('common.loading')" />
    </div>

    <!-- Error overlay -->
    <div v-if="error && !isExcelTable" class="office-error-overlay">
      <FileX :size="48" />
      <div class="office-error-title">{{ t('file.viewer.loadFailed') }}</div>
      <div class="office-error-desc">{{ error }}</div>
      <div class="office-error-actions">
        <button class="office-retry-btn refresh-spin" :class="{ 'refresh-spin--active': loading }" :disabled="loading" @click="reload">
          <RefreshCw :size="14" />
          {{ t('common.retry') }}
        </button>
        <button class="office-download-btn" @click="handleDownload">
          <Download :size="14" />
          {{ t('common.download') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileX, Download, RefreshCw } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import ExcelTableBody from '@/components/media/ExcelTableBody.vue'
import ModeSwitch from '@/components/common/ModeSwitch.vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { appLog } from '@/utils/appLog.ts'

// Static imports — components must be available at mount time to avoid dead-lock
// Reference doc: "组件始终挂载，立刻拉取 src 并渲染"
import VueOfficeDocx from '@vue-office/docx'
import VueOfficePptx from '@vue-office/pptx'

// CSS for docx and excel (PPT has NO CSS file — do NOT import)
import '@vue-office/docx/lib/index.css'
import '@vue-office/excel/lib/index.css'

// Excel 默认走表格视图，原样渲染按需加载 —— 那个包有 1.6MB（内含 exceljs +
// 表格画布），打开 xlsx 不该先付这笔钱。
const VueOfficeExcel = defineAsyncComponent(
  buildAsyncComponentOptions({ loader: () => import('@vue-office/excel').then(m => m.default) }),
)

const TAG = 'OfficePreview'

const MIN_SCALE = 1.0
const MAX_SCALE = 5.0

const props = defineProps({
  file: Object,
})

const { t } = useI18n()

const loading = ref(true)
const error = ref('')
const bodyRef = ref(null)

// --- PPT zoom state ---
const scale = ref(1.0)
const pinchStartDist = ref(0)
const pinchStartScale = ref(1)

// Determine office sub-type from extension
const lower = computed(() => (props.file?.name || '').toLowerCase())
const isWord = computed(() => lower.value.endsWith('.docx'))
const isExcel = computed(() => lower.value.endsWith('.xlsx') || lower.value.endsWith('.xls'))
const isPpt = computed(() => lower.value.endsWith('.pptx'))
// 老 .xls 是二进制 BIFF 格式，exceljs 读不了 —— 只提供原样渲染。
const isXls = computed(() => lower.value.endsWith('.xls'))
const canShowExcelTable = computed(() => isExcel.value && !isXls.value)

const excelView = ref('table')
const isExcelTable = computed(() => canShowExcelTable.value && excelView.value === 'table')

// 页签归这条工具栏所有：表格视图解析完把工作表名报上来，切换只改序号。
const sheetNames = ref([])
const activeSheet = ref(0)

function onSheets(names) {
  sheetNames.value = names
  if (activeSheet.value >= names.length) activeSheet.value = 0
}

// Reactivity trigger: changes the computed URL when file changes or reload() is called,
// forcing the office viewer to re-fetch the file.
// (Server-side Cache-Control: no-store handles browser caching; this handles Vue DOM reuse.)
const mediaTimestamp = ref(Date.now())
const fileUrl = computed(() => {
  const base = buildLocalFileUrl(props.file.path)
  return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}`
})

function setExcelView(view) {
  if (excelView.value === view) return
  excelView.value = view
  error.value = ''
  if (view === 'original') {
    // 原样渲染器是重新挂载的，会再拉一次文件。
    loading.value = true
    mediaTimestamp.value = Date.now()
    // 原样视图没有表格页签，先清掉，免得留一排点不动的名字。
    sheetNames.value = []
    activeSheet.value = 0
  } else {
    loading.value = false
  }
  // 换了渲染器，之前的几何量作废。
  lastFitWidth = -1
}

// --- PPT zoom: CSS transform on pptx-preview-wrapper ---
// `scale` is user zoom on top of the width fit: at 1 the wrapper is laid out at
// the container width and the library's own per-slide scale fits the content.
function applyPptScale() {
  if (!isPpt.value || !bodyRef.value) return
  const wrapper = bodyRef.value.querySelector('.pptx-preview-wrapper')
  if (!wrapper) return
  if (scale.value === 1) {
    wrapper.style.transform = ''
    wrapper.style.transformOrigin = ''
  } else {
    wrapper.style.transform = `scale(${scale.value})`
    wrapper.style.transformOrigin = 'top left'
  }
  // Give the scaled wrapper a layout width that keeps the visible result inside
  // the container, so the scroll extent stays correct.
  wrapper.style.width = `${100 / scale.value}%`
}

// --- PPT zoom: fit-width (reset zoom) ---
function fitWidth() {
  if (!isPpt.value || !bodyRef.value) return
  // After fit, scale=1 means "fit container width" — that's the default
  scale.value = 1.0
  applyPptScale()
}

// --- PPT zoom: pinch gesture (touch) ---
function onTouchStart(e) {
  if (!isPpt.value) return
  if (e.touches.length === 2) {
    pinchStartDist.value = Math.hypot(
      e.touches[0].clientX - e.touches[1].clientX,
      e.touches[0].clientY - e.touches[1].clientY,
    )
    pinchStartScale.value = scale.value
  }
}

function onTouchMove(e) {
  if (!isPpt.value) return
  if (e.touches.length === 2 && pinchStartDist.value > 0) {
    e.preventDefault()
    const dist = Math.hypot(
      e.touches[0].clientX - e.touches[1].clientX,
      e.touches[0].clientY - e.touches[1].clientY,
    )
    const ratio = dist / pinchStartDist.value
    scale.value = Math.max(MIN_SCALE, Math.min(pinchStartScale.value * ratio, MAX_SCALE))
    applyPptScale()
  }
}

function onTouchEnd(e) {
  if (e.touches.length < 2) {
    pinchStartDist.value = 0
  }
}

// --- PPT zoom: Ctrl+scroll (desktop) ---
function onWheel(e) {
  if (!isPpt.value) return
  if (e.ctrlKey || e.metaKey) {
    // Prevent browser page zoom while we zoom the PPT instead.
    e.preventDefault()
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    scale.value = Math.max(MIN_SCALE, Math.min(scale.value + delta, MAX_SCALE))
    applyPptScale()
  }
}

function onRendered() {
  loading.value = false
  error.value = ''
  // Reset zoom on new render
  scale.value = 1.0
  // Apply scale after DOM is ready
  if (isPpt.value) {
    nextTick(() => {
      applyPptScale()
      // The viewer measured the container before it had a usable size (e.g. it
      // was mounted while hidden) and fell back to 960x540. Re-fit against the
      // real width now that the slides exist.
      lastFitWidth = -1
      scheduleRefit()
    })
  } else if (isWord.value) {
    // `.docx` sections are laid out at the Word page width (21cm for A4) —
    // always wider than a phone viewport — and the library also re-renders
    // them asynchronously (fonts, numbering images), which can overwrite our
    // fit afterwards. Re-apply once the render has settled.
    nextTick(() => {
      fitDocxSections()
      // The library keeps re-rendering (fonts, numbering images) after the
      // `rendered` event and can replace the page's geometry.
      pagesFitSettledTimer = setTimeout(fitDocxSections, 300)
    })
  }
}

// --- Docx: fit the page to the container width ---
// The docx viewer lays every `section.docx` out at the Word page width from
// the document properties (21cm for A4), which is wider than a phone viewport —
// the reader has to scroll sideways through half a page. Fit it to the pane
// instead, the way the pptx viewer fits its slides, but without a transform:
// the width is taken over by the stylesheet and the text is re-laid out by the
// browser, so it stays sharp and selectable.
//
// The page's own padding (Word page margins) and font size scale with it. They
// are written as custom properties and multiplied into the real declarations
// by the stylesheet, so what this script writes is only ever off or on —
// restoring the original means REMOVING them, which hands width/padding/font
// back to the library's stylesheet (its own 10px page padding under 800px
// included) instead of pinning an inline value forever.
//
// The stylesheet uses `width: 100%` rather than a pixel width: the wrapper is
// a flex column with `align-items: center`, and a pixel width would let the
// wrapper shrink to the widest page — a landscape section would then shrink
// every page under it.
//
// Only the two custom properties need clearing on reset: they are what turns
// the rules on, and the inline pixel width the library left is stripped when
// the figure is first written.
const DOCX_FIT_PROPS = ['--docx-fit-ratio', '--docx-fit-font-size']
let docxNativeWidth = 0
let docxBaseFont = 0
let pagesFitTimer = 0
let pagesFitSettledTimer = 0

function docxPages() {
  const body = bodyRef.value
  if (!body) return []
  return Array.from(body.querySelectorAll('.docx-wrapper > section.docx'))
}

/**
 * The page width before any fit. The library writes it inline in pixels (a
 * `cm` figure would need pixels per cm, which depends on the page's own
 * font-size); the laid-out box covers a page that was fitted before we saw it.
 * 0 means "not measurable yet" — the caller retries on the next pass.
 */
function nativePageWidth(page) {
  if (page.style.getPropertyValue('--docx-fit-ratio')) return 0 // already fitted
  const inline = /^[\d.]+px$/.test(page.style.width.trim()) ? parseFloat(page.style.width) : 0
  return Math.max(inline, page.offsetWidth)
}

function resetDocxFit() {
  for (const page of docxPages()) {
    page.style.removeProperty('width')
    for (const prop of DOCX_FIT_PROPS) page.style.removeProperty(prop)
  }
  docxNativeWidth = 0
  docxBaseFont = 0
}

function fitDocxSections() {
  if (!isWord.value) return
  const body = bodyRef.value
  const pages = docxPages()
  if (!body || !pages.length) return
  const avail = body.clientWidth
  if (!avail) return // hidden (inactive tab): keep the current fit
  // A percentage width resolves against the wrapper's content box — the same
  // box `clientWidth` measures (the wrapper's own padding is 0, see the
  // stylesheet).
  if (!docxNativeWidth) docxNativeWidth = Math.max(0, ...pages.map(nativePageWidth))
  if (!docxNativeWidth) return

  // The author's page fits: drop the fit and let the library's own
  // width/padding apply. Outside the fitted state the page box is the page
  // width, so `clientWidth` (scrollbar excluded) is the right measure here.
  if (avail >= docxNativeWidth) {
    resetDocxFit()
    return
  }
  // Measured against the inner width — the box the page is laid out in.
  const scale = (avail - 2) / docxNativeWidth
  if (!docxBaseFont) docxBaseFont = parseFloat(getComputedStyle(pages[0]).fontSize) || 16

  // `--docx-fit-ratio` switches the page to the fitted width in the
  // stylesheet (100% of the wrapper, `box-sizing: border-box` so the scaled
  // padding sits inside it). The library's inline pixel width would win over
  // that rule, so it goes first.
  for (const page of pages) {
    page.style.removeProperty('width')
    page.style.setProperty('--docx-fit-ratio', String(scale))
    // Pixels, not rem: `getComputedStyle` already reflects the UI scale (it
    // is a root font-size change), and a rem would apply the scale twice.
    page.style.setProperty('--docx-fit-font-size', `${docxBaseFont * scale}px`)
  }
}

// --- Container resize: re-fit the viewer to the available width ---
// The viewers size themselves once and never re-measure on a layout-only
// change (dragging the splitter, toggling a dock panel) — which produces no
// window `resize` event:
//   * @vue-office/pptx freezes the width/height it measured at mount and bakes
//     a per-slide scale from it, so slides keep the width of the pane they were
//     mounted in.
//   * @vue-office/excel re-measures only from the window `resize` listener it
//     registers internally.
//   * @vue-office/docx bakes the page width from the document properties.
// Watching the body element catches all of them, including a file mounted while
// hidden (0-width → the viewers fall back to 960 / 1200).
let resizeObserver = null
let refitRaf = 0
let refitTimer = 0
let lastFitWidth = -1

function refitPptx() {
  const body = bodyRef.value
  if (!body) return
  const wrapper = body.querySelector('.pptx-preview-wrapper')
  if (!wrapper) return
  // The slide width that scale=1 produces is the scroll container's content
  // width (clientWidth excludes the vertical scrollbar). User zoom is applied
  // separately as a transform on the wrapper, so it must not enter this
  // calculation — the slides stay fitted and the transform magnifies them.
  const fitWidth = body.clientWidth
  if (!fitWidth) return
  for (const slide of wrapper.querySelectorAll('.pptx-preview-slide-wrapper')) {
    const inner = slide.querySelector('.slide-wrapper')
    if (!inner) continue
    const baseW = parseFloat(inner.style.width)
    const baseH = parseFloat(inner.style.height)
    if (!baseW || !baseH) continue
    const ratio = fitWidth / baseW
    slide.style.width = `${fitWidth}px`
    slide.style.height = `${baseH * ratio}px`
    for (const layer of slide.querySelectorAll('.slide-wrapper, .slide-master-wrapper, .slide-layout-wrapper')) {
      layer.style.transform = `scale(${ratio})`
    }
  }
}

function refitExcel() {
  // @vue-office/excel owns its spreadsheet instance and exposes no re-measure
  // API; dispatching the event it listens for makes it re-read
  // clientWidth/clientHeight and redraw the canvas from the loaded data.
  window.dispatchEvent(new Event('resize'))
}

function scheduleRefit() {
  const body = bodyRef.value
  if (!body) return
  const width = body.clientWidth
  // Hidden (e.g. inactive tab): leave lastFitWidth untouched so the observer
  // fires a real re-fit once the element has a size again.
  if (!width || width === lastFitWidth) return
  lastFitWidth = width
  if (isPpt.value) {
    if (refitRaf) return
    refitRaf = requestAnimationFrame(() => {
      refitRaf = 0
      refitPptx()
    })
  } else if (isExcel.value && !isExcelTable.value) {
    // Trailing debounce: a splitter drag emits a resize per frame and each
    // excel re-measure redraws the whole sheet.
    clearTimeout(refitTimer)
    refitTimer = setTimeout(refitExcel, 120)
  } else if (isWord.value) {
    // Same trailing debounce as excel: every pass measures the pages and
    // re-lays the text out.
    clearTimeout(pagesFitTimer)
    pagesFitTimer = setTimeout(fitDocxSections, 120)
  }
}

function onError(err) {
  appLog.e(TAG, 'Error rendering:', err)
  loading.value = false
  error.value = typeof err === 'string' ? err : err?.message || String(err)
}

// Retry: reset src to force re-fetch (pattern from reference doc)
function reload() {
  loading.value = true
  error.value = ''
  scale.value = 1.0
  mediaTimestamp.value = Date.now()
}

function handleDownload() {
  downloadFileByPath(props.file.path, props.file?.name)
}

// Re-load when file changes
watch(() => props.file?.path, (newPath, oldPath) => {
  if (newPath && newPath !== oldPath) {
    loading.value = true
    error.value = ''
    scale.value = 1.0
    mediaTimestamp.value = Date.now()
    // New document, new mount-time geometry.
    lastFitWidth = -1
    resetDocxFit()
    // 新文件回到默认的表格视图。
    if (canShowExcelTable.value) excelView.value = 'table'
    sheetNames.value = []
    activeSheet.value = 0
  }
})

onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !bodyRef.value) return
  resizeObserver = new ResizeObserver(scheduleRefit)
  resizeObserver.observe(bodyRef.value)
})

onUnmounted(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (refitRaf) {
    cancelAnimationFrame(refitRaf)
    refitRaf = 0
  }
  clearTimeout(refitTimer)
  clearTimeout(pagesFitTimer)
  clearTimeout(pagesFitSettledTimer)
})

defineExpose({
  fitWidth,
  onError,
  loading,
  error,
  excelView,
  setExcelView,
})
</script>

<style scoped>
.office-preview-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  position: relative;
  background: var(--bg-primary);
  touch-action: manipulation;
  -webkit-tap-highlight-color: transparent;
  overflow: hidden;
}

/* Excel 顶部那一条工具栏：左边工作表页签，右边「表格 / 原样」。
   硬朗紧凑风 —— 直角、1px 分隔线、2px 主题色下划线表示选中，不做胶囊。
   必须压过加载遮罩（z-index: 10），否则原样视图加载期间没法切回表格。 */
.excel-head {
  position: relative;
  z-index: 20;
  display: flex;
  align-items: stretch;
  flex-shrink: 0;
  min-height: calc(26 * var(--dp));
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
}

/* 页签放不下就横向滚动，绝不换行撑高工具栏 */
.sheet-tabs {
  display: flex;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: thin;
}

.sheet-tabs::-webkit-scrollbar {
  height: calc(2 * var(--dp));
}

.sheet-tabs::-webkit-scrollbar-thumb {
  background: var(--border-color);
}

.sheet-tab {
  flex-shrink: 0;
  padding: 0 var(--space-4);
  border: 0;
  border-right: 1px solid var(--border-color);
  border-radius: 0;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: nowrap;
  cursor: pointer;
}

.sheet-tab.is-on {
  background: var(--bg-tertiary);
  color: var(--text-primary);
  box-shadow: inset 0 -2px 0 var(--accent-color);
}

@media (hover: hover) {
  .sheet-tab:not(.is-on):hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
}

.excel-mode {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  margin-left: auto;
  padding: 0 var(--space-4);
  border-left: 1px solid var(--border-color);
}

.office-preview-body {
  flex: 1;
  overflow: auto;
  -webkit-overflow-scrolling: touch;
}

/* 表格视图要自己撑满并内部滚动：DataTable 的 fill 模式要求父级是定高的
   flex 列容器，否则表头粘不住、整页跟着一起滚。 */
.office-preview-body.is-table {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* PPT zoom: allow pan but disable browser pinch-zoom so our handler takes over */
.office-preview-body:has(.vue-office-pptx) {
  touch-action: pan-x pan-y;
}

/* Word overrides: remove all padding/margin, match bg */
.office-preview-body :deep(.docx-wrapper) {
  padding: 0 !important;
  max-width: 100% !important;
  background: var(--bg-primary) !important;
}

/* The library sizes the page from the Word page width (21cm for A4), which a
   phone viewport cannot show. The script (fitDocxSections) records the scale
   it takes to fit the pane in `--docx-fit-ratio`; these declarations then make
   the page follow the pane, with its own padding (Word page margins) and text
   scaled by the same factor. At 1:1 the property is removed and the library's
   own width/padding/font apply again. */
.office-preview-body :deep(.docx-wrapper > section.docx[style*='--docx-fit-ratio']) {
  box-shadow: none !important;
  margin-bottom: 0 !important;
  box-sizing: border-box !important;
  width: 100% !important;
  /* Page margins are 2.54cm each side ≈ 96px; scale them with the page but
     never let them eat more than a fifth of it. */
  padding-left: min(calc(var(--docx-fit-ratio) * 96px), 20%) !important;
  padding-right: min(calc(var(--docx-fit-ratio) * 96px), 20%) !important;
  font-size: var(--docx-fit-font-size) !important;
}

.office-preview-body :deep(.docx-wrapper > section.docx) {
  box-shadow: none !important;
  margin-bottom: 0 !important;
}

.office-preview-body :deep(.docx img) {
  max-width: 100% !important;
  height: auto !important;
}

.office-preview-body :deep(.docx table) {
  max-width: 100% !important;
  overflow-x: auto !important;
}

/* Excel overrides: small font, limit cell width, hide toolbar */
.office-preview-body :deep(.x-spreadsheet table) {
  font-size: var(--font-size-xs) !important;
}

.office-preview-body :deep(.x-spreadsheet td),
.office-preview-body :deep(.x-spreadsheet th) {
  max-width: calc(120 * var(--dp));
  word-break: break-all;
}

.office-preview-body :deep(.x-spreadsheet-toolbar) {
  display: none !important;
}

/* PPT overrides: wrapper fills container, slides full width, no extra spacing.
   The slide wrapper and the three scaled layers get their pixel width/height
   from the library (and are re-fitted on container resize in script); forcing
   `width: 100% !important` here would override those inline sizes while the
   library still applies its own `transform: scale()`, double-scaling the slide
   and clipping it. Only reset the box spacing. */
.office-preview-body :deep(.vue-office-pptx) {
  width: 100% !important;
  height: 100% !important;
}

.office-preview-body :deep(.pptx-preview-wrapper) {
  width: 100% !important;
  background: var(--bg-primary) !important;
}

.office-preview-body :deep(.pptx-preview-slide-wrapper) {
  margin: 0 auto var(--space-5) !important;
}

.office-preview-body :deep(.slide-wrapper),
.office-preview-body :deep(.slide-master-wrapper),
.office-preview-body :deep(.slide-layout-wrapper) {
  margin: 0 !important;
  padding: 0 !important;
}

/* Loading overlay — opaque so the half-rendered document underneath does not
   show through the placeholder. */
.office-loading-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
  background: var(--bg-primary);
  padding: calc(48 * var(--dp)) calc(24 * var(--dp));
  overflow: hidden;
}

.office-loading-skeleton {
  max-width: calc(720 * var(--dp));
  margin: 0 auto;
}

/* Error overlay */
.office-error-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: calc(48 * var(--dp)) calc(24 * var(--dp));
  text-align: center;
  color: var(--text-muted);
  background: var(--bg-primary);
  z-index: 10;
}

.office-error-overlay > svg {
  width: calc(48 * var(--dp));
  height: calc(48 * var(--dp));
  margin-bottom: var(--space-6);
}

.office-error-title {
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  margin-bottom: var(--space-4);
}

.office-error-desc {
  font-size: var(--font-size-lg);
  margin-bottom: var(--space-8);
  max-width: calc(400 * var(--dp));
  word-break: break-word;
}

.office-error-actions {
  display: flex;
  gap: var(--space-5);
}

.office-retry-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-3) var(--space-7);
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  gap: var(--space-3);
  transition: all var(--duration-base);
}

@media (hover: hover) {
  .office-retry-btn:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}

.office-download-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-3) var(--space-7);
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: var(--radius-lg);
  text-decoration: none;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  gap: var(--space-3);
  transition: filter var(--duration-base);
}

@media (hover: hover) {
  .office-download-btn:hover {
    filter: brightness(1.15);
  }
}
</style>
