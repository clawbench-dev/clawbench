import { describe, expect, it, afterEach } from 'vitest'
import { installPromiseWithResolversPolyfill } from '@/utils/polyfills.ts'

const nativeWithResolvers = Promise.withResolvers

afterEach(() => {
  // Restore the native method so tests don't leak state
  // @ts-expect-error - restore original
  Promise.withResolvers = nativeWithResolvers
})

describe('installPromiseWithResolversPolyfill', () => {
  it('does nothing when the native method exists', () => {
    const impl = Promise.withResolvers
    installPromiseWithResolversPolyfill()
    expect(Promise.withResolvers).toBe(impl)
  })

  it('installs withResolvers when missing', () => {
    // @ts-expect-error - temporarily remove to simulate an old browser
    delete Promise.withResolvers
    expect(Promise.withResolvers).toBeUndefined()

    installPromiseWithResolversPolyfill()

    expect(typeof Promise.withResolvers).toBe('function')
  })

  it('installed polyfill resolves and rejects correctly', async () => {
    // @ts-expect-error - temporarily remove to simulate an old browser
    delete Promise.withResolvers
    installPromiseWithResolversPolyfill()

    const { promise, resolve, reject } = (Promise.withResolvers as any)()

    resolve('ok')
    await expect(promise).resolves.toBe('ok')

    const { promise: p2, reject: r2 } = (Promise.withResolvers as any)()
    r2(new Error('boom'))
    await expect(p2).rejects.toThrow('boom')
  })
})
