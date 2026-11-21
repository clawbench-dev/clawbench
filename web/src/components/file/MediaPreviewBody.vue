<template>
  <div class="code-preview-media" :class="`is-${kind}`">
    <!-- Raster image / SVG: shown at natural aspect, contained in the pane.
         Draggable (wide-screen) onto the chat column to attach the existing
         file — same internal payload the file manager uses, no re-upload.
         A failed element is HIDDEN (not removed) so the broken-image glyph and
         its alt text do not sit beside the failure card below, while a retry
         (path / refreshNonce change) can still revive the same element. -->
    <img
      v-if="kind === 'image'"
      :key="mediaUrl"
      :src="mediaUrl"
      :alt="fileName"
      class="code-preview-media-img"
      :class="{ 'local-media-hidden': loadFailed }"
      v-chat-drag="{ kind: 'attach', path, name: fileName }"
      @load="onLoad"
      @error="onError"
    />

    <!-- Video: native controls, contained in the pane. Opening the preview
         starts it muted — browsers only auto-start muted media. -->
    <video
      v-else-if="kind === 'video'"
      ref="videoRef"
      :src="mediaUrl"
      class="code-preview-media-video"
      :class="{ 'local-media-hidden': loadFailed }"
      controls
      autoplay
      muted
      playsinline
      preload="metadata"
      @loadedmetadata="onLoad"
      @error="onError"
    />

    <!-- Audio: compact player with the file identity above it -->
    <div
      v-else-if="kind === 'audio'"
      class="code-preview-media-audio"
      :class="{ 'local-media-hidden': loadFailed }"
    >
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

    <!-- Load failure fallback: the shared element, filling the pane so the card
         still reads as a deliberate state rather than a blank box. -->
    <MediaLoadError v-if="loadFailed" :kind="mediaErrorKind" :name="fileName" fill />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted, nextTick, defineAsyncComponent } from 'vue'
import { Music } from 'lucide-vue-next'
import MediaLoadError from '@/components/media/MediaLoadError.vue'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
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

const fileName = computed(() => props.path.split('/').pop() || props.path)

/**
 * Which icon the failure element shows. Only the image / video / audio branches
 * set `loadFailed` — the PDF viewer reports its own failures and never reaches
 * this state — so the generic `file` icon only covers the transient frame
 * before `kind` settles.
 */
const mediaErrorKind = computed<'image' | 'video' | 'audio' | 'file'>(() => {
  if (props.kind === 'image' || props.kind === 'video' || props.kind === 'audio') return props.kind
  return 'file'
})

// Cache-bust on path change (or explicit refresh) so a re-opened / replaced
// file is re-fetched rather than served from the element's cached bytes.
const mediaTimestamp = ref(Date.now())
watch(() => [props.path, props.refreshNonce], () => {
  mediaTimestamp.value = Date.now()
  loadFailed.value = false
  // The same element is reused for the next clip. A clip the user unmuted
  // must not stop that one from auto-starting — browsers only auto-start
  // muted media.
  if (videoRef.value) videoRef.value.muted = true
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
onMounted(() => {
  window.addEventListener('resize', fitVideoToCard)
  // The card is placed before this mounts, so its bound is readable now —
  // don't wait for `loadedmetadata` to fit the player.
  nextTick(fitVideoToCard)
})
onUnmounted(() => {
  window.removeEventListener('resize', fitVideoToCard)
  untrackMedia?.()
})

// Raw bytes come from /api/fs/raw/ (correct MIME, inline, no 10 MiB cap),
// not /api/file — which is JSON and reports raster images as binary.
const mediaUrl = computed(() => {
  const base = buildLocalFileUrl(props.path)
  return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}.${mediaVersion.value}`
})

const loadFailed = ref(false)
const emit = defineEmits<{ (e: 'loaded'): void }>()

const videoRef = ref<HTMLVideoElement | null>(null)

/**
 * Cap the player at the height its card actually has.
 *
 * The floating preview card is content-sized, so the pane only gets a height
 * once the video has been laid out — `height: 100%` cannot resolve there and
 * the element keeps its intrinsic height. A clip taller than the card (a
 * portrait phone video) is then cropped top and bottom by the card's
 * `overflow: hidden`. The card's own box — its inline height when the user
 * resized it, otherwise the max-height the placement logic picked — is the
 * bound, so subtracting the rows above the pane caps the element exactly
 * there, and `object-fit: contain` letterboxes instead of cropping.
 *
 * Only the floating card needs this: the docked pane and the touch sheet give
 * the pane a definite height, where the stylesheet already fits the player
 * (there is no `.code-link-preview-floating` ancestor, or its max-height is
 * `none`, and this returns without touching the element).
 */
function fitVideoToCard() {
  const el = videoRef.value
  const card = el?.closest('.code-link-preview-floating') as HTMLElement | null
  const pane = el?.parentElement
  if (!el || !card || !pane) return
  const userHeight = parseFloat(card.style.height)
  const bound = Number.isFinite(userHeight)
    ? userHeight
    : parseFloat(getComputedStyle(card).maxHeight)
  if (!Number.isFinite(bound)) return
  const chrome = pane.getBoundingClientRect().top - card.getBoundingClientRect().top
  el.style.maxHeight = `${Math.max(0, Math.round(bound - chrome))}px`
}

function onLoad() {
  loadFailed.value = false
  if (props.kind === 'video') fitVideoToCard()
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
