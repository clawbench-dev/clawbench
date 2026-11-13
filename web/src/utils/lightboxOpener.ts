/**
 * A one-slot handle on the mounted Lightbox's `open()`.
 *
 * The external-link click layer (`utils/externalLinkClick.ts`) runs at document
 * level with no component context, so `provide`/`inject` cannot reach it: the
 * lightbox is provided by `App.vue` / `ShareView.vue` for their SUBTREES, and
 * that layer is not in one. Rather than have every host pass an opener down,
 * the Lightbox registers itself here on mount.
 *
 * Deliberately a separate module (not `utils/lightbox.ts`): that one is mocked
 * wholesale by the lightbox tests with only `extractImageName`, so an opener
 * exported from there would be `undefined` inside those tests.
 */

export type LightboxImageOpener = (url: string) => void

let opener: LightboxImageOpener | null = null

/**
 * Register (or with `null`, unregister) the mounted lightbox.
 *
 * There is only ever one: the app mounts a single `<Lightbox>` in `App.vue` and
 * the share SPA mounts its own in `ShareView.vue`. Last one wins.
 */
export function registerLightboxImageOpener(fn: LightboxImageOpener | null): void {
    opener = fn
}

/**
 * Show `url` in the lightbox. Returns false when no lightbox is mounted, so the
 * caller can fall back to its normal behaviour instead of swallowing the click.
 */
export function openImageInLightbox(url: string): boolean {
    if (!opener) return false
    opener(url)
    return true
}
