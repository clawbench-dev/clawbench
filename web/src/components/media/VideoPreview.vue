<template>
  <div class="video-preview-container">
    <div class="video-preview-body">
      <!-- A missing / unreadable file leaves a black box with a dead player and
           no explanation. The player is HIDDEN rather than removed so a later
           successful load (`loadedmetadata`) revives it. -->
      <MediaLoadError v-if="loadFailed" kind="video" :name="file.name" fill />
      <!-- Opening a clip starts it muted: browsers only auto-start muted media,
           and a viewer that opens on a paused first frame reads as broken. -->
      <video
        ref="videoRef"
        :src="mediaUrl"
        controls
        autoplay
        muted
        playsinline
        preload="metadata"
        class="video-player"
        :class="{ 'local-media-hidden': loadFailed }"
        @loadedmetadata="onLoaded"
        @error="onError"
      >
        {{ t('media.videoNotSupported') }}
      </video>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MediaLoadError from '@/components/media/MediaLoadError.vue'
import { buildLocalFileUrl } from '@/utils/download.ts'

const { t } = useI18n()

const props = defineProps({
    file: Object,
})

// Reactivity trigger: changes the computed URL when the file prop changes,
// forcing Vue to re-fetch rather than reusing the same <video> element.
// (Server-side Cache-Control: no-store handles browser caching; this handles Vue DOM reuse.)
const mediaTimestamp = ref(Date.now())
watch(() => props.file, () => { mediaTimestamp.value = Date.now() })
const mediaUrl = computed(() => {
    const base = buildLocalFileUrl(props.file.path)
    return base + (base.includes('?') ? '&' : '?') + `t=${mediaTimestamp.value}`
  }
)

const videoRef = ref(null)
/** The file could not be fetched/decoded — the player is unusable. */
const loadFailed = ref(false)

function onLoaded() {
    loadFailed.value = false
    // Autoplay only works while the element is muted, and a clip the user
    // unmuted earlier keeps that state on the SAME element when the next file
    // is opened — re-mute so switching clips always starts playing.
    if (videoRef.value) videoRef.value.muted = true
}

function onError() {
    loadFailed.value = true
}
</script>

<style scoped>
.video-preview-container {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    padding: 0;
    overflow: hidden;
}

.video-preview-body {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #000;
    overflow: hidden;
}

/* The player IS the content: it fills the pane and letterboxes the picture, so
   the video grows with the window instead of sitting at its intrinsic size.
   Native controls then span the pane's full width. */
.video-player {
    width: 100%;
    height: 100%;
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    background: #000;
    display: block;
    outline: none;
}
</style>
