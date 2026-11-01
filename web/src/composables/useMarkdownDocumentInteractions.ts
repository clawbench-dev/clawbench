/**
 * Shared click/drag behaviour for a rendered-markdown surface.
 *
 * The file preview (MarkdownPreview.vue) and the meeting minutes
 * (MeetingMarkdown.vue) must behave identically when the user interacts with
 * rendered markdown: file-path chips, the adjacent open button, directory
 * chips, relative links, in-page anchors, images, mermaid diagrams, code/table
 * headers, table row expansion, and the hover preview.
 *
 * Everything surface-specific is injected:
 *   - `source` is the NavigationSurface recorded as the return target. A file
 *     preview passes 'file' (links push the file stack); an embedded document
 *     passes its own surface ('live') so Back returns to that panel.
 *   - `preHandlers` run before the shared chain (share-mode links, diff
 *     markers) — returning true means "handled, stop".
 *   - `onBlockDoubleClick` replaces the touch double-tap action. The preview
 *     opens its quote bar; the meeting copies the block.
 *   - `onBeforeOpen` banks surface state before a navigation (the preview
 *     captures its scroll position).
 *
 * The returned `codeLinkPreview` must be rendered by the caller as
 * `<CodeLinkPreview :preview="codeLinkPreview" />` — the floating card is a
 * component, not something this composable can mount.
 */
import type { Ref } from 'vue'
import { useCodeLinkPreview, handleVerifiedFilePathClick } from './useCodeLinkPreview.ts'
import { useDoubleClickCopy } from './useDoubleClickCopy.ts'
import { useTableRowExpand } from './useTableRowExpand.ts'
import { useChatContext } from './useChatContext.ts'
import { useToast } from './useToast.ts'
import { gt } from './useLocale.ts'
import { useFilePathAnnotation } from './useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from './useCodeBlockHeader.ts'
import { onMdImageDragStart, onMdImageDragEnd } from '@/utils/mdImageDrag.ts'
import { onMermaidDragStart, onMermaidDragEnd } from '@/utils/mdMermaidDrag.ts'
import { handleMdImageAttachClick, type MdImageAttachActions } from '@/utils/mdImageAttach.ts'
import { handleMermaidAttachClick, type MermaidAttachActions } from '@/utils/mdMermaidAttach.ts'
import { handleBlockAttachClick } from '@/utils/mdBlockAttach.ts'
import { requestAttachmentTarget } from './useConversationTarget.ts'
import { handleMdImageOpenClick } from '@/utils/mdImageOpen.ts'
import { dirName } from '@/utils/path.ts'
import { flashElement } from '@/utils/domFlash.ts'
import type { NavigationSurface } from './useNavigationContext.ts'

export interface MarkdownDocumentInteractionOptions {
  /** The rendered document element (the v-html container's parent). */
  containerRef: Ref<HTMLElement | null>
  /** Current document path; relative links resolve against its directory. */
  filePath: () => string
  /** Surface recorded as the navigation origin when a file is opened. */
  source: NavigationSurface
  /** Runs before an open (the preview banks its scroll position). */
  onBeforeOpen?: () => void
  /** Touch double-tap action. Omitted = the shared copy-to-clipboard default. */
  onBlockDoubleClick?: (target: EventTarget | null, text: string) => void
  /** Surface-specific interceptors, run before the shared chain. */
  preHandlers?: Array<(event: MouseEvent) => boolean>
  /** Whether commit-hash chips navigate to git history. Defaults to true. */
  enableCommitLinks?: boolean
}

export function useMarkdownDocumentInteractions(options: MarkdownDocumentInteractionOptions) {
  // Read the primitives through the composable (not as module-level named
  // imports) so tests can mock one module and both surfaces stay injectable.
  const { openFilePath, parseFileUri, resolveRelativePath, readLineTargetFromEl } = useFilePathAnnotation()
  const { removeAttachedFileByPath, hasAttachedFile } = useChatContext()
  const { show: showToast } = useToast()

  const mdImageAttachActions: MdImageAttachActions = {
    add: (path) => {
      // A comment on the document being read belongs to the conversation the
      // user is working in (issue #560); the picker only appears when there is
      // no active session at all.
      requestAttachmentTarget({ path, isDir: false }, { deliverTo: 'active' })
    },
    remove: removeAttachedFileByPath,
    has: hasAttachedFile,
    toast: (msg, opts) => showToast(msg, opts),
    messages: {
      added: gt('chat.attach.addedToChat'),
      removed: gt('chat.attach.removedFromChat'),
    },
  }

  const mermaidAttachActions: MermaidAttachActions = {
    add: (path, startLine, endLine) => {
      requestAttachmentTarget({ path, isDir: false, startLine, endLine }, { deliverTo: 'active' })
    },
    remove: (path, startLine, endLine) => removeAttachedFileByPath(path, startLine, endLine),
    has: (path, startLine, endLine) => hasAttachedFile(path, startLine, endLine),
    toast: (msg, opts) => showToast(msg, opts),
    messages: {
      added: gt('chat.attach.addedToChat'),
      removed: gt('chat.attach.removedFromChat'),
    },
  }

  const codeLinkPreview = useCodeLinkPreview({
    containerRef: options.containerRef,
    source: options.source,
    onBeforeOpen: options.onBeforeOpen,
  })

  const {
    tableRowModal,
    closeTableRowModal,
    tableRowPrev,
    tableRowNext,
    handleTableRowClick,
    onTableMouseDown,
    onTableTouchStart,
  } = useTableRowExpand()

  const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy: options.onBlockDoubleClick,
  })

  /** Delegated dragstart: images first, then rendered mermaid diagrams. */
  function onDragStart(e: DragEvent): void {
    onMdImageDragStart(e)
    onMermaidDragStart(e)
  }

  function onDragEnd(e: DragEvent): void {
    onMdImageDragEnd(e)
    onMermaidDragEnd(e)
  }

  function openFile(path: string, lineStart?: number, lineEnd?: number, lineRanges?: string): void {
    options.onBeforeOpen?.()
    codeLinkPreview.close()
    if (lineRanges) openFilePath(path, lineStart, lineEnd, options.source, lineRanges)
    else openFilePath(path, lineStart, lineEnd, options.source)
  }

  function handleClick(event: MouseEvent): void {
    for (const handler of options.preHandlers ?? []) {
      if (handler(event)) return
    }

    // Touch image attach badge — first in the chain so its stopPropagation
    // prevents the click from reaching the image/lightbox handlers below.
    if (handleMdImageAttachClick(event, mdImageAttachActions)) return

    // Image header "open file" button — opens the source image in the viewer.
    if (handleMdImageOpenClick(event, (path) => openFilePath(path, undefined, undefined, options.source))) return

    // Touch mermaid range-reference badge (same rationale).
    if (handleMermaidAttachClick(event, mermaidAttachActions)) return

    // Code/table header attach-to-chat buttons (md line range). Runs before the
    // copy/wrap header handlers so the paperclip never reaches them.
    if (handleBlockAttachClick(event, mermaidAttachActions)) return

    // Code block header buttons (copy/wrap)
    if (handleCodeBlockClick(event)) return

    // Table block header buttons (copy/wrap)
    if (handleTableBlockClick(event)) return

    const target = event.target as HTMLElement | null

    // Table row click — open the row-form modal.
    if (handleTableRowClick(event)) return

    // Verified *file* paths are intercepted for the hover preview; directories
    // and paths that have not been verified fall through to the handlers below.
    if (handleVerifiedFilePathClick(event, codeLinkPreview)) return

    // Commit-hash chip → git history.
    if (options.enableCommitLinks !== false) {
      const commitEl = target?.closest('.chat-commit-hash, .chat-commit-open-btn')
      if (commitEl) {
        event.preventDefault()
        event.stopPropagation()
        const sha = commitEl.getAttribute('data-commit-sha')
        if (sha) {
          options.onBeforeOpen?.()
          window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
        }
        return
      }
    }

    // File-open button or directory path text.
    const btn = target?.closest<HTMLElement>('.chat-file-open-btn[data-file-path]')
    const dirEl = target?.closest<HTMLElement>('.chat-file-path[data-file-path][data-path-type="dir"]')
    const linkOrBtn = btn || dirEl
    if (linkOrBtn) {
      event.preventDefault()
      event.stopPropagation()
      const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(linkOrBtn)
      if (filePath) openFile(filePath, lineStart, lineEnd, lineRanges)
      return
    }

    // In-page anchor links.
    const linkEl = target?.closest('a[href^="#"]')
    if (linkEl) {
      const href = linkEl.getAttribute('href') || ''
      if (href.length > 1) {
        const targetId = decodeURIComponent(href.slice(1))
        const targetEl = options.containerRef.value?.querySelector(`#${CSS.escape(targetId)}`)
        if (targetEl) {
          event.preventDefault()
          event.stopPropagation()
          targetEl.scrollIntoView({ behavior: 'auto', block: 'start' })
          flashElement(targetEl)
          return
        }
      }
    }

    // Relative links (and touch double-tap copy / quote).
    handleDblClick(event, (href, lineStart, lineEnd, lineRanges) => {
      event.stopPropagation()
      const anchor = target?.closest<HTMLAnchorElement>('a[href]')
      const annotatedPath = anchor?.getAttribute('data-file-path')
      const currentDir = options.filePath() ? dirName(options.filePath()) : ''
      // Prefer the annotated resolved path; for a file:// link take its path
      // directly; otherwise resolve the relative href against the document dir.
      const resolvedPath = annotatedPath
        || (href.startsWith('file://') ? parseFileUri(href).path : resolveRelativePath(href, currentDir))
      openFile(resolvedPath, lineStart, lineEnd, lineRanges)
    })
  }

  return {
    handleClick,
    onDragStart,
    onDragEnd,
    onTableMouseDown,
    onTableTouchStart,
    tableRowModal,
    closeTableRowModal,
    tableRowPrev,
    tableRowNext,
    codeLinkPreview,
  }
}
