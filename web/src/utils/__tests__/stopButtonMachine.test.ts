import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createStopButtonMachine } from '@/utils/stopButtonMachine'

// ────────────────────────────────────────────────────────────
// Stop button two-click confirmation state machine.
// Now imports the extracted module instead of copying logic.
// ────────────────────────────────────────────────────────────

describe('stop-button-two-click', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  // ── First click → primed state ──
  it('first click enters primed state', () => {
    const machine = createStopButtonMachine()
    const result = machine.click()
    expect(result.primed).toBe(true)
    expect(result.confirmed).toBe(false)
    expect(machine.getPrimed()).toBe(true)
  })

  // ── Second click → confirm ──
  it('second click confirms and calls onConfirm', () => {
    const onConfirm = vi.fn()
    const machine = createStopButtonMachine({ onConfirm })
    machine.click()
    const result = machine.click()
    expect(result.primed).toBe(false)
    expect(result.confirmed).toBe(true)
    expect(machine.getPrimed()).toBe(false)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  // ── Timeout resets primed state ──
  it('primed state resets after 1.5s timeout', () => {
    const machine = createStopButtonMachine()
    machine.click()
    expect(machine.getPrimed()).toBe(true)

    vi.advanceTimersByTime(1500)
    expect(machine.getPrimed()).toBe(false)
  })

  // ── Primed state persists before timeout ──
  it('primed state persists before timeout', () => {
    const machine = createStopButtonMachine()
    machine.click()

    vi.advanceTimersByTime(1400)
    expect(machine.getPrimed()).toBe(true)
  })

  // ── Click after timeout starts new cycle ──
  it('click after timeout starts new primed cycle', () => {
    const machine = createStopButtonMachine()
    machine.click()
    vi.advanceTimersByTime(1500)
    expect(machine.getPrimed()).toBe(false)

    // New click should start primed again (not confirm)
    const result = machine.click()
    expect(result.primed).toBe(true)
    expect(result.confirmed).toBe(false)
  })

  // ── Manual reset ──
  it('reset clears primed state', () => {
    const machine = createStopButtonMachine()
    machine.click()
    expect(machine.getPrimed()).toBe(true)

    machine.reset()
    expect(machine.getPrimed()).toBe(false)
  })

  // ── Reset then click starts fresh ──
  it('reset then click starts fresh cycle', () => {
    const machine = createStopButtonMachine()
    machine.click()
    machine.reset()

    const result = machine.click()
    expect(result.primed).toBe(true)
    expect(result.confirmed).toBe(false)
  })

  // ── Rapid triple click: primed → confirm → primed ──
  it('rapid triple click: primed → confirm → primed again', () => {
    const onConfirm = vi.fn()
    const machine = createStopButtonMachine({ onConfirm })
    const r1 = machine.click() // prime
    const r2 = machine.click() // confirm
    const r3 = machine.click() // prime again (new cycle)

    expect(r1).toEqual({ primed: true, confirmed: false })
    expect(r2).toEqual({ primed: false, confirmed: true })
    expect(r3).toEqual({ primed: true, confirmed: false })
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  // ── onConfirm callback ──
  it('onConfirm callback is invoked on second click', () => {
    const onConfirm = vi.fn()
    const machine = createStopButtonMachine({ onConfirm })

    machine.click()
    expect(onConfirm).not.toHaveBeenCalled()

    machine.click()
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  // ── onPrimeReset callback ──
  it('onPrimeReset callback is invoked when primed state times out', () => {
    const onPrimeReset = vi.fn()
    const machine = createStopButtonMachine({ onPrimeReset })

    machine.click()
    expect(onPrimeReset).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1500)
    expect(onPrimeReset).toHaveBeenCalledTimes(1)
  })

  it('onPrimeReset is not called when second click confirms', () => {
    const onPrimeReset = vi.fn()
    const machine = createStopButtonMachine({ onPrimeReset })

    machine.click()
    machine.click() // confirms immediately
    vi.advanceTimersByTime(1500)
    expect(onPrimeReset).not.toHaveBeenCalled()
  })

  it('onPrimeReset is not called when reset manually', () => {
    const onPrimeReset = vi.fn()
    const machine = createStopButtonMachine({ onPrimeReset })

    machine.click()
    machine.reset()
    vi.advanceTimersByTime(1500)
    expect(onPrimeReset).not.toHaveBeenCalled()
  })

  // ── onConfirm is not called on timeout ──
  it('onConfirm is not called when primed state times out', () => {
    const onConfirm = vi.fn()
    const machine = createStopButtonMachine({ onConfirm })

    machine.click()
    vi.advanceTimersByTime(1500)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  // ── Loading ends resets primed state ──
  it('loading=false resets primed state (simulated)', () => {
    const machine = createStopButtonMachine()
    machine.click()
    expect(machine.getPrimed()).toBe(true)

    // Simulate: loading ends → reset
    machine.reset()
    expect(machine.getPrimed()).toBe(false)
  })

  // ── Destroy clears timer and primed state ──
  it('destroy clears timer and primed state', () => {
    const onPrimeReset = vi.fn()
    const machine = createStopButtonMachine({ onPrimeReset })
    machine.click()
    expect(machine.getPrimed()).toBe(true)

    machine.destroy()
    expect(machine.getPrimed()).toBe(false)

    // Timer should be cleared — advancing time should NOT trigger onPrimeReset
    vi.advanceTimersByTime(3000)
    expect(onPrimeReset).not.toHaveBeenCalled()
  })

  it('destroy is safe when not primed', () => {
    const machine = createStopButtonMachine()
    expect(() => machine.destroy()).not.toThrow()
    expect(machine.getPrimed()).toBe(false)
  })
})
