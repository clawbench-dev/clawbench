<template>
  <div class="video-preview-container">
    <div class="video-preview-body">
      <!-- A missing / unreadable file leaves a black box with a dead player and
           no explanation. The player is HIDDEN rather than removed so a later
           successful load (`loadedmetadata`) revives it. -->
      <MediaLoadError v-if="loadFailed" kind="video" :name="file.name" fill />
      <video
        ref="videoRef"
        :src="mediaUrl"
        controls
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
    padding: var(--space-7);
    background: #000;
    overflow: hidden;
}

.video-player {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border-radius: var(--radius-sm);
    outline: none;
}
</style>
