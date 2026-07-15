/**
 * MseAudioPlayer
 *
 * Streams MP3 audio via MediaSource Extensions (MSE) for real-time playback.
 * Audio chunks are appended to a SourceBuffer and played through an HTMLAudioElement.
 *
 * Usage:
 *   const player = new MseAudioPlayer()
 *   const audio = player.init()
 *   // append chunks as they arrive
 *   player.appendChunk(arrayBuffer)
 *   // signal end of stream
 *   player.endOfStream()
 *   // clean up
 *   player.cleanup()
 */

import { appLog } from '@/utils/appLog'

const TAG = 'MseAudio'

// Memory cap: 2MB total buffered data to prevent OOM on low-end devices
const MAX_BUFFERED_BYTES = 2 * 1024 * 1024

export class MseAudioPlayer {
  private mediaSource: MediaSource | null = null
  private sourceBuffer: SourceBuffer | null = null
  private audioEl: HTMLAudioElement | null = null
  private pendingChunks: ArrayBuffer[] = []
  private appending = false
  private _totalBufferedBytes = 0
  private _isReady = false

  /** Check if MSE + MP3 codec is supported by the browser */
  static isSupported(): boolean {
    return typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported('audio/mpeg')
  }

  get isReady(): boolean { return this._isReady }

  /** Initialize MSE + audio element. Call before appending chunks. */
  init(): HTMLAudioElement {
    this.mediaSource = new MediaSource()
    this.audioEl = new Audio()
    this.audioEl.src = URL.createObjectURL(this.mediaSource)

    this.mediaSource.addEventListener('sourceopen', () => {
      if (!this.mediaSource) return
      try {
        this.sourceBuffer = this.mediaSource.addSourceBuffer('audio/mpeg')
      } catch (e) {
        appLog.e(TAG, 'Failed to add SourceBuffer', e)
        return
      }
      this.sourceBuffer.addEventListener('updateend', () => this.drainPending())
      this.sourceBuffer.addEventListener('error', () => {
        this.appending = false
        appLog.e(TAG, 'SourceBuffer error')
      })
      this._isReady = true
      this.drainPending()
    })

    return this.audioEl
  }

  /** Append an MP3 chunk. Queues if SourceBuffer is busy. */
  appendChunk(data: ArrayBuffer): void {
    // Memory cap: drop chunks if we've buffered too much
    if (this._totalBufferedBytes + data.byteLength > MAX_BUFFERED_BYTES) {
      appLog.w(TAG, `Dropping chunk: buffered ${this._totalBufferedBytes} exceeds ${MAX_BUFFERED_BYTES}`)
      return
    }

    // Coalesce with last pending chunk to reduce appendBuffer calls
    if (this.pendingChunks.length > 0) {
      const last = this.pendingChunks[this.pendingChunks.length - 1]
      const merged = new ArrayBuffer(last.byteLength + data.byteLength)
      new Uint8Array(merged).set(new Uint8Array(last), 0)
      new Uint8Array(merged).set(new Uint8Array(data), last.byteLength)
      this.pendingChunks[this.pendingChunks.length - 1] = merged
    } else {
      this.pendingChunks.push(data)
    }
    this._totalBufferedBytes += data.byteLength

    if (this._isReady && !this.appending && !this.sourceBuffer?.updating) {
      this.drainPending()
    }
  }

  private doAppend(data: ArrayBuffer): void {
    if (!this.sourceBuffer || !this.mediaSource || this.mediaSource.readyState !== 'open') return
    this.appending = true
    try {
      this.sourceBuffer.appendBuffer(data)
    } catch (e) {
      this.appending = false
      appLog.e(TAG, 'appendBuffer failed', e)
    }
  }

  private drainPending(): void {
    this.appending = false
    if (this.pendingChunks.length === 0) return
    const chunk = this.pendingChunks.shift()!
    this._totalBufferedBytes -= chunk.byteLength
    if (this._totalBufferedBytes < 0) this._totalBufferedBytes = 0
    this.doAppend(chunk)
  }

  /** Signal end of stream. Audio element will fire 'ended' when playback finishes. */
  endOfStream(): void {
    if (!this.mediaSource || this.mediaSource.readyState !== 'open') return
    const doEnd = () => {
      try {
        if (this.mediaSource && this.mediaSource.readyState === 'open') {
          this.mediaSource.endOfStream()
        }
      } catch { /* ignore — may already be ended */ }
    }
    if (this.sourceBuffer?.updating) {
      this.sourceBuffer.addEventListener('updateend', doEnd, { once: true })
    } else {
      doEnd()
    }
  }

  /** Clean up all resources. Call on stop or error. */
  cleanup(): void {
    this.pendingChunks = []
    this.appending = false
    this._totalBufferedBytes = 0
    if (this.audioEl) {
      this.audioEl.pause()
      this.audioEl.src = ''
      this.audioEl = null
    }
    if (this.mediaSource) {
      try {
        if (this.mediaSource.readyState === 'open') this.mediaSource.endOfStream()
      } catch { /* ignore */ }
      this.mediaSource = null
    }
    this.sourceBuffer = null
    this._isReady = false
  }
}
