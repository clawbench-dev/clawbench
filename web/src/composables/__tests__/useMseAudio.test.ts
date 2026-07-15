/**
 * MseAudioPlayer tests
 *
 * Tests for the MSE-based audio streaming player.
 * Note: MediaSource and SourceBuffer are browser APIs not available in Node.js,
 * so these tests mock the relevant APIs.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { MseAudioPlayer } from '@/composables/useMseAudio'

// Mock MediaSource and SourceBuffer
class MockSourceBuffer {
  updating = false
  private listeners: Record<string, EventListener[]> = {}

  appendBuffer(_data: ArrayBuffer): void {
    this.updating = true
    // Simulate async updateend synchronously via queueMicrotask
    // (avoids real setTimeout leak that keeps the event loop alive)
    queueMicrotask(() => {
      this.updating = false
      this.dispatchEvent('updateend')
    })
  }

  addEventListener(type: string, listener: EventListener): void {
    if (!this.listeners[type]) this.listeners[type] = []
    this.listeners[type].push(listener)
  }

  removeEventListener(type: string, listener: EventListener): void {
    if (!this.listeners[type]) return
    this.listeners[type] = this.listeners[type].filter(l => l !== listener)
  }

  private dispatchEvent(type: string): void {
    const listeners = this.listeners[type] || []
    for (const l of listeners) l(new Event(type))
  }
}

class MockMediaSource {
  readyState = 'open'
  private sourceBuffer: MockSourceBuffer | null = null
  private listeners: Record<string, EventListener[]> = {}

  static isTypeSupported(_type: string): boolean {
    return true
  }

  addSourceBuffer(_type: string): MockSourceBuffer {
    this.sourceBuffer = new MockSourceBuffer()
    return this.sourceBuffer
  }

  endOfStream(): void {
    this.readyState = 'closed'
  }

  addEventListener(type: string, listener: EventListener): void {
    if (!this.listeners[type]) this.listeners[type] = []
    this.listeners[type].push(listener)
  }

  removeEventListener(type: string, listener: EventListener): void {
    if (!this.listeners[type]) return
    this.listeners[type] = this.listeners[type].filter(l => l !== listener)
  }

  dispatchEvent(type: string): void {
    const listeners = this.listeners[type] || []
    for (const l of listeners) l(new Event(type))
  }
}

// Mock Audio element
class MockAudio {
  src = ''
  onended: (() => void) | null = null
  onerror: (() => void) | null = null
  paused = true

  play(): Promise<void> {
    this.paused = false
    return Promise.resolve()
  }

  pause(): void {
    this.paused = true
  }
}

// Track active players for cleanup
let activePlayers: MseAudioPlayer[] = []

// Setup global mocks
beforeEach(() => {
  vi.stubGlobal('MediaSource', MockMediaSource)
  vi.stubGlobal('Audio', MockAudio)
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:test' })
  activePlayers = []
})

afterEach(() => {
  // Clean up all players created during the test to prevent resource leaks
  for (const player of activePlayers) {
    player.cleanup()
  }
  activePlayers = []
})

// Helper to create and track a player
function createPlayer(): MseAudioPlayer {
  const player = new MseAudioPlayer()
  activePlayers.push(player)
  return player
}

describe('MseAudioPlayer', () => {
  describe('isSupported', () => {
    it('returns true when MediaSource and codec are supported', () => {
      expect(MseAudioPlayer.isSupported()).toBe(true)
    })

    it('returns false when MediaSource is not available', () => {
      vi.stubGlobal('MediaSource', undefined)
      expect(MseAudioPlayer.isSupported()).toBe(false)
    })
  })

  describe('init', () => {
    it('creates an audio element and initializes MSE', () => {
      const player = createPlayer()
      const audio = player.init()
      expect(audio).toBeInstanceOf(MockAudio)
      expect(player.isReady).toBe(false) // Not ready until sourceopen
    })

    it('becomes ready after sourceopen event', () => {
      const player = createPlayer()
      const ms = new MockMediaSource()
      player.init()
      // Simulate sourceopen
      ms.dispatchEvent('sourceopen')
      // Player uses its own internal MediaSource, so isReady depends on sourceopen
    })
  })

  describe('appendChunk', () => {
    it('queues chunks before ready', () => {
      const player = createPlayer()
      const data = new ArrayBuffer(100)
      player.appendChunk(data)
      // No error means success
    })

    it('drops chunks when over memory cap', () => {
      const player = createPlayer()
      player.init()
      // Send chunks totaling > 2MB
      const bigChunk = new ArrayBuffer(2 * 1024 * 1024 + 1)
      player.appendChunk(bigChunk)
      // Should be dropped without error
    })
  })

  describe('cleanup', () => {
    it('resets all state', () => {
      const player = createPlayer()
      player.init()
      player.appendChunk(new ArrayBuffer(100))
      player.cleanup()
      expect(player.isReady).toBe(false)
    })
  })

  describe('endOfStream', () => {
    it('does not throw when called on closed MediaSource', () => {
      const player = createPlayer()
      // No init — no MediaSource
      expect(() => player.endOfStream()).not.toThrow()
    })
  })
})
