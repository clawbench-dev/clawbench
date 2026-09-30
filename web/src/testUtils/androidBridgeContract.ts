import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Real-host contract helpers for the Android `@JavascriptInterface` bridge.
 *
 * Frontend tests that mount a component install a hand-built fake host, so they
 * can only ever prove "the code works when the bridge method is present" — never
 * that the SHIPPED Android host actually exposes it. That blind spot is exactly
 * how the 2026 h2-toggle bug shipped: the row read a getter the real bridge did
 * not implement, and every unit test was green.
 *
 * These helpers read the real `MainActivity.java` source so a contract test can
 * assert on the shipped host itself.
 *
 * NOTE: this module deliberately does NOT end in `.test.ts` — vitest's default
 * include glob (`**\/*.{test,spec}.?(c|m)[jt]s?(x)`) would otherwise treat it as
 * a spec file and fail it for containing no tests. It is a plain helper imported
 * by contract specs. `vitest.config.ts` sets no `include`, so the default glob
 * applies and this file is correctly ignored.
 */

/** Read the real Android `MainActivity.java` bridge source. */
export function readAndroidBridge(): string {
  const candidates = [
    resolve(process.cwd(), 'android/app/src/main/java/com/clawbench/app/MainActivity.java'),
    resolve(process.cwd(), '../android/app/src/main/java/com/clawbench/app/MainActivity.java'),
  ]
  for (const p of candidates) {
    try {
      return readFileSync(p, 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`MainActivity.java not found from cwd: ${process.cwd()}`)
}

/**
 * True when `name` is declared as a `@JavascriptInterface` bridge method.
 *
 * The annotation is part of the match on purpose: a method present but missing
 * `@JavascriptInterface` is invisible to the WebView, so a bare method-name grep
 * would pass while the bridge silently omits it — the exact failure this helper
 * exists to catch.
 */
export function androidBridgeExposes(src: string, name: string): boolean {
  const re = new RegExp(
    `@JavascriptInterface\\s+public\\s+[\\w<>\\[\\].]+\\s+${name}\\s*\\(`,
  )
  return re.test(src)
}
