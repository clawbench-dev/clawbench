import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  browserScreenCaptureAvailable,
  isHttpOrigin,
  isInsecureCaptureContext,
} from '../screenCaptureSupport'

/**
 * The two gates that decide whether the share panel may offer capture. They are
 * separate on purpose: a browser on plain HTTP has no `mediaDevices` at all
 * (insecure), while a mobile browser has `mediaDevices` but no
 * `getDisplayMedia` (unsupported). Collapsing them into one boolean is what
 * makes the panel tell an HTTP user to "update your client".
 */
describe('browserScreenCaptureAvailable', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('is true when the browser exposes getDisplayMedia', () => {
    vi.stubGlobal('navigator', { mediaDevices: { getDisplayMedia: () => undefined } })
    expect(browserScreenCaptureAvailable()).toBe(true)
  })

  it('is false when mediaDevices is missing (insecure origin)', () => {
    vi.stubGlobal('navigator', {})
    expect(browserScreenCaptureAvailable()).toBe(false)
  })

  it('is false when the platform has mediaDevices but no getDisplayMedia', () => {
    // Chrome for Android / Safari on iOS: microphone works, screen capture is
    // not implemented. Checking the object instead of the method would claim
    // support and then fail inside start().
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: () => undefined } })
    expect(browserScreenCaptureAvailable()).toBe(false)
  })
})

describe('isInsecureCaptureContext', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('is true only when the platform says so explicitly', () => {
    vi.stubGlobal('window', { isSecureContext: false })
    expect(isInsecureCaptureContext()).toBe(true)
  })

  it('does not claim insecurity when the flag is absent', () => {
    vi.stubGlobal('window', {})
    expect(isInsecureCaptureContext()).toBe(false)
  })
})

/**
 * The desktop shell promotes its own http origin, so `isSecureContext` is true
 * there while every invite link is still http. This predicate is what the
 * "guests will be limited" banner keys on, and it has to stay true even when
 * the page claims to be secure — that combination IS the desktop shell.
 */
describe('isHttpOrigin', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('is true on a plain http page, even one marked as a secure context', () => {
    vi.stubGlobal('window', { isSecureContext: true, location: { protocol: 'http:' } })
    expect(isHttpOrigin()).toBe(true)
  })

  it('is false on https', () => {
    vi.stubGlobal('window', { isSecureContext: true, location: { protocol: 'https:' } })
    expect(isHttpOrigin()).toBe(false)
  })
})
