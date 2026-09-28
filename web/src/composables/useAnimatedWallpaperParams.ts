/**
 * Per-device, per-style parameters for the animated wallpapers.
 *
 * Stored OUTSIDE `localConfig` on purpose. That pipeline is typed
 * `string | boolean | number | null` and its legacy-migration branch coerces by
 * scalar type — an object written through it would round-trip as
 * `"[object Object]"`. Storing a nested object in its own module is the pattern
 * already used by `useRecentFiles` / `useServerList`.
 *
 * Shape: `{ [styleId]: { [paramKey]: value } }`. Keying by style id means
 * switching styles preserves each one's tuning, and a style's params can never
 * leak into another's.
 *
 * Reading is forgiving: unknown styles, unknown keys, wrong types and
 * out-of-range numbers all fall back to the registry's defaults (see
 * `resolveStyleParams`), so a hand-edited or stale localStorage entry degrades
 * to the shipped look instead of a blank or broken wallpaper.
 */

import { reactive, computed } from 'vue'
import { appLog } from '@/utils/appLog'
import {
  ANIMATED_STYLES,
  getAnimatedStyle,
  isKnownAnimatedStyle,
  resolveStyleParams,
} from '@/utils/animatedWallpapers'
import type { ParamValue } from '@/utils/animatedWallpapers'

const TAG = 'AnimatedWallpaperParams'
const STORAGE_KEY = 'clawbench-animated-wallpaper-params'

/** The raw persisted bag, exactly as stored. */
type StoredBag = Record<string, Record<string, ParamValue>>

function readStorage(): StoredBag {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    // A non-object payload (array, scalar, null) is treated as absent rather
    // than spreading junk into the reactive bag.
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    return parsed as StoredBag
  } catch (e) {
    // Corrupt JSON must not break startup: the defaults are a perfectly good
    // wallpaper, and throwing here would take the whole settings panel down.
    appLog.w(TAG, 'failed to read stored params, falling back to defaults:', e)
    return {}
  }
}

const stored = reactive<StoredBag>(readStorage())

function persist(): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))
  } catch (e) {
    appLog.w(TAG, 'failed to persist params:', e)
  }
}

/** Drop keys the registry no longer declares, and non-object style bags. */
function pruneUnknown(): void {
  const known = new Set(ANIMATED_STYLES.map((s) => s.id))
  let changed = false
  for (const id of Object.keys(stored)) {
    if (!known.has(id)) {
      delete stored[id]
      changed = true
      continue
    }
    const bag = stored[id]
    if (!bag || typeof bag !== 'object' || Array.isArray(bag)) {
      delete stored[id]
      changed = true
      continue
    }
    const valid = new Set(getAnimatedStyle(id).params.map((p) => p.key))
    for (const key of Object.keys(bag)) {
      if (!valid.has(key)) {
        delete bag[key]
        changed = true
      }
    }
  }
  if (changed) persist()
}

pruneUnknown()

/**
 * Resolved params for one style: stored values where valid, spec defaults
 * otherwise. Always complete — a style's `draw` can read any declared key.
 */
export function getAnimatedStyleParams(styleId: string): Record<string, ParamValue> {
  return resolveStyleParams(getAnimatedStyle(styleId), stored[styleId])
}

/** Write one param, clamped to the spec's range. */
export function setAnimatedStyleParam(styleId: string, key: string, value: ParamValue): void {
  // Validate the id STRICTLY (not via getAnimatedStyle, which falls back to the
  // default for unknown ids): that fallback would make an unknown style resolve
  // to the default's params and happily persist a bogus style bag.
  if (!isKnownAnimatedStyle(styleId)) return

  const spec = getAnimatedStyle(styleId).params.find((p) => p.key === key)
  // Ignore writes for a key the registry does not declare: persisting them would
  // leave orphan entries that `pruneUnknown` then has to clean up.
  if (!spec) return

  let next: ParamValue
  if (spec.kind === 'switch') {
    next = value === true
  } else if (typeof value === 'number' && Number.isFinite(value)) {
    next = Math.min(spec.max, Math.max(spec.min, value))
  } else {
    return
  }

  if (!stored[styleId]) stored[styleId] = {}
  stored[styleId][key] = next
  persist()
}

/** Clear one style's overrides, returning it to the shipped defaults. */
export function resetAnimatedStyleParams(styleId: string): void {
  delete stored[styleId]
  persist()
}

/** Clear every style's overrides. */
export function resetAllAnimatedStyleParams(): void {
  for (const id of Object.keys(stored)) delete stored[id]
  persist()
}

/**
 * Reactive params for the active style.
 *
 * Reading through a computed (rather than returning `stored[id]` directly) means
 * a write to any key of that style notifies the renderer, and a style switch
 * yields a fresh object so the canvas redraws.
 */
export function useAnimatedWallpaperParams(styleId: () => string) {
  return computed(() => getAnimatedStyleParams(styleId()))
}

/** Test seam: drop everything held in memory (the storage key is untouched). */
export function __resetAnimatedWallpaperParamsCacheForTest(): void {
  for (const id of Object.keys(stored)) delete stored[id]
}
