/**
 * Unified frontend logger: always prints to browser console,
 * and when running in Android WebView with the native bridge,
 * also relays logs to AppLog via AndroidNative.log().
 *
 * Tag convention: use short PascalCase module name, e.g.
 *   'ClawBench', 'ChatStream', 'PortForward', 'Store', 'useDialog'
 *
 * Level mapping:
 *   appLog.d → console.log  / AppLog.d (D)
 *   appLog.i → console.info / AppLog.i (I)
 *   appLog.w → console.warn / AppLog.w (W)
 *   appLog.e → console.error/ AppLog.e (E)
 */

function safeStringify(a: unknown): string {
  if (typeof a === 'string') return a
  if (typeof a === 'number' || typeof a === 'boolean') return String(a)
  try { return JSON.stringify(a) } catch { return String(a) }
}

function relay(level: string, tag: string, args: unknown[]): void {
  try {
    const native = (window as any).AndroidNative
    if (!native || !native.log) return
    // Check isNativeApp() + top-frame to avoid iframe false positives
    if (native.isNativeApp?.() !== true) return
    if (window !== window.top) return
    const msg = args.map(safeStringify).join(' ')
    native.log(level, tag, msg)
  } catch {
    // bridge not available — silent
  }
}

export const appLog = {
  d(tag: string, ...args: unknown[]) { console.log(`[${tag}]`, ...args); relay('D', tag, args) },
  i(tag: string, ...args: unknown[]) { console.info(`[${tag}]`, ...args); relay('I', tag, args) },
  w(tag: string, ...args: unknown[]) { console.warn(`[${tag}]`, ...args); relay('W', tag, args) },
  e(tag: string, ...args: unknown[]) { console.error(`[${tag}]`, ...args); relay('E', tag, args) },
}
