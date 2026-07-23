import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useReconnect, type ReconnectOptions } from '@/composables/useReconnect'

describe('useReconnect', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  function createReconnect(overrides?: Partial<ReconnectOptions>) {
    const onReconnect = vi.fn()
    const opts: ReconnectOptions = {
      onReconnect,
      baseDelay: 1000,
      maxDelay: 8000,
      ...overrides,
    }
    const reconnect = useReconnect(opts)
    return { reconnect, onReconnect }
  }

  describe('shouldReconnect', () => {
    it('returns true when no fatal error', () => {
      const { reconnect } = createReconnect()
      expect(reconnect.shouldReconnect()).toBe(true)
    })

    it('always returns true after multiple attempts (no max limit)', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 100, maxDelay: 200 })

      // Simulate many failed attempts
      for (let i = 0; i < 10; i++) {
        reconnect.scheduleReconnect()
        vi.advanceTimersByTime(200)
      }
      expect(onReconnect).toHaveBeenCalledTimes(10)
      expect(reconnect.shouldReconnect()).toBe(true)
    })

    it('returns false when disabled', () => {
      const { reconnect } = createReconnect()
      reconnect.disable()
      expect(reconnect.shouldReconnect()).toBe(false)
    })

    it('returns false when getFatalError returns a non-null value', () => {
      let fatalError: boolean | null = null
      const { reconnect } = createReconnect({
        getFatalError: () => fatalError,
      })
      expect(reconnect.shouldReconnect()).toBe(true)

      fatalError = true
      expect(reconnect.shouldReconnect()).toBe(false)
    })

    it('returns true when getFatalError returns null', () => {
      const { reconnect } = createReconnect({
        getFatalError: () => null,
      })
      expect(reconnect.shouldReconnect()).toBe(true)
    })
  })

  describe('reconnecting ref', () => {
    it('becomes true after scheduleReconnect', () => {
      const { reconnect } = createReconnect()
      expect(reconnect.reconnecting.value).toBe(false)
      reconnect.scheduleReconnect()
      expect(reconnect.reconnecting.value).toBe(true)
    })

    it('stays true after timer fires (onReconnect must decide next step)', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 1000 })
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(onReconnect).toHaveBeenCalledTimes(1)
      // reconnecting stays true — caller is expected to call scheduleReconnect again
      expect(reconnect.reconnecting.value).toBe(true)
    })

    it('becomes false after reset()', () => {
      const { reconnect } = createReconnect()
      reconnect.scheduleReconnect()
      expect(reconnect.reconnecting.value).toBe(true)
      reconnect.reset()
      expect(reconnect.reconnecting.value).toBe(false)
    })

    it('becomes false after disable()', () => {
      const { reconnect } = createReconnect()
      reconnect.scheduleReconnect()
      expect(reconnect.reconnecting.value).toBe(true)
      reconnect.disable()
      expect(reconnect.reconnecting.value).toBe(false)
    })
  })

  describe('scheduleReconnect', () => {
    it('calls onReconnect after delay', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 1000 })
      reconnect.scheduleReconnect()

      expect(onReconnect).not.toHaveBeenCalled()
      vi.advanceTimersByTime(999)
      expect(onReconnect).not.toHaveBeenCalled()
      vi.advanceTimersByTime(1)
      expect(onReconnect).toHaveBeenCalledTimes(1)
    })

    it('uses exponential backoff: delay = baseDelay * 2^attempts', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 1000, maxDelay: 15000 })

      // First attempt: delay = 1000 * 2^0 = 1000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(onReconnect).toHaveBeenCalledTimes(1)

      // Second attempt: delay = 1000 * 2^1 = 2000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1999)
      expect(onReconnect).toHaveBeenCalledTimes(1)
      vi.advanceTimersByTime(1)
      expect(onReconnect).toHaveBeenCalledTimes(2)

      // Third attempt: delay = 1000 * 2^2 = 4000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(4000)
      expect(onReconnect).toHaveBeenCalledTimes(3)

      // Fourth attempt: delay = 1000 * 2^3 = 8000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(8000)
      expect(onReconnect).toHaveBeenCalledTimes(4)
    })

    it('caps delay at maxDelay', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 1000, maxDelay: 4000 })

      // Attempt 0: 1000 * 2^0 = 1000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(onReconnect).toHaveBeenCalledTimes(1)

      // Attempt 1: 1000 * 2^1 = 2000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(2000)
      expect(onReconnect).toHaveBeenCalledTimes(2)

      // Attempt 2: min(1000 * 2^2, 4000) = min(4000, 4000) = 4000
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(4000)
      expect(onReconnect).toHaveBeenCalledTimes(3)

      // Attempt 3: min(1000 * 2^3, 4000) = min(8000, 4000) = 4000 (capped)
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(4000)
      expect(onReconnect).toHaveBeenCalledTimes(4)
    })

    it('defaults to 2000ms base delay when not specified', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: undefined })
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(2000)
      expect(onReconnect).toHaveBeenCalledTimes(1)
    })

    it('defaults to 15000ms max delay when not specified', () => {
      const { reconnect, onReconnect } = createReconnect({ baseDelay: 1000, maxDelay: undefined })
      // Advance through many attempts — all should be capped at 15000
      for (let i = 0; i < 20; i++) {
        reconnect.scheduleReconnect()
        vi.advanceTimersByTime(15000) // max possible delay
      }
      // All 20 attempts should have fired (no max attempt limit)
      expect(onReconnect).toHaveBeenCalledTimes(20)
    })
  })

  describe('disable', () => {
    it('prevents shouldReconnect from returning true', () => {
      const { reconnect } = createReconnect()
      expect(reconnect.shouldReconnect()).toBe(true)
      reconnect.disable()
      expect(reconnect.shouldReconnect()).toBe(false)
    })

    it('cancels any pending reconnect timer', () => {
      const { reconnect, onReconnect } = createReconnect()
      reconnect.scheduleReconnect()
      reconnect.disable()
      vi.advanceTimersByTime(10000)
      expect(onReconnect).not.toHaveBeenCalled()
    })
  })

  describe('reset', () => {
    it('resets attempt counter', () => {
      const { reconnect, onReconnect } = createReconnect()
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(onReconnect).toHaveBeenCalledTimes(1)
      expect(reconnect.getAttempts()).toBe(1)

      reconnect.reset()
      expect(reconnect.getAttempts()).toBe(0)
      // After reset, delay should be back to base
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(onReconnect).toHaveBeenCalledTimes(2)
    })

    it('re-enables reconnect after disable', () => {
      const { reconnect } = createReconnect()
      reconnect.disable()
      expect(reconnect.shouldReconnect()).toBe(false)
      reconnect.reset()
      expect(reconnect.shouldReconnect()).toBe(true)
    })

    it('cancels any pending reconnect timer', () => {
      const { reconnect, onReconnect } = createReconnect()
      reconnect.scheduleReconnect()
      reconnect.reset()
      vi.advanceTimersByTime(10000)
      expect(onReconnect).not.toHaveBeenCalled()
    })
  })

  describe('getAttempts', () => {
    it('starts at 0', () => {
      const { reconnect } = createReconnect()
      expect(reconnect.getAttempts()).toBe(0)
    })

    it('increments after each scheduled reconnect fires', () => {
      const { reconnect, onReconnect } = createReconnect()
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(reconnect.getAttempts()).toBe(1)
      expect(onReconnect).toHaveBeenCalledTimes(1)
    })

    it('resets to 0 after reset()', () => {
      const { reconnect, onReconnect } = createReconnect()
      reconnect.scheduleReconnect()
      vi.advanceTimersByTime(1000)
      expect(reconnect.getAttempts()).toBe(1)
      reconnect.reset()
      expect(reconnect.getAttempts()).toBe(0)
    })
  })

  describe('interaction: disable + reset', () => {
    it('allows reconnect after disable then reset', () => {
      const { reconnect } = createReconnect()
      reconnect.disable()
      expect(reconnect.shouldReconnect()).toBe(false)
      reconnect.reset()
      expect(reconnect.shouldReconnect()).toBe(true)
    })
  })

  describe('interaction: getFatalError + reset', () => {
    it('clears fatal error influence after reset', () => {
      let fatalError: boolean | null = true
      const { reconnect } = createReconnect({
        getFatalError: () => fatalError,
      })
      expect(reconnect.shouldReconnect()).toBe(false)

      // Simulate recovery: fatal error cleared + reset
      fatalError = null
      reconnect.reset()
      expect(reconnect.shouldReconnect()).toBe(true)
    })
  })
})
