<template>
  <div class="html-preview-content">
    <!-- Loading -->
    <div v-if="status === 'loading'" class="code-preview-status" aria-live="polite">
      <span class="li-spinner code-preview-spinner" aria-hidden="true" />
      <span>{{ t('file.codePreview.loading') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="status === 'error'" class="code-preview-status" role="status">
      <span>{{ errorMessageText }}</span>
      <button v-if="errorCode === 'network'" class="code-preview-btn" @click="emit('refresh')">
        {{ t('file.codePreview.retry') }}
      </button>
    </div>

    <!-- Rendered HTML document. Loaded by real URL so the document's own
         relative references (stylesheets, scripts, images, fonts) resolve
         beside it; an external (absolute-path) file has no usable URL and
         falls back to srcdoc (see buildHtmlPreviewSrc). -->
    <iframe
      v-else-if="status === 'ready'"
      class="html-preview-iframe"
      :src="htmlSrc || undefined"
      :srcdoc="htmlSrc ? undefined : htmlContent"
      sandbox="allow-scripts allow-same-origin"
      :title="filePath"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

/**
 * Rendered-HTML sibling of CodePreviewBody / MarkdownPreviewBody.
 *
 * The code-slice preview renders a file as numbered source lines. When an HTML
 * file is previewed without a target line range, the document is rendered in a
 * sandboxed iframe instead — the same presentation the full-screen viewer uses,
 * and deliberately showing the COMPLETE file with no line slicing (the iframe
 * loads the document whole).
 *
 * The sandbox (`allow-scripts allow-same-origin`) and the srcdoc fallback for
 * external files are the decisions recorded in fileViewerSandbox.test.ts; see
 * buildHtmlPreviewSrc for the rationale. `htmlSrc === ''` means the caller
 * wants the srcdoc fallback, and `htmlContent` carries the document text.
 *
 * It mirrors MarkdownPreviewBody's exposed surface (status/error shell plus
 * no-op scroll helpers) so the parent can swap the rendered bodies without a
 * special case.
 */

export type HtmlBodyStatus = 'idle' | 'loading' | 'ready' | 'error'

defineProps<{
  status: HtmlBodyStatus
  errorMessageText: string
  errorCode: string | null
  /** Iframe URL. Empty string selects the srcdoc fallback. */
  htmlSrc: string
  /** Raw document text, used only for the srcdoc fallback. */
  htmlContent: string
  /** Previewed file path — used as the iframe title for accessibility. */
  filePath: string
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const { t } = useI18n()

/** No-op target-line centering: the rendered view has no highlighted rows.
 *  Kept so the parent's bodyRef surface is identical across body components. */
function scrollToTargetLine() {
  // rendered view: nothing to center
}

/** No-op search navigation: the rendered view has no per-line rows. */
function scrollLineIntoView(_lineIdx: number) {
  // rendered view: no row-level scrolling
}

defineExpose({
  scrollToTargetLine,
  scrollLineIntoView,
  get scrollContainer(): HTMLElement | null {
    return null
  },
})
</script>

<style scoped>
/* The component owns the outer shell and the iframe it renders. The iframe is
   a real template element (not v-html), so scoped styling reaches it — no
   global rule needed, and a global one would leak onto the full-screen
   viewer's iframe, which styles the same class locally. */
.html-preview-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.html-preview-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}
</style>
