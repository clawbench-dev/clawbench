/**
 * Encode a raw SVG string as an <img>-safe data URI.
 *
 * Kept in its own zero-dependency module (NOT in `lazyAvatar.ts`) so that
 * `AgentIcon.vue` — which lives in the entry bundle — can use it without
 * pulling the whole DiceBear loader (and its 40 dynamic style imports) into the
 * eager dependency graph. `lazyAvatar.ts` imports it from here too, so both the
 * picker previews and AgentIcon encode identically.
 *
 * `encodeURIComponent` is load-bearing: it encodes `#` so same-document
 * `fill="url(#id)"` references survive inside the data URI.
 */
export function svgToDataUri(svg: string): string {
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
}
