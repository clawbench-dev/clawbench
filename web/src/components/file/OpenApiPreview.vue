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
import { ref, computed, watch, inject, onBeforeUnmount } from 'vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'
import { isDarkTheme } from '@/utils/themeMeta'
import { attachOpenApiQuoteBridge } from '@/utils/openapiQuote.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'

const props = defineProps({
  file: Object,
  viewMode: String,
  /** Whether selecting text in the rendered docs surfaces the quote bar.
      The main app enables it; the share SPA (no session/chat) keeps it off. */
  chatQuote: { type: Boolean, default: false },
})

const iframeRef = ref(null)
const loading = ref(true)

const quoteQuestion = useQuoteQuestion()

// Bridge lifecycle: attaches once the srcdoc iframe finished loading and
// selects the rendered doc. Disposed before the iframe is reloaded (spec
// change) or the component unmounts, then re-attached on the next load.
// (Plain <script setup> without lang="ts" — no standalone type annotations.)
let disposeBridge = null
// The content window the bridge is currently attached to. jsdom (and some
// fast-loading srcdoc setups) can fire multiple "load" events for the same
// document; deduping by window keeps the bridge from being churned.
let bridgeWin = null

function disposeQuoteBridge() {
  if (disposeBridge) {
    disposeBridge()
    disposeBridge = null
  }
  bridgeWin = null
}

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
  // The iframe is reloaded with the new srcdoc; drop the old bridge so the
  // pending-load selection listener never leaks across documents.
  disposeQuoteBridge()
  loading.value = true
})

// Attach the quote bridge to the iframe's content window. Browsers expose
// contentWindow as soon as "load" fires, but jsdom can dispatch the load event
// before it finishes attaching the browsing context — and may even replace the
// iframe element mid-navigation (a captured event target can go stale). Re-query
// the live iframe each microtask turn until a contentWindow appears; the bounded
// microtask loop is deterministic and leak-free in tests, and real browsers
// attach on the first check.
const BRIDGE_ATTACH_MICROTURNS = 8

function microtaskSleep(n) {
  let p = Promise.resolve()
  for (let i = 0; i < n; i++) p = p.then(() => {})
  return p
}

function liveOpenApiIframe() {
  return iframeRef.value || document.querySelector('.openapi-iframe')
}

async function attachBridgeToIframe() {
  for (let i = 0; i <= BRIDGE_ATTACH_MICROTURNS; i++) {
    const win = liveOpenApiIframe()?.contentWindow
    if (win) {
      // Multiple load events for the same document must not churn the bridge
      // (dispose+re-attach would tear down and rebuild selection listeners
      // pointlessly). A real spec reload navigates to a new document — a
      // different contentWindow — so dispose there and attach afresh.
      if (bridgeWin === win) return
      disposeQuoteBridge()
      disposeBridge = attachOpenApiQuoteBridge({
        win,
        filePath: () => props.file?.path || '',
        show: (data) => quoteQuestion.showBar(data, { delay: 0 }),
        hide: () => quoteQuestion.hideBar(),
      })
      bridgeWin = win
      return
    }
    if (i < BRIDGE_ATTACH_MICROTURNS) await microtaskSleep(1)
  }
}

function onIframeLoad() {
  loading.value = false
  if (!props.chatQuote) return
  void attachBridgeToIframe()
}

onBeforeUnmount(() => {
  disposeQuoteBridge()
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


.openapi-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: var(--bg-primary, #fff);
}
</style>
