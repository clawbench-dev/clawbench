import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useFileEditor, _resetForTesting } from '@/composables/useFileEditor'

describe('useFileEditor', () => {
  beforeEach(() => {
    _resetForTesting()
  })

  it('starts in browse mode (not editing)', () => {
    const { editing, isEditing } = useFileEditor()
    expect(isEditing()).toBe(false)
    expect(editing.value).toBe(false)
  })

  it('setEditing toggles the shared state', () => {
    const { editing, isEditing, setEditing } = useFileEditor()
    setEditing(true)
    expect(isEditing()).toBe(true)
    expect(editing.value).toBe(true)
    setEditing(false)
    expect(isEditing()).toBe(false)
  })

  it('exitEdit is a no-op when no handler is registered', () => {
    const { exitEdit } = useFileEditor()
    expect(exitEdit()).toBeUndefined()
  })

  it('exitEdit invokes the registered handler', () => {
    const { registerExitEditHandler, exitEdit } = useFileEditor()
    const fn = vi.fn()
    registerExitEditHandler(fn)
    exitEdit()
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('exitEdit awaits an async handler result', async () => {
    const { registerExitEditHandler, exitEdit } = useFileEditor()
    const order: string[] = []
    registerExitEditHandler(async () => {
      await Promise.resolve()
      order.push('handler')
    })
    const result = exitEdit()
    order.push('after-call')
    await result
    expect(order).toEqual(['after-call', 'handler'])
  })

  it('unregister removes the handler so exitEdit becomes a no-op', () => {
    const { registerExitEditHandler, exitEdit } = useFileEditor()
    const fn = vi.fn()
    const unregister = registerExitEditHandler(fn)
    unregister()
    exitEdit()
    expect(fn).not.toHaveBeenCalled()
  })

  it('latest registered handler replaces the previous one', () => {
    const { registerExitEditHandler, exitEdit } = useFileEditor()
    const first = vi.fn()
    const second = vi.fn()
    registerExitEditHandler(first)
    registerExitEditHandler(second)
    exitEdit()
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  describe('confirmLeave', () => {
    it('returns true when no guard is registered', async () => {
      const { confirmLeave } = useFileEditor()
      await expect(confirmLeave()).resolves.toBe(true)
    })

    it('passes through a guard that allows leaving', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      registerLeaveGuard(() => true)
      await expect(confirmLeave()).resolves.toBe(true)
    })

    it('returns false when the guard blocks leaving (user cancelled)', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      registerLeaveGuard(() => false)
      await expect(confirmLeave()).resolves.toBe(false)
    })

    it('normalizes a non-boolean guard result to false (fail closed)', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      // A malformed guard must never be read as "safe to leave".
      registerLeaveGuard((() => undefined) as () => boolean)
      await expect(confirmLeave()).resolves.toBe(false)
    })

    it('awaits an async guard', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      const order: string[] = []
      registerLeaveGuard(async () => {
        await Promise.resolve()
        order.push('guard')
        return true
      })
      const result = confirmLeave()
      order.push('after-call')
      await expect(result).resolves.toBe(true)
      expect(order).toEqual(['after-call', 'guard'])
    })

    it('unregister restores the no-guard default', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      const unregister = registerLeaveGuard(() => false)
      await expect(confirmLeave()).resolves.toBe(false)
      unregister()
      await expect(confirmLeave()).resolves.toBe(true)
    })

    it('latest registered guard replaces the previous one', async () => {
      const { registerLeaveGuard, confirmLeave } = useFileEditor()
      const first = vi.fn(() => false)
      registerLeaveGuard(first)
      registerLeaveGuard(() => true)
      await expect(confirmLeave()).resolves.toBe(true)
      expect(first).not.toHaveBeenCalled()
    })
  })
})
