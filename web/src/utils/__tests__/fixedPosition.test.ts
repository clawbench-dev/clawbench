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

  const el = document.createElement('div')
  el.getBoundingClientRect = () => {
    // Mirrors CSS: `left/top` position the box from the containing block's
    // origin; `right/bottom` position it from the block's far edges.
    const hasRight = el.style.right !== '' && el.style.right != null
    const hasBottom = el.style.bottom !== '' && el.style.bottom != null
    const left = hasRight
      ? originLeft + cbWidth - (parseFloat(el.style.right) || 0) * scale - boxWidth * scale
      : originLeft + (parseFloat(el.style.left) || 0) * scale
    const top = hasBottom
      ? originTop + cbHeight - (parseFloat(el.style.bottom) || 0) * scale - boxHeight * scale
      : originTop + (parseFloat(el.style.top) || 0) * scale
    return {
      top,
      bottom: top + boxHeight * scale,
      left,
      right: left + boxWidth * scale,
      width: boxWidth * scale,
      height: boxHeight * scale,
      x: left,
      y: top,
      toJSON: () => ({}),
    } as DOMRect
  }
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
