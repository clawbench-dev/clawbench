import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the Android native selection menu bridge.
 *
 * ClawBenchWebView (Android) calls
 * `window.__clawbenchSelectionAction('quote'|'comment')` when the user taps one
 * of our items in the system text-selection menu. That name is a cross-language
 * contract: the Java side hard-codes the string in
 * MainActivity.dispatchSelectionAction, and the JS side must expose exactly it.
 *
 * Neither half can be covered by a behaviour test here — the Java side has no
 * browser, and App.vue's install is buried in a setup() that cannot be mounted
 * in isolation — so the contract is pinned by source assertions plus a direct
 * exercise of the handler the callback routes to.
 */
describe('native selection action bridge', () => {
  describe('App.vue installs the global callback', () => {
    const src = readWebFile('src/App.vue')

    it('assigns window.__clawbenchSelectionAction', () => {
      // The exact property name is the contract with MainActivity.java.
      expect(src).toContain('__clawbenchSelectionAction')
      expect(src).toMatch(/__clawbenchSelectionAction\s*=/)
    })

    it('routes it to the composable handler', () => {
      expect(src).toContain('handleNativeSelectionAction')
    })

    it('guards the install so a second setup cannot silently replace it', () => {
      // setup() can run again (HMR); the flag also documents single ownership.
      expect(src).toContain('quoteSelectionBridgeInstalled')
    })

    it('passes the expandRequest through to the bar', () => {
      // Without this the 评论 action would set the counter but the component
      // would never see it, so the input would not open.
      expect(src).toContain(':expandRequest=')
    })
  })

  describe('QuoteQuestionBar consumes expandRequest', () => {
    const src = readWebFile('src/components/common/QuoteQuestionBar.vue')

    it('declares the prop and watches it', () => {
      expect(src).toMatch(/expandRequest:\s*Number/)
      expect(src).toMatch(/watch\(\s*\(\)\s*=>\s*props\.expandRequest/)
    })
  })

  describe('ClawBenchWebView exists and is wired into the layout', () => {
    it('the layout inflates the custom subclass', () => {
      // A plain <WebView> has no startActionMode override, so the menu items
      // would silently never appear.
      const layout = readWebFile('../android/app/src/main/res/layout/activity_main.xml')
      expect(layout).toContain('<com.clawbench.app.ClawBenchWebView')
    })

    it('the subclass overrides both startActionMode overloads', () => {
      const java = readWebFile('../android/app/src/main/java/com/clawbench/app/ClawBenchWebView.java')
      // Both overloads matter: the framework may call either depending on API
      // level / action-mode type.
      expect(java).toContain('public ActionMode startActionMode(ActionMode.Callback callback)')
      expect(java).toMatch(/public ActionMode startActionMode\(ActionMode\.Callback callback, int type\)/)
      // Must delegate to super, not replace the callback outright.
      expect(java).toContain('super.startActionMode(')
    })

    it('MainActivity dispatches to the same JS function name', () => {
      const java = readWebFile('../android/app/src/main/java/com/clawbench/app/MainActivity.java')
      // The Java side must call the exact global name App.vue installs.
      expect(java).toContain('window.__clawbenchSelectionAction')
    })
  })

  describe('handler routes actions correctly', () => {
    beforeEach(() => {
      vi.resetModules()
    })
    afterEach(() => {
      delete (window as unknown as { __clawbenchSelectionAction?: unknown }).__clawbenchSelectionAction
    })

    it('exposes a callable handler from the composable', async () => {
      const { useQuoteQuestion } = await import('@/composables/useQuoteQuestion')
      const qq = useQuoteQuestion()
      expect(typeof qq.handleNativeSelectionAction).toBe('function')
      // Unknown actions must not throw — the native side could send a stale one.
      expect(() => qq.handleNativeSelectionAction('nope')).not.toThrow()
    })
  })
})
