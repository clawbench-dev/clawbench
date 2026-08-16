<template>
  <div class="openapi-preview">
    <div v-if="loading" class="openapi-loading">
      <LoadingIndicator size="md" />
    </div>
    <iframe
      v-show="!loading"
      ref="iframeRef"
      class="openapi-iframe"
      :srcdoc="redocSrcdoc"
      sandbox="allow-scripts"
      @load="onIframeLoad"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { buildRedocSrcdoc } from '@/utils/redocHtml.ts'

const props = defineProps({
  file: Object,
  viewMode: String,
})

const iframeRef = ref<HTMLIFrameElement | null>(null)
const loading = ref(true)

// Determine the spec data for ReDoc:
// - YAML files: backend returns specJson (YAML→JSON conversion)
// - JSON files: use content directly
const specData = computed(() => {
  if (props.file?.specJson) return props.file.specJson
  return props.file?.content || ''
})

// Read scrollbar colors from CSS variables (same as project-wide scrollbar style)
const scrollbarThumb = getComputedStyle(document.documentElement).getPropertyValue('--scrollbar-thumb').trim() || '#c1c1c1'
const scrollbarTrack = getComputedStyle(document.documentElement).getPropertyValue('--scrollbar-track').trim() || 'transparent'

const redocSrcdoc = computed(() => buildRedocSrcdoc(specData.value, scrollbarThumb, scrollbarTrack))

// Reset loading when spec changes
watch(() => [props.file?.content, props.file?.specJson], () => {
  loading.value = true
})

function onIframeLoad() {
  loading.value = false
}
</script>

<style scoped>
.openapi-preview {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.openapi-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
}


.openapi-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}
</style>
