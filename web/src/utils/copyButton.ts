/**
 * Shared toolkit for copy buttons — the parts that must work OUTSIDE Vue.
 *
 * Every copy affordance in the app gives the same feedback: the glyph swaps to
 * a check for a moment (see `components/common/CopyButton.vue` for the
 * declarative path). Three code paths cannot use that component because they
 * build markup by hand:
 *
 *   - `composables/useCodeBlockHeader.ts` — annotates markdown HTML strings
 *   - `utils/renderToolDetail.ts`         — renders tool-detail HTML strings
 *   - `utils/exportMarkdownHtml.ts`       — emits a standalone .html document
 *
 * They all need the same glyphs, the same timing and the same class name, so
 * those live here once. Anything that CAN use the component should — this
 * module exists for the leftovers, not as the primary API.
 *
 * The glyph paths mirror the installed lucide release (v1.0.0 `copy` /
 * `check`). If lucide is upgraded, update them here too or the injected
 * buttons will drift from the ones the component renders.
 */
import { gt } from '@/composables/useLocale'
import { copyText } from '@/utils/clipboard'

/** How long the check stays up before reverting to the copy glyph. */
export const COPY_FEEDBACK_MS = 1500

/** Lucide `copy`, as an inline SVG string. */
function copyIconSvg(size = 14): string {
    return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`
}

/** Lucide `check`, as an inline SVG string. */
function checkIconSvg(size = 14): string {
    return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>`
}

export const COPY_ICON_SVG = copyIconSvg()
export const CHECK_ICON_SVG = checkIconSvg()

/**
 * Copy `text` from a hand-built button and flash its feedback.
 *
 * The single entry point for the imperative path. It owns the ordering that is
 * easy to get wrong when the two halves are written out by hand:
 *
 *   1. the re-entrancy guard runs BEFORE the clipboard write, so a second click
 *      while the check is showing neither re-copies nor re-schedules the revert
 *      (the revert would otherwise capture the "copied" label as the idle one
 *      and leave the button permanently stuck on it)
 *   2. empty text is not a copy — no write, no feedback
 *
 * The button must be icon-only and its idle glyph must be `COPY_ICON_SVG` —
 * that is what gets restored. The title/aria-label are captured on entry and
 * restored verbatim, so callers do not need to know the idle wording.
 */
export function copyWithFlash(btn: HTMLElement, text: string, duration = COPY_FEEDBACK_MS): void {
    if (btn.classList.contains('is-copied')) return
    if (!text) return
    copyText(text)

    const prevTitle = btn.getAttribute('title')
    const prevAriaLabel = btn.getAttribute('aria-label')

    btn.innerHTML = CHECK_ICON_SVG
    btn.classList.add('is-copied')
    btn.setAttribute('title', gt('common.copied'))
    btn.setAttribute('aria-label', gt('common.copied'))

    setTimeout(() => {
        btn.innerHTML = COPY_ICON_SVG
        btn.classList.remove('is-copied')
        if (prevTitle === null) btn.removeAttribute('title')
        else btn.setAttribute('title', prevTitle)
        if (prevAriaLabel === null) btn.removeAttribute('aria-label')
        else btn.setAttribute('aria-label', prevAriaLabel)
    }, duration)
}
