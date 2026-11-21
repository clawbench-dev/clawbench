/**
 * Shared avatar presentation policy.
 *
 * Every generated avatar gets a soft, solid background so a transparent
 * character style still reads as a tile instead of a floating face. The same
 * palette is used for:
 *
 *   - the background baked into a newly generated SVG (picker, group members,
 *     meeting participants), so the image travels with its colour into the
 *     public share page and exports;
 *   - the CSS fallback behind a stored avatar that predates this policy, so old
 *     avatars gain a background without being regenerated.
 *
 * The colour is a pure function of the seed, which keeps a person's avatar the
 * same colour across reloads, devices and both chat surfaces.
 */
import { fnv1a32 } from '@/utils/stableHash'

/**
 * Light, low-saturation colours. Deliberately light: the character styles draw
 * with black/dark line art, so a theme-adaptive dark background would swallow
 * them. A light tile works on both light and dark themes (it reads as a small
 * card), which also keeps the public share page identical to the app.
 */
export const AVATAR_BACKGROUNDS = [
  '#E3ECF7',
  '#E4F0E9',
  '#F0E8F5',
  '#F5EDE4',
  '#E9E9EC',
  '#E2EFF1',
] as const

/** Stable background colour for a seed (agent name, member name, nickname). */
export function avatarBackgroundFor(seed: string): string {
  const key = (seed || 'agent').normalize('NFC').trim().toLowerCase()
  return AVATAR_BACKGROUNDS[fnv1a32(key) % AVATAR_BACKGROUNDS.length]
}
