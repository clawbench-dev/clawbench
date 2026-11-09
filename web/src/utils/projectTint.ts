/**
 * Stable colour for a project, used by the "other projects" cards in the
 * session list (header band, icon, count badge, borders, hover tint).
 *
 * Ten hues, not the four status tokens this used to draw from: four colours
 * spread over a dozen projects repeat so often that the colour stops saying
 * anything. The palette is deliberately the same one the tool-call accents and
 * session tags use (emerald / cyan / blue / indigo / violet / pink / red /
 * orange / amber / olive) so the app has one colour language rather than three.
 *
 * Every entry carries a light and a dark value. The card paints its accent at
 * 15% behind the header and uses the bare colour for the icon, so both values
 * were checked against all 36 themes' real surface colours: every combination
 * clears 4.5:1 for the icon and 4.5:1 against its own 15% header band (WCAG
 * asks 3:1 for non-text UI). Re-check with the numbers in
 * `projectTint.test.ts` if you change a value.
 *
 * Assignment stays a hash of the project PATH — not the project's activity or
 * mode — so a project keeps its colour across reloads, devices and restarts.
 * Collisions are acceptable: the header also carries the project name.
 */
export interface ProjectTint {
  /** Value used on light themes. */
  light: string
  /** Value used on dark themes. */
  dark: string
}

export const PROJECT_TINTS: ProjectTint[] = [
  { light: '#096848', dark: '#34d399' }, // green
  { light: '#036475', dark: '#22d3ee' }, // teal
  { light: '#1a4fa8', dark: '#7db8ff' }, // blue
  { light: '#3b2a9e', dark: '#a5a6ff' }, // indigo
  { light: '#7b1fa2', dark: '#d8a8ff' }, // violet
  { light: '#ad125f', dark: '#f57dbc' }, // pink
  { light: '#9e1b1b', dark: '#ff8f8f' }, // red
  { light: '#994104', dark: '#fb923c' }, // orange
  { light: '#6b5c05', dark: '#e8cf5a' }, // yellow
  { light: '#3f6b0a', dark: '#a3e635' }, // lime
]

/**
 * FNV-1a (32-bit) over the path.
 *
 * Position-sensitive on purpose: a char-sum hash is commutative, so `/a/b` and
 * `/b/a` — two different projects — would land on the same colour. Case is
 * folded so the same project reached through differently-cased paths keeps one
 * colour on case-insensitive filesystems.
 */
function hashProjectPath(path: string): number {
  let hash = 0x811c9dc5
  const normalized = (path || '').toLowerCase()
  for (let i = 0; i < normalized.length; i++) {
    hash ^= normalized.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

/** The palette entry for a project path. Deterministic across sessions. */
export function projectTint(path: string): ProjectTint {
  return PROJECT_TINTS[hashProjectPath(path) % PROJECT_TINTS.length]
}

/**
 * Inline style object for a project card. Both values are published at once;
 * the card's CSS picks one with a `[data-theme-base]` attribute selector, so a
 * theme switch needs no JavaScript.
 */
export function projectTintStyle(path: string): Record<string, string> {
  const { light, dark } = projectTint(path)
  return {
    '--pc-light': light,
    '--pc-dark': dark,
  }
}
