import { describe, expect, it, vi } from 'vitest'
import { createUnhandledRejectionHandler, isRecursiveUpdateError } from '../unhandledRejectionGuard'

describe('isRecursiveUpdateError', () => {
  it('matches Vue recursive-update errors', () => {
    expect(isRecursiveUpdateError(new Error('Maximum recursive updates exceeded'))).toBe(true)
    expect(isRecursiveUpdateError('Maximum recursive updates exceeded in component')).toBe(true)
  })

  it('ignores unrelated reasons', () => {
    expect(isRecursiveUpdateError(new Error('boom'))).toBe(false)
    expect(isRecursiveUpdateError('boom')).toBe(false)
    expect(isRecursiveUpdateError(undefined)).toBe(false)
    expect(isRecursiveUpdateError({ code: 'EPIPE' })).toBe(false)
  })
})

describe('createUnhandledRejectionHandler', () => {
  it('swallows recursive-update errors without forwarding', () => {
    const onRethrow = vi.fn()
    const handler = createUnhandledRejectionHandler(onRethrow)

    handler(new Error('Maximum recursive updates exceeded'))

    expect(onRethrow).not.toHaveBeenCalled()
  })

  it('forwards an unrelated reason exactly once', () => {
    const onRethrow = vi.fn()
    const handler = createUnhandledRejectionHandler(onRethrow)
    const reason = new Error('boom')

    handler(reason)
    handler(reason)
    handler(reason)

    expect(onRethrow).toHaveBeenCalledTimes(1)
    expect(onRethrow).toHaveBeenCalledWith(reason)
  })

  // Regression: forwarding the same reason again would re-enter the handler
  // through the re-thrown rejection, looping forever at 100% CPU.
  it('does not recurse when the forwarder re-invokes the handler', () => {
    let calls = 0
    let handler: (reason: unknown) => void = () => {}
    // Simulate Promise.reject() re-entering the same handler synchronously.
    handler = createUnhandledRejectionHandler((reason) => {
      calls++
      if (calls > 10) throw new Error('infinite recursion')
      handler(reason)
    })

    expect(() => handler(new Error('boom'))).not.toThrow()
    expect(calls).toBe(1)
  })

  it('forwards distinct reasons independently', () => {
    const onRethrow = vi.fn()
    const handler = createUnhandledRejectionHandler(onRethrow)
    const a = new Error('a')
    const b = new Error('b')

    handler(a)
    handler(b)
    handler(a)

    expect(onRethrow).toHaveBeenCalledTimes(2)
    expect(onRethrow).toHaveBeenNthCalledWith(1, a)
    expect(onRethrow).toHaveBeenNthCalledWith(2, b)
  })

  it('defaults to re-throwing via Promise.reject', async () => {
    const handler = createUnhandledRejectionHandler()
    const reason = new Error('default-rethrow')

    // The default forwarder creates a real unhandled rejection; capture it so
    // the assertion is deterministic and no stray rejection escapes the test.
    const captured: unknown[] = []
    const onUnhandled = (r: unknown) => captured.push(r)
    process.on('unhandledRejection', onUnhandled)
    try {
      handler(reason)
      await new Promise((resolve) => setImmediate(resolve))
    } finally {
      process.off('unhandledRejection', onUnhandled)
    }

    expect(captured).toContain(reason)
  })
})
