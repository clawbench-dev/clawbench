/**
 * Proportional fill-width sizing for SVG media in rendered markdown.
 *
 * Rendered markdown shows SVG media in two forms:
 *   - an image reference:  `![alt](chart.svg)`  → `<img src="…chart.svg">`
 *   - a bare inline block: `<svg viewBox="…">…</svg>`
 *
 * Both are lifted into the unified `.image-block-wrapper` figure by the
 * media-block factory. By default a figure is `width: fit-content`, so an SVG
 * renders at its intrinsic size and leaves the reading column mostly empty.
 *
 * This module makes SVG figures GROW to fill the column while never exceeding
 * a height cap (60dvh, the same cap raster images already carry). Because a
 * plain `width:100%; max-height:60dvh` on a replaced element would STRETCH the
 * media (the browser clamps the height without shrinking the width), the
 * figure's width is instead driven by the media's aspect ratio:
 *
 *   width = min(100%, 60dvh × aspect-ratio)   →   height ≤ 60dvh, ratio kept
 *
 * The ratio is derived here (not in CSS, which cannot read it) and handed to
 * the stylesheet as the `--svg-ar` custom property plus the `.svg-fit` marker
 * class. A figure that never gets stamped keeps the previous shrink-only
 * behavior, so unknown / non-SVG media is unaffected.
 *
 * Sizing is only ever applied to figures in the FILE-PREVIEW family (file
 * preview, code-link card, share SPA, HTML export). The chat pipeline never
 * stamps, so chat bubble thumbnails keep their own caps.
 */

/** Marker class consumed by media-block.css to opt a figure into fill-width. */
export const SVG_FIT_CLASS = 'svg-fit'

/** Custom property carrying the media aspect ratio (width ÷ height). */
export const SVG_AR_PROP = '--svg-ar'

/**
 * `.svg` file reference, wherever it appears in a URL: a plain path (`a.svg`),
 * a cache-busted path (`a.svg?t=1`), a hash (`a.svg#id`), or an encoded query
 * parameter (`?path=%2Ftmp%2Fa.svg&t=1`, the share-mode / absolute-path form).
 * The lookahead keeps a longer extension (`a.svgz`) from matching.
 */
const SVG_SRC_RE = /\.svg(?![A-Za-z0-9])/i

/** Inline SVG served as a data URI (the HTML export inlines images). */
const SVG_DATA_RE = /^data:image\/svg\+xml/i

/** True when an image `src` points at an SVG, in any of the served URL forms. */
export function isSvgImageSrc(src: string | null | undefined): boolean {
    if (!src) return false
    return SVG_SRC_RE.test(src) || SVG_DATA_RE.test(src)
}

/**
 * Parse a plain CSS length (`200`, `200px`) into a positive number. Percentage
 * and unit-bearing values are rejected: they cannot express an aspect ratio.
 */
function parseLengthAttr(value: string | null): number | null {
    if (!value) return null
    const m = value.trim().match(/^(\d+(?:\.\d+)?)(?:px)?$/i)
    if (!m) return null
    const n = parseFloat(m[1])
    return Number.isFinite(n) && n > 0 ? n : null
}

/**
 * Aspect ratio (width ÷ height) of an SVG media element, or null when the
 * element is not SVG media or its ratio cannot be determined yet.
 *
 * - inline `<svg>`: `width`/`height` attributes win; otherwise `viewBox`
 *   (this needs no load, so inline blocks are sized on the first pass);
 * - `<img>` pointing at an SVG: the browser-derived intrinsic size, which is
 *   only available once the image has loaded (null until then — the figure
 *   simply keeps its previous size and is re-stamped on the load event).
 */
export function svgMediaAspectRatio(el: Element | null): number | null {
    if (!el) return null
    const tag = el.tagName.toLowerCase()

    if (tag === 'svg') {
        const w = parseLengthAttr(el.getAttribute('width'))
        const h = parseLengthAttr(el.getAttribute('height'))
        if (w !== null && h !== null) return w / h

        const vb = (el.getAttribute('viewBox') || '').trim().split(/[\s,]+/)
        if (vb.length === 4) {
            const vw = parseFloat(vb[2])
            const vh = parseFloat(vb[3])
            if (Number.isFinite(vw) && Number.isFinite(vh) && vw > 0 && vh > 0) return vw / vh
        }
        return null
    }

    if (tag === 'img') {
        const img = el as HTMLImageElement
        if (!isSvgImageSrc(img.getAttribute('src'))) return null
        if (img.naturalWidth > 0 && img.naturalHeight > 0) {
            return img.naturalWidth / img.naturalHeight
        }
        return null
    }

    return null
}

/**
 * The figure's CONTENT cell media — deliberately `:scope >`-anchored so the
 * toolbar's own icon SVGs (inside `.image-block-header`) are never mistaken for
 * content media.
 *
 * Three shapes are covered:
 *   - `.lightbox-img-wrap > img`   an `<img>` (an SVG file reference)
 *   - `.lightbox-svg-wrap > svg`   a bare inline `<svg>` (marked `lightbox-svg`)
 *   - `.mermaid > svg`             a rendered Mermaid diagram
 * A Mermaid diagram renders through its own path (mermaid.ts → armMermaidFigure)
 * and its `<svg>` carries no `lightbox-svg` marker, so it needs its own entry.
 */
const CONTENT_MEDIA_SELECTOR =
    ':scope > .lightbox-svg-wrap > svg.lightbox-svg, ' +
    ':scope > .lightbox-img-wrap > img, ' +
    ':scope > .mermaid > svg'

/**
 * Stamp one `.image-block-wrapper` with its media's aspect ratio. Returns true
 * when the figure is now (or already was) sized; false when the media is not
 * SVG or its ratio is not yet known.
 */
export function stampSvgFigure(wrapper: Element): boolean {
    const media = wrapper.querySelector(CONTENT_MEDIA_SELECTOR)
    if (!media) return false
    const ar = svgMediaAspectRatio(media)
    if (ar === null || !Number.isFinite(ar) || ar <= 0) return false
    ;(wrapper as HTMLElement).style.setProperty(SVG_AR_PROP, String(ar))
    wrapper.classList.add(SVG_FIT_CLASS)
    return true
}

/**
 * Stamp every figure under `root` whose ratio can be resolved synchronously.
 * Idempotent — safe to call after each render and on every media load event.
 * Returns the number of figures sized by this pass.
 */
export function stampSvgFigures(root: ParentNode | null): number {
    if (!root) return 0
    let stamped = 0
    for (const wrapper of Array.from(root.querySelectorAll('.image-block-wrapper'))) {
        if (stampSvgFigure(wrapper)) stamped++
    }
    return stamped
}

/**
 * Await the load of any not-yet-decoded SVG image under `root`, then stamp.
 *
 * The HTML exporter needs this: it mounts the rendered document into a hidden
 * host, where `<img>` elements have not loaded, so their intrinsic size is not
 * available to the synchronous pass.
 */
export async function stampSvgFiguresAsync(root: ParentNode | null): Promise<void> {
    if (!root) return
    const pending: Array<Promise<unknown>> = []
    for (const el of Array.from(root.querySelectorAll('.image-block-wrapper > .lightbox-img-wrap > img'))) {
        const img = el as HTMLImageElement
        if (!isSvgImageSrc(img.getAttribute('src'))) continue
        if (img.naturalWidth > 0 && img.naturalHeight > 0) continue
        if (typeof img.decode === 'function') pending.push(img.decode().catch(() => undefined))
    }
    if (pending.length > 0) await Promise.all(pending)
    stampSvgFigures(root)
}
