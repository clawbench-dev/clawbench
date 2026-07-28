<template>
  <div class="video-preview-container">
    <div class="video-preview-body">
      <video
        ref="videoRef"
        :src="mediaUrl"
        controls
        class="video-player"
        @loadedmetadata="onLoaded"
      >
        {{ t('media.videoNotSupported') }}
      </video>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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

function onLoaded() {
    // Video is ready to play
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
    padding: 16px;
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
