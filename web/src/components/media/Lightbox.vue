<template>
  <Teleport to="body">
    <div id="lightbox" class="lightbox" :style="{ display: lightboxVisible ? 'flex' : 'none' }">
      <div class="lightbox-backdrop" @click="close" />
      <div class="lightbox-toolbar">
        <div v-if="currentFileName" class="lb-filename">{{ currentFileName }}</div>
        <div class="lb-actions">
          <button v-if="currentUrl || currentSvg" class="lb-btn" @click="handleDownload" title="Download">
            <Download :size="20" />
          </button>
          <button class="lb-btn" @click="resetAndRefresh" title="Reset & Reload">
            <RotateCcw :size="20" />
          </button>
          <button class="lb-btn lb-close" @click="close" title="Close">
            <X :size="20" />
          </button>
        </div>
      </div>
      <div
        class="lightbox-content"
        :class="{ grabbing: isDragging, 'slide-left': slideDirection === 'left', 'slide-right': slideDirection === 'right', 'can-drag': canDrag }"
        ref="contentRef"
        @click="handleContentClick"
        @wheel.prevent="handleWheel"
        @mousedown="handleMouseDown"
        @touchstart.passive="handleTouchStart"
        @touchmove="handleTouchMove"
        @touchend="handleTouchEnd"
        @touchcancel="handleTouchEnd"
        @animationend="slideDirection = ''"
      >
        <img
          v-if="currentUrl && !currentSvg"
          v-show="!imageLoading"
          ref="imgRef"
          :key="currentUrl"
          :src="currentUrl"
          :style="imgStyle"
          draggable="false"
          @mousedown.prevent
          @load="onImageLoad"
          @error="imageLoading = false"
        />
        <div v-if="imageLoading" class="lb-loading-spinner">
          <Loader :size="32" />
        </div>
        <div v-if="currentSvg" ref="svgContainerRef" :style="imgStyle" v-html="currentSvg" />
      </div>
      <div class="lightbox-bottom-bar">
        <template v-if="showNav">
          <button class="lb-btn lb-nav-btn" @click="navigatePrev" title="Previous">
            <ChevronLeft :size="20" />
          </button>
          <span class="lb-counter">{{ navCurrentIndex + 1 }}/{{ navTotalCount }}</span>
          <button class="lb-btn lb-nav-btn" @click="navigateNext" title="Next">
            <ChevronRight :size="20" />
          </button>
        </template>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { RotateCcw, X, Loader, ChevronLeft, ChevronRight, Download } from 'lucide-vue-next'
import { ref, computed, provide, watch, onMounted, onUnmounted } from 'vue'
import { store } from '@/stores/app.ts'
import { baseName, joinPath } from '@/utils/path.ts'
import { getFileType } from '@/utils/fileType.ts'
import { downloadBlob, buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'
import { extractImageName } from '@/utils/lightbox.ts'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

let unregisterBack = null
const lightboxVisible = ref(false)
const currentUrl = ref('')
const currentSvg = ref('')
const currentFilePath = ref('')
const scale = ref(1)
const tx = ref(0)
const ty = ref(0)
const lastTx = ref(0)
const lastTy = ref(0)
const isDragging = ref(false)
const dragStartX = ref(0)
const dragStartY = ref(0)
const contentRef = ref(null)
const imgRef = ref(null)
const svgContainerRef = ref(null)

// Fit-to-screen: the initial scale that makes image/svg fully visible
const fitScale = ref(1)
const naturalW = ref(0)
const naturalH = ref(0)
const dimensionsReady = ref(false)

// Directory-based navigation state
const siblingFiles = ref([])
const currentIndex = ref(-1)
const slideDirection = ref('') // '', 'left', 'right'
const imageLoading = ref(false)

// Markdown image navigation state
const mdImages = ref([]) // [{src, name}]
const mdCurrentIndex = ref(-1)

// Touch state
const pinchStartDist = ref(0)
const pinchStartScale = ref(1)
const touchStartX = ref(0)
const touchStartY = ref(0)
const touchLastX = ref(0)
const touchLastY = ref(0)
const hasMoved = ref(false)

// Navigation mode: 'dir' = directory-based, 'md' = markdown images
const navMode = computed(() => mdCurrentIndex.value >= 0 ? 'md' : 'dir')

const showNav = computed(() =>
    (currentIndex.value >= 0 && siblingFiles.value.length > 1) ||
    (mdCurrentIndex.value >= 0 && mdImages.value.length > 1)
)

const navCurrentIndex = computed(() =>
    navMode.value === 'md' ? mdCurrentIndex.value : currentIndex.value
)

const navTotalCount = computed(() =>
    navMode.value === 'md' ? mdImages.value.length : siblingFiles.value.length
)

const currentFileName = computed(() => {
    if (navMode.value === 'md' && mdImages.value.length > 0) {
        return mdImages.value[mdCurrentIndex.value]?.name || ''
    }
    if (!currentFilePath.value) {
        if (currentSvg.value) return 'diagram.svg'
        return ''
    }
    return baseName(currentFilePath.value)
})

// Computed style for transform
const imgStyle = computed(() => {
    const style = {
        transform: `translate(${tx.value}px, ${ty.value}px) scale(${scale.value})`,
        transition: isDragging.value ? 'none' : 'transform 0.1s ease-out'
    }
    // For images: once natural dimensions are known, set explicit width/height
    // and disable CSS max-width/max-height so transform: scale() handles fitting.
    // Before dimensions are ready, CSS max-width/max-height constrains as fallback.
    // For SVGs: dimensions are set directly on the SVG element in onSvgMounted;
    // the container div just needs the transform.
    if (dimensionsReady.value && naturalW.value > 0 && naturalH.value > 0 && !currentSvg.value) {
        style.width = naturalW.value + 'px'
        style.height = naturalH.value + 'px'
        style.maxWidth = 'none'
        style.maxHeight = 'none'
    }
    return style
})

// Calculate fit-to-screen scale based on natural dimensions vs viewport
function calcFitScale(naturalW, naturalH) {
    if (!contentRef.value || naturalW <= 0 || naturalH <= 0) return 1
    const vw = contentRef.value.clientWidth
    const vh = contentRef.value.clientHeight
    if (vw <= 0 || vh <= 0) return 1
    // Leave padding for toolbar and bottom bar (each ~56px)
    const padding = 56
    const availH = vh - padding * 2
    const s = Math.min(vw / naturalW, availH / naturalH, 1)
    return s > 0 ? s : 1
}

function onImageLoad() {
    imageLoading.value = false
    const img = imgRef.value
    if (!img) return
    naturalW.value = img.naturalWidth
    naturalH.value = img.naturalHeight
    const s = calcFitScale(img.naturalWidth, img.naturalHeight)
    fitScale.value = s
    scale.value = s
    dimensionsReady.value = true
}

function onSvgMounted() {
    // SVG is rendered via v-html, measure after next repaint
    requestAnimationFrame(() => {
        const container = svgContainerRef.value
        if (!container) return
        const svg = container.querySelector('svg')
        if (!svg) return

        // Try viewBox first, fallback to width/height attributes or bounding box
        let w, h
        if (svg.viewBox?.baseVal && svg.viewBox.baseVal.width > 0) {
            w = svg.viewBox.baseVal.width
            h = svg.viewBox.baseVal.height
        } else {
            w = parseFloat(svg.getAttribute('width')) || (typeof svg.getBBox === 'function' ? svg.getBBox().width : 0)
            h = parseFloat(svg.getAttribute('height')) || (typeof svg.getBBox === 'function' ? svg.getBBox().height : 0)
        }
        naturalW.value = w
        naturalH.value = h

        // For SVGs: set explicit width/height attributes to fit the viewport,
        // keeping scale=1 as the baseline (fully visible).
        // This handles SVGs with fixed pixel dimensions that CSS max-width
        // alone may not properly constrain.
        const vw = contentRef.value?.clientWidth || window.innerWidth
        const vh = contentRef.value?.clientHeight || window.innerHeight
        const padding = 56
        const availH = vh - padding * 2
        if (w > 0 && h > 0) {
            const s = Math.min(vw / w, availH / h)
            svg.setAttribute('width', Math.round(w * s) + 'px')
            svg.setAttribute('height', Math.round(h * s) + 'px')
            svg.style.maxWidth = 'none'
            svg.style.maxHeight = 'none'
        }
        fitScale.value = 1
        scale.value = 1
        dimensionsReady.value = true
    })
}

function getMediaType(filePath) {
    if (!filePath) return null
    const ft = getFileType(filePath)
    if (ft.isImage) return 'image'
    if (ft.isAudio) return 'audio'
    if (ft.isVideo) return 'video'
    return null
}

function buildSiblingList(filePath) {
    if (!filePath) { siblingFiles.value = []; currentIndex.value = -1; return }
    const mediaType = getMediaType(filePath)
    if (!mediaType) { siblingFiles.value = []; currentIndex.value = -1; return }

    const entries = store.state.dirEntries || []
    const siblings = entries.filter(e => {
        if (e.type === 'dir') return false
        return getMediaType(e.name) === mediaType
    })

    siblingFiles.value = siblings

    const fileName = baseName(filePath)
    const dir = filePath.substring(0, filePath.lastIndexOf('/'))
    const idx = siblings.findIndex(e => {
        const entryPath = dir ? dir + '/' + e.name : e.name
        return entryPath === filePath || e.name === fileName
    })
    currentIndex.value = idx
}

function navigatePrev() {
    if (navMode.value === 'md') {
        if (mdImages.value.length <= 1) return
        const newIdx = (mdCurrentIndex.value - 1 + mdImages.value.length) % mdImages.value.length
        navigateMdImage(newIdx, 'right')
        return
    }
    if (!showNav.value) return
    const newIdx = (currentIndex.value - 1 + siblingFiles.value.length) % siblingFiles.value.length
    navigateToIndex(newIdx, 'right')
}

function navigateNext() {
    if (navMode.value === 'md') {
        if (mdImages.value.length <= 1) return
        const newIdx = (mdCurrentIndex.value + 1) % mdImages.value.length
        navigateMdImage(newIdx, 'left')
        return
    }
    if (!showNav.value) return
    const newIdx = (currentIndex.value + 1) % siblingFiles.value.length
    navigateToIndex(newIdx, 'left')
}

function navigateToIndex(newIdx, direction) {
    const entry = siblingFiles.value[newIdx]
    if (!entry) return

    const entryPath = joinPath(store.state.currentDir || '', entry.name)

    // Show loading immediately, hide old image
    imageLoading.value = true
    slideDirection.value = direction

    // Reset transform for new image
    fitScale.value = 1
    naturalW.value = 0
    naturalH.value = 0
    dimensionsReady.value = false
    scale.value = 1
    tx.value = 0
    ty.value = 0
    lastTx.value = 0
    lastTy.value = 0

    currentIndex.value = newIdx
    currentFilePath.value = entryPath

    // Build URL for the new file
    currentUrl.value = buildLocalFileUrl(entryPath)
    currentSvg.value = ''

    // Sync with store
    store.selectFile(entryPath)
}

/**
 * Resolve the full-size image URL for the lightbox.
 * Inline images may use a low-res thumbnail src with the original stored in
 * data-full-src; the lightbox must always show the original.
 */
function fullImgSrc(img) {
    return (img && img.dataset && img.dataset.fullSrc) || (img ? img.src : '')
}

/**
 * Normalize a URL to a single clean form for the lightbox: strip any existing
 * cache-buster t= params and clean up stray '?/'&' separators they leave behind.
 * Sources may already carry a ?t= timestamp (e.g. ImagePreview.mediaUrl,
 * MarkdownPreview.fixLocalImagePaths), so appending another t= directly would
 * accumulate params and produce malformed URLs on refresh.
 */
function normalizeUrl(url) {
    return url
        .replace(/[?&]t=\d+/g, '')
        .replace(/[?&]+$/g, '')
        .replace(/\?&/g, '?')
}

function navigateMdImage(newIdx, direction) {
    const img = mdImages.value[newIdx]
    if (!img) return
    // Show loading immediately
    imageLoading.value = true
    slideDirection.value = direction

    // Reset transform for new image
    fitScale.value = 1
    naturalW.value = 0
    naturalH.value = 0
    dimensionsReady.value = false
    scale.value = 1
    tx.value = 0
    ty.value = 0
    lastTx.value = 0
    lastTy.value = 0

    mdCurrentIndex.value = newIdx
    currentUrl.value = normalizeUrl(fullImgSrc(img)) + '?t=' + Date.now()
    currentSvg.value = ''
}

function open(url, svg = '') {
    currentUrl.value = svg ? '' : normalizeUrl(url) + '?t=' + Date.now()
    currentSvg.value = svg
    lightboxVisible.value = true
    imageLoading.value = !svg
    fitScale.value = 1
    naturalW.value = 0
    naturalH.value = 0
    dimensionsReady.value = false
    scale.value = 1
    tx.value = 0
    ty.value = 0
    lastTx.value = 0
    lastTy.value = 0
    pinchStartDist.value = 0
    pinchStartScale.value = 1
    isDragging.value = false
    hasMoved.value = false
    slideDirection.value = ''

    // Reset markdown image navigation
    mdImages.value = []
    mdCurrentIndex.value = -1

    // Build navigation from store's current file
    if (!svg && store.state.currentFile?.path) {
        currentFilePath.value = store.state.currentFile.path
        buildSiblingList(store.state.currentFile.path)
    } else {
        currentFilePath.value = ''
        siblingFiles.value = []
        currentIndex.value = -1
    }

    document.body.style.overflow = 'hidden'
}

function openMdImages(imgs, startIndex) {
    mdImages.value = imgs
    mdCurrentIndex.value = startIndex

    const img = imgs[startIndex]
    currentUrl.value = normalizeUrl(fullImgSrc(img)) + '?t=' + Date.now()
    currentSvg.value = ''
    currentFilePath.value = ''

    lightboxVisible.value = true
    imageLoading.value = true
    fitScale.value = 1
    naturalW.value = 0
    naturalH.value = 0
    dimensionsReady.value = false
    scale.value = 1
    tx.value = 0
    ty.value = 0
    lastTx.value = 0
    lastTy.value = 0
    pinchStartDist.value = 0
    pinchStartScale.value = 1
    isDragging.value = false
    hasMoved.value = false
    slideDirection.value = ''

    // Disable directory-based navigation
    siblingFiles.value = []
    currentIndex.value = -1

    document.body.style.overflow = 'hidden'
}

function openSvg(svgContent) {
    open('', svgContent)
}

function close() {
    lightboxVisible.value = false
    currentUrl.value = ''
    currentFilePath.value = ''
    siblingFiles.value = []
    currentIndex.value = -1
    mdImages.value = []
    mdCurrentIndex.value = -1
    document.body.style.overflow = ''
}

function resetAndRefresh() {
    imageLoading.value = true
    fitScale.value = 1
    naturalW.value = 0
    naturalH.value = 0
    dimensionsReady.value = false
    scale.value = 1
    tx.value = 0
    ty.value = 0
    lastTx.value = 0
    lastTy.value = 0
    if (currentUrl.value) {
        currentUrl.value = normalizeUrl(currentUrl.value) + '?t=' + Date.now()
    }
}

function handleDownload() {
    if (currentSvg.value) {
        // Download SVG content as .svg file
        const svgName = currentFileName.value || 'diagram.svg'
        const name = svgName.endsWith('.svg') ? svgName : svgName.replace(/\.\w+$/, '') + '.svg'
        downloadBlob(currentSvg.value, name, 'image/svg+xml')
        return
    }
    if (!currentUrl.value) return

    // Resolve the relative file path for downloadFileByPath / buildLocalFileUrl
    let filePath = currentFilePath.value
    if (!filePath) {
        try {
            const url = new URL(currentUrl.value, window.location.origin)
            const prefix = '/api/local-file/'
            if (url.pathname.startsWith(prefix)) {
                filePath = decodeURIComponent(url.pathname.slice(prefix.length))
            }
        } catch { /* ignore */ }
    }

    if (filePath) {
        downloadFileByPath(filePath, currentFileName.value)
        return
    }

    // External URL — construct download link directly
    const a = document.createElement('a')
    const baseUrl = normalizeUrl(currentUrl.value)
    a.href = baseUrl
    a.download = currentFileName.value || ''
    document.body.appendChild(a)
    a.click()
    setTimeout(() => { document.body.removeChild(a) }, 1000)
}

function handleContentClick(e) {
    // Close lightbox when clicking the blank area (not on the image/svg itself)
    if (e.target === contentRef.value || e.target.closest('.lb-loading-spinner')) {
        close()
    }
}

function handleWheel(e) {
    const delta = e.deltaY > 0 ? 0.85 : 1.2
    const newScale = Math.min(Math.max(scale.value * delta, 0.1), 10)
    const minScale = fitScale.value
    if (newScale < minScale && scale.value >= minScale) { tx.value = 0; ty.value = 0; lastTx.value = 0; lastTy.value = 0 }
    scale.value = newScale
}

// Can drag only when zoomed in beyond fit-to-screen
const canDrag = computed(() => scale.value > fitScale.value)

// Mouse events
function handleMouseDown(e) {
    if (e.button !== 0) return // Only left click
    if (!canDrag.value) return
    e.preventDefault()
    isDragging.value = true
    dragStartX.value = e.clientX - lastTx.value
    dragStartY.value = e.clientY - lastTy.value
}

function handleMouseMove(e) {
    if (!isDragging.value) return
    e.preventDefault()
    tx.value = e.clientX - dragStartX.value
    ty.value = e.clientY - dragStartY.value
}

function handleMouseUp() {
    if (isDragging.value) {
        isDragging.value = false
        lastTx.value = tx.value
        lastTy.value = ty.value
    }
}

// Touch events
function handleTouchStart(e) {
    if (e.touches.length === 2) {
        // Pinch to zoom
        pinchStartDist.value = Math.hypot(
            e.touches[0].clientX - e.touches[1].clientX,
            e.touches[0].clientY - e.touches[1].clientY
        )
        pinchStartScale.value = scale.value
        isDragging.value = false
    } else if (e.touches.length === 1) {
        touchStartX.value = e.touches[0].clientX
        touchStartY.value = e.touches[0].clientY
        touchLastX.value = e.touches[0].clientX
        touchLastY.value = e.touches[0].clientY
        hasMoved.value = false

        if (canDrag.value) {
            isDragging.value = true
            dragStartX.value = e.touches[0].clientX - lastTx.value
            dragStartY.value = e.touches[0].clientY - lastTy.value
        }
    }
}

function handleTouchMove(e) {
    if (e.touches.length === 2) {
        // Pinch zoom
        e.preventDefault()
        const dist = Math.hypot(
            e.touches[0].clientX - e.touches[1].clientX,
            e.touches[0].clientY - e.touches[1].clientY
        )
        if (pinchStartDist.value > 0) {
            const s = dist / pinchStartDist.value
            scale.value = Math.min(Math.max(pinchStartScale.value * s, 0.1), 10)
        }
    } else if (e.touches.length === 1) {
        touchLastX.value = e.touches[0].clientX
        touchLastY.value = e.touches[0].clientY

        if (isDragging.value) {
            e.preventDefault()
            const dx = e.touches[0].clientX - touchStartX.value
            const dy = e.touches[0].clientY - touchStartY.value

            // Check if finger moved significantly
            if (Math.abs(dx) > 5 || Math.abs(dy) > 5) {
                hasMoved.value = true
            }

            tx.value = e.touches[0].clientX - dragStartX.value
            ty.value = e.touches[0].clientY - dragStartY.value
        }
    }
}

function handleTouchEnd(_e) {
    // Check for swipe navigation (only at fit scale)
    if (scale.value <= fitScale.value && showNav.value && !hasMoved.value) {
        const dx = touchStartX.value - touchLastX.value
        const dy = touchStartY.value - touchLastY.value
        const absDx = Math.abs(dx)
        const absDy = Math.abs(dy)

        if (absDx > 50 && absDx > absDy) {
            if (dx > 0) navigateNext()  // Swipe left → next
            else navigatePrev()          // Swipe right → prev
            isDragging.value = false
            pinchStartDist.value = 0
            return
        }
    }

    // If zoomed out below fit scale, reset
    if (scale.value < fitScale.value) {
        scale.value = fitScale.value
        tx.value = 0
        ty.value = 0
        lastTx.value = 0
        lastTy.value = 0
    } else {
        lastTx.value = tx.value
        lastTy.value = ty.value
    }

    isDragging.value = false
    pinchStartDist.value = 0
}

function collectMdImages(container, clickedImg) {
    const imgs = container.querySelectorAll('img')
    const list = []
    let startIdx = 0
    imgs.forEach((img) => {
        const src = img.src
        if (!src) return
        const alt = img.alt || ''
        const name = alt || extractImageName(src)
        list.push({ src, name })
        if (img === clickedImg) startIdx = list.length - 1
    })
    return { list, startIdx }
}

provide('openLightbox', open)
provide('openSvgLightbox', openSvg)
provide('openMdImages', openMdImages)

defineExpose({ open, openMdImages, openSvg })

// When SVG content changes, recalculate fit scale
watch(currentSvg, (val) => {
    if (val) onSvgMounted()
})

// Register back handler when lightbox opens, unregister when it closes
watch(lightboxVisible, (visible) => {
    if (visible) {
        unregisterBack = registerBackHandler({
            id: 'lightbox',
            canGoBack: () => lightboxVisible.value,
            goBack: () => close(),
            priority: PRIORITY_OVERLAY,
        })
    } else if (unregisterBack) {
        unregisterBack()
        unregisterBack = null
    }
})

onMounted(() => {
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    // Listen for clicks on images and mermaid diagrams to open lightbox
    document.addEventListener('click', (e) => {
        // Touch mode: direct click on .lightbox-img or .mermaid opens lightbox
        // PC mode: only click on .lightbox-expand-icon opens lightbox
        const isExpandIcon = !!e.target.closest('.lightbox-expand-icon')
        // PC mode: only expand icon opens lightbox (not the image/mermaid itself)
        if (!isExpandIcon && e.pointerType !== 'touch') return

        // When clicking the expand icon, find the image from the wrapper
        // (the icon is a sibling of the img, not a child)
        let img
        if (isExpandIcon) {
            const wrap = e.target.closest('.lightbox-img-wrap')
            img = wrap ? wrap.querySelector('.lightbox-img') : null
        } else {
            img = e.target.closest('.lightbox-img')
        }
        if (img) {
            e.preventDefault()
            // Check if the image is inside a markdown body — collect sibling images for navigation
            const mdContainer = img.closest('.markdown-body, .chat-message')
            if (mdContainer) {
                const allImgs = mdContainer.querySelectorAll('img')
                if (allImgs.length > 1) {
                    const { list, startIdx } = collectMdImages(mdContainer, img)
                    if (list.length > 1) {
                        openMdImages(list, startIdx)
                        return
                    }
                }
            }
            open(fullImgSrc(img))
            return
        }
        const mermaidDiv = e.target.closest('.markdown-body .mermaid, .chat-message .mermaid')
        if (mermaidDiv) {
            e.preventDefault()
            const svg = mermaidDiv.querySelector('svg')
            if (svg) openSvg(svg.outerHTML)
        }
    })
})

onUnmounted(() => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
    if (unregisterBack) {
        unregisterBack()
        unregisterBack = null
    }
})
</script>

<style scoped>
.lightbox {
    position: fixed;
    inset: 0;
    z-index: 3000;
    display: flex;
    align-items: center;
    justify-content: center;
    touch-action: none;
    overscroll-behavior: none;
}

.lightbox-backdrop {
    position: absolute;
    inset: 0;
    background: var(--lb-bg, rgba(0,0,0,0.65));
    cursor: zoom-out;
}

.lightbox-toolbar {
    position: absolute;
    top: calc(16px + var(--header-safe-area-top, 0px));
    left: 16px;
    right: 16px;
    display: flex;
    gap: 8px;
    z-index: 10;
    align-items: center;
}

.lb-filename {
    color: rgba(255,255,255,0.85);
    font-size: 13px;
    user-select: none;
    pointer-events: none;
    background: rgba(0,0,0,0.5);
    padding: 4px 12px;
    border-radius: 12px;
    backdrop-filter: blur(4px);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.lb-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-left: auto;
    flex-shrink: 0;
}

.lb-nav-btn {
    background: rgba(0,0,0,0.5) !important;
    color: rgba(255,255,255,0.9) !important;
    backdrop-filter: blur(4px);
}

.lb-nav-btn:hover {
    background: rgba(255,255,255,0.2) !important;
}

.lightbox-bottom-bar {
    position: absolute;
    bottom: 16px;
    left: 16px;
    right: 16px;
    display: flex;
    gap: 8px;
    z-index: 10;
    align-items: center;
    justify-content: center;
}

.lb-counter {
    color: rgba(255,255,255,0.7);
    font-size: 12px;
    min-width: 40px;
    text-align: center;
    user-select: none;
    pointer-events: none;
    background: rgba(0,0,0,0.5);
    padding: 2px 8px;
    border-radius: 10px;
    backdrop-filter: blur(4px);
}

.lb-btn {
    width: 40px;
    height: 40px;
    border: none;
    border-radius: 8px;
    background: var(--lb-toolbar-bg, rgba(255,255,255,0.9));
    color: var(--text-primary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.15s, transform 0.15s;
    backdrop-filter: blur(8px);
    touch-action: manipulation;
    flex-shrink: 0;
}

.lb-btn:hover {
    background: var(--accent-color);
    transform: scale(1.05);
}

.lb-btn svg {
    width: 20px;
    height: 20px;
}

.lb-btn.lb-close:hover {
    background: #ef4444;
}

.lightbox-content {
    position: relative;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    touch-action: none;
    overscroll-behavior: none;
}

.lightbox-content.can-drag {
    cursor: grab;
}

.lightbox-content.grabbing {
    cursor: grabbing;
}

.lightbox-content.slide-left {
    animation: slideLeft 0.25s ease-out;
}

.lightbox-content.slide-right {
    animation: slideRight 0.25s ease-out;
}

@keyframes slideLeft {
    from { opacity: 0; transform: translateX(40px); }
    to { opacity: 1; transform: translateX(0); }
}

@keyframes slideRight {
    from { opacity: 0; transform: translateX(-40px); }
    to { opacity: 1; transform: translateX(0); }
}

.lb-loading-spinner {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 6;
    color: rgba(255,255,255,0.5);
}

.lb-loading-spinner svg {
    animation: spin 1s linear infinite;
}

@keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
}

.lightbox-content img {
    max-width: 100%;
    max-height: 100%;
    display: block;
    transform-origin: center center;
    user-select: none;
    -webkit-user-drag: none;
    -webkit-user-select: none;
    pointer-events: auto;
}

.lightbox-content :deep(svg) {
    max-width: 100%;
    max-height: 100%;
    display: block;
    transform-origin: center center;
    user-select: none;
    background: var(--bg-primary);
}
</style>
