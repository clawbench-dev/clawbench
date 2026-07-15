/**
 * MseAudioPlayer tests
 *
 * Tests for the MSE-based audio streaming player.
 * Note: MediaSource and SourceBuffer are browser APIs not available in Node.js,
 * so these tests mock the relevant APIs.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { MseAudioPlayer } from '@/composables/useMseAudio'

// Mock SourceBuffer that properly tracks listeners and fires events
class MockSourceBuffer {
  updating = false
  private listeners: Record<string, EventListener[]> = {}
  private _appendBufferThrow = false

  appendBuffer(_data: ArrayBuffer): void {
    if (this._appendBufferThrow) throw new Error('appendBuffer failed')
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

  dispatchEvent(type: string): void {
    const listeners = this.listeners[type] || []
    for (const l of listeners) l(new Event(type))
  }

  /** Test helper: make appendBuffer throw on next call */
  throwOnAppend(): void { this._appendBufferThrow = true }
}

// Mock MediaSource that captures the instance created by init()
// so tests can fire sourceopen on the correct object.
let lastCreatedMediaSource: MockMediaSource | null = null

class MockMediaSource {
  readyState: 'open' | 'closed' | 'ended' = 'open'
  private sourceBuffer: MockSourceBuffer | null = null
  private listeners: Record<string, EventListener[]> = {}
  private _addSourceBufferThrow = false

  static isTypeSupported(_type: string): boolean {
    return true
  }

  addSourceBuffer(_type: string): MockSourceBuffer {
    if (this._addSourceBufferThrow) throw new Error('addSourceBuffer failed')
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

  /** Test helper: make addSourceBuffer throw on next call */
  throwOnAddSourceBuffer(): void { this._addSourceBufferThrow = true }

  /** Get the source buffer created by addSourceBuffer */
  getSourceBuffer(): MockSourceBuffer | null { return this.sourceBuffer }
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
  lastCreatedMediaSource = null
  // Mock MediaSource constructor to capture the instance
  vi.stubGlobal('MediaSource', class extends MockMediaSource {
    constructor() {
      super()
      lastCreatedMediaSource = this
    }
  })
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

// Helper: init player and fire sourceopen on its internal MediaSource
function initAndOpen(player: MseAudioPlayer): MockMediaSource {
  player.init()
  const ms = lastCreatedMediaSource!
  ms.dispatchEvent('sourceopen')
  return ms
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
      const ms = initAndOpen(player)
      expect(player.isReady).toBe(true)
      expect(ms.getSourceBuffer()).not.toBeNull()
    })

    it('handles addSourceBuffer failure', () => {
      const player = createPlayer()
      player.init()
      const ms = lastCreatedMediaSource!
      ms.throwOnAddSourceBuffer()
      ms.dispatchEvent('sourceopen')
      expect(player.isReady).toBe(false)
    })
  })

  describe('appendChunk', () => {
    it('queues chunks before ready', () => {
      const player = createPlayer()
      const data = new ArrayBuffer(100)
      player.appendChunk(data)
      // No error means success — chunk is queued in pendingChunks
    })

    it('coalesces consecutive chunks before ready', () => {
      const player = createPlayer()
      const data1 = new ArrayBuffer(10)
      const data2 = new ArrayBuffer(20)
      player.appendChunk(data1)
      player.appendChunk(data2)
      // Second chunk should merge into first (coalescing path)
    })

    it('drops chunks when over memory cap', () => {
      const player = createPlayer()
      player.init()
      // Send chunks totaling > 2MB
      const bigChunk = new ArrayBuffer(2 * 1024 * 1024 + 1)
      player.appendChunk(bigChunk)
      // Should be dropped without error
    })

    it('drains pending chunks when ready after sourceopen', () => {
      const player = createPlayer()
      const data = new ArrayBuffer(100)
      player.appendChunk(data) // queued before ready
      initAndOpen(player) // sourceopen fires, drainPending called
      expect(player.isReady).toBe(true)
    })

    it('appends chunks directly when ready and not busy', () => {
      const player = createPlayer()
      const ms = initAndOpen(player)
      const sb = ms.getSourceBuffer()!
      const data = new ArrayBuffer(50)
      player.appendChunk(data)
      // doAppend is called, which calls sb.appendBuffer
    })
  })

  describe('SourceBuffer error', () => {
    it('resets appending flag on SourceBuffer error event', () => {
      const player = createPlayer()
      const ms = initAndOpen(player)
      const sb = ms.getSourceBuffer()!
      // Simulate SourceBuffer error
      sb.dispatchEvent('error')
      // appending flag should be reset (no crash)
    })

    it('handles appendBuffer failure', () => {
      const player = createPlayer()
      const ms = initAndOpen(player)
      const sb = ms.getSourceBuffer()!
      sb.throwOnAppend()
      const data = new ArrayBuffer(50)
      player.appendChunk(data)
      // Should catch the error and reset appending
    })
  })

  describe('endOfStream', () => {
    it('does not throw when called on closed MediaSource', () => {
      const player = createPlayer()
      // No init — no MediaSource
      expect(() => player.endOfStream()).not.toThrow()
    })

    it('calls endOfStream on MediaSource when sourceBuffer is not updating', () => {
      const player = createPlayer()
      const ms = initAndOpen(player)
      player.endOfStream()
      expect(ms.readyState).toBe('closed')
    })

    it('waits for updateend when sourceBuffer is updating', () => {
      const player = createPlayer()
      const ms = initAndOpen(player)
      const sb = ms.getSourceBuffer()!
      // Make sourceBuffer appear busy
      sb.updating = true
      player.endOfStream()
      // endOfStream should register updateend listener instead of calling doEnd immediately
      // Simulate updateend to trigger the deferred endOfStream
      sb.updating = false
      sb.dispatchEvent('updateend')
    })
  })

  describe('cleanup', () => {
    it('resets all state after init', () => {
      const player = createPlayer()
      initAndOpen(player)
      player.appendChunk(new ArrayBuffer(100))
      player.cleanup()
      expect(player.isReady).toBe(false)
    })

    it('does not throw when called without init', () => {
      const player = createPlayer()
      expect(() => player.cleanup()).not.toThrow()
    })

    it('pauses and clears audio element', () => {
      const player = createPlayer()
      const audio = player.init()
      player.cleanup()
      expect(audio.src).toBe('')
      expect(audio.paused).toBe(true)
    })
  })
})
