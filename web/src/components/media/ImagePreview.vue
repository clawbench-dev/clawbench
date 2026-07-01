<template>
  <div class="image-preview-container"
    @keydown="handleKeyDown"
    tabindex="0"
    ref="containerRef">
    <div class="image-preview-body"
      @mousedown="handleMouseDown"
      @touchstart.passive="handleTouchStart"
      @touchmove="handleTouchMove"
      @touchend="handleTouchEnd"
      @touchcancel="handleTouchEnd">
      <img :src="mediaUrl" :alt="file.name" class="image-preview-img lightbox-img"
        :style="{ transform: `translateX(${dragOffsetX}px)`, transition: isDragging ? 'none' : 'transform 0.25s ease-out' }" />
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
  </div>
</template>

<script setup>
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { store } from '@/stores/app.ts'
import { baseName, joinPath } from '@/utils/path.ts'
import { getFileType } from '@/utils/fileType.ts'

const props = defineProps({
    file: Object,
})

// Cache-busting: update timestamp when file changes to bust browser cache
const mediaTimestamp = ref(Date.now())
watch(() => props.file, () => { mediaTimestamp.value = Date.now() })
const mediaUrl = computed(() =>
    `/api/local-file/${encodeURIComponent(props.file.path)}?t=${mediaTimestamp.value}`
)

const containerRef = ref(null)
const dragOffsetX = ref(0)
const isDragging = ref(false)
const dragStartX = ref(0)
const dragLastX = ref(0)
const hasMoved = ref(false)

// Touch state
const touchStartX = ref(0)
const touchLastX = ref(0)

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
}

// Mouse drag
function handleMouseDown(e) {
    if (e.button !== 0) return
    isDragging.value = true
    dragStartX.value = e.clientX
    dragLastX.value = e.clientX
    hasMoved.value = false
}

function handleGlobalMouseMove(e) {
    if (!isDragging.value) return
    const dx = e.clientX - dragStartX.value
    if (Math.abs(dx) > 5) hasMoved.value = true
    dragLastX.value = e.clientX
    dragOffsetX.value = dx * 0.3 // resistance
}

function handleGlobalMouseUp() {
    if (!isDragging.value) return
    isDragging.value = false

    const dx = dragStartX.value - dragLastX.value
    const absDx = Math.abs(dx)

    if (hasMoved.value && absDx > 60) {
        if (dx > 0) goNext()
        else goPrev()
    }

    dragOffsetX.value = 0
}

// Touch swipe
function handleTouchStart(e) {
    if (e.touches.length !== 1) return
    isDragging.value = true
    touchStartX.value = e.touches[0].clientX
    touchLastX.value = e.touches[0].clientX
    hasMoved.value = false
}

function handleTouchMove(e) {
    if (!isDragging.value || e.touches.length !== 1) return
    const dx = e.touches[0].clientX - touchStartX.value
    if (Math.abs(dx) > 5) hasMoved.value = true
    touchLastX.value = e.touches[0].clientX
    dragOffsetX.value = dx * 0.3
}

function handleTouchEnd() {
    if (!isDragging.value) return
    isDragging.value = false

    const dx = touchStartX.value - touchLastX.value
    if (hasMoved.value && Math.abs(dx) > 50) {
        if (dx > 0) goNext()
        else goPrev()
    }

    dragOffsetX.value = 0
}

// Focus container on mount for keyboard events
onMounted(() => {
    document.addEventListener('mousemove', handleGlobalMouseMove)
    document.addEventListener('mouseup', handleGlobalMouseUp)
    containerRef.value?.focus()
})

onUnmounted(() => {
    document.removeEventListener('mousemove', handleGlobalMouseMove)
    document.removeEventListener('mouseup', handleGlobalMouseUp)
})

// Re-focus when file changes
watch(() => props.file, () => {
    dragOffsetX.value = 0
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
    padding: 24px;
    background: var(--bg-primary);
    position: relative;
    user-select: none;
}

.image-preview-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border-radius: var(--radius-sm);
    cursor: zoom-in;
    box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
    will-change: transform;
}

:global([data-theme="dark"]) .image-preview-img {
    box-shadow: 0 2px 12px rgba(0, 0, 0, 0.3);
}

/* Navigation hint arrows */
.img-nav-hint {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.35);
    color: rgba(255, 255, 255, 0.9);
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: background 0.15s, transform 0.15s;
    z-index: 2;
    backdrop-filter: blur(4px);
}

.img-nav-hint svg {
    width: 18px;
    height: 18px;
}

.img-nav-hint:hover {
    background: rgba(0, 0, 0, 0.6);
    transform: translateY(-50%) scale(1.1);
}

.img-nav-prev {
    left: 12px;
}

.img-nav-next {
    right: 12px;
}

/* Counter badge */
.img-counter {
    position: absolute;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    background: rgba(0, 0, 0, 0.5);
    color: rgba(255, 255, 255, 0.85);
    font-size: 12px;
    padding: 2px 10px;
    border-radius: 10px;
    backdrop-filter: blur(4px);
    pointer-events: none;
    user-select: none;
}
</style>
