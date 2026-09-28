import { describe, it, expect, beforeEach, vi } from 'vitest'
import { ref } from 'vue'
import {
  __resetAnimatedWallpaperParamsCacheForTest,
  getAnimatedStyleParams,
  resetAllAnimatedStyleParams,
  resetAnimatedStyleParams,
  setAnimatedStyleParam,
  useAnimatedWallpaperParams,
} from '../useAnimatedWallpaperParams'
import { getAnimatedStyle } from '@/utils/animatedWallpapers'

/**
 * The animated-wallpaper param store.
 *
 * This deliberately lives outside `localConfig` (which is a scalar pipeline), so
 * the interesting behaviour is the forgiving read path: a corrupt or stale
 * localStorage entry must degrade to the shipped defaults rather than a broken
 * wallpaper.
 */
const KEY = 'clawbench-animated-wallpaper-params'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

describe('useAnimatedWallpaperParams', () => {
  beforeEach(() => {
    localStorage.clear()
    __resetAnimatedWallpaperParamsCacheForTest()
    vi.clearAllMocks()
  })

  describe('defaults', () => {
    it('returns every declared param at its default when nothing is stored', () => {
      for (const style of [getAnimatedStyle('xmb'), getAnimatedStyle('silk')]) {
        const out = getAnimatedStyleParams(style.id)
        for (const spec of style.params) {
          expect(out[spec.key], `${style.id}.${spec.key}`).toBe(spec.defaultValue)
        }
      }
    })

    it('does not leak one style params into another', () => {
      setAnimatedStyleParam('silk', 'thick', 200)
      expect(getAnimatedStyleParams('silk').thick).toBe(200)
      // xmb has no `thick`; it must not appear, and its own params stay default.
      expect(getAnimatedStyleParams('xmb')).not.toHaveProperty('thick')
      expect(getAnimatedStyleParams('xmb').lam).toBe(100)
    })
  })

  describe('setAnimatedStyleParam', () => {
    it('stores and reads back a value', () => {
      setAnimatedStyleParam('xmb', 'lam', 150)
      expect(getAnimatedStyleParams('xmb').lam).toBe(150)
    })

    it('persists to localStorage', () => {
      setAnimatedStyleParam('xmb', 'lam', 150)
      const raw = JSON.parse(localStorage.getItem(KEY)!)
      expect(raw.xmb.lam).toBe(150)
    })

    it('clamps to the spec range', () => {
      const lam = getAnimatedStyle('xmb').params.find((p) => p.key === 'lam') as { min: number; max: number }
      setAnimatedStyleParam('xmb', 'lam', 99999)
      expect(getAnimatedStyleParams('xmb').lam).toBe(lam.max)
      setAnimatedStyleParam('xmb', 'lam', -99999)
      expect(getAnimatedStyleParams('xmb').lam).toBe(lam.min)
    })

    it('stores a switch as a boolean', () => {
      setAnimatedStyleParam('xmb', 'fadeEdges', false)
      expect(getAnimatedStyleParams('xmb').fadeEdges).toBe(false)
    })

    it('ignores a non-finite number', () => {
      setAnimatedStyleParam('xmb', 'lam', Number.NaN)
      expect(getAnimatedStyleParams('xmb').lam).toBe(100)
      setAnimatedStyleParam('xmb', 'lam', Number.POSITIVE_INFINITY)
      expect(getAnimatedStyleParams('xmb').lam).toBe(100)
    })

    it('ignores an undeclared key', () => {
      setAnimatedStyleParam('xmb', 'ghost', 5)
      expect(getAnimatedStyleParams('xmb')).not.toHaveProperty('ghost')
      expect(JSON.parse(localStorage.getItem(KEY) ?? '{}').xmb?.ghost).toBeUndefined()
    })

    it('ignores an undeclared style', () => {
      setAnimatedStyleParam('nope', 'lam', 5)
      expect(localStorage.getItem(KEY)).toBeNull()
    })
  })

  describe('reset', () => {
    it('clears only the requested style', () => {
      setAnimatedStyleParam('xmb', 'lam', 150)
      setAnimatedStyleParam('silk', 'thick', 200)

      resetAnimatedStyleParams('xmb')

      expect(getAnimatedStyleParams('xmb').lam).toBe(100)
      expect(getAnimatedStyleParams('silk').thick).toBe(200)
    })

    it('clears every style', () => {
      setAnimatedStyleParam('xmb', 'lam', 150)
      setAnimatedStyleParam('silk', 'thick', 200)

      resetAllAnimatedStyleParams()

      expect(getAnimatedStyleParams('xmb').lam).toBe(100)
      expect(getAnimatedStyleParams('silk').thick).toBe(100)
    })
  })

  describe('forgiving reads at load', () => {
    /**
     * The store reads localStorage once, at module load. Testing the read path
     * therefore requires re-importing the module with the storage pre-seeded —
     * otherwise the assertions only exercise the already-parsed in-memory bag and
     * pass no matter what the read path does.
     */
    async function loadWith(payload: string | null) {
      vi.resetModules()
      localStorage.clear()
      if (payload !== null) localStorage.setItem(KEY, payload)
      return import('../useAnimatedWallpaperParams')
    }

    it('falls back to defaults for corrupt JSON', async () => {
      const mod = await loadWith('{not json')
      expect(mod.getAnimatedStyleParams('xmb').lam).toBe(100)
    })

    it('rejects a non-object payload', async () => {
      for (const payload of [JSON.stringify([1, 2, 3]), JSON.stringify('nope'), JSON.stringify(null), JSON.stringify(42)]) {
        const mod = await loadWith(payload)
        expect(mod.getAnimatedStyleParams('xmb').lam, `payload ${payload}`).toBe(100)
      }
    })

    it('replaces an out-of-range stored value with the default', async () => {
      // Written straight to storage, bypassing the clamping setter — this is what
      // a hand-edited or version-skewed entry looks like. Out-of-range falls back
      // to the DEFAULT rather than being clamped: a clamped value would look
      // "adjusted" when it is really the result of a corrupt entry.
      const mod = await loadWith(JSON.stringify({ xmb: { lam: 99999 } }))
      expect(mod.getAnimatedStyleParams('xmb').lam).toBe(100)
    })

    it('rejects a wrong-typed stored value', async () => {
      const mod = await loadWith(JSON.stringify({ xmb: { lam: 'fast', fadeEdges: 1 } }))
      expect(mod.getAnimatedStyleParams('xmb').lam).toBe(100)
      expect(mod.getAnimatedStyleParams('xmb').fadeEdges).toBe(true)
    })

    it('drops an unknown style and an unknown key, and persists the cleanup', async () => {
      const mod = await loadWith(JSON.stringify({ ghost: { a: 1 }, xmb: { lam: 150, nope: 5 } }))
      const bag = JSON.parse(localStorage.getItem(KEY) ?? '{}')
      expect(bag).not.toHaveProperty('ghost')
      expect(bag.xmb).not.toHaveProperty('nope')
      // The legitimate value survives the prune.
      expect(mod.getAnimatedStyleParams('xmb').lam).toBe(150)
    })

    it('keeps a valid stored value', async () => {
      const mod = await loadWith(JSON.stringify({ silk: { thick: 180 } }))
      expect(mod.getAnimatedStyleParams('silk').thick).toBe(180)
    })
  })

  describe('useAnimatedWallpaperParams', () => {
    it('tracks the active style reactively', () => {
      const active = useAnimatedWallpaperParams(() => 'xmb')
      expect(active.value.lam).toBe(100)
      setAnimatedStyleParam('xmb', 'lam', 175)
      expect(active.value.lam).toBe(175)
    })

    it('yields the other style params after a switch', () => {
      // A real ref, so the computed actually re-evaluates on change.
      const id = ref('xmb')
      const active = useAnimatedWallpaperParams(() => id.value)
      // `band` is xmb-only, `thick` is silk-only, `lam` exists on both (with
      // different ranges) — so the style-specific keys are the discriminator.
      expect(active.value).toHaveProperty('band')
      expect(active.value).not.toHaveProperty('thick')

      id.value = 'silk'
      expect(active.value).toHaveProperty('thick')
      expect(active.value).not.toHaveProperty('band')
    })

    it('reflects a switch to an unknown style as the default style params', () => {
      // An unknown id must still yield a usable bag (the registry falls back), or
      // the canvas would receive an empty param object.
      const id = ref('nope')
      const active = useAnimatedWallpaperParams(() => id.value)
      expect(active.value.lam).toBe(100)
    })
  })
})
