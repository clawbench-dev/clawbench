import { appLog } from '@/utils/appLog'

const TAG = 'AndroidNet'

/**
 * Truthy check for Android WebView bridge returns.
 * Do NOT use `=== true`: some WebView builds box booleans or return `1`.
 * Explicit false/0/'false' → not app mode; anything else truthy (incl. boxed Boolean) → app mode.
 */
export function isNativeBridgeFlag(value: unknown): boolean {
  if (value === false || value === 0 || value === 'false' || value === '0' || value == null) {
    return false
  }
  if (value === true || value === 1 || value === 'true' || value === '1') return true
  // Boxed Boolean / unusual bridge wrappers from older WebViews
  try {
    if (typeof value === 'object' && value !== null && typeof (value as any).valueOf === 'function') {
      const inner = (value as any).valueOf()
      if (inner !== value) return isNativeBridgeFlag(inner)
    }
  } catch {
    /* ignore */
  }
  return Boolean(value)
}

/**
 * Top-frame + AndroidNative bridge ⇒ APK WebView.
 * Presence of the bridge is enough; only an explicit false isNativeApp() opts out.
 */
export function isAndroidAppMode(): boolean {
  try {
    if (window !== window.top) return false
    const native = (window as any).AndroidNative
    if (!native) return false
    if (typeof native.isNativeApp === 'function') {
      try {
        return isNativeBridgeFlag(native.isNativeApp())
      } catch {
        return true
      }
    }
    return true
  } catch {
    return false
  }
}

/** POST a diagnostic line to /api/android-log (works without APK bridge log()). */
export function postWebDiagLog(tag: string, msg: string, level: 'D' | 'I' | 'W' | 'E' = 'I'): void {
  try {
    void fetch('/api/android-log', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        entries: [{ level, tag, msg, ts: Date.now() }],
      }),
      keepalive: true,
    }).catch(() => {})
  } catch {
    /* ignore */
  }
}

/** Keyboard dismiss on Android fires `hidden` briefly — defer network until visible. */
export async function ensureDocumentVisibleForNetwork(maxMs = 8000): Promise<void> {
  if (document.visibilityState === 'visible') return
  appLog.w(TAG, `document hidden — waiting up to ${maxMs}ms before network`)
  await new Promise<void>((resolve) => {
    const finish = () => {
      document.removeEventListener('visibilitychange', onVis)
      resolve()
    }
    const onVis = () => {
      if (document.visibilityState === 'visible') finish()
    }
    document.addEventListener('visibilitychange', onVis)
    setTimeout(finish, maxMs)
  })
}
