import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  resolveMdImageOpenClick,
  handleMdImageOpenClick,
  type ImageOpenHit,
} from '@/utils/mdImageOpen'

function makeBlock(overrides: { attachSrc?: string | null; external?: boolean } = {}): {
  block: HTMLElement
  btn: HTMLElement
  img: HTMLImageElement
} {
  const block = document.createElement('div')
  block.className = 'image-block-wrapper'
  const imgWrap = document.createElement('span')
  imgWrap.className = 'lightbox-img-wrap'
  const img = document.createElement('img')
  img.className = 'lightbox-img'
  if (overrides.attachSrc) img.setAttribute('data-attach-src', overrides.attachSrc)
  if (overrides.external) img.setAttribute('src', 'https://x.com/a.png')
  imgWrap.appendChild(img)
  const btn = document.createElement('button')
  btn.className = 'image-block-open-btn'
  block.appendChild(imgWrap)
  block.appendChild(btn)
  return { block, btn, img }
}

function clickOn(btn: HTMLElement): Event {
  return { target: btn, preventDefault: () => {}, stopPropagation: () => {} } as unknown as Event
}

describe('resolveMdImageOpenClick', () => {
  it('resolves an open-file button to its local image path', () => {
    const { btn } = makeBlock({ attachSrc: 'docs/a.png' })
    expect(resolveMdImageOpenClick(clickOn(btn))).toEqual({ path: 'docs/a.png' } satisfies ImageOpenHit)
  })

  it('returns null for clicks not on the open button', () => {
    const { img } = makeBlock({ attachSrc: 'docs/a.png' })
    expect(resolveMdImageOpenClick({ target: img } as unknown as Event)).toBeNull()
    expect(resolveMdImageOpenClick({ target: document.createElement('p') } as unknown as Event)).toBeNull()
  })

  it('returns null when the image has no data-attach-src (external/data)', () => {
    const { btn } = makeBlock({ external: true })
    expect(resolveMdImageOpenClick(clickOn(btn))).toBeNull()
  })
})

describe('handleMdImageOpenClick', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('opens the file and stops propagation', () => {
    const { btn } = makeBlock({ attachSrc: 'docs/a.png' })
    const stopPropagation = vi.fn()
    const preventDefault = vi.fn()
    const open = vi.fn()
    const ret = handleMdImageOpenClick({
      target: btn,
      stopPropagation,
      preventDefault,
    } as unknown as Event, open)
    expect(ret).toBe(true)
    expect(open).toHaveBeenCalledWith('docs/a.png')
    expect(stopPropagation).toHaveBeenCalled()
    expect(preventDefault).toHaveBeenCalled()
  })

  it('does nothing for non-open-button clicks', () => {
    const open = vi.fn()
    const ret = handleMdImageOpenClick({ target: document.createElement('p') } as unknown as Event, open)
    expect(ret).toBe(false)
    expect(open).not.toHaveBeenCalled()
  })
})
