/**
 * "Open source file" affordance for a rendered markdown image.
 *
 * The file-preview image header (`.image-block-open-btn`, data-action="open",
 * injected by createFixLocalImagePaths) opens the LOCAL image's source file in
 * the file viewer/manager via openFilePath. Only meaningful for images that
 * carry data-attach-src (the decoded project-relative path); external/data:
 * images have no local file and never get this button.
 */

/** Selector of the image-header "open file" button. */
export const MD_IMAGE_OPEN_BTN = '.image-block-open-btn'

export interface ImageOpenHit {
  /** Decoded project-relative file path from the wrapped img's data-attach-src. */
  path: string
}

/**
 * Resolve a click target to the open-file hit for a local image.
 * Returns null when not on the open button, or the image has no data-attach-src.
 */
export function resolveMdImageOpenClick(e: Event): ImageOpenHit | null {
  const target = e.target as HTMLElement | null
  if (!target || !target.closest(MD_IMAGE_OPEN_BTN)) return null
  const wrap = target.closest<HTMLElement>('.image-block-wrapper')
  const img = wrap?.querySelector<HTMLImageElement>('img.lightbox-img')
  const path = img?.getAttribute('data-attach-src')
  if (!path) return null
  return { path }
}

/**
 * Open the source image file. Stops propagation so the click doesn't also reach
 * the lightbox's document listener or other delegated handlers.
 * Returns true when the button handled the event.
 */
export function handleMdImageOpenClick(
  e: Event,
  open: (path: string) => void | Promise<unknown>
): boolean {
  const hit = resolveMdImageOpenClick(e)
  if (!hit) return false

  e.preventDefault()
  e.stopPropagation()

  open(hit.path)
  return true
}
