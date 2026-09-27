<template>
  <!-- Row 2: File Meta & Remaining Action Tools.

       This is the ONE tool row shared by every CodeLinkPreview surface — the
       desktop floating card, the file manager's docked pane, and the touch
       BottomSheet. Before extraction the sheet rendered its own
       `.code-preview-sheet-row2` + a bottom pill-button footer while the other
       two rendered this row, so the same preview had two different toolbars.
       Keep the surfaces on this markup: any divergence here is the bug. -->
  <div class="code-preview-meta" @pointerdown="onRowPointerDown">
    <div class="code-preview-meta-info">
      <span>{{ metaText || t('file.codePreview.title') }}</span>
    </div>

    <div class="code-preview-actions" @pointerdown.stop>
      <!-- Rendered / Source toggle (Markdown only, no line range) -->
      <button
        v-if="showRenderToggle"
        class="code-preview-btn"
        :class="{ 'is-active': isRenderedView }"
        :aria-pressed="isRenderedView"
        :title="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
        :aria-label="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
        :data-tooltip="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
        @pointerenter="showTooltip($event, isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView'))"
        @pointerleave="hideTooltip()"
        @click="$emit('toggle-render-view')"
      >
        <Eye :size="12" />
      </button>
      <!-- Viewer Tools: Find, Wrap, Line Numbers, Refresh (code-slice view only) -->
      <button
        v-if="showTextTools && !isRenderedView"
        ref="firstActionBtnRef"
        class="code-preview-btn"
        :class="{ 'is-active': isSearchOpen }"
        :title="t('file.codePreview.findInPreview')"
        :aria-label="t('file.codePreview.findInPreview')"
        :data-tooltip="t('file.codePreview.findInPreview')"
        @pointerenter="showTooltip($event, t('file.codePreview.findInPreview'))"
        @pointerleave="hideTooltip()"
        @click="$emit('toggle-search')"
      >
        <Search :size="12" />
      </button>
      <button
        v-if="showTextTools && !isRenderedView"
        class="code-preview-btn"
        :class="{ 'is-active': isWordWrap }"
        :title="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
        :aria-label="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
        :aria-pressed="isWordWrap"
        :data-tooltip="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
        @pointerenter="showTooltip($event, isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap'))"
        @pointerleave="hideTooltip()"
        @click="$emit('toggle-word-wrap')"
      >
        <TextWrap :size="12" />
      </button>
      <button
        v-if="showTextTools && !isRenderedView"
        class="code-preview-btn"
        :class="{ 'is-active': showLineNumbers }"
        :aria-pressed="showLineNumbers"
        :title="t('file.header.lineNumbers')"
        :aria-label="t('file.header.lineNumbers')"
        :data-tooltip="t('file.header.lineNumbers')"
        @pointerenter="showTooltip($event, t('file.header.lineNumbers'))"
        @pointerleave="hideTooltip()"
        @click="$emit('toggle-line-numbers')"
      >
        <Hash :size="12" />
      </button>
      <button
        class="code-preview-btn"
        :title="t('file.codePreview.refresh')"
        :aria-label="t('file.codePreview.refresh')"
        :data-tooltip="t('file.codePreview.refresh')"
        @pointerenter="showTooltip($event, t('file.codePreview.refresh'))"
        @pointerleave="hideTooltip()"
        @click="$emit('refresh')"
      >
        <RefreshCw :size="12" />
      </button>

      <span class="code-preview-actions-divider" />

      <!-- Actions: Quote, Copy Code (code view). Then the contiguous file
           tools — Copy Path / Open Directory (reveal) / Open File — with no
           dividers between them, in that left-to-right order. -->
      <button
        v-if="showTextTools"
        class="code-preview-btn"
        :title="t('file.codePreview.quoteToChat')"
        :aria-label="t('file.codePreview.quoteToChat')"
        :data-tooltip="t('file.codePreview.quoteToChat')"
        @pointerenter="showTooltip($event, t('file.codePreview.quoteToChat'))"
        @pointerleave="hideTooltip()"
        @click="$emit('quote')"
      >
        <MessageSquareQuote :size="12" />
      </button>
      <button
        v-if="showTextTools && !isRenderedView"
        class="code-preview-btn"
        :class="{ 'is-copied': copied }"
        :title="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
        :aria-label="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
        :data-tooltip="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
        @pointerenter="showTooltip($event, copied ? t('file.codePreview.copied') : t('file.codePreview.copy'))"
        @pointerleave="hideTooltip()"
        @click="$emit('copy')"
      >
        <Check v-if="copied" :size="12" />
        <Copy v-else :size="12" />
      </button>

      <!-- Copy Path -->
      <button
        class="code-preview-btn copy-path-btn"
        :class="{ 'is-copied': isPathCopied }"
        :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
        :aria-label="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
        :data-tooltip="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
        @pointerenter="showTooltip($event, isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath'))"
        @pointerleave="hideTooltip()"
        @click="$emit('copy-path')"
      >
        <Check v-if="isPathCopied" :size="12" />
        <Link v-else :size="12" />
      </button>
      <!-- Open Directory (reveal in tree) -->
      <button
        class="code-preview-btn"
        :title="t('file.codePreview.revealInTree')"
        :aria-label="t('file.codePreview.revealInTree')"
        :data-tooltip="t('file.codePreview.revealInTree')"
        @pointerenter="showTooltip($event, t('file.codePreview.revealInTree'))"
        @pointerleave="hideTooltip()"
        @click="$emit('reveal')"
      >
        <Folder :size="12" />
      </button>
      <!-- Open File / View Details.
           A too-large file used to swap this for a wide text button reading
           "View details / Download", which was 4x the width of every other
           control in the row (111px vs 26px) and broke the icon strip. It
           also did nothing different: it called the same openFull() as the
           normal case. So the icon is used in both cases, and only the
           tooltip changes — it carries the "download" affordance for an
           oversize file. -->
      <button
        v-if="!isDirView"
        class="code-preview-btn"
        :title="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
        :aria-label="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
        :data-tooltip="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
        @pointerenter="showTooltip($event, tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull'))"
        @pointerleave="hideTooltip()"
        @click="$emit('open-full')"
      >
        <ExternalLink :size="12" />
      </button>
      <!-- Zoom image (image targets only): opens the shared Lightbox.
           Sits directly beside Open File — both are file-level actions, so
           they stay grouped and separate from the code tools above. -->
      <button
        v-if="isImageTarget"
        class="code-preview-btn"
        :title="t('file.codePreview.openLightbox')"
        :aria-label="t('file.codePreview.openLightbox')"
        :data-tooltip="t('file.codePreview.openLightbox')"
        @pointerenter="showTooltip($event, t('file.codePreview.openLightbox'))"
        @pointerleave="hideTooltip()"
        @click="$emit('view-lightbox')"
      >
        <Maximize2 :size="12" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Copy, ExternalLink, Eye, Folder, Hash, Link, Maximize2, MessageSquareQuote, RefreshCw, Search, TextWrap } from 'lucide-vue-next'

const { t } = useI18n()

withDefaults(defineProps<{
  /** Line/size (or entry-count) summary shown on the left. Empty falls back to
   *  the generic "preview" label. */
  metaText?: string
  /** Text-slice tools are meaningful only for a code/markdown body. */
  showTextTools?: boolean
  showRenderToggle?: boolean
  isRenderedView?: boolean
  isDirView?: boolean
  isImageTarget?: boolean
  /** The open control's tooltip becomes "view details / download". */
  tooLarge?: boolean
  isSearchOpen?: boolean
  isWordWrap?: boolean
  showLineNumbers?: boolean
  copied?: boolean
  isPathCopied?: boolean
  /** Row pointerdown. Floating passes its drag starter; docked/sheet pass a
   *  no-op (their row is not a drag handle). */
  onRowPointerDown?: (e: PointerEvent) => void
  /** Custom fast-tooltip hooks. The sheet passes no-ops: it has no card
   *  element to anchor the tooltip to, so it relies on native `title`. */
  showTooltip?: (e: Event, text: string) => void
  hideTooltip?: () => void
}>(), {
  metaText: '',
  showTextTools: false,
  showRenderToggle: false,
  isRenderedView: false,
  isDirView: false,
  isImageTarget: false,
  tooLarge: false,
  isSearchOpen: false,
  isWordWrap: true,
  showLineNumbers: true,
  copied: false,
  isPathCopied: false,
  onRowPointerDown: () => {},
  showTooltip: () => {},
  hideTooltip: () => {},
})

defineEmits<{
  (e: 'toggle-render-view'): void
  (e: 'toggle-search'): void
  (e: 'toggle-word-wrap'): void
  (e: 'toggle-line-numbers'): void
  (e: 'refresh'): void
  (e: 'quote'): void
  (e: 'copy'): void
  (e: 'copy-path'): void
  (e: 'reveal'): void
  (e: 'open-full'): void
  (e: 'view-lightbox'): void
}>()

/** The search toggle doubles as the F2 focus target (see CodeLinkPreview). */
const firstActionBtnRef = ref<HTMLButtonElement | null>(null)

defineExpose({ focusFirstAction: () => firstActionBtnRef.value?.focus() })
</script>
