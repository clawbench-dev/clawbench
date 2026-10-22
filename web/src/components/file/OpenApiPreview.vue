<template>
  <div class="openapi-preview">
    <div v-if="loading" class="openapi-loading">
      <PanelSkeleton variant="text" :rows="9" :label="loadingLabel" />    </div>
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
import { useI18n } from 'vue-i18n'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'
import { isDarkTheme } from '@/utils/themeMeta'
import { attachIframeSelectionQuoteBridge, resolveIframeWindow } from '@/utils/iframeSelectionQuote.ts'
import { makeIframeQuoteShowHandler, useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'

const props = defineProps({
  file: Object,
  viewMode: String,
  /** Whether selecting text in the rendered docs surfaces the quote bar.
      The main app enables it; the share SPA (no session/chat) keeps it off. */
  chatQuote: { type: Boolean, default: false },
})

const iframeRef = ref(null)
const loading = ref(true)
const { t } = useI18n()
const loadingLabel = computed(() => t('common.loading'))

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
const theme = inject('theme', ref('gruvbox-dark'))
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

// Attach the quote bridge to the iframe's content window (shared with the HTML
// preview's bridge; see resolveIframeWindow for the attach-retry contract).
function liveOpenApiIframe() {
  return iframeRef.value || document.querySelector('.openapi-iframe')
}

async function attachBridgeToIframe() {
  const win = await resolveIframeWindow(liveOpenApiIframe)
  if (!win) return
  // Multiple load events for the same document must not churn the bridge
  // (dispose+re-attach would tear down and rebuild selection listeners
  // pointlessly). A real spec reload navigates to a new document — a
  // different contentWindow — so dispose there and attach afresh.
  if (bridgeWin === win) return
  disposeQuoteBridge()
  disposeBridge = attachIframeSelectionQuoteBridge({
    win,
    filePath: () => props.file?.path || '',
    // Shared with the HTML previews. It was the one host still hand-rolling
    // this callback, and it silently dropped `anchor` — so the bar fell back to
    // its pinned top position (the reported "menu pops up at the top") — and
    // `suppressBar`, so Android got both the system menu and the in-page bar.
    show: makeIframeQuoteShowHandler(quoteQuestion.showBar),
    hide: () => quoteQuestion.hideBar(),
  })
  bridgeWin = win
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
  align-items: stretch;
  justify-content: center;
  flex: 1;
  padding: var(--space-5);
  min-width: 0;
}

.openapi-loading > * {
  flex: 1;
  min-width: 0;
}


.openapi-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: var(--bg-primary, #fff);
}
</style>
