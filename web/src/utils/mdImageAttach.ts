/**
 * "Attach image to chat" affordance for rendered markdown views.
 *
 * The file-preview pipeline lifts every image into a block-level
 * `.image-block-wrapper` figure whose header carries a paperclip
 * `.image-block-attach-btn` (view / attach / open — uniform across mobile and
 * PC; see createFixLocalImagePaths in useMarkdownRenderPipeline). Tapping it
 * attaches the image file to the chat as a reference attachment — mirroring
 * the desktop drag-out (`mdImageDrag.ts`) and the file-manager / file-header
 * attach actions.
 *
 * The button stops propagation so the click never reaches the lightbox's
 * document listener; the image body itself still opens the lightbox.
 */

/** Selector of the injected attach button in the image block header. */
export const MD_IMAGE_ATTACH_BADGE = '.image-block-attach-btn'

export interface MdImageBadgeHit {
  /** Decoded project-relative file path from the wrapped img's data-attach-src. */
  path: string
  /** Wrapper element; its center is used as the fly-animation origin. */
  wrap: HTMLElement
}

/** Badge click / tap handling injected by callers (from useChatContext + useToast). */
export interface MdImageAttachActions {
  add: (path: string) => void
  remove: (path: string) => void
  has: (path: string) => boolean
  toast: (msg: string, opts?: { icon?: string; type?: 'success' | 'error' | 'info'; duration?: number }) => void
  /** i18n message keys resolved by the caller. */
  messages: { added: string; removed: string }
}

/**
 * Resolve a click/tap target to an attach-hit for a local image.
 * Returns null when the target is not the attach button, the image figure is
 * missing, or the wrapped image carries no data-attach-src (external / data:
 * images). The image itself sits inside `.image-block-wrapper > .lightbox-img-wrap`.
 */
export function resolveMdImageBadgeClick(e: Event): MdImageBadgeHit | null {
  const target = e.target as HTMLElement | null
  if (!target || !target.closest(MD_IMAGE_ATTACH_BADGE)) return null
  const wrap = target.closest<HTMLElement>('.image-block-wrapper')
  const img = wrap?.querySelector<HTMLImageElement>('img.lightbox-img')
  const path = img?.getAttribute('data-attach-src')
  if (!wrap || !path) return null
  return { path, wrap }
}

/**
 * Toggle the image attachment and fire the chat fly-to-dock particle.
 * Returns true when the badge handled the event (caller should stopPropagation).
 */
export function handleMdImageAttachClick(
  e: Event,
  actions: MdImageAttachActions
): boolean {
  const hit = resolveMdImageBadgeClick(e)
  if (!hit) return false

  e.preventDefault()
  e.stopPropagation()

  if (actions.has(hit.path)) {
    actions.remove(hit.path)
    actions.toast(actions.messages.removed, { icon: '📎', type: 'info', duration: 1500 })
  } else {
    actions.add(hit.path)
    actions.toast(actions.messages.added, { icon: '📎', type: 'success', duration: 1500 })
  }

  // Fly-to-chat particle from the badge center (App listens; silent when the
  // mobile chat dock is not on screen, e.g. wide-screen layout).
  const rect = hit.wrap.getBoundingClientRect()
  const from = rect.width > 0 && rect.height > 0
    ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }
    : { x: rect.left, y: rect.top }
  const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
  const to = dockChatBtn?.getBoundingClientRect()
  if (to) {
    window.dispatchEvent(new CustomEvent('attach-to-chat', {
      detail: {
        from,
        to: { x: to.left + to.width / 2, y: to.top + to.height / 2 },
      },
    }))
  }

  return true
}
