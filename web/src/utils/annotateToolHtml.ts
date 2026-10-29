/**
 * Annotation pass for TOOL CALL input/output HTML.
 *
 * Chat prose already goes through the full annotation pipeline (bare URL →
 * worktree → file path → commit hash → localhost) inside the markdown
 * renderer, so a path the AI mentions in a sentence is clickable. Tool call
 * details did not: a Bash command, a Grep result, an `ls` listing or the JSON
 * of an unregistered tool all rendered as dead text, even though those are
 * exactly where file paths, forwarded ports and commit ids show up most.
 *
 * This module runs the same five annotators over the tool's own HTML. Order is
 * the pipeline's, and it is load-bearing:
 *
 *   bare URL → worktree → file path → commit hash → localhost
 *
 * Bare URLs must come first or the file-path annotator reads the tail of
 * `github.com/foo/bar` as a local path; worktree before file paths or a
 * worktree directory is claimed as a relative path.
 *
 * Regions that must NOT be annotated are marked by their producer with
 * `data-no-annotate` (see utils/annotationScope.ts) — an already-annotated file
 * header, an ask card's option labels, a file's own source body.
 *
 * Detection results are returned rather than verified here: verification is a
 * network round-trip and belongs to the host that knows when the HTML has
 * actually landed in the DOM (same contract as `renderMarkdown`).
 */

import { annotateBareUrlsIn } from '@/composables/useUrlAnnotation.ts'
import { annotateWorktreePathsIn } from '@/composables/useWorktreeAnnotation.ts'
import { annotateFilePathsIn } from '@/composables/useFilePathAnnotation.ts'
import { annotateCommitHashesIn } from '@/composables/useCommitHashAnnotation.ts'
import { annotateLocalhostUrlsIn } from '@/composables/useLocalhostAnnotation.ts'
import { store } from '@/stores/app.ts'

export interface ToolHtmlAnnotation {
  html: string
  /** Project-relative (or absolute, for external) paths found in the HTML. */
  detectedPaths: string[]
  /** Commit-hash candidates; the backend decides which are real commits. */
  detectedSHAs: string[]
}

/**
 * Annotate a fragment of tool-detail HTML.
 *
 * Returns the input untouched when there is nothing to do, so callers that pass
 * an empty string pay nothing.
 */
export function annotateToolHtml(html: string): ToolHtmlAnnotation {
  if (!html) return { html, detectedPaths: [], detectedSHAs: [] }

  const doc = new DOMParser().parseFromString(html, 'text/html')
  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''

  const urlsAnnotated = annotateBareUrlsIn(doc)
  const worktree = annotateWorktreePathsIn(doc, { projectRoot })
  const detectedPaths = annotateFilePathsIn(doc, { projectRoot, homeDir })
  const detectedSHAs = annotateCommitHashesIn(doc)
  const localhostAnnotated = annotateLocalhostUrlsIn(doc)

  // Serializing the parsed document normalizes attribute escaping (a `&quot;`
  // in text becomes a literal `"`), so returning it unconditionally would
  // rewrite fragments that had nothing to annotate. Renderers produce that
  // escaping deliberately, and their tests pin it — so hand back the ORIGINAL
  // string whenever this pass changed nothing.
  const changed = urlsAnnotated || worktree.applied || localhostAnnotated
    || detectedPaths.length > 0 || detectedSHAs.length > 0
  if (!changed) return { html, detectedPaths: [], detectedSHAs: [] }

  return { html: doc.body.innerHTML, detectedPaths, detectedSHAs }
}
