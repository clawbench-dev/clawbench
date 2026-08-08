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
})
