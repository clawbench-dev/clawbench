<template>
  <div class="image-preview-container"
    @keydown="handleKeyDown"
    tabindex="0"
    ref="containerRef">
    <div class="image-preview-body" ref="bodyRef" :class="{ 'is-zoomed': isZoomed }"
      @wheel.prevent="handleWheel">
      <!-- Draggable (wide-screen) onto the chat column to attach the existing
           file — same internal payload the file manager uses, no re-upload.
           `:key` is load-bearing: the pending-media placeholder is keyed on the
           absence of `data-media-settled`, which localMediaFallback stamps on the
           element once its bytes arrive. Reusing the element across a `src`
           change (navigating to a sibling image) would keep the stamp and leave
           the placeholder permanently suppressed for the new image. -->
      <img :key="mediaUrl" :src="mediaUrl" :alt="file.name" class="image-preview-img lightbox-img"
        ref="imgRef"
        :style="imgStyle"
        :class="{ 'is-zoomed': isZoomed, 'is-panning': isDragging }"
        :draggable="isWideScreen && !isZoomed"
        @dragstart="onImageDragStart"
        @dragend="onImageDragEnd"
        @mousedown="handleMouseDown"
        @dblclick="resetZoom" />
      <!-- Prev overlay -->
      <div v-if="hasPrev" class="img-nav-hint img-nav-prev" @click="goPrev">
        <ChevronLeft :size="18" />
      </div>
      <!-- Next overlay -->
      <div v-if="hasNext" class="img-nav-hint img-nav-next" @click="goNext">
        <ChevronRight :size="18" />
      </div>
    </div>
    <!-- Counter badge -->
    <div v-if="imageCount > 1" class="img-counter">{{ currentIndex + 1 }} / {{ imageCount }}</div>
    <!-- Zoom badge: shows the current zoom and is the explicit way back to
         "fit the pane" (double-click / Escape do the same). -->
    <button v-if="isZoomed" class="img-zoom-badge" :title="t('file.header.resetZoom')"
      @click="resetZoom">{{ zoomPercent }}%</button>
  </div>
</template>

<script setup>
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { store } from '@/stores/app.ts'
import { baseName, joinPath } from '@/utils/path.ts'
import { getFileType } from '@/utils/fileType.ts'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { mediaVersionFor, trackMediaPath } from '@/composables/useMediaWatch.ts'

const props = defineProps({
    file: Object,
})

const { t } = useI18n()

// Reactivity trigger: changes the computed URL when the file prop changes,
// forcing Vue to re-fetch rather than reusing the same <img> element.
// (Server-side Cache-Control: no-store handles browser caching; this handles Vue DOM reuse.)
const mediaTimestamp = ref(Date.now())
watch(() => props.file, () => { mediaTimestamp.value = Date.now() })

// A background rewrite of the same path bumps the shared media version. Reading
// it here keeps the URL fresh across re-renders; useMediaWatch also patches the
// live <img> so the change shows even without a re-render.
const mediaVersion = computed(() => mediaVersionFor(props.file?.path || ''))

// Watch the displayed image file even if the element has not mounted yet.
let untrackMedia = null
watch(() => props.file?.path, (path) => {
    untrackMedia?.()
    untrackMedia = path ? trackMediaPath(path) : null
}, { immediate: true })
onUnmounted(() => { untrackMedia?.() })

const mediaUrl = computed(() => {
    const base = buildLocalFileUrl(props.file.path)
    return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}.${mediaVersion.value}`
  }
)

const containerRef = ref(null)
const { isWideScreen } = useWideScreenLayout()

// ── Zoom & pan ──
// The pane is dedicated to one image, so the wheel always belongs to it: no
// click-to-activate step like inline markdown media (there is no page text to
// scroll past here). Zoom is anchored on the cursor, panning is clamped to the
// scaled image's own bounds so it can never be dragged out of the frame.
const MAX_ZOOM = 8
const scale = ref(1)
const tx = ref(0)
const ty = ref(0)
const isDragging = ref(false)
const bodyRef = ref(null)
const imgRef = ref(null)
const isZoomed = computed(() => scale.value > 1.001)
const zoomPercent = computed(() => Math.round(scale.value * 100))
const imgStyle = computed(() => ({
    transform: `translate(${tx.value}px, ${ty.value}px) scale(${scale.value})`,
    transition: isDragging.value ? 'none' : 'transform 120ms ease-out',
}))

function clampPan() {
    const img = imgRef.value
    const body = bodyRef.value
    if (!img || !body) return
    // `offsetWidth` is the layout size (unaffected by the transform), which is
    // exactly the base the scale multiplies.
    const maxX = Math.max(0, (img.offsetWidth * scale.value - body.clientWidth) / 2)
    const maxY = Math.max(0, (img.offsetHeight * scale.value - body.clientHeight) / 2)
    tx.value = Math.min(maxX, Math.max(-maxX, tx.value))
    ty.value = Math.min(maxY, Math.max(-maxY, ty.value))
}

function resetZoom() {
    scale.value = 1
    tx.value = 0
    ty.value = 0
}

function handleWheel(e) {
    const body = bodyRef.value
    if (!body) return
    const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15
    const next = Math.min(MAX_ZOOM, Math.max(1, scale.value * factor))
    if (next === scale.value) return
    if (next === 1) { resetZoom(); return }
    // Keep the point under the cursor stationary: with the transform origin at
    // the centre, a point at screen offset `p` from the centre sits at
    // `tx + p * scale`, so preserving it means scaling `tx - p` too.
    const rect = body.getBoundingClientRect()
    const px = e.clientX - rect.left - rect.width / 2
    const py = e.clientY - rect.top - rect.height / 2
    const k = next / scale.value
    tx.value = px - (px - tx.value) * k
    ty.value = py - (py - ty.value) * k
    scale.value = next
    clampPan()
}

let panStartX = 0
let panStartY = 0
let panBaseX = 0
let panBaseY = 0

function handleMouseDown(e) {
    if (e.button !== 0 || !isZoomed.value) return
    e.preventDefault()
    isDragging.value = true
    panStartX = e.clientX
    panStartY = e.clientY
    panBaseX = tx.value
    panBaseY = ty.value
    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
}

function handleMouseMove(e) {
    if (!isDragging.value) return
    e.preventDefault()
    tx.value = panBaseX + (e.clientX - panStartX)
    ty.value = panBaseY + (e.clientY - panStartY)
    clampPan()
}

function handleMouseUp() {
    isDragging.value = false
    window.removeEventListener('mousemove', handleMouseMove)
    window.removeEventListener('mouseup', handleMouseUp)
}

onUnmounted(handleMouseUp)

// Build list of image files in the same directory
const siblingImages = computed(() => {
    const entries = store.state.dirEntries || []
    return entries.filter(e => e.type !== 'dir' && getFileType(e.name)?.isImage)
})

const imageCount = computed(() => siblingImages.value.length)

const currentIndex = computed(() => {
    if (!props.file) return -1
    const name = baseName(props.file.path)
    return siblingImages.value.findIndex(e => e.name === name)
})

const hasPrev = computed(() => currentIndex.value > 0)
const hasNext = computed(() => currentIndex.value >= 0 && currentIndex.value < imageCount.value - 1)

function goPrev() {
    if (!hasPrev.value) return
    const prev = siblingImages.value[currentIndex.value - 1]
    const path = joinPath(store.state.currentDir || '', prev.name)
    store.selectFile(path, true)
}

function goNext() {
    if (!hasNext.value) return
    const next = siblingImages.value[currentIndex.value + 1]
    const path = joinPath(store.state.currentDir || '', next.name)
    store.selectFile(path, true)
}

// Keyboard navigation
function handleKeyDown(e) {
    if (e.key === 'ArrowLeft') { e.preventDefault(); goPrev() }
    else if (e.key === 'ArrowRight') { e.preventDefault(); goNext() }
    else if (e.key === 'Escape' && isZoomed.value) { e.preventDefault(); resetZoom() }
}

/** Drag the previewed image onto the chat column → attach this path (no upload). */
function onImageDragStart(e) {
    const path = props.file?.path
    if (!path) return
    startAttachDrag(e, path, { name: baseName(path) })
}

function onImageDragEnd() {
    cleanupDragGhost()
}

// Focus container on mount for keyboard events
onMounted(() => {
    containerRef.value?.focus()
})

// Re-focus when file changes, and drop any zoom: it belongs to the previous
// image, not to the pane.
watch(() => props.file, () => {
    resetZoom()
    containerRef.value?.focus()
})
</script>

<style scoped>
.image-preview-container {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 0;
    position: relative;
    outline: none;
}

.image-preview-body {
    flex: 1;
    overflow: auto;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: calc(24 * var(--dp));
    background: var(--bg-primary);
    position: relative;
    user-select: none;
}

/* The image is displayed full-bleed (centered) with no per-image header —
   the file's action buttons (attach / lightbox view) live in the shared
   file header above (FileHeader.vue). */
.image-preview-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    cursor: default;
    box-shadow: 0 calc(2 * var(--dp)) calc(12 * var(--dp)) rgba(0, 0, 0, 0.08);
}

/* Zoomed: the transform is the only thing that grows, so the scrollable
   overflow it creates would just add scrollbars next to the pan gesture. */
.image-preview-body.is-zoomed {
    overflow: hidden;
}

.image-preview-img.is-zoomed {
    cursor: grab;
}

.image-preview-img.is-panning {
    cursor: grabbing;
}

:global([data-theme-base="dark"]) .image-preview-img {
    box-shadow: 0 calc(2 * var(--dp)) calc(12 * var(--dp)) rgba(0, 0, 0, 0.3);
}

/* Navigation hint arrows */
.img-nav-hint {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    width: calc(36 * var(--dp));
    height: calc(36 * var(--dp));
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.35);
    color: rgba(255, 255, 255, 0.9);
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: background var(--duration-base), transform var(--duration-base);
    z-index: 2;
    backdrop-filter: blur(calc(4 * var(--dp)));
}

.img-nav-hint svg {
    width: calc(18 * var(--dp));
    height: calc(18 * var(--dp));
}

@media (hover: hover) {
    .img-nav-hint:hover {
        background: rgba(0, 0, 0, 0.6);
        transform: translateY(-50%) scale(1.1);
    }
}

.img-nav-prev {
    left: calc(12 * var(--dp));
}

.img-nav-next {
    right: calc(12 * var(--dp));
}

/* Counter badge */
.img-counter {
    position: absolute;
    bottom: calc(12 * var(--dp));
    left: 50%;
    transform: translateX(-50%);
    background: rgba(0, 0, 0, 0.5);
    color: rgba(255, 255, 255, 0.85);
    font-size: var(--font-size-sm);
    padding: var(--space-1) var(--space-5);
    border-radius: var(--radius-md);
    backdrop-filter: blur(calc(4 * var(--dp)));
    pointer-events: none;
    user-select: none;
}

/* Zoom badge / reset affordance, mirroring the nav hints' floating style. */
.img-zoom-badge {
    position: absolute;
    bottom: calc(12 * var(--dp));
    right: calc(12 * var(--dp));
    border: none;
    background: rgba(0, 0, 0, 0.5);
    color: rgba(255, 255, 255, 0.85);
    font-size: var(--font-size-sm);
    font-family: inherit;
    padding: var(--space-1) var(--space-4);
    border-radius: var(--radius-md);
    backdrop-filter: blur(calc(4 * var(--dp)));
    cursor: pointer;
    user-select: none;
    z-index: 2;
    transition: background var(--duration-base);
}

@media (hover: hover) {
    .img-zoom-badge:hover {
        background: rgba(0, 0, 0, 0.7);
    }
}
</style>
