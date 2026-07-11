<template>
  <div class="office-preview-container" @wheel.prevent="onWheel">
    <!-- Preview body — component always mounted to avoid dead-lock -->
    <div
      ref="bodyRef"
      class="office-preview-body"
      :data-file-path="file?.path || ''"
      @touchstart.passive="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @touchcancel="onTouchEnd"
    >
      <VueOfficeDocx v-if="isWord" :src="fileUrl" @rendered="onRendered" @error="onError" />
      <VueOfficeExcel v-else-if="isExcel" :src="fileUrl" @rendered="onRendered" @error="onError" />
      <VueOfficePptx v-else-if="isPpt" :src="fileUrl" @rendered="onRendered" @error="onError" />
    </div>

    <!-- Loading overlay (absolute, does not block component mount) -->
    <div v-if="loading" class="office-loading-overlay">
      <Loader :size="32" />
      <span class="office-loading-text">{{ t('common.loading') }}</span>
    </div>

    <!-- Error overlay -->
    <div v-if="error" class="office-error-overlay">
      <FileX :size="48" />
      <div class="office-error-title">{{ t('file.viewer.loadFailed') }}</div>
      <div class="office-error-desc">{{ error }}</div>
      <div class="office-error-actions">
        <button class="office-retry-btn" @click="reload">
          <RefreshCw :size="14" />
          {{ t('common.retry') }}
        </button>
        <a v-if="!isAppMode" :href="buildLocalFileUrl(file.path, { download: true })" class="office-download-btn" :download="file.name">
          <Download :size="14" />
          {{ t('common.download') }}
        </a>
        <button v-else class="office-download-btn" @click="handleDownload">
          <Download :size="14" />
          {{ t('common.download') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Loader, FileX, Download, RefreshCw } from 'lucide-vue-next'
import { useAppMode } from '@/composables/useAppMode.ts'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { appLog } from '@/utils/appLog.ts'

// Static imports — components must be available at mount time to avoid dead-lock
// Reference doc: "组件始终挂载，立刻拉取 src 并渲染"
import VueOfficeDocx from '@vue-office/docx'
import VueOfficeExcel from '@vue-office/excel'
import VueOfficePptx from '@vue-office/pptx'

// CSS for docx and excel (PPT has NO CSS file — do NOT import)
import '@vue-office/docx/lib/index.css'
import '@vue-office/excel/lib/index.css'

const TAG = 'OfficePreview'

const MIN_SCALE = 1.0
const MAX_SCALE = 5.0

const props = defineProps({
  file: Object,
})

const { t } = useI18n()
const { isAppMode } = useAppMode()

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

// File URL via /api/local-file/ (same pattern as ImagePreview/PdfPreview)
const mediaTimestamp = ref(Date.now())
const fileUrl = computed(() =>
  `/api/local-file/${encodeURIComponent(props.file.path)}?t=${mediaTimestamp.value}`
)

// --- PPT zoom: CSS transform on pptx-preview-wrapper ---
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
  // Scale the wrapper's layout size so the scroll container adjusts correctly
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
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    scale.value = Math.max(MIN_SCALE, Math.min(scale.value + delta, MAX_SCALE))
    applyPptScale()
  }
}

function onRendered() {
  appLog.d(TAG, 'Rendered:', props.file?.name)
  loading.value = false
  error.value = ''
  // Reset zoom on new render
  scale.value = 1.0
  // Apply scale after DOM is ready
  if (isPpt.value) {
    nextTick(applyPptScale)
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
  }
})

onMounted(() => {
  appLog.d(TAG, 'Mounted, file:', props.file?.name)
})

onUnmounted(() => {
  appLog.d(TAG, 'Unmounted')
})

defineExpose({
  fitWidth,
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

.office-preview-body {
  flex: 1;
  overflow: auto;
  -webkit-overflow-scrolling: touch;
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
  font-size: 11px !important;
}

.office-preview-body :deep(.x-spreadsheet td),
.office-preview-body :deep(.x-spreadsheet th) {
  max-width: 120px;
  word-break: break-all;
}

.office-preview-body :deep(.x-spreadsheet-toolbar) {
  display: none !important;
}

/* PPT overrides: wrapper fills container, slides full width, no extra spacing */
.office-preview-body :deep(.vue-office-pptx) {
  width: 100% !important;
  height: 100% !important;
}

.office-preview-body :deep(.pptx-preview-wrapper) {
  width: 100% !important;
  background: var(--bg-primary) !important;
}

.office-preview-body :deep(.pptx-preview-slide-wrapper) {
  width: 100% !important;
  margin: 0 auto 10px !important;
}

.office-preview-body :deep(.slide-wrapper),
.office-preview-body :deep(.slide-master-wrapper),
.office-preview-body :deep(.slide-layout-wrapper) {
  width: 100% !important;
  max-width: 100% !important;
  margin: 0 !important;
  padding: 0 !important;
}

/* Loading overlay */
.office-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: var(--bg-primary);
  color: var(--text-muted);
  gap: 12px;
  z-index: 10;
}

.office-loading-overlay svg {
  animation: office-spin 1s linear infinite;
}

.office-loading-text {
  font-size: 14px;
}

@keyframes office-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Error overlay */
.office-error-overlay {
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
  z-index: 10;
}

.office-error-overlay > svg {
  width: 48px;
  height: 48px;
  margin-bottom: 12px;
}

.office-error-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.office-error-desc {
  font-size: 14px;
  margin-bottom: 20px;
  max-width: 400px;
  word-break: break-word;
}

.office-error-actions {
  display: flex;
  gap: 10px;
}

.office-retry-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 6px 16px;
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  border-radius: 14px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  gap: 6px;
  transition: all 0.15s;
}

.office-retry-btn:hover {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.office-download-btn {
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

.office-download-btn:hover {
  filter: brightness(1.15);
}
</style>
