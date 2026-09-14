/**
 * Session tag colors.
 *
 * Mirrors the tool-call color logic in `components/chat/ContentBlocks.vue`:
 * there, a tool name maps to a fixed `category`, and each category has a fixed
 * accent (`[data-category] { --tool-accent: ... }`). User tags have no natural
 * category, so we hash the tag name onto the SAME palette — which keeps the two
 * color systems visually consistent and guarantees a given tag always renders
 * in the same color.
 *
 * The light/dark pair is returned as an inline CSS custom property pair
 * (`--tag-accent-light` / `--tag-accent-dark`) plus a `--tag-accent` that
 * follows the active theme, because the palette values are raw hex (not
 * theme tokens) and the tag row lives outside `.content-blocks`, so it cannot
 * inherit the `[data-theme-base="dark"]` overrides there.
 */

export interface TagAccent {
  light: string
  dark: string
}

/**
 * The tool-call palette, copied from ContentBlocks.vue. Keep these in sync with
 * the `[data-category]` rules there — `tagColor.test.ts` asserts the values so
 * a drift is caught by the test suite rather than by eye.
 *
 * `file`/`plan` are deliberately excluded: they resolve to `var(--accent-color)`,
 * which is theme-dependent and would collapse to a single hue for every tag
 * landing on those buckets.
 */
export const TAG_PALETTE: TagAccent[] = [
  { light: '#10b981', dark: '#34d399' }, // bash  — emerald
  { light: '#8b5cf6', dark: '#a78bfa' }, // search — violet
  { light: '#f59e0b', dark: '#fbbf24' }, // task  — amber
  { light: '#ec4899', dark: '#f472b6' }, // agent — pink
  { light: '#06b6d4', dark: '#22d3ee' }, // skill — cyan
  { light: '#f97316', dark: '#fb923c' }, // ask   — orange
  { light: '#eab308', dark: '#fbbf24' }, // permission — yellow
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
