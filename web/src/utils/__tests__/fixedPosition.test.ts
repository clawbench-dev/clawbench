import { describe, expect, it, afterEach } from 'vitest'
import {
  measureFixedContainingBlock,
  toFixedLeft,
  toFixedTop,
  toFixedRight,
  toFixedBottom,
  containingBlockWidthCss,
  placeFixedAt,
} from '../fixedPosition'

/**
 * jsdom has no layout engine, so every rect is 0x0. These tests therefore
 * simulate a containing block: a fixed child's rect = origin + CSS offset ×
 * scale. That is exactly the geometry the module has to invert.
 */
function makeEl(opts: {
  originLeft?: number
  originTop?: number
  width?: number
  height?: number
  scale?: number
  boxWidth?: number
  boxHeight?: number
  cbWidth?: number
  cbHeight?: number
}) {
  const originLeft = opts.originLeft ?? 0
  const originTop = opts.originTop ?? 0
  const scale = opts.scale ?? 1
  const boxWidth = opts.boxWidth ?? 140
  const boxHeight = opts.boxHeight ?? 92

  const cbWidth = opts.cbWidth ?? 700
  const cbHeight = opts.cbHeight ?? 600

  // The measurement probes a CLONE of the element (see fixedPosition.ts), so
  // the simulated geometry has to be installed on every node of this family —
  // `cloneNode` does not copy an own-property override.
  const installRect = (el: HTMLElement) => {
    el.getBoundingClientRect = () => {
    // Mirrors CSS: `left/top` position the box from the containing block's
    // origin; `right/bottom` position it from the block's far edges.
    const hasRight = el.style.right !== '' && el.style.right != null
    const hasBottom = el.style.bottom !== '' && el.style.bottom != null
    const hasLeft = el.style.left !== '' && el.style.left != null
    const hasTop = el.style.top !== '' && el.style.top != null

    // CSS: with BOTH `left` and `right` set the box is stretched between them
    // (then clamped by max-width). With only one of the pair it keeps its own
    // size. Modelling this is what makes the max-width regression observable.
    const maxW = parseFloat(el.style.maxWidth || '') || Infinity
    const maxH = parseFloat(el.style.maxHeight || '') || Infinity

    let width = boxWidth
    if (hasLeft && hasRight) {
      const stretch = cbWidth - (parseFloat(el.style.left) || 0) - (parseFloat(el.style.right) || 0)
      width = Math.max(0, Math.min(stretch, maxW))
    } else {
      width = Math.min(width, maxW)
    }

    let height = boxHeight
    if (hasTop && hasBottom) {
      const stretch = cbHeight - (parseFloat(el.style.top) || 0) - (parseFloat(el.style.bottom) || 0)
      height = Math.max(0, Math.min(stretch, maxH))
    } else {
      height = Math.min(height, maxH)
    }

    const left = hasRight
      ? originLeft + cbWidth - (parseFloat(el.style.right) || 0) * scale - width * scale
      : originLeft + (parseFloat(el.style.left) || 0) * scale
    const top = hasBottom
      ? originTop + cbHeight - (parseFloat(el.style.bottom) || 0) * scale - height * scale
      : originTop + (parseFloat(el.style.top) || 0) * scale
    return {
      top,
      bottom: top + height * scale,
      left,
      right: left + width * scale,
      width: width * scale,
      height: height * scale,
      x: left,
      y: top,
      toJSON: () => ({}),
    } as DOMRect
    }
  }

  const el = document.createElement('div')
  installRect(el)
  // The clone must keep measuring the same simulated block: it is appended to
  // the same parent and reuses the identical geometry function.
  const nativeClone = el.cloneNode.bind(el)
  el.cloneNode = ((deep?: boolean) => {
    const copy = nativeClone(deep) as HTMLElement
    installRect(copy)
    copy.cloneNode = el.cloneNode
    return copy
  }) as typeof el.cloneNode
  return el
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('measureFixedContainingBlock', () => {
  it('reports the viewport origin and scale 1 when nothing transforms', () => {
    const el = makeEl({})
    const cb = measureFixedContainingBlock(el)
    expect(cb.left).toBe(0)
    expect(cb.top).toBe(0)
    expect(cb.scaleX).toBe(1)
    expect(cb.scaleY).toBe(1)
    // Far edges land at the viewport's own far edges when nothing transforms.
    expect(cb.right).toBe(700)
    expect(cb.bottom).toBe(600)
  })

  it('reports the transformed ancestor as origin — this is the app-mode bug', () => {
    // .chat-message with will-change:transform sits 60px down / 120px right;
    // a naive `top: rect.bottom` would therefore render 60px too low.
    const el = makeEl({ originLeft: 120, originTop: 60 })
    const cb = measureFixedContainingBlock(el)
    expect(cb.left).toBe(120)
    expect(cb.top).toBe(60)
  })

  it('reports the CSS-zoom scale', () => {
    const el = makeEl({ scale: 1.25 })
    const cb = measureFixedContainingBlock(el)
    expect(cb.scaleX).toBeCloseTo(1.25, 5)
    expect(cb.scaleY).toBeCloseTo(1.25, 5)
    // Extent is reported in CSS px, not rect px: the simulated block is 700
    // RECT px wide, which is 700/1.25 = 560 CSS px — the same number the old
    // toFixedCSS(viewportWidth) path produced, so the two agree.
    expect(containingBlockWidthCss(cb)).toBeCloseTo(560, 5)
  })

  it('restores the inline left/top it used for probing', () => {
    const el = makeEl({})
    el.style.left = '17px'
    el.style.top = '23px'
    measureFixedContainingBlock(el)
    expect(el.style.left).toBe('17px')
    expect(el.style.top).toBe('23px')
  })

  it('does not leave the live element offsets changed after measuring', () => {
    // The probe runs on a clone; a regression that writes to the real element
    // would show up here as a moved box (and as an unclickable menu in a browser).
    const el = makeEl({})
    el.style.left = '90px'
    el.style.bottom = '62.4px'
    const before = el.getBoundingClientRect()
    measureFixedContainingBlock(el)
    const after = el.getBoundingClientRect()
    expect(after.left).toBe(before.left)
    expect(after.top).toBe(before.top)
  })
})

describe('viewport → CSS conversions', () => {
  const cb = { left: 120, top: 60, right: 820, bottom: 660, scaleX: 1.25, scaleY: 1.25 }

  it('inverts origin and scale for left/top', () => {
    expect(toFixedLeft(220, cb)).toBeCloseTo(80, 5)   // (220-120)/1.25
    expect(toFixedTop(160, cb)).toBeCloseTo(80, 5)    // (160-60)/1.25
  })

  it('measures right/bottom from the containing block edges, not the viewport', () => {
    // cb spans x 120..820, y 60..660.
    expect(toFixedRight(820, cb)).toBeCloseTo(0, 5)
    expect(toFixedRight(770, cb)).toBeCloseTo(40, 5)  // (820-770)/1.25
    expect(toFixedBottom(660, cb)).toBeCloseTo(0, 5)
    expect(toFixedBottom(610, cb)).toBeCloseTo(40, 5)
  })
})

describe('placeFixedAt', () => {
  it('lands the element at the requested viewport position (no transform)', () => {
    const el = makeEl({})
    placeFixedAt(el, 200, 300)
    const r = el.getBoundingClientRect()
    expect(Math.round(r.left)).toBe(200)
    expect(Math.round(r.top)).toBe(300)
  })

  it('lands at the requested position despite a transformed ancestor', () => {
    const el = makeEl({ originLeft: 120, originTop: 60 })
    placeFixedAt(el, 400, 328)
    const r = el.getBoundingClientRect()
    expect(Math.round(r.left)).toBe(400)
    expect(Math.round(r.top)).toBe(328)
  })

  it('lands at the requested position under CSS zoom', () => {
    const el = makeEl({ scale: 1.25 })
    placeFixedAt(el, 400, 328)
    const r = el.getBoundingClientRect()
    expect(Math.round(r.left)).toBe(400)
    expect(Math.round(r.top)).toBe(328)
  })

  it('lands at the requested position with transform AND zoom combined', () => {
    const el = makeEl({ originLeft: 120, originTop: 60, scale: 1.25 })
    placeFixedAt(el, 400, 328)
    const r = el.getBoundingClientRect()
    expect(Math.round(r.left)).toBe(400)
    expect(Math.round(r.top)).toBe(328)
  })

  it('clears right/bottom, which are relative to the containing block', () => {
    const el = makeEl({})
    el.style.right = '10px'
    el.style.bottom = '20px'
    placeFixedAt(el, 100, 100)
    expect(el.style.right).toBe('')
    expect(el.style.bottom).toBe('')
  })
})
