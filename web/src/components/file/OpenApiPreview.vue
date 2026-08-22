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
import { ref, computed, watch, inject } from 'vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'
import { isDarkTheme } from '@/utils/themeMeta'

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

// Reactively track theme via App.vue provide('theme', theme)
const theme = inject('theme', ref('github-dark'))
const isDark = computed(() => isDarkTheme(theme.value))

// Fixed scrollbar colors — one for light, one for dark.
// No per-theme customization needed; Swagger UI has its own color scheme.
const scrollbarColors = computed(() => isDark.value
  ? { thumb: '#585858', track: '#1e1e1e' }
  : { thumb: '#c1c1c1', track: '#f5f5f5' }
)

const swaggerSrcdoc = computed(() => buildSwaggerSrcdoc(
  specData.value,
  isDark.value,
  scrollbarColors.value.thumb,
  scrollbarColors.value.track,
))

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
