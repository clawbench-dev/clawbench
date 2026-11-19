import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  _resetStartupGateForTesting,
  isStartupSettled,
  settleStartupGate,
  whenStartupSettled,
} from './startupGate'

/**
 * The gate is module-level state, so each test resets it. Its only job is
 * ordering: the window must not navigate to `serverUrl` before the embedded
 * server has finished starting, because that address may still name the
 * previous run's port.
 */
describe('startup gate', () => {
  beforeEach(() => {
    _resetStartupGateForTesting()
  })

  // Resolve any promise a test left pending, so the runner does not report it.
  afterEach(() => {
    settleStartupGate()
  })

  it('starts pending', async () => {
    expect(isStartupSettled()).toBe(false)
    let resolved = false
    void whenStartupSettled().then(() => { resolved = true })
    await Promise.resolve()
    expect(resolved, 'the pending gate must not resolve early').toBe(false)
  })

  it('releases every waiter once settled', async () => {
    const order: string[] = []
    void whenStartupSettled().then(() => order.push('first'))
    void whenStartupSettled().then(() => order.push('second'))

    settleStartupGate()

    await whenStartupSettled()
    expect(isStartupSettled()).toBe(true)
    expect(order).toEqual(['first', 'second'])
  })

  it('is idempotent and resolves immediately after settling', async () => {
    settleStartupGate()
    settleStartupGate()
    await expect(whenStartupSettled()).resolves.toBeUndefined()
    expect(isStartupSettled()).toBe(true)
  })
})
