import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { detectPlatformKey } from '../useDesktopDownload'

const originalUA = navigator.userAgent
const originalUserAgentData = (navigator as unknown as { userAgentData?: unknown }).userAgentData

function setUA(ua: string, arch?: string) {
  Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: arch ? { architecture: arch } : undefined, configurable: true })
}

beforeEach(() => { setUA(originalUA) })
afterEach(() => {
  Object.defineProperty(navigator, 'userAgent', { value: originalUA, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: originalUserAgentData, configurable: true })
  vi.restoreAllMocks()
})

describe('detectPlatformKey', () => {
  it('detects windows', () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    expect(detectPlatformKey()).toBe('win32-x64')
  })
  it('detects intel mac', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')
    expect(detectPlatformKey()).toBe('darwin-x64')
  })
  it('detects apple silicon mac via userAgentData', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'arm')
    expect(detectPlatformKey()).toBe('darwin-arm64')
  })
  it('detects linux x64', () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    expect(detectPlatformKey()).toBe('linux-x64')
  })
  it('returns empty for mobile', () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)')
    expect(detectPlatformKey()).toBe('')
  })
})
