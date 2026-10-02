/**
 * Rendered-markdown HTML builder for the code-link preview's document view.
 *
 * The code-slice preview normally renders a file as numbered source lines. When
 * a Markdown file is previewed *without* a line annotation, the preview shows a
 * read-only rendered document instead. This module reuses the shared file-preview
 * markdown pipeline (same output as MarkdownPreview.vue) so a document looks
 * identical whether peeked in the floating card or opened in the full viewer.
 *
 * The document view is deliberately STATIC: file-path annotation / commit / etc.
 * are skipped so no embedded path link responds to clicks inside the card.
 */

import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { createFixLocalImagePaths } from '@/composables/useMarkdownRenderPipeline.ts'
import { dirName } from '@/utils/path.ts'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout.ts'

export interface BuildPreviewMarkdownHtmlOptions {
  /** Source markdown text (already line-sliced for large-file truncation). */
  content: string
  /** Previewed file path — its directory resolves relative images/links. */
  path: string
  isWideScreen?: boolean
  imageTimestamp?: number
}

/** Render a markdown slice into read-only HTML for the preview card. */
export function buildPreviewMarkdownHtml(source: BuildPreviewMarkdownHtmlOptions): string {
  const { content, path } = source
  const currentDir = path ? dirName(path) : ''
  const { isWideScreen: currentIsWideScreen } = useWideScreenLayout()
  const effectiveIsWideScreen = source.isWideScreen ?? currentIsWideScreen.value
  const imageTimestamp = source.imageTimestamp ?? Date.now()

  return renderMarkdownHtml(content, {
    // Trusted local artifact — matches the full MarkdownPreview pipeline
    // (sanitize: false) so it renders pixel-identically to the file viewer.
    sanitize: false,
    skipEnhancements: true,
    fixImagePaths: createFixLocalImagePaths({
      baseDir: currentDir,
      imageTimestamp,
      isWideScreen: effectiveIsWideScreen,
    }),
  })
}
