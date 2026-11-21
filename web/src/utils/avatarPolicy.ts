/**
 * The single source of truth for avatar presentation and generation policy.
 *
 * Everything that needs to know "which styles exist", "what they are called",
 * "which subset automatic avatars may use", "what colour sits behind them" or
 * "how a seed maps to a style" reads it from here. The DiceBear loader
 * (`lazyAvatar.ts`) only knows how to fetch and render; the picker, group
 * members, meeting participants and AgentIcon all share this policy.
 */
import { fnv1a32 } from '@/utils/stableHash'

/** Curated style set shown in the picker. Extending it requires a loader entry
 *  in `lazyAvatar.ts` (TypeScript enforces that map is complete). */
export const AVATAR_STYLES = [
  'avataaars',
  'avataaars-neutral',
  'big-ears',
  'big-smile',
  'blobs',
  'bottts',
  'bottts-neutral',
  'cameo',
  'clay',
  'critters',
  'disco',
  'dylan',
  'glyphs',
  'icons',
  'identicon',
  'landscape',
  'line-face',
  'loops',
  'micah',
  'miniavs',
  'moods',
  'patchwork',
  'personas',
  'pixel-art',
  'pixel-art-neutral',
  'planets',
  'rings',
  'shape-grid',
  'shapes',
  'slice',
  'sprouts',
  'squircles',
  'stack',
  'stripes',
  'thumbs',
  'toon-head',
  'voxel-art',
  'voxel-bot',
  'waves',
  'weave',
] as const

export type AvatarStyle = typeof AVATAR_STYLES[number]

export type AvatarGroup = 'people' | 'bots' | 'geometry' | 'pattern'

export interface AvatarStyleMeta {
  group: AvatarGroup
}

/** Coarse group for each style. Human labels live in the i18n locale files,
 *  keyed by the style id (see avatarStyleLabelKey). */
export const AVATAR_STYLE_META: Record<AvatarStyle, AvatarStyleMeta> = {
  avataaars: { group: 'people' },
  'avataaars-neutral': { group: 'people' },
  'big-ears': { group: 'people' },
  'big-smile': { group: 'people' },
  blobs: { group: 'geometry' },
  bottts: { group: 'bots' },
  'bottts-neutral': { group: 'bots' },
  cameo: { group: 'people' },
  clay: { group: 'geometry' },
  critters: { group: 'bots' },
  disco: { group: 'geometry' },
  dylan: { group: 'people' },
  glyphs: { group: 'geometry' },
  icons: { group: 'geometry' },
  identicon: { group: 'geometry' },
  landscape: { group: 'pattern' },
  'line-face': { group: 'people' },
  loops: { group: 'geometry' },
  micah: { group: 'people' },
  miniavs: { group: 'people' },
  moods: { group: 'people' },
  patchwork: { group: 'pattern' },
  personas: { group: 'people' },
  'pixel-art': { group: 'people' },
  'pixel-art-neutral': { group: 'people' },
  planets: { group: 'pattern' },
  rings: { group: 'geometry' },
  'shape-grid': { group: 'pattern' },
  shapes: { group: 'geometry' },
  slice: { group: 'geometry' },
  sprouts: { group: 'pattern' },
  squircles: { group: 'geometry' },
  stack: { group: 'geometry' },
  stripes: { group: 'pattern' },
  thumbs: { group: 'people' },
  'toon-head': { group: 'people' },
  'voxel-art': { group: 'geometry' },
  'voxel-bot': { group: 'bots' },
  waves: { group: 'pattern' },
  weave: { group: 'pattern' },
}

/** Groups in display order. Every style appears exactly once (tested). */
export const AVATAR_STYLE_GROUPS: Array<{ id: AvatarGroup; styles: AvatarStyle[] }> = (
  ['people', 'bots', 'geometry', 'pattern'] as AvatarGroup[]
).map((id) => ({
  id,
  styles: AVATAR_STYLES.filter((style) => AVATAR_STYLE_META[style].group === id),
}))

/** i18n key for a style's label. */
export function avatarStyleLabelKey(style: string): string {
  return `settings.items.avatarStyles.${style}`
}

/**
 * Styles an AUTOMATIC avatar may use (group members, meeting participants).
 *
 * Deliberately NOT all forty: `landscape` is a mountain range, `stripes` is a
 * texture, `shape-grid` is a pattern — none read as "a person" at 20dp.
 * Everything here is a character, a face or a robot, so a group mixing two of
 * them still looks like a set of avatars rather than a mistake.
 */
export const AUTO_AVATAR_STYLES: AvatarStyle[] = [
  'avataaars',
  'avataaars-neutral',
  'big-ears',
  'big-smile',
  'bottts',
  'bottts-neutral',
  'cameo',
  'clay',
  'critters',
  'dylan',
  'line-face',
  'micah',
  'miniavs',
  'moods',
  'personas',
  'pixel-art',
  'pixel-art-neutral',
  'thumbs',
  'toon-head',
  'voxel-art',
  'voxel-bot',
]

/** Stable style for an automatic avatar. Pure function of the seed. */
export function autoAvatarStyle(seed: string): AvatarStyle {
  const key = (seed || 'agent').normalize('NFC').trim().toLowerCase()
  return AUTO_AVATAR_STYLES[fnv1a32(key) % AUTO_AVATAR_STYLES.length]
}

/**
 * Light, low-saturation background colours. Deliberately light: character
 * styles draw with black/dark line art, so a theme-adaptive dark background
 * would swallow them. A light tile works on both light and dark themes and
 * keeps the public share page identical to the app.
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

/** What the picker emits when the user saves (or clears) an avatar. */
export interface AvatarSaveSpec {
  avatar: string
  style: string
  seed: string
}
