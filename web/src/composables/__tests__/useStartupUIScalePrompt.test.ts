import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Startup UI-scale prompt.
 *
 * The gap this closes: the UI scale is a stored value with a manual "auto fit"
 * button, so a fresh install on a 1440p/4K panel renders everything tiny with
 * nothing pointing at the slider. Startup now measures the screen once and
 * ASKS — it must never apply the factor on its own (that is what used to make
 * the UI jump), and it must not nag.
 */

const confirmMock = vi.hoisted(() => vi.fn())
const dialogState = vi.hoisted(() => ({ visible: false }))
const setLocalConfigMock = vi.hoisted(() => vi.fn())
const getEffectiveUIScaleMock = vi.hoisted(() => vi.fn(() => 1))
const platform = vi.hoisted(() => ({ android: false }))
/** UA-derived mobile gate: false = desktop browser (jsdom's default UA). */
const mobileUa = vi.hoisted(() => ({ value: false }))
const screenHeight = vi.hoisted(() => ({ value: 1080, useReal: false }))
const realUiScale = vi.hoisted(() => ({
  currentScreenShortEdge: (() => 0) as () => number,
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    state: ref(dialogState),
    confirm: confirmMock,
    prompt: vi.fn(),
    alert: vi.fn(),
    resolve: vi.fn(),
  }),
}))

// `isMobileOSUA` is a module-load constant (UA-derived) and must survive the
// mock: a bare factory would leave it undefined, silently disabling the
// mobile gate in every case here.
vi.mock('@/composables/usePlatformDetect', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/usePlatformDetect')>()
  return {
    ...actual,
    // A getter, not a snapshot: the factory runs once at import time, so a
    // plain value would freeze whatever the first case set.
    get isMobileOSUA() { return mobileUa.value },
    usePlatformDetect: () => ({
      isAndroidApp: ref(platform.android),
      isElectron: ref(false),
      isWebApp: ref(true),
      isNativeApp: ref(false),
      isTouchPrimary: ref(false),
    }),
  }
})

vi.mock('@/composables/useSettingsConfig', () => ({
  getEffectiveUIScale: getEffectiveUIScaleMock,
  setLocalConfig: setLocalConfigMock,
}))

vi.mock('@/utils/uiScale', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/uiScale')>()
  // Keep the REAL `currentScreenHeight` reachable so the pixel-normalization
  // cases can exercise it while everything else sees the stub.
  realUiScale.currentScreenShortEdge = actual.currentScreenShortEdge
  return {
    ...actual,
    // `useReal` lets the pixel-normalization cases drive the REAL measurement
    // (which reads window.screen / devicePixelRatio) instead of a stub — a
    // stubbed edge would make those cases vacuous.
    currentScreenShortEdge: () =>
      screenHeight.useReal ? actual.currentScreenShortEdge() : screenHeight.value,
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import {
  maybePromptStartupUIScale,
  _resetStartupUIScalePromptForTest,
} from '@/composables/useStartupUIScalePrompt'

const DECLINED_KEY = 'clawbench_ui_scale_prompt_declined'

/**
 * CSS-pixel handling — the trap this feature can fall into.
 *
 * `screen.height` is in CSS pixels on Windows/macOS/X11 but in DEVICE pixels
 * under native Wayland (GNOME's default). The composable must therefore go
 * through `currentScreenHeight()`, which normalizes; reading `screen.height`
 * directly would double up with the OS scale factor on Wayland and offer a 4x
 * UI. These cases drive the REAL measurement (not the stub), so the
 * normalization is what is under test.
 */
describe('maybePromptStartupUIScale — CSS pixel handling', () => {
  const originalScreen = window.screen
  const originalDpr = window.devicePixelRatio
  const originalOuter = window.outerHeight
  const originalInner = window.innerHeight
  const originalUa = navigator.userAgent

  function setScreen(opts: { height: number; dpr: number; outerH: number; innerH: number }) {
    Object.defineProperty(window, 'screen', { value: { height: opts.height }, configurable: true })
    Object.defineProperty(window, 'devicePixelRatio', { value: opts.dpr, configurable: true })
    Object.defineProperty(window, 'outerHeight', { value: opts.outerH, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: opts.innerH, configurable: true })
    Object.defineProperty(navigator, 'userAgent', { value: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/154', configurable: true })
  }

  afterEach(() => {
    screenHeight.useReal = false
    Object.defineProperty(window, 'screen', { value: originalScreen, configurable: true })
    Object.defineProperty(window, 'devicePixelRatio', { value: originalDpr, configurable: true })
    Object.defineProperty(window, 'outerHeight', { value: originalOuter, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: originalInner, configurable: true })
    Object.defineProperty(navigator, 'userAgent', { value: originalUa, configurable: true })
  })

  it('does not double up on native Wayland, which reports DEVICE pixels', () => {
    // Measured on GNOME 46 Wayland: a 4K panel at scale 2 reports height 2160
    // with dpr 2, while a maximized window is 1048 CSS (2096 device) tall.
    // Taken raw that is factor 2.0 ON TOP of the OS's 2x — a 4x UI. Normalized
    // it is 1080 → factor 1.0 → nothing to offer.
    setScreen({ height: 2160, dpr: 2, outerH: 1048, innerH: 961 })
    return asksForRealScreen().then((asked) => {
      expect(asked, 'raw 2160 would offer 200% on an already-2x-scaled panel').toBe(false)
    })
  })

  it('does not double up on a 4K panel the OS already scaled to 200% (X11/Windows)', () => {
    // Same panel on X11/XWayland: height is already CSS px (1080) at dpr 2, and
    // the maximized window's 2096 device px EXCEEDS that report — so it must
    // not be halved again. Either way the answer is "nothing to offer".
    setScreen({ height: 1080, dpr: 2, outerH: 1048, innerH: 961 })
    return asksForRealScreen().then((asked) => {
      expect(asked).toBe(false)
    })
  })

  it('still offers on a 4K panel driven at 100% (2160 CSS px)', () => {
    // No OS scaling: 2160 really is the CSS height, so the fit is 2.0 and the
    // UI genuinely is half-size. Must offer.
    setScreen({ height: 2160, dpr: 1, outerH: 1048, innerH: 961 })
    return asksForRealScreen().then((asked) => {
      expect(asked).toBe(true)
    })
  })
})

/** Drive the composable with the REAL screen measurement, return whether it asked. */
async function asksForRealScreen(): Promise<boolean> {
  screenHeight.useReal = true
  confirmMock.mockResolvedValue(false)
  _resetStartupUIScalePromptForTest()
  await maybePromptStartupUIScale()
  return confirmMock.mock.calls.length > 0
}

describe('maybePromptStartupUIScale', () => {
  beforeEach(() => {
    localStorage.clear()
    confirmMock.mockReset()
    setLocalConfigMock.mockReset()
    getEffectiveUIScaleMock.mockReturnValue(1)
    platform.android = false
    mobileUa.value = false
    dialogState.visible = false
    screenHeight.value = 1080
    _resetStartupUIScalePromptForTest()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('offers the fitted factor on a large screen and applies it when accepted', async () => {
    screenHeight.value = 2160 // 4K at 100% → 2.0
    confirmMock.mockResolvedValue(true)

    await maybePromptStartupUIScale()

    expect(confirmMock).toHaveBeenCalledTimes(1)
    // Persisted through the settings writer, so the value is stored AND applied
    // by the same side effect the slider uses (not a one-off CSS write).
    expect(setLocalConfigMock).toHaveBeenCalledWith('uiScale', 2)
  })

  it('does not touch the scale when the user keeps the current value', async () => {
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(false)

    await maybePromptStartupUIScale()

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(setLocalConfigMock).not.toHaveBeenCalled()
  })

  it('never applies the factor without asking', async () => {
    // The whole reason this is a prompt: silently re-deriving the factor at
    // startup is the "UI jumps on launch" bug. Even when the fit is huge, the
    // only write happens after an explicit accept.
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(null) // dialog replaced/cancelled
    await maybePromptStartupUIScale()
    expect(setLocalConfigMock).not.toHaveBeenCalled()
  })

  it('stays silent on a 1080p screen', async () => {
    screenHeight.value = 1080
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()
    expect(setLocalConfigMock).not.toHaveBeenCalled()
  })

  it('stays silent when the difference is below the threshold', async () => {
    // 1200p fits 1.11 vs 1.0 — noticeable only if you look for it.
    screenHeight.value = 1200
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('asks again on a 1440p screen after a 4K decline', async () => {
    // The stored value is the SUGGESTED factor, not a boolean: moving to a
    // different panel is a new question.
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(false)
    await maybePromptStartupUIScale()
    expect(JSON.parse(localStorage.getItem(DECLINED_KEY)!)).toBe(2)

    // Next launch, same screen: silent.
    _resetStartupUIScalePromptForTest()
    confirmMock.mockClear()
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()

    // Different panel, different suggestion: asks.
    screenHeight.value = 1440
    _resetStartupUIScalePromptForTest()
    await maybePromptStartupUIScale()
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(setLocalConfigMock).not.toHaveBeenCalled()
  })

  it('does not remember a dismissal that was never a decision', async () => {
    // `null` means another dialog replaced ours (the store resolves the old one
    // as cancelled). That is not a "no", so the next launch must still offer.
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(null)
    await maybePromptStartupUIScale()
    expect(localStorage.getItem(DECLINED_KEY)).toBeNull()
  })

  it('asks only once per session', async () => {
    // Both startup paths call in (cold start and post-login).
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(true)
    await maybePromptStartupUIScale()
    await maybePromptStartupUIScale()
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(setLocalConfigMock).toHaveBeenCalledTimes(1)
  })

  it('does not offer on a phone or tablet, even at a size that would qualify', async () => {
    // A 12.9" iPad Pro in portrait used to ask for 126% (its short edge is
    // 1024, but the height-based measurement read 1366). Tablets and phones are
    // scaled by the OS to their density, so a second in-page factor would
    // compound — the same reason the settings "auto fit" row is hidden on
    // Android. The gate must hold regardless of what the screen reports.
    mobileUa.value = true
    screenHeight.value = 2160
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('still offers on a desktop browser of the same size', async () => {
    // The counterpart: the mobile gate must not be the thing that makes the
    // desktop case pass. Same 2160 short edge, desktop UA → it asks.
    mobileUa.value = false
    screenHeight.value = 2160
    confirmMock.mockResolvedValue(false)
    await maybePromptStartupUIScale()
    expect(confirmMock).toHaveBeenCalledTimes(1)
  })

  it('does not offer on the Android shell', async () => {
    // Android scales by screen density and hides the settings "auto fit" row
    // for the same reason; a second, in-page factor would compound.
    platform.android = true
    screenHeight.value = 2160
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('does not clobber a dialog that is already open', async () => {
    // The dialog store is a singleton: opening a second one resolves the first
    // as cancelled, which would silently dismiss whatever the app was asking.
    screenHeight.value = 2160
    dialogState.visible = true
    await maybePromptStartupUIScale()
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('survives a failure without breaking startup', async () => {
    screenHeight.value = 2160
    confirmMock.mockRejectedValue(new Error('boom'))
    await expect(maybePromptStartupUIScale()).resolves.toBeUndefined()
  })
})

describe('startup wiring', () => {
  const app = readWebFile('src/App.vue')

  it('is invoked on both startup paths', () => {
    // Cold start (onMounted) and post-login (handleLoginSuccess) each finish
    // initialization themselves; wiring only one leaves half the users
    // unprompted, and a behaviour test cannot see App.vue.
    const calls = app.match(/offerStartupUIScale\(\)/g) ?? []
    // Two call sites plus the function's own declaration/definition.
    expect(calls.length).toBeGreaterThanOrEqual(3)
    const invocations = app.match(/^\s{4}offerStartupUIScale\(\)$/gm) ?? []
    expect(invocations.length).toBe(2)
  })

  it('waits for the first-run welcome panel to close', () => {
    // Both are full-screen modals and the welcome panel opens in the same tick
    // on a fresh install. Calling the prompt straight away would stack a second
    // modal on top of it before the user read the first.
    expect(app).toMatch(/if \(!welcome\?\.visible\) \{/)
    expect(app).toMatch(/watch\(\(\) => welcome\.visible/)
  })

  it('does not block startup on the decision', () => {
    // Awaited would hold the rest of onMounted (dock resize, deep links) until
    // the user answers a dialog.
    expect(app).not.toMatch(/await (?:maybePromptStartupUIScale|offerStartupUIScale)\(\)/)
  })
})
