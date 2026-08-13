/**
 * Decide whether the terminal should re-focus its xterm textarea after it blurs.
 *
 * Android WebView quirk: touching the terminal surface blurs the focused xterm
 * textarea BEFORE `touchstart` is dispatched (the keyboard collapses), and xterm
 * re-focuses on the synthesized mousedown (the keyboard reopens) — a visible
 * collapse-then-reopen on every tap. This blur can't be prevented with
 * `preventDefault` because it happens before the touch event. Instead we restore
 * focus as soon as the textarea blurs.
 *
 * We only auto-refocus when the terminal panel is still active AND the focus did
 * not move to a real control (a toolbar/dock button, an input, etc.). Tapping a
 * control should keep the control focused; only body/document (i.e. a tap on the
 * terminal surface) is restored.
 */
export function shouldAutoRefocusTerminal(
  terminalActive: boolean,
  nextActive: Element | null,
): boolean {
  if (!terminalActive) return false
  if (!nextActive) return true
  return nextActive.tagName === 'BODY'
}

/**
 * Whether the terminal blur-refocus workaround should be installed at all.
 *
 * The workaround exists only for the Android WebView soft-keyboard quirk (see
 * the comment above). On desktop (isPC) it is not only unnecessary but harmful:
 * in the wide-screen split layout the terminal panel stays active while the
 * chat pane is on the right, so a blur on the xterm textarea can re-claim focus
 * from the chat input in the same tick, making the chat textarea unfocusable.
 */
export function shouldInstallTerminalBlurRefocus(isPC: boolean): boolean {
  return !isPC
}
