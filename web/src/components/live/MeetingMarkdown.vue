<template>
  <div
    class="meeting-md markdown-body"
    ref="bodyRef"
    :data-file-path="path"
    @click="interactions.handleClick"
    @dblclick="onDblClickCopy"
    @mousedown="interactions.onTableMouseDown"
    @touchstart.passive="interactions.onTableTouchStart"
    @dragstart="interactions.onDragStart"
    @dragend="interactions.onDragEnd"
    @load.capture="onMediaLoad"
  >
    <div class="markdown-content"><RichTextBlock :html="renderedHtml" /></div>
  </div>

  <TableRowModal
    :data="interactions.tableRowModal.value"
    @close="interactions.closeTableRowModal"
    @prev="interactions.tableRowPrev"
    @next="interactions.tableRowNext"
  />

  <CodeLinkPreview
    v-if="interactions.codeLinkPreview.enabled.value"
    :preview="interactions.codeLinkPreview"
  />
</template>

<script setup lang="ts">
/**
 * The meeting minutes, rendered through the SAME pipeline and interaction layer
 * as the file preview (MarkdownPreview.vue).
 *
 * Why this exists instead of `v-html="renderMarkdownHtml(text)"`:
 *  - relative links resolve against the meeting folder, not the project root;
 *  - inline file-path chips get verified and become openable;
 *  - images, mermaid, code/table headers and drag-to-chat behave identically;
 *  - the navigation source is 'live', so Back returns to the meeting detail.
 *
 * The minutes are AI-generated, so `sanitize: true` (the file preview trusts a
 * local artifact and passes false).
 */
import { watch } from 'vue'
import TableRowModal from '@/components/common/TableRowModal.vue'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import { useMarkdownDocumentRender } from '@/composables/useMarkdownDocumentRender.ts'
import { useMarkdownDocumentInteractions } from '@/composables/useMarkdownDocumentInteractions.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { stampSvgFigures } from '@/utils/svgMediaFit.ts'
import RichTextBlock from '@/components/chat/RichTextBlock.vue'

const props = defineProps<{
  /** Raw minutes markdown. */
  content: string
  /** Minutes file path (absolute or project-relative); relative links resolve
   *  against its directory. */
  path: string
}>()

const emit = defineEmits<{ copy: [text: string] }>()

const { isTouchPrimary } = usePlatformDetect()

const { html: renderedHtml, bodyRef, render } = useMarkdownDocumentRender()

const interactions = useMarkdownDocumentInteractions({
  containerRef: bodyRef,
  filePath: () => props.path,
  source: 'live',
  enableCommitLinks: false,
})

watch(
  () => [props.content, props.path] as const,
  () => {
    void render(props.content, props.path, {
      sanitize: true,
      mermaidPrefix: 'meeting-md',
    })
  },
  { immediate: true },
)

/** Re-stamp proportional SVG sizing once a media file's intrinsic size lands. */
function onMediaLoad(): void {
  stampSvgFigures(bodyRef.value)
}

/**
 * Desktop double-click copies the block.
 *
 * The shared composable's touch double-tap already copies (or opens the native
 * menu); this branch is the desktop affordance the meeting detail always had.
 * Interactive chrome is skipped so double-clicking a path chip or a header
 * button never copies its label instead of acting.
 */
function onDblClickCopy(event: MouseEvent): void {
  if (isTouchPrimary.value) return
  const target = event.target as HTMLElement | null
  if (target?.closest('a, button, .chat-file-path, .chat-file-open-btn, .image-block-wrapper, .code-block-header, .table-block-header')) {
    return
  }
  const text = (target?.innerText ?? target?.textContent ?? '').trim()
  if (text) emit('copy', text)
}
</script>

<style scoped>
/* The parent owns the reading column (font size, padding). This component is
   only the shared document body, exactly like MarkdownPreview's .markdown-body. */
.meeting-md {
  flex: 0 0 auto;
  overflow: visible;
  padding: 0;
  margin: 0;
}
</style>
