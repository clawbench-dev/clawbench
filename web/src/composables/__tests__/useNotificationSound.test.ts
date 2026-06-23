import { describe, expect, it } from 'vitest'

describe('useNotificationSound', () => {
  it('exports play function without throwing', async () => {
    const { useNotificationSound } = await import('@/composables/useNotificationSound')
    const { play } = useNotificationSound()
    // playNotificationSound uses AudioContext which doesn't exist in Node
    // but it should catch errors silently
    expect(typeof play).toBe('function')
  })

  it('playNotificationSound catches AudioContext errors gracefully', async () => {
    const { playNotificationSound } = await import('@/composables/useNotificationSound')
    // In test environment, AudioContext is not available
    // The function should not throw — it catches errors internally
    expect(() => playNotificationSound()).not.toThrow()
  })
})
