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
  }
}

// --- Container resize: re-fit the viewer to the available width ---
// Both viewers size themselves once and never re-measure on a layout-only
// change (dragging the splitter, toggling a dock panel) — which produces no
// window `resize` event:
//   * @vue-office/pptx freezes the width/height it measured at mount and bakes
//     a per-slide scale from it, so slides keep the width of the pane they were
//     mounted in.
//   * @vue-office/excel re-measures only from the window `resize` listener it
//     registers internally.
// Watching the body element catches both, including a file mounted while hidden
// (0-width → the viewers fall back to 960 / 1200).
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

/* Word overrides: remove all padding/margin, full-width, match bg */
.office-preview-body :deep(.docx-wrapper) {
  padding: 0 !important;
  max-width: 100% !important;
  background: var(--bg-primary) !important;
}

.office-preview-body :deep(.docx-wrapper > section.docx) {
  box-shadow: none !important;
  margin-bottom: 0 !important;
  width: 100% !important;
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
