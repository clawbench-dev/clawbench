<template>
  <div class="code-preview-media" :class="`is-${kind}`">
    <!-- Raster image / SVG: shown at natural aspect, contained in the pane.
         Draggable (wide-screen) onto the chat column to attach the existing
         file — same internal payload the file manager uses, no re-upload. -->
    <img
      v-if="kind === 'image'"
      :src="mediaUrl"
      :alt="fileName"
      class="code-preview-media-img"
      :draggable="isWideScreen"
      @load="onLoad"
      @error="onError"
      @dragstart="onImageDragStart"
      @dragend="onImageDragEnd"
    />

    <!-- Video: native controls, contained in the pane -->
    <video
      v-else-if="kind === 'video'"
      :src="mediaUrl"
      class="code-preview-media-video"
      controls
      preload="metadata"
      @loadedmetadata="onLoad"
      @error="onError"
    />

    <!-- Audio: compact player with the file identity above it -->
    <div v-else-if="kind === 'audio'" class="code-preview-media-audio">
      <div class="code-preview-media-audio-icon">
        <Music :size="40" />
      </div>
      <div class="code-preview-media-audio-name">{{ fileName }}</div>
      <audio
        :src="mediaUrl"
        class="code-preview-media-audio-player"
        controls
        preload="metadata"
        @loadedmetadata="onLoad"
        @error="onError"
      />
    </div>

    <!-- PDF: lazy-loaded pdf.js viewer, fills the pane. It reports no error
         event, so a load failure surfaces through the viewer's own UI. -->
    <PdfPreview v-else-if="kind === 'pdf'" :file="{ path, name: fileName }" />

    <!-- Load failure fallback: overlays the failed element so the card stays
         usable (Open file still works). -->
    <div v-if="loadFailed" class="code-preview-media-error">
      <FileX :size="32" />
      <span>{{ t('file.codePreview.mediaLoadError') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileX, Music } from 'lucide-vue-next'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { mediaVersionFor, trackMediaPath } from '@/composables/useMediaWatch.ts'

// pdf.js is heavy (~500KB) and only pulled in when a PDF is actually previewed.
const PdfPreview = defineAsyncComponent(
  buildAsyncComponentOptions({ loader: () => import('@/components/media/PdfPreview.vue') })
)

const props = defineProps<{
  /** Project-relative or absolute path of the media file. */
  path: string
  /** Media kind — drives which element renders. Null only in the transient
   *  frame before the target's flags settle; the parent gates on isMediaView. */
  kind: 'image' | 'video' | 'audio' | 'pdf' | null
  /** Bumped by the card's Refresh action so the media element re-requests. */
  refreshNonce?: number
}>()

const { t } = useI18n()
const { isWideScreen } = useWideScreenLayout()

const fileName = computed(() => props.path.split('/').pop() || props.path)

/** Drag the previewed image onto the chat column → attach this path (no upload). */
function onImageDragStart(e: DragEvent) {
  startAttachDrag(e, props.path, fileName.value)
}

function onImageDragEnd() {
  cleanupDragGhost()
}

// Cache-bust on path change (or explicit refresh) so a re-opened / replaced
// file is re-fetched rather than served from the element's cached bytes.
const mediaTimestamp = ref(Date.now())
watch(() => [props.path, props.refreshNonce], () => {
  mediaTimestamp.value = Date.now()
  loadFailed.value = false
})

// A background rewrite of the SAME path (the AI redrawing this image) bumps the
// shared version. Reading it here keeps the URL correct across re-renders; the
// composable additionally patches the live element so a no-re-render surface
// still refreshes.
const mediaVersion = computed(() => mediaVersionFor(props.path))

// Keep the file watched even when its element is not in the DOM yet (async
// media mount), so a change arriving early is not missed.
let untrackMedia: (() => void) | null = null
watch(() => props.path, (path) => {
  untrackMedia?.()
  untrackMedia = path ? trackMediaPath(path) : null
}, { immediate: true })
onUnmounted(() => { untrackMedia?.() })

// Raw bytes come from /api/local-file/ (correct MIME, inline, no 10 MiB cap),
// not /api/file — which is JSON and reports raster images as binary.
const mediaUrl = computed(() => {
  const base = buildLocalFileUrl(props.path)
  return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}.${mediaVersion.value}`
})

const loadFailed = ref(false)
const emit = defineEmits<{ (e: 'loaded'): void }>()

function onLoad() {
  loadFailed.value = false
  emit('loaded')
}

function onError() {
  loadFailed.value = true
}

// The card calls the shared body contract on every open / status change
// (`bodyRef.value?.scrollToTargetLine()`). A <script setup> component without
// defineExpose still resolves to a truthy empty proxy, so the optional chain
// does NOT protect the call — provide no-op implementations to satisfy it.
defineExpose({
  scrollToTargetLine: () => {},
  scrollLineIntoView: () => {},
})
</script>
