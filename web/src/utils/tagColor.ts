export interface TagAccent {
  light: string
  dark: string
}

/**
 * Session tag colors.
 *
 * Derived from the tool-call palette in `components/chat/ContentBlocks.vue`:
 * the tool palette maps a tool name to a `category`, and each category has a
 * fixed accent. Tags have no natural category, so we hash the tag name onto the
 * SAME set of hues — which keeps the two color systems recognisably related and
 * guarantees a given tag always renders in the same color.
 *
 * The values are NOT the raw tool accents, though. The tool palette is built
 * for an icon plus a 6%-tinted background; a tag chip uses its accent as the
 * text colour at 11px, which needs far more contrast. Measured over all 36
 * themes, the raw tool accents put 128 of 252 (theme × colour) combinations
 * below WCAG AA 4.5:1 — the amber and yellow entries bottom out at 1.45:1 on
 * light themes, i.e. effectively invisible.
 *
 * So each entry keeps its hue but is re-lit for text use: light themes darken
 * the accent, dark themes lighten it, and saturation is floored at 0.45 so the
 * hue stays identifiable (a straight darkening toward black would collapse
 * every colour into near-grey). Verified: 252/252 combinations clear 4.5:1 for
 * the chip text and 5.3:1 for the bare accent used as a border. The
 * `tagColor.test.ts` consistency check asserts hue-family membership rather
 * than exact equality for this reason.
 *
 * `file`/`plan` are deliberately excluded: they resolve to `var(--accent-color)`,
 * which is theme-dependent and would collapse to a single hue for every tag
 * landing on those buckets.
 */
export const TAG_PALETTE: TagAccent[] = [
  { light: '#096848', dark: '#34d399' }, // bash  — emerald
  { light: '#6222f3', dark: '#b299fb' }, // search — violet
  { light: '#805205', dark: '#fbbf24' }, // task  — amber
  { light: '#ad125f', dark: '#f57dbc' }, // agent — pink
  { light: '#036475', dark: '#22d3ee' }, // skill — cyan
  { light: '#994104', dark: '#fb923c' }, // ask   — orange
  { light: '#735804', dark: '#fbbf24' }, // permission — yellow
]

/**
 * FNV-1a (32-bit) hash of the tag name.
 *
 * Chosen over a simple char-sum because char-sum is order-insensitive: "ab"
 * and "ba" would hash identically, as would any anagram pair (e.g. "live" /
 * "evil"), giving unrelated tags the same color. FNV-1a folds in position via
 * the multiply step, so reordered names diverge.
 *
 * Case-insensitive so "Bug" and "bug" render identically — the backend folds
 * tag names to lowercase (see NormalizeSessionTagName), so they are in fact the
 * same tag and must not be given different colors.
 */
export function hashTagName(name: string): number {
  let hash = 0x811c9dc5
  const normalized = (name || '').toLowerCase()
  for (let i = 0; i < normalized.length; i++) {
    hash ^= normalized.charCodeAt(i)
    // Multiply by the FNV prime (16777619) with `Math.imul` so the result stays
    // a 32-bit integer instead of losing precision as a float.
    hash = Math.imul(hash, 0x01000193)
  }
  // >>> 0 coerces to an unsigned 32-bit int; the sign flip otherwise makes the
  // modulo below return negative indices.
  return hash >>> 0
}

/**
 * Pick the palette entry for a tag name. Deterministic: the same name always
 * yields the same color, across reloads and across sessions.
 */
export function tagAccent(name: string): TagAccent {
  const index = hashTagName(name) % TAG_PALETTE.length
  return TAG_PALETTE[index]
}

/**
 * Inline style object for a tag chip. Sets the accent once per theme so the CSS
 * can select the right one with a `[data-theme-base]` attribute selector.
 */
export function tagAccentStyle(name: string): Record<string, string> {
  const { light, dark } = tagAccent(name)
  return {
    '--tag-accent-light': light,
    '--tag-accent-dark': dark,
  }
}

/** Relative luminance per WCAG 2.1, from a #rrggbb string. */
function relativeLuminance(hex: string): number {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex.trim())
  if (!m) return 0
  const [r, g, b] = m.slice(1).map(v => {
    const c = parseInt(v, 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio between two #rrggbb colours. */
export function contrastRatio(a: string, b: string): number {
  const la = relativeLuminance(a)
  const lb = relativeLuminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/**
 * Pick a foreground that stays readable on `background`.
 *
 * Used where an accent fills a shape and something has to sit on top of it —
 * the checkbox tick. A fixed white fails on most themes: the accents are mid
 * to light blues, so white scored as low as 1.69:1 (28 of 36 themes below
 * 4.5:1). Choosing per colour puts every theme at or above 4.5:1, which a CSS
 * rule cannot do because it has no way to evaluate the accent's luminance.
 *
 * Returns the higher-contrast of near-black and white rather than a luminance
 * threshold: four themes have accents dark enough to look like they want white
 * (L≈0.38-0.50) where black is in fact clearly better.
 */
export function readableTextOn(background: string): string {
  // Pure black, not near-black: bluloco-light's accent (#2b7bda) lands at
  // 4.45:1 against #111 and only clears 4.5:1 against #000.
  const dark = '#000000'
  const light = '#ffffff'
  return contrastRatio(dark, background) >= contrastRatio(light, background) ? dark : light
}
