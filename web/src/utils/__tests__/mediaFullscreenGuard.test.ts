import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  exitFullscreenIfContains,
  installMediaFullscreenGuard,
  uninstallMediaFullscreenGuard,
} from '@/utils/mediaFullscreenGuard'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

/**
 * Drive fullscreen state through the real `document` object. jsdom implements
 * neither `fullscreenElement` nor `exitFullscreen`, so both are defined here and
 * removed afterwards — the guard must read the live document, not a captured
 * copy, for the state to be observable.
 */
function setFullscreenElement(el: Element | null): void {
  Object.defineProperty(document, 'fullscreenElement', {
    configurable: true,
    get: () => el,
  })
}

function defineExitFullscreen(fn: (() => Promise<void> | void) | undefined): void {
  if (fn === undefined) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (document as any).exitFullscreen
    return
  }
  Object.defineProperty(document, 'exitFullscreen', { configurable: true, value: fn })
}

function dispatchMediaError(el: Element): void {
  el.dispatchEvent(new Event('error', { bubbles: false }))
}

describe('exitFullscreenIfContains', () => {
  let exitFullscreen: ReturnType<typeof vi.fn>

  beforeEach(() => {
    exitFullscreen = vi.fn(() => Promise.resolve())
    defineExitFullscreen(exitFullscreen)
    setFullscreenElement(null)
  })

  afterEach(() => {
    defineExitFullscreen(undefined)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (document as any).fullscreenElement
  })

  it('exits when the element itself is fullscreen', () => {
    const video = document.createElement('video')
    setFullscreenElement(video)
    expect(exitFullscreenIfContains(video)).toBe(true)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
  })

  it('exits when the fullscreen element is a descendant of the errored element', () => {
    const wrapper = document.createElement('div')
    const inner = document.createElement('video')
    wrapper.appendChild(inner)
    setFullscreenElement(inner)
    expect(exitFullscreenIfContains(wrapper)).toBe(true)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
  })

  it('exits when the fullscreen element is an ancestor of the errored element', () => {
    const wrapper = document.createElement('div')
    const inner = document.createElement('video')
    wrapper.appendChild(inner)
    setFullscreenElement(wrapper)
    expect(exitFullscreenIfContains(inner)).toBe(true)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
  })

  it('does nothing when the fullscreen element is unrelated', () => {
    const other = document.createElement('video')
    const errored = document.createElement('video')
    setFullscreenElement(other)
    expect(exitFullscreenIfContains(errored)).toBe(false)
    expect(exitFullscreen).not.toHaveBeenCalled()
  })

  it('does nothing when nothing is fullscreen', () => {
    const video = document.createElement('video')
    expect(exitFullscreenIfContains(video)).toBe(false)
    expect(exitFullscreen).not.toHaveBeenCalled()
  })

  it('never throws when exitFullscreen rejects', async () => {
    const video = document.createElement('video')
    setFullscreenElement(video)
    defineExitFullscreen(vi.fn(() => Promise.reject(new Error('no gesture'))))
    expect(() => exitFullscreenIfContains(video)).not.toThrow()
    // Swallow the rejection the guard attaches a .catch to.
    await Promise.resolve()
  })

  it('never throws when exitFullscreen throws synchronously', () => {
    const video = document.createElement('video')
    setFullscreenElement(video)
    defineExitFullscreen(() => { throw new Error('boom') })
    expect(exitFullscreenIfContains(video)).toBe(false)
  })

  it('falls back to the webkit-prefixed API when unprefixed is absent', () => {
    const video = document.createElement('video')
    setFullscreenElement(video)
    defineExitFullscreen(undefined)
    const webkitExit = vi.fn()
    Object.defineProperty(document, 'webkitExitFullscreen', { configurable: true, value: webkitExit })
    expect(exitFullscreenIfContains(video)).toBe(true)
    expect(webkitExit).toHaveBeenCalledTimes(1)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (document as any).webkitExitFullscreen
  })

  it('reports no exit when no fullscreen API exists at all', () => {
    const video = document.createElement('video')
    setFullscreenElement(video)
    defineExitFullscreen(undefined)
    expect(exitFullscreenIfContains(video)).toBe(false)
  })
})

describe('installMediaFullscreenGuard', () => {
  let exitFullscreen: ReturnType<typeof vi.fn>

  beforeEach(() => {
    uninstallMediaFullscreenGuard()
    exitFullscreen = vi.fn(() => Promise.resolve())
    defineExitFullscreen(exitFullscreen)
  })

  afterEach(() => {
    uninstallMediaFullscreenGuard()
    defineExitFullscreen(undefined)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (document as any).fullscreenElement
  })

  it('exits fullscreen when the fullscreen video errors', () => {
    const video = document.createElement('video')
    document.body.appendChild(video)
    setFullscreenElement(video)
    const stop = installMediaFullscreenGuard()
    dispatchMediaError(video)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
    stop()
    video.remove()
  })

  it('also covers <audio>', () => {
    const audio = document.createElement('audio')
    document.body.appendChild(audio)
    setFullscreenElement(audio)
    installMediaFullscreenGuard()
    dispatchMediaError(audio)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
    audio.remove()
  })

  it('ignores a non-media element (an <img> error must not touch fullscreen)', () => {
    const img = document.createElement('img')
    document.body.appendChild(img)
    setFullscreenElement(img)
    installMediaFullscreenGuard()
    dispatchMediaError(img)
    expect(exitFullscreen).not.toHaveBeenCalled()
    img.remove()
  })

  it('does not exit when the failing video is unrelated to the fullscreen one', () => {
    const fullscreenVideo = document.createElement('video')
    const otherVideo = document.createElement('video')
    document.body.append(fullscreenVideo, otherVideo)
    setFullscreenElement(fullscreenVideo)
    installMediaFullscreenGuard()
    dispatchMediaError(otherVideo)
    expect(exitFullscreen).not.toHaveBeenCalled()
    fullscreenVideo.remove()
    otherVideo.remove()
  })

  it('is idempotent — a second install does not double-fire', () => {
    const video = document.createElement('video')
    document.body.appendChild(video)
    setFullscreenElement(video)
    installMediaFullscreenGuard()
    installMediaFullscreenGuard()
    dispatchMediaError(video)
    expect(exitFullscreen).toHaveBeenCalledTimes(1)
    video.remove()
  })

  it('stops responding after teardown', () => {
    const video = document.createElement('video')
    document.body.appendChild(video)
    setFullscreenElement(video)
    const stop = installMediaFullscreenGuard()
    stop()
    dispatchMediaError(video)
    expect(exitFullscreen).not.toHaveBeenCalled()
    video.remove()
  })
})
