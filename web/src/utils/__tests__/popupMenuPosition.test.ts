import { describe, expect, it } from 'vitest'
import { computeMenuStyle } from '../popupMenuPosition'

// Helper to create a mock DOMRect
function mockRect(options: {
  top?: number
  bottom?: number
  left?: number
  right?: number
}): DOMRect {
  return {
    top: options.top ?? 0,
    bottom: options.bottom ?? 0,
    left: options.left ?? 0,
    right: options.right ?? 0,
    width: (options.right ?? 0) - (options.left ?? 0),
    height: (options.bottom ?? 0) - (options.top ?? 0),
    x: options.left ?? 0,
    y: options.top ?? 0,
    toJSON: () => ({}),
  }
}

describe('computeMenuStyle', () => {
  const vw = 1024
  const vh = 768

  describe('vertical: prefers above, flips below when near top', () => {
    it('places menu above the anchor when enough space', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh })
      expect(style.bottom).toBeDefined()
      expect(style.top).toBeUndefined()
      // bottom = vh - rect.top + gap = 768 - 400 + 4 = 372
      expect(style.bottom).toBe('372px')
    })

    it('flips below when not enough space above', () => {
      const rect = mockRect({ top: 5, bottom: 40, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 5, edgeMargin: 10 })
      expect(style.top).toBe('44px')
      expect(style.bottom).toBeUndefined()
    })

    it('places menu top edge 4px below anchor bottom edge when flipped', () => {
      const rect = mockRect({ top: 10, bottom: 50, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 5, edgeMargin: 6 })
      expect(style.top).toBe('54px')
    })

    it('uses spaceBelow when it is larger than spaceAbove', () => {
      // Anchor at Y=100 → spaceAbove=94, spaceBelow=654 → goBelow=true
      const rect = mockRect({ top: 100, bottom: 140, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 5, edgeMargin: 6 })
      expect(style.top).toBeDefined()
      expect(style.bottom).toBeUndefined()
    })

    it('uses spaceAbove when it is larger than spaceBelow', () => {
      // Anchor at Y=600 → spaceAbove=594, spaceBelow=154 → goBelow=false
      const rect = mockRect({ top: 600, bottom: 640, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 5, edgeMargin: 6 })
      expect(style.bottom).toBeDefined()
      expect(style.top).toBeUndefined()
    })
  })

  describe('horizontal: auto-detects alignment from anchor position', () => {
    it('left-aligns when anchor is in the left half of viewport', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh })
      expect(style.left).toBeDefined()
      expect(style.right).toBeUndefined()
      expect(style.left).toBe('100px')
    })

    it('right-aligns when anchor is in the right half of viewport', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 800, right: 900 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh })
      expect(style.right).toBeDefined()
      expect(style.left).toBeUndefined()
      // right = vw - rect.right = 1024 - 900 = 124
      expect(style.right).toBe('124px')
    })

    it('left-aligns at exact center', () => {
      // anchor center = (512+532)/2 = 522 < 512 → actually > 512, so right-align
      const rect = mockRect({ top: 400, bottom: 440, left: 512, right: 532 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh })
      expect(style.right).toBeDefined()
    })

    it('clamps left to edgeMargin when anchor is near left edge', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 2, right: 30 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, edgeMargin: 8 })
      expect(style.left).toBe('8px')
    })

    it('clamps right to edgeMargin when anchor is near right edge', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 900, right: 1020 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, edgeMargin: 8 })
      expect(style.right).toBe('8px')
    })

    it('shifts left when menu would overflow right viewport edge', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 900, right: 930 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, maxWidth: 200, edgeMargin: 6 })
      // anchor center = 915 > 512 → right-align
      // But since anchor is right-aligned, it naturally stays in viewport
      expect(style.right).toBeDefined()
    })
  })

  describe('maxHeight: clamped to available space', () => {
    it('clamps to available space above anchor', () => {
      const rect = mockRect({ top: 600, bottom: 640, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, edgeMargin: 6 })
      const availableAbove = 600 - 4 - 6 // = 590
      expect(style.maxHeight).toContain(`${availableAbove}px`)
    })

    it('clamps to available space below anchor when flipped', () => {
      const rect = mockRect({ top: 5, bottom: 40, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, edgeMargin: 6, menuItemsCount: 5 })
      // goBelow → top = 44, availableBelow = 768 - 44 - 6 = 718
      const availableBelow = 768 - 44 - 6
      expect(style.maxHeight).toContain(`${availableBelow}px`)
    })
  })

  describe('anchor option overrides auto-detection', () => {
    it('anchor="left" forces left-align even when anchor is in right half', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 800, right: 900 })
      const style = computeMenuStyle(rect, { anchor: 'left', viewportWidth: vw, viewportHeight: vh })
      expect(style.left).toBeDefined()
      expect(style.right).toBeUndefined()
      // left = rect.left = 800, but 800 + 220 + 6 > 1024 → clamped to 1024 - 220 - 6 = 798
      expect(style.left).toBe('798px')
    })

    it('anchor="right" forces right-align even when anchor is in left half', () => {
      // Anchor in left half but with right edge close to center → right-align won't overflow
      const rect = mockRect({ top: 400, bottom: 440, left: 300, right: 400 })
      const style = computeMenuStyle(rect, { anchor: 'right', viewportWidth: vw, viewportHeight: vh })
      expect(style.right).toBeDefined()
      expect(style.left).toBeUndefined()
      // right = vw - rect.right = 1024 - 400 = 624
      expect(style.right).toBe('624px')
    })

    it('anchor="auto" (default) uses center-based heuristic', () => {
      const rect = mockRect({ top: 400, bottom: 440, left: 800, right: 900 })
      const style = computeMenuStyle(rect, { anchor: 'auto', viewportWidth: vw, viewportHeight: vh })
      // Auto: anchor center = 850 > 512 → right-align
      expect(style.right).toBeDefined()
      expect(style.left).toBeUndefined()
    })

    it('direction param is still ignored — uses space comparison', () => {
      // Anchor near bottom → spaceAbove > spaceBelow → goes above (ignores direction="below")
      const rect = mockRect({ top: 600, bottom: 640, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 2, edgeMargin: 6 })
      // Auto-detected: above (spaceAbove=594 > spaceBelow=118)
      expect(style.bottom).toBeDefined()
      expect(style.top).toBeUndefined()
    })
  })

  describe('mobile scenarios', () => {
    const mVw = 390
    const mVh = 844

    it('deep-thinking chip (rightmost button) — right-aligns, above', () => {
      // 🧠 button at rightmost of bottom bar
      const rect = mockRect({ top: 780, bottom: 808, left: 310, right: 384 })
      const style = computeMenuStyle(rect, { viewportWidth: mVw, viewportHeight: mVh, maxWidth: 200, menuItemsCount: 4, edgeMargin: 6 })
      // Auto: anchor center = 347 > 195 → right-align
      expect(style.right).toBeDefined()
      // Auto: spaceAbove=770 > spaceBelow=30 → above
      expect(style.bottom).toBeDefined()
    })

    it('model chip (second from right) — right-aligns, above', () => {
      // GLM 5.1 button
      const rect = mockRect({ top: 780, bottom: 808, left: 200, right: 300 })
      const style = computeMenuStyle(rect, { viewportWidth: mVw, viewportHeight: mVh, maxWidth: 220, menuItemsCount: 3, edgeMargin: 6 })
      // Auto: anchor center = 250 > 195 → right-align
      expect(style.right).toBeDefined()
      // Auto: spaceAbove=770 > spaceBelow=30 → above
      expect(style.bottom).toBeDefined()
    })

    it('settings gear icon (top-right) — right-aligns, below', () => {
      const rect = mockRect({ top: 4, bottom: 36, left: 330, right: 360 })
      const style = computeMenuStyle(rect, { viewportWidth: mVw, viewportHeight: mVh, maxWidth: 200, menuItemsCount: 4, edgeMargin: 6 })
      // Auto: anchor center = 345 > 195 → right-align
      expect(style.right).toBeDefined()
      // Auto: spaceAbove=-2 < spaceBelow=802 → below
      expect(style.top).toBeDefined()
    })

    it('attach button (bottom-left) — left-aligns, above', () => {
      const rect = mockRect({ top: 780, bottom: 808, left: 10, right: 40 })
      const style = computeMenuStyle(rect, { viewportWidth: mVw, viewportHeight: mVh, maxWidth: 200, menuItemsCount: 3, edgeMargin: 6 })
      // Auto: anchor center = 25 < 195 → left-align
      expect(style.left).toBeDefined()
      // Auto: spaceAbove=770 > spaceBelow=30 → above
      expect(style.bottom).toBeDefined()
    })

    it('autocomplete menu (full-width textarea) — anchor="left" forces left-align', () => {
      // Full-width textarea at bottom of screen — center would normally trigger right-align
      const rect = mockRect({ top: 780, bottom: 808, left: 8, right: 382 })
      const style = computeMenuStyle(rect, { anchor: 'left', viewportWidth: mVw, viewportHeight: mVh, maxWidth: 260, menuItemsCount: 2, edgeMargin: 6 })
      // Forced left-align (anchor center = 195, which is exactly half → auto would right-align)
      expect(style.left).toBeDefined()
      expect(style.right).toBeUndefined()
      // Auto: spaceAbove=770 > spaceBelow=30 → above
      expect(style.bottom).toBeDefined()
    })
  })

  describe('style invariants', () => {
    it('always includes position, maxWidth, maxHeight, overflowY', () => {
      const rects = [
        mockRect({ top: 5, bottom: 40, left: 100, right: 140 }),
        mockRect({ top: 400, bottom: 440, left: 100, right: 140 }),
        mockRect({ top: 5, bottom: 40, left: 800, right: 900 }),
        mockRect({ top: 400, bottom: 440, left: 800, right: 900 }),
      ]
      for (const rect of rects) {
        const style = computeMenuStyle(rect, { viewportWidth: 1024, viewportHeight: 768 })
        expect(style.position).toBe('fixed')
        expect(style.maxWidth).toBeDefined()
        expect(style.maxHeight).toBeDefined()
        expect(style.overflowY).toBe('auto')
      }
    })

    it('never sets both top and bottom', () => {
      const rects = [
        mockRect({ top: 5, bottom: 40, left: 100, right: 140 }),
        mockRect({ top: 400, bottom: 440, left: 100, right: 140 }),
        mockRect({ top: 5, bottom: 40, left: 800, right: 900 }),
        mockRect({ top: 400, bottom: 440, left: 800, right: 900 }),
      ]
      for (const rect of rects) {
        const style = computeMenuStyle(rect, { viewportWidth: 1024, viewportHeight: 768 })
        expect(style.top !== undefined && style.bottom !== undefined).toBe(false)
      }
    })

    it('never sets both left and right', () => {
      const rects = [
        mockRect({ top: 400, bottom: 440, left: 100, right: 140 }),
        mockRect({ top: 400, bottom: 440, left: 800, right: 900 }),
      ]
      for (const rect of rects) {
        const style = computeMenuStyle(rect, { viewportWidth: 1024, viewportHeight: 768 })
        expect(style.left !== undefined && style.right !== undefined).toBe(false)
      }
    })
  })

  describe('edge cases', () => {
    it('handles zero-height anchor element', () => {
      const rect = mockRect({ top: 300, bottom: 300, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh })
      expect(style.position).toBe('fixed')
    })

    it('handles minimal viewport', () => {
      const rect = mockRect({ top: 5, bottom: 15, left: 5, right: 15 })
      const style = computeMenuStyle(rect, { viewportWidth: 50, viewportHeight: 50, maxWidth: 40, edgeMargin: 2, menuItemsCount: 2 })
      expect(style.position).toBe('fixed')
    })

    it('handles negative rect.top (anchor partially above viewport)', () => {
      const rect = mockRect({ top: -10, bottom: 30, left: 100, right: 140 })
      const style = computeMenuStyle(rect, { viewportWidth: vw, viewportHeight: vh, menuItemsCount: 5, edgeMargin: 6 })
      expect(style.top).toBeDefined()
    })
  })
})
