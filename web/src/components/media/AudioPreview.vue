<template>
  <div class="audio-preview-container">
    <div class="audio-preview-body">
      <div class="audio-icon">
        <Music :size="40" />
      </div>
      <div class="audio-info">
        <div class="audio-name">{{ file.name }}</div>
        <div class="audio-size" v-if="fileSize">{{ fileSize }}</div>
      </div>
      <audio
        ref="audioRef"
        :src="mediaUrl"
        controls
        class="audio-player"
        @loadedmetadata="onLoaded"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { Music } from 'lucide-vue-next'

const props = defineProps({
    file: Object,
})

// Reactivity trigger: changes the computed URL when the file prop changes,
// forcing Vue to re-fetch rather than reusing the same <audio> element.
// (Server-side Cache-Control: no-store handles browser caching; this handles Vue DOM reuse.)
const mediaTimestamp = ref(Date.now())
watch(() => props.file, () => { mediaTimestamp.value = Date.now() })
const mediaUrl = computed(() =>
    `/api/local-file/${encodeURIComponent(props.file.path)}?t=${mediaTimestamp.value}`
)

const audioRef = ref(null)
const duration = ref(0)

const fileSize = computed(() => {
    if (!props.file?.size) return null
    const size = props.file.size
    if (size < 1024) return size + ' B'
    if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' KB'
    return (size / (1024 * 1024)).toFixed(1) + ' MB'
})

function onLoaded() {
    if (audioRef.value) {
        duration.value = audioRef.value.duration
    }
}
</script>

<style scoped>
.audio-preview-container {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 0;
}

.audio-preview-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--bg-primary);
    gap: 16px;
}

.audio-icon {
    width: 80px;
    height: 80px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-tertiary);
    border-radius: 50%;
    color: var(--accent-color);
}

.audio-icon svg {
    width: 40px;
    height: 40px;
}

.audio-info {
    text-align: center;
}

.audio-name {
    font-size: 15px;
    font-weight: 500;
    color: var(--text-primary);
    word-break: break-all;
    max-width: 300px;
}

.audio-size {
    font-size: 13px;
    color: var(--text-muted);
    margin-top: 4px;
}

.audio-player {
    width: 100%;
    max-width: 400px;
    height: 42px;
    border-radius: var(--radius-sm);
    outline: none;
}

.audio-player::-webkit-media-controls-panel {
    background: var(--bg-tertiary);
}

.audio-player::-webkit-media-controls-current-time-display,
.audio-player::-webkit-media-controls-time-remaining-display {
    color: var(--text-secondary);
}
</style>
