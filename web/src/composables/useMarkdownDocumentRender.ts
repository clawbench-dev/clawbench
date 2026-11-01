/**
 * Shared render lifecycle for a rendered-markdown surface.
 *
 * Both the file preview (MarkdownPreview.vue) and the meeting minutes
 * (MeetingMarkdown.vue) show a markdown document read-only. They must use the
 * SAME pipeline, or the same text renders differently on the two surfaces:
 * relative image paths, inline file-path chips, mermaid, and the existence
 * check behind those chips all have to agree.
 *
 * This composable owns everything after "I have markdown text":
 *   1. buildMarkdownPreviewDom()  → annotated HTML + detected paths
 *   2. nextTick                   → the v-html write has landed
 *   3. verifyFilePaths()          → stamp data-path-type on every chip
 *   4. renderMermaidInElement()   → diagrams
 *   5. stampSvgFigures()          → proportional SVG sizing
 *
 * The caller keeps its own watchers (what triggers a re-render differs: the
 * preview re-renders on file/language changes, the meeting on minutes load).
 * `onAfterRender` is where surface-specific work runs (the preview computes
 * diff-marker positions and announces a scroll realign).
 */
import { nextTick, ref } from 'vue'
import { buildMarkdownPreviewDom } from './useMarkdownRenderPipeline.ts'
import { useFilePathAnnotation } from './useFilePathAnnotation.ts'
import { renderMermaidInElement } from './useMarkdownRenderer.ts'
import { useWideScreenLayout } from './useWideScreenLayout.ts'
import { stampSvgFigures } from '@/utils/svgMediaFit.ts'
import { store } from '@/stores/app.ts'

export interface MarkdownDocumentRenderOptions {
  /** Project root used to resolve project-relative paths. Defaults to the store. */
  projectRoot?: string
  /** User home used for `~/` expansion. Defaults to the store. */
  homeDir?: string
  /**
   * Sanitize the rendered HTML. File previews are trusted local artifacts and
   * pass false (the default); GENERATED markdown (meeting minutes) must pass
   * true — its content can be influenced by meeting participants.
   */
  sanitize?: boolean
  /** Mermaid id prefix; keeps diagram ids unique across surfaces. */
  mermaidPrefix?: string
  /** Runs after the DOM has been rendered and verified. */
  onAfterRender?: (container: HTMLElement, detectedPaths: string[]) => void
}

export function useMarkdownDocumentRender() {
  const { verifyFilePaths } = useFilePathAnnotation()
  const html = ref('')
  const bodyRef = ref<HTMLElement | null>(null)
  /** Paths found in the document; the caller may use them for a resource list. */
  const detectedPaths = ref<string[]>([])
  const { isWideScreen } = useWideScreenLayout()

  /** Bumped per render so a slow verify/mermaid pass cannot overwrite a newer one. */
  let renderSeq = 0

  /**
   * Invalidate an in-flight render without starting a new one.
   *
   * The preview calls this when the file becomes an error or disappears: an
   * older render must not paint its HTML over the cleared surface.
   */
  function cancel(): void {
    renderSeq++
    html.value = ''
    detectedPaths.value = []
  }

  async function render(
    content: string,
    path: string,
    options: MarkdownDocumentRenderOptions = {},
  ): Promise<void> {
    const seq = ++renderSeq
    const projectRoot = options.projectRoot ?? store.state.projectRoot ?? ''
    const homeDir = options.homeDir ?? store.state.homeDir ?? ''

    if (!content) {
      html.value = ''
      detectedPaths.value = []
      return
    }

    const result = buildMarkdownPreviewDom(
      { content, path, projectRoot, homeDir },
      {
        isWideScreen: isWideScreen.value,
        imageTimestamp: Date.now(),
        sanitize: options.sanitize ?? false,
      },
    )
    if (seq !== renderSeq) return

    html.value = result.html
    detectedPaths.value = result.detectedPaths

    await nextTick()
    if (seq !== renderSeq) return
    const el = bodyRef.value
    if (!el) return

    if (result.detectedPaths.length > 0) {
      const unique = [...new Set(result.detectedPaths)]
      // Deliberately not awaited: verification stamps attributes asynchronously
      // and the document must not wait for it to become interactive.
      void verifyFilePaths(unique, el.querySelector('.markdown-content') || el)
    }

    const target = (el.querySelector('.markdown-content') as HTMLElement | null) || el
    await renderMermaidInElement(target, options.mermaidPrefix ?? 'md-preview')
    if (seq !== renderSeq) return

    stampSvgFigures(target)
    options.onAfterRender?.(el, result.detectedPaths)
  }

  return { html, bodyRef, detectedPaths, render, cancel }
}
