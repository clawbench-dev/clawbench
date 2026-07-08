<template>
  <div class="openapi-preview">
    <div v-if="loading" class="openapi-loading">
      <div class="loading-spinner"></div>
    </div>
    <iframe
      v-show="!loading"
      ref="iframeRef"
      class="openapi-iframe"
      :srcdoc="swaggerSrcdoc"
      sandbox="allow-scripts"
      @load="onIframeLoad"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { buildSwaggerSrcdoc } from '@/utils/redocHtml.ts'

const props = defineProps({
  file: Object,
  viewMode: String,
})

const iframeRef = ref<HTMLIFrameElement | null>(null)
const loading = ref(true)

// Detect ClawBench dark mode from data-theme attribute
const isDark = computed(() => document.documentElement.getAttribute('data-theme') === 'dark')

// Determine the spec data for Swagger UI:
// - YAML files: backend returns specJson (YAML→JSON conversion)
// - JSON files: use content directly
const specData = computed(() => {
  if (props.file.specJson) return props.file.specJson
  return props.file.content || ''
})

const swaggerSrcdoc = computed(() => buildSwaggerSrcdoc(specData.value, isDark.value))

function onIframeLoad() {
  loading.value = false
}

// Reset loading when spec changes
watch(() => [props.file?.content, props.file?.specJson], () => {
  loading.value = true
})
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

.loading-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: loading-spin 0.7s linear infinite;
}

@keyframes loading-spin {
  to { transform: rotate(360deg); }
}

.openapi-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}

[data-theme="dark"] .openapi-iframe {
  background: #1a1a2e;
}
</style>
