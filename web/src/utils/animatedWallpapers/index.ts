/**
 * Registry of animated wallpaper styles.
 *
 * Adding a style is three steps and touches nothing else:
 *   1. create `./<id>.ts` exporting an `AnimatedStyle`
 *   2. import it and add it to `ANIMATED_STYLES` below
 *   3. add its `labelKey` + each param's `labelKey` to `en.ts` / `zh.ts`
 *
 * The settings panel renders the style picker and every slider from `params`, so
 * a new style needs no UI work. The renderer (`AnimatedWallpaper.vue`) dispatches
 * on `id`, so it needs no change either.
 */

import { silkStyle } from './silk'
import { xmbStyle } from './xmb'
import type { AnimatedStyle, ParamValue } from './types'

export type { AnimatedStyle, FrameContext, ParamSpec, ParamValue, SliderSpec, SwitchSpec } from './types'

/** Display order in the settings picker. */
export const ANIMATED_STYLES: readonly AnimatedStyle[] = [xmbStyle, silkStyle]

/**
 * Style used when nothing is stored, or when a stored id no longer exists.
 *
 * The original wave, so an existing device (whose stored value predates the
 * picker) keeps showing exactly what it showed before.
 */
export const DEFAULT_ANIMATED_STYLE = 'xmb'

/** Look up a style by id, falling back to the default for unknown ids. */
export function getAnimatedStyle(id: string | null | undefined): AnimatedStyle {
  const found = ANIMATED_STYLES.find((s) => s.id === id)
  if (found) return found
  // A stored id can outlive the style (removed, or hand-edited storage), so an
  // unknown value must degrade to the default rather than render nothing.
  return ANIMATED_STYLES.find((s) => s.id === DEFAULT_ANIMATED_STYLE) ?? ANIMATED_STYLES[0]
}

/** True when `id` names a registered style. */
export function isKnownAnimatedStyle(id: string | null | undefined): boolean {
  return ANIMATED_STYLES.some((s) => s.id === id)
}

/**
 * Fill in a style's parameter bag: stored values win, everything else falls back
 * to the spec default.
 *
 * Out-of-range and wrong-typed stored values are replaced by the default rather
 * than clamped — a clamped value would silently look "adjusted" when it is
 * really the result of a corrupt entry, and the sliders' own `min`/`max` already
 * prevent the user from producing one.
 */
export function resolveStyleParams(
  style: AnimatedStyle,
  stored: Record<string, ParamValue> | undefined,
): Record<string, ParamValue> {
  const out: Record<string, ParamValue> = {}
  for (const spec of style.params) {
    const raw = stored?.[spec.key]
    if (spec.kind === 'switch') {
      out[spec.key] = typeof raw === 'boolean' ? raw : spec.defaultValue
    } else {
      const ok = typeof raw === 'number' && Number.isFinite(raw) && raw >= spec.min && raw <= spec.max
      out[spec.key] = ok ? (raw as number) : spec.defaultValue
    }
  }
  return out
}
