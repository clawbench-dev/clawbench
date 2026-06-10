/**
 * Stop button two-click confirmation state machine.
 * Extracted from ChatInputBar.vue for isolated testing.
 *
 * Usage:
 *   const machine = createStopButtonMachine(() => onConfirm())
 *   // First click: enters primed state (shows confirmation UI)
 *   machine.click() // → { primed: true, confirmed: false }
 *   // Second click within 1.5s: confirms
 *   machine.click() // → { primed: false, confirmed: true }
 *   // After 1.5s timeout: primed auto-resets
 */

export interface StopButtonResult {
  primed: boolean
  confirmed: boolean
}

export interface StopButtonOptions {
  onConfirm?: () => void
  onPrimeReset?: () => void
}

export function createStopButtonMachine(options: StopButtonOptions = {}) {
  const { onConfirm = () => {}, onPrimeReset } = options
  let primed = false
  let timer: ReturnType<typeof setTimeout> | null = null

  function click(): StopButtonResult {
    if (!primed) {
      // First click: enter confirmation state
      primed = true
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        primed = false
        onPrimeReset?.()
      }, 1500)
      return { primed: true, confirmed: false }
    } else {
      // Second click: confirmed — execute stop
      primed = false
      if (timer) { clearTimeout(timer); timer = null }
      onConfirm()
      return { primed: false, confirmed: true }
    }
  }

  function reset() {
    primed = false
    if (timer) { clearTimeout(timer); timer = null }
  }

  function getPrimed() { return primed }

  function destroy() {
    if (timer) { clearTimeout(timer); timer = null }
    primed = false
  }

  return { click, reset, getPrimed, destroy }
}
