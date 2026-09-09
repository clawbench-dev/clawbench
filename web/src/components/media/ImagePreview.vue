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
      <!-- The image is wrapped in the SAME .image-block-wrapper / header
           structure markdown file previews use, so the view / attach buttons
           live on the image's own header (fitting the image width), not a
           separate full-width toolbar. Share SPA has no chat / lightbox →
           bare image (no header). -->
      <div v-if="showHeader" class="image-block-wrapper">
        <div class="image-block-header">
          <span class="image-block-header-actions">
            <button
              class="image-block-view-btn"
              type="button"
              :title="viewLabel"
              :aria-label="viewLabel"
              @click.stop="onView">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>
            </button>
            <button
              ref="attachBtnRef"
              class="image-block-attach-btn"
              type="button"
              :title="attachLabel"
              :aria-label="attachLabel"
              :class="{ 'is-attached': isAttached }"
              @click.stop="onToggleAttach"
              v-html="ATTACH_BADGE_SVG">
            </button>
          </span>
        </div>
        <img :src="mediaUrl" :alt="file.name" class="image-preview-img lightbox-img"
          :style="{ transform: `translateX(${dragOffsetX}px)`, transition: isDragging ? 'none' : 'transform 0.25s ease-out' }" />
      </div>
      <img v-else :src="mediaUrl" :alt="file.name" class="image-preview-img lightbox-img"
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
import { ref, computed, watch, onMounted, onUnmounted, inject } from 'vue'
import { store } from '@/stores/app.ts'
import { baseName, joinPath } from '@/utils/path.ts'
import { getFileType } from '@/utils/fileType.ts'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { isShareMode } from '@/share/shareMode'
import { gt } from '@/composables/useLocale'
import { useChatContext } from '@/composables/useChatContext'
import { useToast } from '@/composables/useToast'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'

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

// ── Inline action header (view / attach) ───────────────────────────────
const showHeader = computed(() => !isShareMode())
const viewLabel = gt('imageBlock.view')
const attachLabel = gt('chat.attach.attachImageToChat')
const addedLabel = gt('chat.attach.addedToChat')
const removedLabel = gt('chat.attach.removedFromChat')

const openLightbox = inject('openLightbox', null)
const { addAttachedFile, removeAttachedFileByPath, hasAttachedFile } = useChatContext()
const { show: showToast } = useToast()

const attachBtnRef = ref(null)
const isAttached = computed(() => !!(props.file?.path && hasAttachedFile(props.file.path)))

function onView() {
    if (typeof openLightbox === 'function') {
        openLightbox(mediaUrl.value)
    }
}

function onToggleAttach() {
    const path = props.file?.path
    if (!path) return
    if (hasAttachedFile(path)) {
        removeAttachedFileByPath(path)
        showToast(removedLabel, { icon: '📎', type: 'info', duration: 1500 })
        return
    }
    addAttachedFile(path)
    showToast(addedLabel, { icon: '📎', type: 'success', duration: 1500 })
    // Fly-to-chat particle from the attach button (App listens; silent when the
    // chat dock is not on screen, e.g. wide-screen layout without a dock button).
    const btn = attachBtnRef.value
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const from = btn?.getBoundingClientRect() ?? null
    const to = dockChatBtn?.getBoundingClientRect() ?? null
    if (from && to) {
        window.dispatchEvent(new CustomEvent('attach-to-chat', {
            detail: {
                from: { x: from.left + from.width / 2, y: from.top + from.height / 2 },
                to: { x: to.left + to.width / 2, y: to.top + to.height / 2 },
            },
        }))
    }
}

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
    // Ignore when focus is on the header buttons (their space/arrow handling
    // must not switch the image underneath).
    const kTarget = e.target instanceof Element ? e.target : null
    if (kTarget?.closest('.image-block-header button')) return
    if (e.key === 'ArrowLeft') { e.preventDefault(); goPrev() }
    else if (e.key === 'ArrowRight') { e.preventDefault(); goNext() }
}

// Mouse drag
function handleMouseDown(e) {
    if (e.button !== 0) return
    // Header buttons must not start a swipe-drag.
    const mTarget = e.target instanceof Element ? e.target : null
    if (mTarget?.closest('.image-block-wrapper')) return
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
    // Header buttons must not start a swipe-drag.
    const tTarget = e.target instanceof Element ? e.target : null
    if (tTarget?.closest('.image-block-wrapper')) return
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

/* The image reuses markdown's .image-block-wrapper header structure. Inside
   the full-screen viewer the wrapper must center + constrain to the available
   height (the global markdown rule uses width:fit-content + a text-flow
   margin that does not apply here). */
.image-preview-body .image-block-wrapper {
    margin: 0;
    max-width: 100%;
    max-height: 100%;
    display: flex;
    flex-direction: column;
}

.image-preview-body .image-block-wrapper .image-block-header {
    flex: none;
}

.image-preview-body .image-block-wrapper img {
    max-width: 100%;
    max-height: calc(100% - 26px);
    object-fit: contain;
}

.image-preview-body .image-block-wrapper .image-block-attach-btn.is-attached {
    opacity: 1;
    color: var(--accent-color);
}

.image-preview-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    cursor: default;
    box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
    will-change: transform;
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
    font-size: 12px;
    padding: 2px 10px;
    border-radius: 10px;
    backdrop-filter: blur(4px);
    pointer-events: none;
    user-select: none;
}
</style>
