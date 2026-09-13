<template>
  <div class="image-preview-container"
    @keydown="handleKeyDown"
    tabindex="0"
    ref="containerRef">
    <div class="image-preview-body">
      <!-- Draggable (wide-screen) onto the chat column to attach the existing
           file — same internal payload the file manager uses, no re-upload. -->
      <img :src="mediaUrl" :alt="file.name" class="image-preview-img lightbox-img"
        :draggable="isWideScreen"
        @dragstart="onImageDragStart"
        @dragend="onImageDragEnd" />
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
import { ref, computed, watch, onMounted } from 'vue'
import { store } from '@/stores/app.ts'
import { baseName, joinPath } from '@/utils/path.ts'
import { getFileType } from '@/utils/fileType.ts'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'

const props = defineProps({
    file: Object,
})

// Reactivity trigger: changes the computed URL when the file prop changes,
// forcing Vue to re-fetch rather than reusing the same <img> element.
// (Server-side Cache-Control: no-store handles browser caching; this handles Vue DOM reuse.)
const mediaTimestamp = ref(Date.now())
watch(() => props.file, () => { mediaTimestamp.value = Date.now() })
const mediaUrl = computed(() => {
    const base = buildLocalFileUrl(props.file.path)
    return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}`
  }
)

const containerRef = ref(null)
const { isWideScreen } = useWideScreenLayout()

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

/** Drag the previewed image onto the chat column → attach this path (no upload). */
function onImageDragStart(e) {
    const path = props.file?.path
    if (!path) return
    startAttachDrag(e, path, baseName(path))
}

function onImageDragEnd() {
    cleanupDragGhost()
}

// Focus container on mount for keyboard events
onMounted(() => {
    containerRef.value?.focus()
})

// Re-focus when file changes
watch(() => props.file, () => {
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

/* The image is displayed full-bleed (centered) with no per-image header —
   the file's action buttons (attach / lightbox view) live in the shared
   file header above (FileHeader.vue). */
.image-preview-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    cursor: default;
    box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
}

:global([data-theme-base="dark"]) .image-preview-img {
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

@media (hover: hover) {
    .img-nav-hint:hover {
        background: rgba(0, 0, 0, 0.6);
        transform: translateY(-50%) scale(1.1);
    }
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
    font-size: var(--font-size-sm);
    padding: 2px 10px;
    border-radius: 10px;
    backdrop-filter: blur(4px);
    pointer-events: none;
    user-select: none;
}
</style>
