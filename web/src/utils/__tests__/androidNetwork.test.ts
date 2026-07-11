import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ensureDocumentVisibleForNetwork,
  isAndroidAppMode,
  isNativeBridgeFlag,
} from '@/utils/androidNetwork.ts'

describe('isNativeBridgeFlag', () => {
  it('accepts true, 1, and string forms from WebView bridges', () => {
    expect(isNativeBridgeFlag(true)).toBe(true)
    expect(isNativeBridgeFlag(1)).toBe(true)
    expect(isNativeBridgeFlag('true')).toBe(true)
    expect(isNativeBridgeFlag('1')).toBe(true)
    expect(isNativeBridgeFlag(false)).toBe(false)
    expect(isNativeBridgeFlag(0)).toBe(false)
    expect(isNativeBridgeFlag('false')).toBe(false)
  })

  it('accepts boxed Boolean(true) from older WebView bridges', () => {
    // eslint-disable-next-line no-new-wrappers
    expect(isNativeBridgeFlag(new Boolean(true))).toBe(true)
    // eslint-disable-next-line no-new-wrappers
    expect(isNativeBridgeFlag(new Boolean(false))).toBe(false)
  })
})

describe('isAndroidAppMode', () => {
  const origWindow = globalThis.window

  afterEach(() => {
    vi.restoreAllMocks()
    // @ts-expect-error test cleanup
    globalThis.window = origWindow
  })

  function mockTopWindow(androidNative?: any) {
    const w = { top: null as any, AndroidNative: androidNative } as any
    w.top = w
    globalThis.window = w
  }

  it('returns true when isNativeApp returns boxed-style 1', () => {
    mockTopWindow({ isNativeApp: () => 1 })
    expect(isAndroidAppMode()).toBe(true)
  })

  it('returns true when bridge exists without isNativeApp', () => {
    mockTopWindow({ setKeepScreenOn: () => {} })
    expect(isAndroidAppMode()).toBe(true)
  })

  it('returns false in iframe even with bridge', () => {
    const top = { AndroidNative: { isNativeApp: () => true } } as any
    const frame = { top, AndroidNative: { isNativeApp: () => true } } as any
    globalThis.window = frame
    expect(isAndroidAppMode()).toBe(false)
  })
})

describe('ensureDocumentVisibleForNetwork', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('returns immediately when document is visible', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    await ensureDocumentVisibleForNetwork(1000)
  })

  it('waits until visible or timeout', async () => {
    let state: DocumentVisibilityState = 'hidden'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => state)
    const promise = ensureDocumentVisibleForNetwork(50)
    setTimeout(() => { state = 'visible' }, 10)
    await promise
  })
})
