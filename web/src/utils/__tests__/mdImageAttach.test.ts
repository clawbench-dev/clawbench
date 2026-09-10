import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  resolveMdImageBadgeClick,
  handleMdImageAttachClick,
  MD_IMAGE_ATTACH_BADGE,
  type MdImageAttachActions,
} from '@/utils/mdImageAttach'

function makeWrap(overrides: { attachSrc?: string | null; external?: boolean } = {}): {
  wrap: HTMLElement
  badge: HTMLElement
  img: HTMLImageElement
} {
  // File-preview images are lifted into .image-block-wrapper figures whose
  // header holds the attach button; the img sits in .lightbox-img-wrap.
  const wrap = document.createElement('div')
  wrap.className = 'image-block-wrapper'
  const imgWrap = document.createElement('span')
  imgWrap.className = 'lightbox-img-wrap'
  const img = document.createElement('img')
  img.className = 'lightbox-img'
  if (overrides.attachSrc) img.setAttribute('data-attach-src', overrides.attachSrc)
  if (overrides.external) img.setAttribute('src', 'https://x.com/a.png')
  imgWrap.appendChild(img)
  const badge = document.createElement('button')
  badge.className = MD_IMAGE_ATTACH_BADGE.slice(1)
  wrap.appendChild(imgWrap)
  wrap.appendChild(badge)
  return { wrap, badge, img }
}

function clickOn(badge: HTMLElement): Event {
  return { target: badge, preventDefault: () => {}, stopPropagation: () => {} } as unknown as Event
}

function makeActions(overrides: Partial<MdImageAttachActions> = {}): MdImageAttachActions {
  return {
    add: vi.fn(),
    remove: vi.fn(),
    has: vi.fn(() => false),
    toast: vi.fn(),
    messages: { added: 'Added to chat', removed: 'Removed from chat attachments' },
    ...overrides,
  }
}

describe('resolveMdImageBadgeClick', () => {
  it('resolves a local image badge to its decoded path + wrapper', () => {
    const { wrap, badge } = makeWrap({ attachSrc: 'docs/a.png' })
    expect(resolveMdImageBadgeClick(clickOn(badge))).toEqual({ path: 'docs/a.png', wrap })
  })

  it('returns null for clicks not on the badge', () => {
    const { wrap } = makeWrap({ attachSrc: 'docs/a.png' })
    expect(resolveMdImageBadgeClick({ target: wrap } as unknown as Event)).toBeNull()
    expect(resolveMdImageBadgeClick({ target: document.createElement('p') } as unknown as Event)).toBeNull()
    expect(resolveMdImageBadgeClick({ target: null } as unknown as Event)).toBeNull()
  })

  it('returns null when the wrapped image has no data-attach-src (external image)', () => {
    const { badge } = makeWrap({ attachSrc: null })
    expect(resolveMdImageBadgeClick(clickOn(badge))).toBeNull()
  })
})

describe('handleMdImageAttachClick', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('adds the file and shows the added toast on first tap', () => {
    const { badge } = makeWrap({ attachSrc: 'img/x.png' })
    const actions = makeActions()
    const ret = handleMdImageAttachClick(clickOn(badge), actions)
    expect(ret).toBe(true)
    expect(actions.has).toHaveBeenCalledWith('img/x.png')
    expect(actions.add).toHaveBeenCalledWith('img/x.png')
    expect(actions.remove).not.toHaveBeenCalled()
    expect(actions.toast).toHaveBeenCalledWith('Added to chat', { icon: '📎', type: 'success', duration: 1500 })
  })

  it('removes the file when already attached', () => {
    const { badge } = makeWrap({ attachSrc: 'img/x.png' })
    const actions = makeActions({ has: vi.fn(() => true) })
    handleMdImageAttachClick(clickOn(badge), actions)
    expect(actions.remove).toHaveBeenCalledWith('img/x.png')
    expect(actions.add).not.toHaveBeenCalled()
    expect(actions.toast).toHaveBeenCalledWith('Removed from chat attachments', { icon: '📎', type: 'info', duration: 1500 })
  })

  it('does nothing and returns false for non-badge clicks', () => {
    const actions = makeActions()
    const ret = handleMdImageAttachClick({ target: document.createElement('p') } as unknown as Event, actions)
    expect(ret).toBe(false)
    expect(actions.add).not.toHaveBeenCalled()
    expect(actions.toast).not.toHaveBeenCalled()
  })

  it('stops propagation and prevents default so the lightbox does not open', () => {
    const { badge } = makeWrap({ attachSrc: 'a.png' })
    const stopPropagation = vi.fn()
    const preventDefault = vi.fn()
    const actions = makeActions()
    handleMdImageAttachClick({
      target: badge,
      stopPropagation,
      preventDefault,
    } as unknown as Event, actions)
    expect(stopPropagation).toHaveBeenCalled()
    expect(preventDefault).toHaveBeenCalled()
  })

  it('dispatches the fly-to-chat particle when a mobile chat dock exists', () => {
    const { badge } = makeWrap({ attachSrc: 'a.png' })
    const dockBtn = document.createElement('div')
    dockBtn.className = 'dock-btn'
    const dockCenter = document.createElement('div')
    dockCenter.className = 'dock-center'
    dockCenter.appendChild(dockBtn)
    document.body.appendChild(dockCenter)
    dockBtn.getBoundingClientRect = () => ({ left: 100, top: 100, width: 20, height: 20, right: 120, bottom: 120, x: 100, y: 100, toJSON: () => ({}) }) as DOMRect

    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    handleMdImageAttachClick(clickOn(badge), makeActions())
    const attachEvent = dispatchSpy.mock.calls.find(([ev]) => ev.type === 'attach-to-chat')
    expect(attachEvent).toBeTruthy()
    const detail = (attachEvent![0] as CustomEvent).detail
    expect(detail.to).toEqual({ x: 110, y: 110 })
    expect(typeof detail.from.x).toBe('number')
    document.body.removeChild(dockCenter)
  })

  it('skips the particle silently when no dock button is present', () => {
    const { badge } = makeWrap({ attachSrc: 'a.png' })
    document.querySelectorAll('.dock-center').forEach(el => el.remove())
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    handleMdImageAttachClick(clickOn(badge), makeActions())
    expect(dispatchSpy.mock.calls.some(([ev]) => ev.type === 'attach-to-chat')).toBe(false)
  })
})
