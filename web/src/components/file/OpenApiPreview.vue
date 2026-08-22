<template>
  <div class="openapi-preview">
    <div v-if="loading" class="openapi-loading">
      <LoadingIndicator size="md" />
    </div>
    <iframe
      v-show="!loading"
      ref="iframeRef"
      class="openapi-iframe"
      :srcdoc="swaggerSrcdoc"
      sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
      @load="onIframeLoad"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'

const props = defineProps({
  file: Object,
  viewMode: String,
})

const iframeRef = ref<HTMLIFrameElement | null>(null)
const loading = ref(true)

// Determine the spec data for Swagger UI:
// - YAML files: backend returns specJson (YAML→JSON conversion)
// - JSON files: use content directly
const specData = computed(() => {
  if (props.file?.specJson) return props.file.specJson
  return props.file?.content || ''
})

// Detect dark theme from ClawBench's data-theme-base attribute
const isDark = computed(() => document.documentElement.getAttribute('data-theme-base') === 'dark')

// Read scrollbar colors from CSS variables (same as project-wide scrollbar style)
const scrollbarThumb = getComputedStyle(document.documentElement).getPropertyValue('--scrollbar-thumb').trim() || '#c1c1c1'
const scrollbarTrack = getComputedStyle(document.documentElement).getPropertyValue('--scrollbar-track').trim() || 'transparent'

const swaggerSrcdoc = computed(() => buildSwaggerSrcdoc(specData.value, isDark.value, scrollbarThumb, scrollbarTrack))

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
  background: var(--bg-primary, #fff);
}
</style>
