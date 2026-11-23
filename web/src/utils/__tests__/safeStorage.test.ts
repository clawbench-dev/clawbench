import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { storageGet, storageRemove, storageSet } from '../safeStorage'

// The jsdom localStorage is an accessor on globalThis; replacing it with a
// plain value (or a throwing getter) is how we reproduce the broken
// environments this module exists for. Restore the original descriptor after
// each case so later test files see a normal Storage.
let saved: PropertyDescriptor | undefined

beforeEach(() => {
  saved = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
})

afterEach(() => {
  if (saved) Object.defineProperty(globalThis, 'localStorage', saved)
  else Reflect.deleteProperty(globalThis, 'localStorage')
})

function replaceStorage(value: unknown) {
  Object.defineProperty(globalThis, 'localStorage', { value, configurable: true, writable: true })
}

describe('safeStorage', () => {
  it('reads and writes through a real Storage', () => {
    localStorage.clear()
    storageSet('probe', 'value')
    expect(storageGet('probe')).toBe('value')
    expect(localStorage.getItem('probe')).toBe('value')
  })

  it('removes a key through a real Storage', () => {
    localStorage.clear()
    storageSet('probe', 'value')
    storageRemove('probe')
    expect(storageGet('probe')).toBeNull()
  })

  it('returns null and stays silent when getItem is missing (Node 25/26)', () => {
    replaceStorage({ getItem: undefined, setItem: undefined })
    expect(storageGet('probe')).toBeNull()
    expect(() => storageSet('probe', 'value')).not.toThrow()
  })

  it('returns null and stays silent when localStorage itself is undefined', () => {
    replaceStorage(undefined)
    expect(storageGet('probe')).toBeNull()
    expect(() => storageSet('probe', 'value')).not.toThrow()
  })

  it('swallows errors thrown by a blocked storage', () => {
    replaceStorage({
      getItem() { throw new Error('blocked') },
      setItem() { throw new Error('blocked') },
    })
    expect(storageGet('probe')).toBeNull()
    expect(() => storageSet('probe', 'value')).not.toThrow()
  })

  it('swallows an error raised while reading the localStorage property itself', () => {
    Object.defineProperty(globalThis, 'localStorage', {
      get() { throw new Error('blocked at property level') },
      configurable: true,
    })
    expect(storageGet('probe')).toBeNull()
    expect(() => storageSet('probe', 'value')).not.toThrow()
  })
})
