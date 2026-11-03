/**
 * Pass the viewer nickname gate in a mounted LiveShareView.
 *
 * The gate sits before the signalling socket (an empty name must not take an
 * audience slot), so every test that wants to reach the viewer has to join
 * first. Kept in one helper so the gate's markup can change without touching
 * five files.
 */
import type { VueWrapper } from '@vue/test-utils'

export function passJoinGate(w: VueWrapper<unknown>, name = 'Tester'): void {
  const input = w.find('[data-role="join-nick"]')
  if (!input.exists()) {
    // Never a no-op: the six LiveShareView suites call this to reach the
    // viewer, and returning silently would let them pass in exactly the state
    // they are supposed to rule out (a viewer that connected without a name).
    // LiveJoinGate.test.ts covers the gate itself; this keeps the *callers*
    // honest about the gate being there at all.
    throw new Error('join gate missing: the viewer would connect without a nickname')
  }
  const el = input.element as HTMLInputElement
  el.value = name
  // v-model updates synchronously on the input event...
  el.dispatchEvent(new Event('input'))
  // ...but the button's `disabled` attribute only clears on the next render, and
  // jsdom refuses to dispatch a click on a disabled button. The submit handler
  // re-validates the name anyway, so clearing the attribute here is safe.
  const button = w.find('[data-action="join-meeting"]')
  ;(button.element as HTMLButtonElement).removeAttribute('disabled')
  void button.trigger('click')
}
