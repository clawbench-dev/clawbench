/**
 * Annotation for PLAIN-TEXT surfaces — the meeting transcript and the
 * proofread transcript.
 *
 * Those rows are not markdown: one utterance per line, no paragraph breaks and
 * no links. They still name files and URLs constantly ("打开 web/src/main.ts",
 * "see github.com/foo/bar"), and those references must behave exactly like the
 * ones in the minutes: an inline preview card, an open button, an external
 * link that asks before leaving the app.
 *
 * This is deliberately NOT a second detector. It runs the two passes the
 * markdown preview runs, in the same order and for the same reason (a bare
 * domain would otherwise be read as the tail of a local path):
 *
 *   bare URL → file paths
 *
 * The only thing it adds is the escape step: transcript text is plain text, so
 * it must be HTML-escaped BEFORE annotation, or a `<` in an utterance would be
 * parsed as markup. Detection results are returned rather than verified here —
 * verification is a network round-trip and belongs to the host that knows when
 * the HTML has actually landed in the DOM (same contract as `annotateToolHtml`
 * and `renderMarkdown`).
 */

import { escapeHtml } from '@/utils/html.ts'
import { annotateBareUrlsIn } from './useUrlAnnotation.ts'
import { annotateFilePathsIn } from './useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import { isShareMode } from '@/share/shareMode'

export interface PlainTextAnnotation {
  /** Escaped, annotated HTML — safe for `v-html`. */
  html: string
  /** Project-relative (or absolute, for external) paths found in the text. */
  detectedPaths: string[]
}

export interface PlainTextAnnotationOptions {
  projectRoot?: string
  homeDir?: string
  /** Base directory for relative paths (defaults to the project root). */
  baseDir?: string
}

/**
 * Annotate one run of plain text.
 *
 * The share page is anonymous: the file viewer and `/api/file/batch-exists`
 * are auth-protected, so a path chip there could only fail on click. File
 * paths are therefore left as text, while URLs stay clickable (the share SPA
 * installs the same external-link click layer as the app).
 */
export function annotatePlainText(
  text: string,
  options: PlainTextAnnotationOptions = {},
): PlainTextAnnotation {
  if (!text) return { html: '', detectedPaths: [] }

  const doc = new DOMParser().parseFromString(escapeHtml(text), 'text/html')
  annotateBareUrlsIn(doc)

  const detectedPaths = isShareMode()
    ? []
    : annotateFilePathsIn(doc, {
        projectRoot: options.projectRoot ?? store.state.projectRoot ?? '',
        homeDir: options.homeDir ?? store.state.homeDir ?? '',
        baseDir: options.baseDir,
      })

  return { html: doc.body.innerHTML, detectedPaths }
}
